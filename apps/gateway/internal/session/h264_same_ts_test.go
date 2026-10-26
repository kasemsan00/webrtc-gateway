package session

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

// sliceNAL is a P (0x41) or IDR (0x65) slice with the given first_mb_in_slice.
func sliceNAL(header byte, firstMB uint) []byte {
	w := bitWriter{}
	w.ue(firstMB)
	w.ue(0) // slice_type
	w.u(1, 1)
	return append([]byte{header}, append(w.bytes(), 0xaa, 0xbb)...)
}

func sameTSNormalizer(now *time.Time, reuse bool) (*H264AccessUnitNormalizer, *[]NormalizedH264AccessUnit) {
	var emitted []NormalizedH264AccessUnit
	var n *H264AccessUnitNormalizer
	n = NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{ReuseSameSourceTimestamp: reuse}, func(au NormalizedH264AccessUnit) {
		n.NumberAccessUnit(&au)
		emitted = append(emitted, au)
	})
	n.SetNow(func() time.Time { return *now })
	return n, &emitted
}

func outTS(au NormalizedH264AccessUnit) uint32 { return au.Packets[0].Timestamp }

func assertSeqContinuousAcross(t *testing.T, aus []NormalizedH264AccessUnit) {
	t.Helper()
	var last uint16
	first := true
	for i, au := range aus {
		for _, p := range au.Packets {
			if !first && p.SequenceNumber != last+1 {
				t.Fatalf("AU %d seq %d after %d", i, p.SequenceNumber, last)
			}
			last, first = p.SequenceNumber, false
		}
	}
}

// k2 rqfl8nz1Y4WQ 12:44:56.229: in_prev=842400 in=842400, 1ms apart, then
// the IDR at 846000. The second AU must keep the first one's outbound ts.
func TestSameSourceTimestampReusesOutboundTimestamp(t *testing.T) {
	for _, tc := range []struct {
		name    string
		firstMB uint
	}{
		{"second picture on the same tick", 0},
		{"tail of one split picture", 69},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1_700_000_000, 0)
			n, emitted := sameTSNormalizer(&now, true)
			n.Push(h264Packet(10, 838800, true, sliceNAL(0x41, 0)))
			now = now.Add(40 * time.Millisecond)
			n.Push(h264Packet(11, 842400, false, sliceNAL(0x41, 0)))
			n.Push(h264Packet(12, 842400, true, sliceNAL(0x41, 30)))
			now = now.Add(time.Millisecond)
			n.Push(h264Packet(13, 842400, true, sliceNAL(0x41, tc.firstMB)))
			now = now.Add(31 * time.Millisecond)
			n.Push(h264Packet(14, 846000, true, sliceNAL(0x65, 0)))

			aus := *emitted
			if len(aus) != 4 {
				t.Fatalf("emitted %d AUs, want 4", len(aus))
			}
			if outTS(aus[2]) != outTS(aus[1]) {
				t.Fatalf("same source ts got out=%d and out=%d", outTS(aus[1]), outTS(aus[2]))
			}
			if aus[2].Discontinuity != nil {
				t.Fatalf("reuse reported as discontinuity: %+v", aus[2].Discontinuity)
			}
			split := aus[2].SameTimestampSplit
			if split == nil || split.Count != 1 || split.Incoming != 842400 || split.Outgoing != outTS(aus[1]) ||
				split.FirstMB != int(tc.firstMB) || split.Packets != 1 || split.PreviousPackets != 2 || split.Wall != time.Millisecond {
				t.Fatalf("split = %+v", split)
			}
			if step := outTS(aus[3]) - outTS(aus[1]); step != 3600 || aus[3].Discontinuity != nil {
				t.Fatalf("IDR after the split step=%d disc=%+v, want continuous 3600", step, aus[3].Discontinuity)
			}
			for _, i := range []int{0, 1, 3} {
				if aus[i].SameTimestampSplit != nil {
					t.Fatalf("AU %d wrongly marked as a same-ts split", i)
				}
			}
			assertSeqContinuousAcross(t, aus)
		})
	}
}

func TestSameSourceTimestampRealGapsStillStep(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	n, emitted := sameTSNormalizer(&now, true)
	n.Push(h264Packet(10, 900000, true, sliceNAL(0x41, 0)))

	// Source runs backwards: still a wall-clock step.
	now = now.Add(50 * time.Millisecond)
	n.Push(h264Packet(11, 800000, true, sliceNAL(0x41, 0)))
	// Jump of more than 10s: still a wall-clock step.
	now = now.Add(70 * time.Millisecond)
	n.Push(h264Packet(12, 800000+maxReasonableH264TimestampGap+1, true, sliceNAL(0x41, 0)))
	// A stream switch whose first AU repeats the old ts must step too.
	n.ResetForStreamSwitch(1, "ssrc")
	now = now.Add(20 * time.Millisecond)
	n.Push(h264Packet(13, 800000+maxReasonableH264TimestampGap+1, true, sliceNAL(0x65, 0)))

	aus := *emitted
	if len(aus) != 4 {
		t.Fatalf("emitted %d AUs, want 4", len(aus))
	}
	for i, want := range map[int]struct {
		reason string
		wall   time.Duration
	}{1: {"source-gap", 50 * time.Millisecond}, 2: {"source-gap", 70 * time.Millisecond}, 3: {"ssrc+source-gap", 20 * time.Millisecond}} {
		d := aus[i].Discontinuity
		if d == nil || d.Reason != want.reason || aus[i].SameTimestampSplit != nil {
			t.Fatalf("AU %d disc=%+v split=%+v, want %s", i, d, aus[i].SameTimestampSplit, want.reason)
		}
		assertWallClockTimestampStep(t, outTS(aus[i])-outTS(aus[i-1]), want.wall)
	}
	assertSeqContinuousAcross(t, aus)
}

