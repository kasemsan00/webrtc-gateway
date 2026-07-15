package session

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

func TestH264AccessUnitNormalizerEmitsCompleteFUAIDR(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})

	n.Push(h264Packet(100, 9000, false, []byte{0x7c, 0x85, 0x11}))
	n.Push(h264Packet(101, 9000, false, []byte{0x7c, 0x05, 0x22}))
	n.Push(h264Packet(102, 9000, true, []byte{0x7c, 0x45, 0x33}))

	if len(emitted) != 1 {
		t.Fatalf("expected one complete access unit, got %d", len(emitted))
	}
	if !emitted[0].IsIDR {
		t.Fatal("expected complete FU-A IDR to be marked as keyframe")
	}
	assertContinuousH264Output(t, emitted[0].Packets)
}

func TestH264AccessUnitNormalizerDropsFUAWithSequenceGapAndResyncs(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})

	n.Push(h264Packet(100, 9000, false, []byte{0x7c, 0x85, 0x11}))
	n.Push(h264Packet(102, 9000, true, []byte{0x7c, 0x45, 0x33}))
	n.Push(h264Packet(103, 12000, true, []byte{0x41, 0x55}))

	if len(emitted) != 1 {
		t.Fatalf("expected only the post-gap access unit, got %d", len(emitted))
	}
	if emitted[0].IsIDR || emitted[0].SourceTimestamp != 12000 {
		t.Fatalf("expected resync on the following non-IDR access unit, got %+v", emitted[0])
	}
}

func TestH264AccessUnitNormalizerDropsUnmarkedAndUnterminatedFUA(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})

	// Timestamp changes without a marker and while the FU-A chain is still open.
	n.Push(h264Packet(200, 15000, false, []byte{0x7c, 0x85, 0x11}))
	n.Push(h264Packet(201, 18000, true, []byte{0x41, 0x66}))

	if len(emitted) != 1 || emitted[0].SourceTimestamp != 18000 {
		t.Fatalf("expected incomplete AU to be dropped and next AU emitted, got %+v", emitted)
	}
}

func TestH264AccessUnitNormalizerRewritesContinuityAcrossSourceReset(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})

	n.Push(h264Packet(65000, 900000, true, []byte{0x41, 0x01}))
	n.ResetSource()
	n.Push(h264Packet(7, 1000, true, []byte{0x41, 0x02}))

	if len(emitted) != 2 {
		t.Fatalf("expected two access units, got %d", len(emitted))
	}
	first := emitted[0].Packets[0]
	second := emitted[1].Packets[0]
	if second.SequenceNumber != first.SequenceNumber+1 {
		t.Fatalf("outbound sequence discontinuity: %d then %d", first.SequenceNumber, second.SequenceNumber)
	}
	if int32(second.Timestamp-first.Timestamp) <= 0 {
		t.Fatalf("outbound timestamp did not remain monotonic: %d then %d", first.Timestamp, second.Timestamp)
	}
}

func TestH264AccessUnitNormalizerResetForSwitchRequiresFreshParameterSetsAndPreservesTimeline(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})
	n.SetParameterSets([]byte{0x67, 0x42, 0x00, 0x1f}, []byte{0x68, 0xce, 0x06, 0xe2})

	n.Push(h264Packet(300, 24000, true, []byte{0x65, 0x99}))
	n.Push(h264Packet(301, 25000, false, []byte{0x41, 0x01}))
	n.ResetForSwitch(42)
	n.Push(h264Packet(7, 1000, true, []byte{0x65, 0xaa}))

	if len(emitted) != 2 {
		t.Fatalf("expected access units before and after switch reset, got %d", len(emitted))
	}
	before, withoutFreshSets := emitted[0], emitted[1]
	if !before.ParameterSetsReady || before.Generation != 0 {
		t.Fatalf("expected initial cached parameter sets in generation 0, got %+v", before)
	}
	if withoutFreshSets.ParameterSetsReady || withoutFreshSets.Generation != 42 {
		t.Fatalf("expected reset generation without ready parameter sets, got %+v", withoutFreshSets)
	}
	if withoutFreshSets.InjectedParameterSets || len(withoutFreshSets.Packets) != 1 {
		t.Fatalf("expected IDR without stale parameter-set injection, got %+v", withoutFreshSets)
	}
	beforeLast := before.Packets[len(before.Packets)-1]
	afterFirst := withoutFreshSets.Packets[0]
	if afterFirst.SequenceNumber != beforeLast.SequenceNumber+1 {
		t.Fatalf("outbound sequence discontinuity across switch reset: %d then %d", beforeLast.SequenceNumber, afterFirst.SequenceNumber)
	}
	if int32(afterFirst.Timestamp-beforeLast.Timestamp) <= 0 {
		t.Fatalf("outbound timestamp did not remain monotonic across switch reset: %d then %d", beforeLast.Timestamp, afterFirst.Timestamp)
	}

	n.Push(h264Packet(8, 4000, false, []byte{0x67, 0x64}))
	n.Push(h264Packet(9, 4000, false, []byte{0x68, 0xef}))
	n.Push(h264Packet(10, 4000, true, []byte{0x65, 0xbb}))

	if len(emitted) != 3 {
		t.Fatalf("expected fresh parameter-set access unit, got %d emissions", len(emitted))
	}
	withFreshSets := emitted[2]
	if !withFreshSets.ParameterSetsReady || withFreshSets.Generation != 42 {
		t.Fatalf("expected fresh parameter sets to be ready in generation 42, got %+v", withFreshSets)
	}
	if withFreshSets.InjectedParameterSets || len(withFreshSets.Packets) != 3 {
		t.Fatalf("expected present fresh parameter sets without injection, got %+v", withFreshSets)
	}
	if withFreshSets.Packets[0].SequenceNumber != afterFirst.SequenceNumber+1 {
		t.Fatalf("outbound sequence discontinuity after fresh parameter sets: %d then %d", afterFirst.SequenceNumber, withFreshSets.Packets[0].SequenceNumber)
	}
}

