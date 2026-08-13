package session

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

func testIDRAccessUnit(generation int, timestamp uint32) NormalizedH264AccessUnit {
	return NormalizedH264AccessUnit{
		IsIDR:              true,
		ParameterSetsReady: true,
		Generation:         generation,
		SourceTimestamp:    timestamp,
		Packets: []*rtp.Packet{
			{
				Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: 10, Timestamp: timestamp, SSRC: 99, Marker: false},
				Payload: []byte{0x67, 0x42, 0x00, 0x1f},
			},
			{
				Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: 11, Timestamp: timestamp, SSRC: 99, Marker: false},
				Payload: []byte{0x68, 0xce, 0x06, 0xe2},
			},
			{
				Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: 12, Timestamp: timestamp, SSRC: 99, Marker: true},
				Payload: []byte{0x65, 0xaa, 0xbb},
			},
		},
	}
}

func TestWritePendingSIPVideoIDRRetriesUndeliveredKeyframe(t *testing.T) {
	sess := &Session{ID: "idr-undelivered"}
	sess.RememberSIPVideoIDR(testIDRAccessUnit(0, 90000), false)
	if !sess.HasPendingSIPVideoIDRWrite() {
		t.Fatal("expected undelivered IDR to be pending")
	}

	writes := 0
	wrote, reason := sess.WritePendingSIPVideoIDR(func(b []byte) (int, error) {
		writes++
		return len(b), nil
	})
	if !wrote || writes != 3 {
		t.Fatalf("wrote=%v writes=%d reason=%s", wrote, writes, reason)
	}
	if sess.HasPendingSIPVideoIDRWrite() {
		t.Fatal("expected pending write to clear after successful delivery")
	}
}

func TestRequestSIPVideoIDRReplayQueuesDecoderReplay(t *testing.T) {
	sess := &Session{ID: "idr-replay"}
	sess.RememberSIPVideoIDR(testIDRAccessUnit(0, 90000), true)
	if sess.HasPendingSIPVideoIDRWrite() {
		t.Fatal("delivered IDR should not stay pending")
	}
	if !sess.RequestSIPVideoIDRReplay("ws-request_keyframe") {
		t.Fatal("expected replay to queue")
	}
	if !sess.HasPendingSIPVideoIDRWrite() {
		t.Fatal("expected queued replay")
	}

	writes := 0
	wrote, reason := sess.WritePendingSIPVideoIDR(func(b []byte) (int, error) {
		writes++
		return len(b), nil
	})
	if !wrote || writes != 3 {
		t.Fatalf("wrote=%v writes=%d reason=%s", wrote, writes, reason)
	}
}

func TestRequestSIPVideoIDRReplayThrottlesDeliveredRepeats(t *testing.T) {
	sess := &Session{ID: "idr-throttle"}
	sess.RememberSIPVideoIDR(testIDRAccessUnit(0, 90000), true)
	if !sess.RequestSIPVideoIDRReplay("browser-pli") {
		t.Fatal("expected first replay")
	}
	if _, reason := sess.WritePendingSIPVideoIDR(func(b []byte) (int, error) { return len(b), nil }); reason == "" {
		t.Fatal("expected first replay write")
	}
	if sess.RequestSIPVideoIDRReplay("browser-pli") {
		t.Fatal("expected throttle inside minimum interval")
	}
}

func TestStartSwitchVideoGateClearsCachedWaitVideoIDR(t *testing.T) {
	sess := &Session{ID: "idr-switch", VideoAUNormalizeEnabled: true, SwitchGeneration: 2}
	sess.RememberSIPVideoIDR(testIDRAccessUnit(0, 90000), true)
	if !sess.RequestSIPVideoIDRReplay("ws-request_keyframe") {
		t.Fatal("expected replay before switch")
	}
	if !sess.StartSwitchVideoGate(2, time.Now(), "switch") {
		t.Fatal("expected gate start")
	}
	if sess.RequestSIPVideoIDRReplay("ws-request_keyframe") {
		t.Fatal("queue IDR must not replay after @switch gate starts")
	}
}

func TestSendBrowserRecoveryToAsteriskReplaysCachedIDRInsteadOfPLI(t *testing.T) {
	sess := newBurstTestSession("idr-fresh-replay")
	makeSIPVideoRecoveryReady(t, sess)
	sess.StartVideoRecoveryBurst("unit-test")
	sess.LastKeyframe = time.Now()
	sess.RememberSIPVideoIDR(testIDRAccessUnit(0, 90000), true)

	if action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe"); action != "replay" {
		t.Fatalf("expected replay of cached wait-video IDR, got %s", action)
	}
	if !sess.LastSipPLISent.IsZero() {
		t.Fatal("replay must not send SIP PLI when a complete IDR is already cached")
	}
}

func TestSetStateEndedClearsCachedIDRSoHangupCannotReplay(t *testing.T) {
	sess := &Session{ID: "idr-ended"}
	sess.RememberSIPVideoIDR(testIDRAccessUnit(0, 90000), false)
	if !sess.HasPendingSIPVideoIDRWrite() {
		t.Fatal("expected pending IDR before hangup")
	}
	sess.SetState(StateEnded)
	if sess.HasPendingSIPVideoIDRWrite() {
		t.Fatal("ended session must not keep a pending IDR write")
	}
	if sess.RequestSIPVideoIDRReplay("ice-connected") {
		t.Fatal("ended session must not queue IDR replay")
	}
	if wrote, _ := sess.WritePendingSIPVideoIDR(func(b []byte) (int, error) {
		t.Fatal("must not write WebRTC video after hangup")
		return len(b), nil
	}); wrote {
		t.Fatal("expected no write after hangup")
	}
}

func TestStopVideoEgressRejectsWritesBeforePeerConnectionClose(t *testing.T) {
	sess := &Session{ID: "egress-stop"}
	sess.StopVideoEgress()
	if _, err := sess.WriteVideoToWebRTC([]byte{0x80, 0x60}); err == nil {
		t.Fatal("expected stopped egress to reject writes")
	}
}

func TestDetachPeerConnectionNilsTrackOwner(t *testing.T) {
	sess := &Session{ID: "detach-pc"}
	if got := sess.DetachPeerConnection(); got != nil {
		t.Fatal("expected nil peer connection")
	}
	if !sess.videoEgressIsStopped() {
		t.Fatal("detach must stop video egress")
	}
}
