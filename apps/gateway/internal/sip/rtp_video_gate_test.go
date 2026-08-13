package sip

import (
	"errors"
	"testing"
	"time"

	"github.com/pion/rtp"

	"k2-gateway/internal/session"
)

func normalizedVideoAU(generation int, idr, ready bool, packets int) session.NormalizedH264AccessUnit {
	au := session.NormalizedH264AccessUnit{
		Generation:         generation,
		IsIDR:              idr,
		ParameterSetsReady: ready,
		SourceTimestamp:    90000,
	}
	for i := 0; i < packets; i++ {
		au.Packets = append(au.Packets, &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: uint16(100 + i),
				Timestamp:      90000,
				SSRC:           4242,
				Marker:         i == packets-1,
			},
			Payload: []byte{0x65, byte(i + 1)},
		})
	}
	return au
}

func TestWriteNormalizedVideoAccessUnitCommitsGateAfterAllPackets(t *testing.T) {
	now := time.Unix(100, 0)
	sess := &session.Session{ID: "gate-write", VideoAUNormalizeEnabled: true, SwitchGeneration: 3}
	if !sess.StartSwitchVideoGate(3, now, "test") {
		t.Fatal("expected gate start")
	}

	writes := 0
	result := writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(3, true, true, 3), now.Add(time.Second), func([]byte) (int, error) {
		writes++
		return 1, nil
	})

	if !result.emitted || writes != 3 {
		t.Fatalf("emitted=%v writes=%d, want true/3", result.emitted, writes)
	}
	if !result.gateReleased {
		t.Fatal("expected gate release")
	}
	if sess.IsSwitchVideoGateActive() || sess.SwitchVideoGateAcceptedGeneration != 3 {
		t.Fatalf("gate active=%v accepted=%d", sess.IsSwitchVideoGateActive(), sess.SwitchVideoGateAcceptedGeneration)
	}
}

func TestWriteNormalizedVideoAccessUnitAbortsReservationOnWriteFailure(t *testing.T) {
	now := time.Unix(200, 0)
	sess := &session.Session{ID: "gate-write-fail", VideoAUNormalizeEnabled: true, SwitchGeneration: 4}
	sess.StartSwitchVideoGate(4, now, "test")

	writes := 0
	result := writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(4, true, true, 3), now.Add(time.Second), func([]byte) (int, error) {
		writes++
		if writes == 2 {
			return 0, errors.New("track failed")
		}
		return 1, nil
	})

	if result.emitted || writes != 2 || !sess.IsSwitchVideoGateActive() || sess.SwitchVideoGateReleasing {
		t.Fatalf("emitted=%v writes=%d active=%v releasing=%v", result.emitted, writes, sess.IsSwitchVideoGateActive(), sess.SwitchVideoGateReleasing)
	}

	writes = 0
	if writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(4, false, true, 1), now.Add(2*time.Second), func([]byte) (int, error) {
		writes++
		return 1, nil
	}).emitted || writes != 0 {
		t.Fatalf("P-frame passed after failed IDR: writes=%d", writes)
	}
}

func TestWriteNormalizedVideoAccessUnitPreservesPacketSSRC(t *testing.T) {
	now := time.Unix(300, 0)
	sess := &session.Session{ID: "gate-ssrc", VideoAUNormalizeEnabled: true, SwitchGeneration: 1}
	sess.VideoRTPHistorySize = 1024
	sess.VideoRTPHistoryPackets = make([][]byte, sess.VideoRTPHistorySize)
	sess.VideoRTPHistorySeq = make([]uint16, sess.VideoRTPHistorySize)
	if !sess.StartSwitchVideoGate(1, now, "test") {
		t.Fatal("expected gate start")
	}

	var gotSSRCs []uint32
	result := writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(1, true, true, 3), now.Add(time.Second), func(b []byte) (int, error) {
		pkt := &rtp.Packet{}
		if err := pkt.Unmarshal(b); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		gotSSRCs = append(gotSSRCs, pkt.SSRC)
		return len(b), nil
	})
	if !result.emitted {
		t.Fatal("expected write success")
	}
	if len(gotSSRCs) != 3 {
		t.Fatalf("expected 3 packets, got %d", len(gotSSRCs))
	}
	for i, gotSSRC := range gotSSRCs {
		if gotSSRC != 4242 {
			t.Fatalf("packet %d: expected original SSRC 4242, got %d", i, gotSSRC)
		}
	}
}

