package session

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/pion/rtp"
)

func assertParameterSetIDR(t *testing.T, got []rtp.Packet, sps, pps []byte, slices int) {
	t.Helper()
	if len(got) != 2+slices {
		t.Fatalf("got %d packets, want one SPS/PPS pair + %d slices", len(got), slices)
	}
	if !bytes.Equal(got[0].Payload, sps) || !bytes.Equal(got[1].Payload, pps) {
		t.Fatalf("prefix = %x / %x, want latest %x / %x", got[0].Payload, got[1].Payload, sps, pps)
	}
	for i, p := range got {
		if p.Timestamp != got[2].Timestamp || p.SSRC != got[2].SSRC || p.PayloadType != 96 {
			t.Fatalf("packet %d has a different timestamp/SSRC/PT: %+v", i, p.Header)
		}
		if i > 0 && p.SequenceNumber != got[i-1].SequenceNumber+1 {
			t.Fatalf("sequence gap at %d: %d -> %d", i, got[i-1].SequenceNumber, p.SequenceNumber)
		}
		if p.Marker != (i == len(got)-1) {
			t.Fatalf("packet %d marker=%v, want marker on last slice only", i, p.Marker)
		}
		if i >= 2 && p.Payload[0]&0x1f != 5 {
			t.Fatalf("packet %d NAL=%d, want single-NAL IDR", i, p.Payload[0]&0x1f)
		}
	}
}

func TestUplinkIDRPrefixesLatestSetsWithoutDuplicates(t *testing.T) {
	sps := []byte{0x67, 0x42, 0, 0x1f, 0xab}
	pps := []byte{0x68, 0xce, 6, 0xe2}
	for _, mode := range []uint8{0, 1} {
		for _, inband := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("mode%d_inband%d", mode, inband), func(t *testing.T) {
				var input [][]byte
				if inband >= 1 {
					input = append(input, mode0SlicePacket(1, 90000, false, sps))
				}
				if inband == 2 {
					input = append(input, mode0SlicePacket(2, 90000, false, pps))
				}
				input = append(input,
					mode0SlicePacket(3, 90000, false, paddedNAL(0x65, 50)),
					mode0SlicePacket(4, 90000, true, paddedNAL(0x65, 60)),
					// A later IDR without encoder-provided sets still needs the pair.
					mode0SlicePacket(5, 180000, true, paddedNAL(0x65, 70)))
				got := forwardPictureToAsterisk(t, false, mode, input)
				if len(got) != 7 {
					t.Fatalf("packets=%d, want 4 + 3", len(got))
				}
				assertParameterSetIDR(t, got[:4], sps, pps, 2)
				assertParameterSetIDR(t, got[4:], sps, pps, 1)
			})
		}
	}
}

func TestUplinkStartDiscardsIDRBeforeParameterSets(t *testing.T) {
	sps := []byte{0x67, 0x42, 0, 0x1f, 0xab}
	pps := []byte{0x68, 0xce, 6, 0xe2}
	input := [][]byte{
		mode0SlicePacket(1, 90000, false, paddedNAL(0x65, 30)),
		mode0SlicePacket(2, 90000, false, sps),
		mode0SlicePacket(3, 90000, false, pps),
		// Sets arrived late in the same picture: do not release only its tail.
		mode0SlicePacket(4, 90000, true, paddedNAL(0x65, 40)),
		mode0SlicePacket(5, 93000, true, paddedNAL(0x41, 20)),
		mode0SlicePacket(6, 96000, false, paddedNAL(0x65, 50)),
		mode0SlicePacket(7, 96000, true, paddedNAL(0x65, 60)),
	}
	for _, mode := range []uint8{0, 1} {
		t.Run(fmt.Sprintf("mode%d", mode), func(t *testing.T) {
			got, sess := forwardUplinkVideoForTest(t, input, nil, func(s *Session) {
				// Exercise a cold media session with no cached parameter sets.
				s.CachedSPS, s.CachedPPS = nil, nil
				s.SetSIPVideoPacketizationMode(mode)
			})
			var fresh []rtp.Packet
			for _, p := range got {
				if p.Timestamp == 96000 {
					fresh = append(fresh, p)
				} else if p.Payload[0]&0x1f != 7 && p.Payload[0]&0x1f != 8 {
					t.Fatalf("forwarded an undecodable early slice: %+v", p)
				}
			}
			assertParameterSetIDR(t, fresh, sps, pps, 2)
			if sess.uplinkParameterObservation.idrs != 1 || sess.uplinkParameterObservation.missing != 0 {
				t.Fatalf("wire observations: %+v", sess.uplinkParameterObservation)
			}
		})
	}
}

func TestUplinkIDRRepeatedTimestampAfterMarkerGetsFreshPrefix(t *testing.T) {
	input := [][]byte{
		mode0SlicePacket(1, 90000, true, paddedNAL(0x65, 30)),
		mode0SlicePacket(2, 90000, true, paddedNAL(0x65, 40)),
	}
	got := forwardPictureToAsterisk(t, false, 0, input)
	if len(got) != 6 {
		t.Fatalf("packets=%d, want two SPS/PPS/IDR access units", len(got))
	}
	for i := 0; i < 2; i++ {
		assertParameterSetIDR(t, got[i*3:i*3+3], got[0].Payload, got[1].Payload, 1)
	}
}