func TestH264AccessUnitNormalizerInjectsCachedParameterSetsBeforeIDR(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})
	n.SetParameterSets([]byte{0x67, 0x42, 0x00, 0x1f}, []byte{0x68, 0xce, 0x06, 0xe2})

	n.Push(h264Packet(300, 24000, true, []byte{0x65, 0x99}))

	if len(emitted) != 1 {
		t.Fatalf("expected one access unit, got %d", len(emitted))
	}
	got := emitted[0]
	if !got.IsIDR || !got.InjectedParameterSets || len(got.Packets) != 3 {
		t.Fatalf("expected SPS/PPS injection before IDR, got %+v", got)
	}
	if got.Packets[0].Payload[0]&0x1f != 7 || got.Packets[1].Payload[0]&0x1f != 8 || got.Packets[2].Payload[0]&0x1f != 5 {
		t.Fatalf("unexpected NAL order: %d, %d, %d", got.Packets[0].Payload[0]&0x1f, got.Packets[1].Payload[0]&0x1f, got.Packets[2].Payload[0]&0x1f)
	}
	assertContinuousH264Output(t, got.Packets)
}

func TestH264AccessUnitNormalizerDoesNotDuplicatePresentParameterSets(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})
	n.SetParameterSets([]byte{0x67, 0x42}, []byte{0x68, 0xce})

	n.Push(h264Packet(400, 27000, false, []byte{0x67, 0x64}))
	n.Push(h264Packet(401, 27000, false, []byte{0x68, 0xef}))
	n.Push(h264Packet(402, 27000, true, []byte{0x65, 0xaa}))

	if len(emitted) != 1 || emitted[0].InjectedParameterSets || len(emitted[0].Packets) != 3 {
		t.Fatalf("expected existing SPS/PPS to be preserved without duplication, got %+v", emitted)
	}
}

func TestH264AccessUnitNormalizerCachesParameterSetsFromSTAPA(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})

	stapA := []byte{0x78, 0x00, 0x03, 0x67, 0x42, 0x01, 0x00, 0x03, 0x68, 0xce, 0x02}
	n.Push(h264Packet(450, 28000, true, stapA))
	n.Push(h264Packet(451, 31000, true, []byte{0x65, 0xaa}))

	if len(emitted) != 2 {
		t.Fatalf("expected parameter-set AU and IDR AU, got %d", len(emitted))
	}
	idr := emitted[1]
	if !idr.InjectedParameterSets || len(idr.Packets) != 3 {
		t.Fatalf("expected STAP-A parameter sets to be cached and injected, got %+v", idr)
	}
}

func TestH264AccessUnitNormalizerDropsOverflowAndResyncs(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{MaxPackets: 2}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})

	n.Push(h264Packet(500, 30000, false, []byte{0x41, 0x01}))
	n.Push(h264Packet(501, 30000, false, []byte{0x41, 0x02}))
	n.Push(h264Packet(502, 30000, true, []byte{0x41, 0x03}))
	n.Push(h264Packet(503, 33000, true, []byte{0x41, 0x04}))

	if len(emitted) != 1 || emitted[0].SourceTimestamp != 33000 {
		t.Fatalf("expected overflow AU dropped and following AU emitted, got %+v", emitted)
	}
	if stats := n.Stats(); stats.DroppedOverflow != 1 {
		t.Fatalf("expected one bounded overflow drop, got %+v", stats)
	}
}

func TestH264AccessUnitNormalizerDropsExpiredAccessUnit(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	now := time.Unix(100, 0)
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{MaxAge: 50 * time.Millisecond}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})
	n.now = func() time.Time { return now }

	n.Push(h264Packet(600, 36000, false, []byte{0x41, 0x01}))
	now = now.Add(60 * time.Millisecond)
	n.Push(h264Packet(601, 36000, true, []byte{0x41, 0x02}))
	n.Push(h264Packet(602, 39000, true, []byte{0x41, 0x03}))

	if len(emitted) != 2 || emitted[0].SourceTimestamp != 36000 || emitted[1].SourceTimestamp != 39000 {
		t.Fatalf("expected expired partial AU dropped before a fresh marked AU, got %+v", emitted)
	}
	if stats := n.Stats(); stats.DroppedIncomplete != 1 {
		t.Fatalf("expected one expired incomplete drop, got %+v", stats)
	}
}

func h264Packet(seq uint16, timestamp uint32, marker bool, payload []byte) *rtp.Packet {
	return &rtp.Packet{Header: rtp.Header{
		Version: 2, PayloadType: 96, SequenceNumber: seq, Timestamp: timestamp, SSRC: 1234, Marker: marker,
	}, Payload: append([]byte(nil), payload...)}
}

func assertContinuousH264Output(t *testing.T, packets []*rtp.Packet) {
	t.Helper()
	for i := 1; i < len(packets); i++ {
		if packets[i].SequenceNumber != packets[i-1].SequenceNumber+1 {
			t.Fatalf("output sequence is not continuous at packet %d: %d then %d", i, packets[i-1].SequenceNumber, packets[i].SequenceNumber)
		}
		if packets[i].Timestamp != packets[0].Timestamp {
			t.Fatalf("access-unit timestamps differ: %d and %d", packets[0].Timestamp, packets[i].Timestamp)
		}
	}
}