func TestWriteNormalizedVideoAccessUnitRejectsStaleGenerationAfterRelease(t *testing.T) {
	now := time.Unix(300, 0)
	sess := &session.Session{ID: "gate-stale", VideoAUNormalizeEnabled: true, SwitchGeneration: 5}
	sess.StartSwitchVideoGate(5, now, "test")
	if !writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(5, true, true, 1), now, func([]byte) (int, error) { return 1, nil }).gateReleased {
		t.Fatal("expected generation 5 release")
	}

	writes := 0
	if writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(4, true, true, 1), now, func([]byte) (int, error) {
		writes++
		return 1, nil
	}).emitted || writes != 0 {
		t.Fatalf("stale generation passed after release: writes=%d", writes)
	}
}

func TestWriteNormalizedVideoAccessUnitDoesNotBurnSeqOnGateReject(t *testing.T) {
	now := time.Unix(400, 0)
	normalizer := session.NewH264AccessUnitNormalizer(session.H264AccessUnitNormalizerConfig{}, nil)
	sess := &session.Session{ID: "gate-seq-hole", VideoAUNormalizeEnabled: true, SwitchGeneration: 1}
	sess.BindH264AUNumberer(normalizer.NumberAccessUnit)
	t.Cleanup(func() { sess.BindH264AUNumberer(nil) })
	sess.VideoRTPHistorySize = 1024
	sess.VideoRTPHistoryPackets = make([][]byte, sess.VideoRTPHistorySize)
	sess.VideoRTPHistorySeq = make([]uint16, sess.VideoRTPHistorySize)

	var seqs []uint16
	write := func(b []byte) (int, error) {
		pkt := &rtp.Packet{}
		if err := pkt.Unmarshal(b); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		seqs = append(seqs, pkt.SequenceNumber)
		return len(b), nil
	}

	pre := normalizedVideoAU(0, false, true, 7)
	if !writeNormalizedVideoAccessUnit(sess, pre, now, write).emitted {
		t.Fatal("expected pre-gate P-frame to emit")
	}
	if len(seqs) != 7 || seqs[0] != 100 || seqs[6] != 106 {
		t.Fatalf("pre-gate seqs=%v", seqs)
	}

	if !sess.StartSwitchVideoGate(1, now.Add(time.Second), "test") {
		t.Fatal("expected gate start")
	}
	if writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(1, false, true, 7), now.Add(2*time.Second), write).emitted {
		t.Fatal("expected gated P-frame to be rejected")
	}
	if writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(1, false, true, 7), now.Add(3*time.Second), write).emitted {
		t.Fatal("expected second gated P-frame to be rejected")
	}
	if len(seqs) != 7 {
		t.Fatalf("gate reject wrote packets: seqs=%v", seqs)
	}

	idr := normalizedVideoAU(1, true, true, 7)
	result := writeNormalizedVideoAccessUnit(sess, idr, now.Add(4*time.Second), write)
	if !result.emitted || !result.gateReleased {
		t.Fatalf("expected IDR to release gate: %+v", result)
	}
	if len(seqs) != 14 {
		t.Fatalf("expected 14 written packets, got %d seqs=%v", len(seqs), seqs)
	}
	if seqs[7] != 107 || seqs[13] != 113 {
		t.Fatalf("gate reject burned outbound seq, got %v", seqs[7:])
	}
}

func TestWriteNormalizedVideoAccessUnitCachesIDRAfterTrackWriteFailure(t *testing.T) {
	sess := &session.Session{ID: "idr-write-fail", VideoAUNormalizeEnabled: true}
	result := writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(0, true, true, 2), time.Now(), func([]byte) (int, error) {
		return 0, errors.New("DTLS transport has not started yet")
	})
	if result.emitted {
		t.Fatal("expected write failure")
	}
	if !sess.HasPendingSIPVideoIDRWrite() {
		t.Fatal("expected failed IDR to remain queued for replay")
	}

	writes := 0
	wrote, reason := sess.WritePendingSIPVideoIDR(func(b []byte) (int, error) {
		writes++
		return len(b), nil
	})
	if !wrote || writes != 2 {
		t.Fatalf("wrote=%v writes=%d reason=%s", wrote, writes, reason)
	}
}