func TestUplinkLaterIDRUsesUpdatedCachedParameterSets(t *testing.T) {
	sps := buildBaselineSPS(66, 0xe0, 31, 640, 368, 8)
	pps := []byte{0x68, 0xce, 6, 0xe3}
	input := [][]byte{
		mode0SlicePacket(1, 90000, true, paddedNAL(0x65, 30)),
		mode0SlicePacket(2, 180000, false, sps),
		mode0SlicePacket(3, 180000, false, pps),
		mode0SlicePacket(4, 180000, true, paddedNAL(0x65, 40)),
		mode0SlicePacket(5, 270000, true, paddedNAL(0x65, 50)),
	}
	got := forwardPictureToAsterisk(t, false, 0, input)
	if len(got) != 9 {
		t.Fatalf("packets=%d, want three SPS/PPS/IDR access units", len(got))
	}
	assertParameterSetIDR(t, got[3:6], sps, pps, 1)
	assertParameterSetIDR(t, got[6:9], sps, pps, 1)
}

func TestUplinkSTAPAReplacesEarlierParameterSetsInSameAU(t *testing.T) {
	sps := []byte{0x67, 0x42, 0, 0x1f, 0xab}
	pps := []byte{0x68, 0xce, 6, 0xe2}
	oldSPS := []byte{0x67, 0x42, 0, 0x1f, 0xaa}
	stapa := buildSTAPAPayload(sps, pps)
	idr := paddedNAL(0x65, 60)
	stapa = append(stapa, 0, byte(len(idr)))
	stapa = append(stapa, idr...)
	input := [][]byte{
		mode0SlicePacket(1, 90000, false, oldSPS),
		mode0SlicePacket(2, 90000, true, stapa),
	}
	got := forwardPictureToAsterisk(t, false, 1, input)
	if len(got) != 4 {
		t.Fatalf("packets=%d, want old SPS followed by latest SPS/PPS/IDR", len(got))
	}
	assertParameterSetIDR(t, got[1:], sps, pps, 1)
}

func TestUplinkFUAIDRWithoutInBandSetsGetsPrefix(t *testing.T) {
	// Exercise the same admission/injection path with mode-0 reassembly and
	// mode-1 FU-A passthrough. Only FU-A starts should trigger the prefix.
	input := openH264FUAPicture(t)[2:]
	for _, mode := range []uint8{0, 1} {
		t.Run(fmt.Sprintf("mode%d", mode), func(t *testing.T) {
			got := forwardPictureToAsterisk(t, false, mode, input)
			if mode == 1 {
				assertDecodableFUAPicture(t, got)
			} else {
				if len(got) != 5 {
					t.Fatalf("packets=%d, want prefix + one reassembled IDR + two P slices", len(got))
				}
				assertParameterSetIDR(t, got[:3], got[0].Payload, got[1].Payload, 1)
			}
		})
	}
}

func TestUplinkSTAPAIDRParameterSetPrefix(t *testing.T) {
	sps := []byte{0x67, 0x42, 0, 0x1f, 0xab}
	pps := []byte{0x68, 0xce, 6, 0xe2}
	idr := paddedNAL(0x65, 60)
	for _, mode := range []uint8{0, 1} {
		for _, kind := range []string{"missing", "present", "partial", "after-idr", "inband"} {
			t.Run(fmt.Sprintf("mode%d_%s", mode, kind), func(t *testing.T) {
				nals := [][]byte{idr}
				switch kind {
				case "present":
					nals = [][]byte{sps, pps, idr}
				case "partial":
					nals = [][]byte{sps, idr}
				case "after-idr":
					nals = [][]byte{idr, sps, pps}
				}
				payload := []byte{0x78}
				for _, nal := range nals {
					payload = append(payload, byte(len(nal)>>8), byte(len(nal)))
					payload = append(payload, nal...)
				}
				var input [][]byte
				if kind == "inband" {
					input = append(input, mode0SlicePacket(1, 90000, false, sps), mode0SlicePacket(2, 90000, false, pps))
				}
				input = append(input, mode0SlicePacket(3, 90000, true, payload))
				got := forwardPictureToAsterisk(t, false, mode, input)
				assertParameterSetIDR(t, got, sps, pps, 1)
			})
		}
	}
}

func TestMissingParameterSetsRequestKeyframeIsRateLimited(t *testing.T) {
	s := &Session{ID: "missing-sets"}
	a := uplinkH264AU{}
	now := time.Now()
	for i, delta := range []time.Duration{0, time.Millisecond, time.Second} {
		a.beginLocked(s, &rtp.Packet{Header: rtp.Header{Timestamp: uint32(i), Marker: true}})
		hold, pli := a.holdLocked(s, true, now.Add(delta))
		if !hold || pli != (i != 1) {
			t.Fatalf("attempt %d hold=%v pli=%v", i, hold, pli)
		}
		if s.uplinkVideoOpenLocked() {
			t.Fatal("missing SPS/PPS opened the startup gate")
		}
	}
}
