package sip

import (
	"testing"
	"time"

	"github.com/pion/rtp"

	"webrtc-sip-gateway/internal/session"
)

// Both orderings of the queue→agent race must emit one wall-clock timestamp
// step and start the new segment on an IDR. 15:09-style calls deliver the
// agent keyframe a few milliseconds before the @switch notice; 15:00-style
// calls deliver the notice first.
func TestVideoTimelineAgentMediaBeforeSwitchNotice(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &session.Session{ID: "media-first", VideoAUNormalizeEnabled: true}
	emitted := newTimelineHarness(t, sess, &now)

	pushTimelinePacket(emitted.n, 100, 99000, true, []byte{0x41, 0x01})
	if len(emitted.aus) != 1 {
		t.Fatalf("early frames = %d", len(emitted.aus))
	}
	earlyTS := emitted.aus[0].Packets[0].Timestamp

	now = now.Add(2900 * time.Millisecond)
	emitted.n.ResetForStreamSwitch(sess.GetSwitchGeneration(), "timestamp-jump")
	if !sess.StartSwitchVideoGate(sess.GetSwitchGeneration(), now, "timestamp-jump") {
		t.Fatal("timestamp jump should arm the keyframe gate")
	}
	pushTimelinePacket(emitted.n, 200, 1_000_555_300, true, []byte{0x41, 0x02})
	if len(emitted.aus) != 1 {
		t.Fatal("P-frame on the timestamp jump was forwarded")
	}
	pushTimelineIDR(emitted.n, 201, 1_000_555_300)
	if len(emitted.aus) != 2 || !emitted.aus[1].IsIDR {
		t.Fatalf("first post-jump frame = %+v", emitted.aus)
	}
	assertTimelineStep(t, emitted.aus[1].Packets[0].Timestamp-earlyTS, 2900*time.Millisecond)

	now = now.Add(16 * time.Millisecond)
	decision, activation := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	if activation.Outcome != session.SwitchVideoGateActivationSatisfied {
		t.Fatalf("notice after the agent IDR should not hold again, got %+v", activation)
	}
	emitted.n.ResetForSwitch(decision.Generation)
	now = now.Add(114 * time.Millisecond)
	// Source advanced one frame; real time since the IDR is 130ms.
	pushTimelinePacket(emitted.n, 210, 1_000_555_300+3000, true, []byte{0x41, 0x03})
	if len(emitted.aus) != 3 {
		t.Fatalf("expected the post-notice frame to flow, got %d", len(emitted.aus))
	}
	step := emitted.aus[2].Packets[0].Timestamp - emitted.aus[1].Packets[0].Timestamp
	assertTimelineStep(t, step, 130*time.Millisecond)
}

func TestVideoTimelineSwitchNoticeBeforeAgentMedia(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &session.Session{ID: "switch-first", VideoAUNormalizeEnabled: true}
	emitted := newTimelineHarness(t, sess, &now)

	pushTimelinePacket(emitted.n, 100, 99000, true, []byte{0x41, 0x01})
	earlyTS := emitted.aus[0].Packets[0].Timestamp

	sess.SwitchGeneration = 1
	if !sess.StartSwitchVideoGate(1, now, "agent-switch") {
		t.Fatal("expected the notice to arm the gate")
	}
	emitted.n.ResetForSwitch(1)
	now = now.Add(44 * time.Millisecond)

	pushTimelinePacket(emitted.n, 200, 1_000_188_990, true, []byte{0x41, 0x02})
	if len(emitted.aus) != 1 {
		t.Fatal("P-frame before the agent IDR was forwarded")
	}
	pushTimelineIDR(emitted.n, 201, 1_000_188_990)
	if len(emitted.aus) != 2 || !emitted.aus[1].IsIDR {
		t.Fatalf("first post-switch frame = %+v", emitted.aus)
	}
	assertTimelineStep(t, emitted.aus[1].Packets[0].Timestamp-earlyTS, 44*time.Millisecond)

	now = now.Add(50 * time.Millisecond)
	pushTimelinePacket(emitted.n, 210, 1_000_188_990+3000, true, []byte{0x41, 0x04})
	if len(emitted.aus) != 3 {
		t.Fatalf("expected locked frame, got %d", len(emitted.aus))
	}
	if step := emitted.aus[2].Packets[0].Timestamp - emitted.aus[1].Packets[0].Timestamp; step != 3000 {
		t.Fatalf("source step = %d, want 3000", step)
	}
}

type timelineHarness struct {
	n   *session.H264AccessUnitNormalizer
	aus []session.NormalizedH264AccessUnit
}

func newTimelineHarness(t *testing.T, sess *session.Session, now *time.Time) *timelineHarness {
	t.Helper()
	h := &timelineHarness{}
	h.n = session.NewH264AccessUnitNormalizer(session.H264AccessUnitNormalizerConfig{}, func(au session.NormalizedH264AccessUnit) {
		result := writeNormalizedVideoAccessUnit(sess, au, *now, func([]byte) (int, error) { return 1, nil })
		if result.emitted {
			h.aus = append(h.aus, au)
		}
	})
	h.n.SetNow(func() time.Time { return *now })
	sess.BindH264AUNumberer(h.n.NumberAccessUnit)
	t.Cleanup(func() { sess.BindH264AUNumberer(nil) })
	return h
}

func pushTimelinePacket(n *session.H264AccessUnitNormalizer, seq uint16, ts uint32, marker bool, payload []byte) {
	n.Push(&rtp.Packet{Header: rtp.Header{
		Version: 2, PayloadType: 96, SequenceNumber: seq, Timestamp: ts, SSRC: 4242, Marker: marker,
	}, Payload: payload})
}

func pushTimelineIDR(n *session.H264AccessUnitNormalizer, seq uint16, ts uint32) {
	pushTimelinePacket(n, seq, ts, false, []byte{0x67, 0x42, 0x00, 0x1f})
	pushTimelinePacket(n, seq+1, ts, false, []byte{0x68, 0xce, 0x06, 0xe2})
	pushTimelinePacket(n, seq+2, ts, true, []byte{0x65, 0xaa})
}

func assertTimelineStep(t *testing.T, got uint32, elapsed time.Duration) {
	t.Helper()
	want := uint32(elapsed.Nanoseconds() * 90000 / int64(time.Second))
	var diff uint32
	if got > want {
		diff = got - want
	} else {
		diff = want - got
	}
	if diff > 90*3 {
		t.Fatalf("outbound timestamp step %d, want %d (±3ms) for %s", got, want, elapsed)
	}
}