// SIP_VIDEO_SAME_TS_REUSE=off: exactly the k2 4aa51bd output.
func TestSameSourceTimestampFlagOffKeepsOldStep(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	n, emitted := sameTSNormalizer(&now, false)
	n.Push(h264Packet(11, 842400, true, sliceNAL(0x41, 0)))
	now = now.Add(time.Millisecond)
	n.Push(h264Packet(12, 842400, true, sliceNAL(0x41, 0)))
	now = now.Add(31 * time.Millisecond)
	n.Push(h264Packet(13, 846000, true, sliceNAL(0x65, 0)))

	aus := *emitted
	d := aus[1].Discontinuity
	if d == nil || d.Reason != "source-gap" || d.Incoming != 842400 || d.PreviousIncoming != 842400 || aus[1].SameTimestampSplit != nil {
		t.Fatalf("flag off disc=%+v split=%+v, want the old source-gap", d, aus[1].SameTimestampSplit)
	}
	if step := outTS(aus[1]) - outTS(aus[0]); step != 90 {
		t.Fatalf("flag off step=%d, want 90 ticks for 1ms", step)
	}
	if step := outTS(aus[2]) - outTS(aus[1]); step != 3600 {
		t.Fatalf("next frame step=%d, want 3600", step)
	}
}

// A cached-IDR replay moves outputTS past the last live AU; reusing it would
// stamp the next live AU with the replay's timestamp.
func TestSameSourceTimestampNotReusedAfterReplay(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	n, emitted := sameTSNormalizer(&now, true)
	n.Push(h264Packet(11, 842400, true, sliceNAL(0x65, 0)))
	replay := n.RewriteForReplay(cloneRTPPackets((*emitted)[0].Packets))
	now = now.Add(time.Millisecond)
	n.Push(h264Packet(12, 842400, true, sliceNAL(0x41, 0)))

	live := (*emitted)[1]
	if live.SameTimestampSplit != nil || outTS(live) == replay[0].Timestamp || outTS(live) == outTS((*emitted)[0]) {
		t.Fatalf("live AU after replay out=%d replay=%d first=%d split=%+v",
			outTS(live), replay[0].Timestamp, outTS((*emitted)[0]), live.SameTimestampSplit)
	}
	if live.Discontinuity == nil || live.Discontinuity.Reason != "source-gap" {
		t.Fatalf("disc = %+v, want the old source-gap step", live.Discontinuity)
	}
}

// Root-cause scenario from the k2 logs: the phone sends two marker-terminated
// AUs on one 10ms-tick timestamp, and the Android leg reorders them. The
// reorder buffer restores order with nothing dropped (drop=0 to=0 skip=0),
// so the normalizer correctly emits two complete AUs on one timestamp.
func TestReorderedSameTickAccessUnitsKeepOneTimestamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	n, emitted := sameTSNormalizer(&now, true)
	rb := NewVideoReorderBuffer("same-ts", func(data []byte, _ bool) {
		p := &rtp.Packet{}
		if err := p.Unmarshal(data); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}
		n.Push(p)
	})
	t.Cleanup(rb.Drain)
	push := func(p *rtp.Packet) {
		raw, err := p.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		rb.Push(p.SequenceNumber, raw, false)
	}
	pkts := []*rtp.Packet{
		h264Packet(100, 838800, true, sliceNAL(0x41, 0)),
		h264Packet(101, 842400, false, sliceNAL(0x41, 0)),
		h264Packet(102, 842400, true, sliceNAL(0x41, 30)),
		h264Packet(103, 842400, false, sliceNAL(0x41, 0)),
		h264Packet(104, 842400, true, sliceNAL(0x41, 30)),
		h264Packet(105, 846000, true, sliceNAL(0x65, 0)),
	}
	for _, i := range []int{0, 2, 1, 4, 3, 5} { // arrival order
		push(pkts[i])
	}

	aus := *emitted
	if len(aus) != 4 {
		t.Fatalf("emitted %d AUs, want 4 (nothing dropped)", len(aus))
	}
	if aus[1].SourceTimestamp != 842400 || aus[2].SourceTimestamp != 842400 {
		t.Fatalf("source ts %d,%d, want both 842400", aus[1].SourceTimestamp, aus[2].SourceTimestamp)
	}
	if outTS(aus[1]) != outTS(aus[2]) || aus[2].SameTimestampSplit == nil || aus[2].Discontinuity != nil {
		t.Fatalf("same-tick AUs out=%d,%d split=%+v disc=%+v", outTS(aus[1]), outTS(aus[2]), aus[2].SameTimestampSplit, aus[2].Discontinuity)
	}
	if outTS(aus[3])-outTS(aus[1]) != 3600 {
		t.Fatalf("IDR step %d, want 3600", outTS(aus[3])-outTS(aus[1]))
	}
	if st := n.Stats(); st.DroppedIncomplete != 0 || st.DroppedOverflow != 0 {
		t.Fatalf("normalizer dropped AUs: %+v", st)
	}
	assertSeqContinuousAcross(t, aus)
}
