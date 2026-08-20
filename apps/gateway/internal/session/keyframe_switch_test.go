package session

import (
	"net"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
)

func newSwitchFeedbackSession(t *testing.T) (*Session, *net.UDPConn) {
	t.Helper()
	receiver, port := newUDPConn(t)
	t.Cleanup(func() { _ = receiver.Close() })
	sender, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("open feedback sender: %v", err)
	}
	t.Cleanup(func() { _ = sender.Close() })
	sess := &Session{
		ID:                      "switch-feedback",
		VideoAUNormalizeEnabled: true,
		VideoRTCPConn:           sender,
		AsteriskVideoAddr:       &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port},
		RemoteVideoSSRC:         1234,
		VideoSSRC:               5678,
		VideoFeedbackTransport:  config.SIPVideoFeedbackTransportRTP,
	}
	return sess, receiver
}

func TestSwitchFeedbackStaleTokenDoesNotMutateOrSend(t *testing.T) {
	sess, receiver := newSwitchFeedbackSession(t)
	now := time.Now()
	old, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	current, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00026", now.Add(time.Second), time.Minute, true)
	sess.MarkPendingBrowserKeyframeRequest()

	beforePLI := sess.PLISent
	beforeFIRSeq := sess.FIRSeq
	beforeLastPLI := sess.LastPLISent
	beforeLastSIPPLI := sess.LastSipPLISent
	beforeLastSIPFIR := sess.LastSipFIRSent
	pendingAt := sess.PendingBrowserKeyframeRequestAt
	pendingEpoch := sess.PendingBrowserKeyframeRequestEpoch

	if sess.SendSwitchFIRToAsterisk(old.Generation, old.MediaEpoch) {
		t.Fatalf("stale SIP FIR reported authorized")
	}
	if sess.SendSwitchPLIToAsteriskForced(old.Generation, old.MediaEpoch, "switch") {
		t.Fatalf("stale SIP PLI reported authorized")
	}
	if sess.SendSwitchFIRToWebRTC(old.Generation, old.MediaEpoch) {
		t.Fatalf("stale WebRTC FIR reported authorized")
	}
	if sess.SendSwitchPLIToWebRTC(old.Generation, old.MediaEpoch) {
		t.Fatalf("stale WebRTC PLI reported authorized")
	}
	if sess.FlushPendingBrowserKeyframeRequestForSwitch(old.Generation, old.MediaEpoch, "switch") {
		t.Fatalf("stale pending flush reported authorized")
	}

	if sess.PLISent != beforePLI || sess.FIRSeq != beforeFIRSeq ||
		!sess.LastPLISent.Equal(beforeLastPLI) || !sess.LastSipPLISent.Equal(beforeLastSIPPLI) ||
		!sess.LastSipFIRSent.Equal(beforeLastSIPFIR) {
		t.Fatalf("stale feedback mutated counters/timestamps")
	}
	if !sess.PendingBrowserKeyframeRequest || !sess.PendingBrowserKeyframeRequestAt.Equal(pendingAt) ||
		sess.PendingBrowserKeyframeRequestEpoch != pendingEpoch || pendingEpoch != current.MediaEpoch {
		t.Fatalf("stale flush mutated current pending request")
	}
	buf := make([]byte, 1500)
	_ = receiver.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, _, err := receiver.ReadFromUDP(buf); err == nil {
		t.Fatalf("stale feedback emitted UDP packet")
	}
}

func TestSwitchFeedbackValidTokenSendsToSnapshottedSIPEndpoint(t *testing.T) {
	sess, receiver := newSwitchFeedbackSession(t)
	decision, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", time.Now(), time.Minute, true)

	if !sess.SendSwitchFIRToAsterisk(decision.Generation, decision.MediaEpoch) {
		t.Fatalf("valid SIP FIR rejected")
	}
	buf := make([]byte, 1500)
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	if n, _, err := receiver.ReadFromUDP(buf); err != nil || n == 0 {
		t.Fatalf("valid SIP FIR was not sent: n=%d err=%v", n, err)
	}
	if sess.PLISent != 1 || sess.FIRSeq != 1 || sess.LastSipFIRSent.IsZero() {
		t.Fatalf("valid SIP FIR did not reserve feedback state")
	}
}

func TestSwitchPendingFlushResetTokenCannotClaimNewEpoch(t *testing.T) {
	sess, _ := newSwitchFeedbackSession(t)
	old, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", time.Now(), time.Minute, true)
	sess.ResetMediaState()
	sess.MarkPendingBrowserKeyframeRequest()
	pendingAt := sess.PendingBrowserKeyframeRequestAt
	pendingEpoch := sess.PendingBrowserKeyframeRequestEpoch

	if sess.FlushPendingBrowserKeyframeRequestForSwitch(old.Generation, old.MediaEpoch, "switch") {
		t.Fatalf("reset-stale pending flush reported authorized")
	}
	if !sess.PendingBrowserKeyframeRequest || !sess.PendingBrowserKeyframeRequestAt.Equal(pendingAt) ||
		sess.PendingBrowserKeyframeRequestEpoch != pendingEpoch {
		t.Fatalf("reset-stale flush claimed new-epoch pending request")
	}
}

func TestSwitchPendingFlushValidTokenClaimsAndSends(t *testing.T) {
	sess, receiver := newSwitchFeedbackSession(t)
	decision, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", time.Now(), time.Minute, true)
	sess.MarkPendingBrowserKeyframeRequest()

	if !sess.FlushPendingBrowserKeyframeRequestForSwitch(decision.Generation, decision.MediaEpoch, "switch") {
		t.Fatalf("valid pending flush rejected")
	}
	if sess.HasPendingBrowserKeyframeRequest() {
		t.Fatalf("valid pending flush did not claim request")
	}
	buf := make([]byte, 1500)
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	if n, _, err := receiver.ReadFromUDP(buf); err != nil || n == 0 {
		t.Fatalf("valid pending flush did not send recovery: n=%d err=%v", n, err)
	}
}
