package session

import (
	"net"
	"testing"
	"time"
)

func newBurstTestSession(id string) *Session {
	return &Session{
		ID:                         id,
		VideoRecoveryBurstEnabled:  true,
		VideoRecoveryBurstWindow:   12 * time.Second,
		VideoRecoveryBurstInterval: 800 * time.Millisecond,
		VideoRecoveryBurstStale:    1200 * time.Millisecond,
		VideoRecoveryBurstFIRStale: 2500 * time.Millisecond,
	}
}

func makeSIPVideoRecoveryReady(t *testing.T, sess *Session) {
	t.Helper()

	conn, port := newUDPConn(t)
	t.Cleanup(func() {
		_ = conn.Close()
	})

	sess.VideoRTCPConn = conn
	sess.AsteriskVideoAddr = &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: port,
	}
	sess.RemoteVideoSSRC = 1234
}

func TestVideoRecoveryBurstPolicyLifecycle(t *testing.T) {
	sess := newBurstTestSession("burst-policy")

	baseInterval := 3 * time.Second
	baseStale := 5 * time.Second
	baseFirStale := 10 * time.Second

	interval, stale, firStale, active := sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFirStale)
	if active {
		t.Fatalf("expected burst policy inactive before start")
	}
	if interval != baseInterval || stale != baseStale || firStale != baseFirStale {
		t.Fatalf("expected base policy before burst, got interval=%s stale=%s firStale=%s", interval, stale, firStale)
	}

	sess.StartVideoRecoveryBurst("unit-test")

	interval, stale, firStale, active = sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFirStale)
	if !active {
		t.Fatalf("expected burst policy active after start")
	}
	if interval != 800*time.Millisecond || stale != 1200*time.Millisecond || firStale != 2500*time.Millisecond {
		t.Fatalf("unexpected burst policy: interval=%s stale=%s firStale=%s", interval, stale, firStale)
	}
	if !sess.ShouldUseVideoRTCPFallback() {
		t.Fatalf("expected RTCP fallback active during burst window")
	}

	sess.StopVideoRecoveryBurstIfActive("unit-test-stop")
	interval, stale, firStale, active = sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFirStale)
	if active {
		t.Fatalf("expected burst policy inactive after stop")
	}
	if interval != baseInterval || stale != baseStale || firStale != baseFirStale {
		t.Fatalf("expected base policy after stop, got interval=%s stale=%s firStale=%s", interval, stale, firStale)
	}
	if sess.ShouldUseVideoRTCPFallback() {
		t.Fatalf("expected RTCP fallback disabled after burst stop")
	}
}

func TestSendBrowserRecoveryToAsterisk_UsesBothInBurstForWSKeyframe(t *testing.T) {
	sess := newBurstTestSession("burst-ws-request")
	makeSIPVideoRecoveryReady(t, sess)
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	if action != "both" {
		t.Fatalf("expected action=both during burst ws-request_keyframe, got %s", action)
	}
}

func TestSendBrowserRecoveryToAsterisk_DoesNotSuppressFreshWSKeyframeInBurst(t *testing.T) {
	sess := newBurstTestSession("burst-fresh-ws-request")
	makeSIPVideoRecoveryReady(t, sess)
	sess.LastKeyframe = time.Now()
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	if action == "none" {
		t.Fatalf("expected startup ws-request_keyframe to force recovery despite fresh keyframe")
	}
}

func TestSendBrowserRecoveryToAsterisk_DoesNotSuppressFreshBrowserPLIInBurst(t *testing.T) {
	sess := newBurstTestSession("burst-fresh-browser-pli")
	makeSIPVideoRecoveryReady(t, sess)
	sess.LastKeyframe = time.Now()
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("browser-pli")
	if action == "none" {
		t.Fatalf("expected browser PLI to force recovery during startup despite fresh keyframe")
	}
}

func TestForcedPLIStillHonorsMinimumInterval(t *testing.T) {
	sess := newBurstTestSession("burst-pli-throttle")
	now := time.Now()
	sess.LastSipPLISent = now

	if sess.shouldSendPLIToAsterisk(now.Add(pliForceMinInterval/2), true) {
		t.Fatalf("expected forced PLI to be throttled inside minimum interval")
	}
	if !sess.shouldSendPLIToAsterisk(now.Add(pliForceMinInterval+time.Millisecond), true) {
		t.Fatalf("expected forced PLI after minimum interval")
	}
}

func TestRecordKeyframe_KeepsVideoRecoveryBurstOverrideActive(t *testing.T) {
	sess := newBurstTestSession("burst-keyframe")
	sess.StartVideoRecoveryBurst("unit-test")

	sess.RecordKeyframe()

	_, _, _, active := sess.GetVideoRecoveryPolicy(3*time.Second, 5*time.Second, 10*time.Second)
	if !active {
		t.Fatalf("expected burst policy to remain active after first keyframe")
	}
}

func TestSendBrowserRecoveryToAsterisk_ForcesRecoveryAfterFreshKeyframeAndSSRCInBurst(t *testing.T) {
	sess := newBurstTestSession("burst-fresh-keyframe-ssrc")
	makeSIPVideoRecoveryReady(t, sess)
	now := time.Now()
	sess.LastKeyframe = now
	sess.LastSipFIRSent = now
	sess.LastSipPLISent = now.Add(-time.Second)
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	if action == "none" {
		t.Fatalf("expected fresh keyframe request to force recovery while burst window is active")
	}
}

func TestSendBrowserRecoveryToAsterisk_DefersToWebRTCWhenSIPSSRCMissing(t *testing.T) {
	sess := newBurstTestSession("missing-sip-ssrc")
	conn, port := newUDPConn(t)
	defer conn.Close()

	sess.VideoRTCPConn = conn
	sess.AsteriskVideoAddr = &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: port,
	}
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	if action != "webrtc" {
		t.Fatalf("expected missing SIP SSRC to defer recovery to WebRTC, got %s", action)
	}
	if !sess.LastSipPLISent.IsZero() {
		t.Fatalf("expected SIP PLI timestamp to remain unset when recovery is deferred")
	}
}
