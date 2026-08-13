package session

import (
	"net"
	"testing"
	"time"
)

func newBurstTestSession(id string) *Session {
	return &Session{
		ID:                                   id,
		VideoRecoveryBurstEnabled:            true,
		VideoRecoveryBurstWindow:             12 * time.Second,
		VideoRecoveryBurstInterval:           800 * time.Millisecond,
		VideoRecoveryBurstStale:              1200 * time.Millisecond,
		VideoRecoveryBurstFIRStale:           2500 * time.Millisecond,
		SwitchVideoRTPStabilityEnabled:       true,
		SwitchVideoRTPMinPacketDelta:         30,
		SwitchVideoRTPMaxGapDelta:            12,
		SwitchVideoRTPMaxMissingDelta:        20,
		SwitchVideoRTPMaxOutOfOrderDelta:     20,
		SwitchVideoRTPMaxReorderDropDelta:    0,
		SwitchVideoRTPMaxReorderTimeoutDelta: 5,
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

func TestEndVideoRecoveryBurstIsIdempotentAndFinishesSwitchAfterReasonOverride(t *testing.T) {
	sess := newBurstTestSession("switch-end-idempotent")
	sess.StartSwitchVideoRecovery(5*time.Second, 750*time.Millisecond)
	sess.StartVideoRecoveryBurst("ice-reconnecting")

	sess.mu.Lock()
	first := sess.endVideoRecoveryBurst(time.Now(), "stable-rtp")
	second := sess.endVideoRecoveryBurst(time.Now(), "stable-rtp")
	switchActive := !sess.SwitchVideoRecoveryStartedAt.IsZero() || !sess.SwitchVideoRecoveryUntil.IsZero()
	sess.mu.Unlock()

	if !first {
		t.Fatal("expected first recovery end to change state")
	}
	if second {
		t.Fatal("expected repeated recovery end to be a no-op")
	}
	if switchActive {
		t.Fatal("expected switch recovery cleared even after burst reason override")
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

func TestSendBrowserRecoveryToAsterisk_SuppressesFreshWSKeyframeInBurst(t *testing.T) {
	sess := newBurstTestSession("burst-fresh-ws-request")
	makeSIPVideoRecoveryReady(t, sess)
	sess.LastKeyframe = time.Now()
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	if action != "none" {
		t.Fatalf("expected fresh keyframe to suppress startup ws-request_keyframe, got %s", action)
	}
}

func TestSendBrowserRecoveryToAsterisk_LegacyFreshRequestRemainsSuppressedOutsideBurst(t *testing.T) {
	sess := newBurstTestSession("legacy-fresh-keyframe")
	makeSIPVideoRecoveryReady(t, sess)
	sess.LastKeyframe = time.Now()

	if action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe"); action != "none" {
		t.Fatalf("expected legacy fresh request to keep existing action=none behavior, got %s", action)
	}
}

func TestSendBrowserRecoveryToAsterisk_SuppressesFreshBrowserPLIInBurst(t *testing.T) {
	sess := newBurstTestSession("burst-fresh-browser-pli")
	makeSIPVideoRecoveryReady(t, sess)
	sess.LastKeyframe = time.Now()
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("browser-pli")
	if action != "none" {
		t.Fatalf("expected fresh keyframe to suppress browser PLI during startup, got %s", action)
	}
}

func TestSendBrowserRecoveryToAsterisk_ForcesRecoveryWhenBurstKeyframeIsStale(t *testing.T) {
	sess := newBurstTestSession("burst-stale-browser-pli")
	makeSIPVideoRecoveryReady(t, sess)
	sess.LastKeyframe = time.Now().Add(-3 * time.Second)
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("browser-pli")
	if action == "none" {
		t.Fatalf("expected stale keyframe during burst to still request recovery")
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

func TestSwitchVideoRecoveryLifecycleEndsAfterStableProgress(t *testing.T) {
	sess := newBurstTestSession("switch-stable")
	sess.StartSwitchVideoRecovery(2*time.Second, 20*time.Millisecond)

	_, _, _, active := sess.GetVideoRecoveryPolicy(3*time.Second, 5*time.Second, 10*time.Second)
	if !active {
		t.Fatalf("expected switch recovery burst to be active")
	}
	if !sess.IsSwitchVideoRecoveryActive() {
		t.Fatalf("expected switch recovery state to be active")
	}

	sess.MarkSwitchVideoKeyframe(time.Now())
	sess.UpdateSwitchVideoRecoverySummary(VideoRecoverySummary{
		Packets:         100,
		Gaps:            10,
		Missing:         15,
		OutOfOrder:      15,
		LastKeyframeAge: 5 * time.Millisecond,
	})
	time.Sleep(25 * time.Millisecond)
	sess.UpdateSwitchVideoRecoverySummary(VideoRecoverySummary{
		Packets:         160,
		Gaps:            12,
		Missing:         18,
		OutOfOrder:      18,
		LastKeyframeAge: 30 * time.Millisecond,
	})
	sess.MarkSwitchVideoProgress(time.Now(), false)

	_, _, _, active = sess.GetVideoRecoveryPolicy(3*time.Second, 5*time.Second, 10*time.Second)
	if active {
		t.Fatalf("expected switch recovery burst to stop after stable progress")
	}
	if sess.IsSwitchVideoRecoveryActive() {
		t.Fatalf("expected switch recovery state to stop after stable progress")
	}
}

func TestSwitchVideoRecoveryStaysActiveWhenRTPDeltaUnstable(t *testing.T) {
	sess := newBurstTestSession("switch-unstable-rtp")
	sess.StartSwitchVideoRecovery(2*time.Second, 20*time.Millisecond)

	sess.UpdateSwitchVideoRecoverySummary(VideoRecoverySummary{
		Packets:         100,
		Gaps:            10,
		Missing:         20,
		OutOfOrder:      20,
		LastKeyframeAge: 5 * time.Millisecond,
	})
	sess.MarkSwitchVideoKeyframe(time.Now())
	time.Sleep(25 * time.Millisecond)
	sess.UpdateSwitchVideoRecoverySummary(VideoRecoverySummary{
		Packets:         200,
		Gaps:            50,
		Missing:         100,
		OutOfOrder:      100,
		LastKeyframeAge: 30 * time.Millisecond,
	})
	sess.MarkSwitchVideoProgress(time.Now(), false)

	if !sess.IsSwitchVideoRecoveryActive() {
		t.Fatalf("expected switch recovery to remain active while RTP disorder delta is high")
	}
	_, _, _, active := sess.GetVideoRecoveryPolicy(3*time.Second, 5*time.Second, 10*time.Second)
	if !active {
		t.Fatalf("expected recovery burst to remain active while RTP disorder delta is high")
	}
}

func TestSwitchVideoRecoveryEndsAfterRTPDeltaStabilizes(t *testing.T) {
	sess := newBurstTestSession("switch-rtp-stabilizes")
	sess.StartSwitchVideoRecovery(2*time.Second, 20*time.Millisecond)

	sess.UpdateSwitchVideoRecoverySummary(VideoRecoverySummary{
		Packets:         100,
		Gaps:            10,
		Missing:         20,
		OutOfOrder:      20,
		LastKeyframeAge: 5 * time.Millisecond,
	})
	sess.MarkSwitchVideoKeyframe(time.Now())
	time.Sleep(25 * time.Millisecond)
	sess.UpdateSwitchVideoRecoverySummary(VideoRecoverySummary{
		Packets:         200,
		Gaps:            50,
		Missing:         100,
		OutOfOrder:      100,
		LastKeyframeAge: 30 * time.Millisecond,
	})
	sess.MarkSwitchVideoProgress(time.Now(), false)

	if !sess.IsSwitchVideoRecoveryActive() {
		t.Fatalf("expected first unstable window to keep recovery active")
	}

	time.Sleep(25 * time.Millisecond)
	sess.UpdateSwitchVideoRecoverySummary(VideoRecoverySummary{
		Packets:         260,
		Gaps:            52,
		Missing:         108,
		OutOfOrder:      108,
		LastKeyframeAge: 55 * time.Millisecond,
	})
	sess.MarkSwitchVideoProgress(time.Now(), false)

	if sess.IsSwitchVideoRecoveryActive() {
		t.Fatalf("expected recovery to end after RTP disorder delta stabilizes")
	}
}

func TestSwitchVideoRecoveryTimesOut(t *testing.T) {
	sess := newBurstTestSession("switch-timeout")
	sess.StartSwitchVideoRecovery(30*time.Millisecond, 10*time.Millisecond)

	time.Sleep(40 * time.Millisecond)
	_, _, _, active := sess.GetVideoRecoveryPolicy(3*time.Second, 5*time.Second, 10*time.Second)
	if active {
		t.Fatalf("expected switch recovery burst to time out")
	}
	if sess.IsSwitchVideoRecoveryActive() {
		t.Fatalf("expected switch recovery state to time out")
	}
}

func TestSwitchVideoRecoveryRestartIsIdempotent(t *testing.T) {
	sess := newBurstTestSession("switch-restart")
	sess.StartSwitchVideoRecovery(2*time.Second, 500*time.Millisecond)
	firstUntil := sess.VideoRecoveryBurstUntil
	time.Sleep(time.Millisecond)
	sess.StartSwitchVideoRecovery(2*time.Second, 500*time.Millisecond)

	if !sess.VideoRecoveryBurstUntil.After(firstUntil) {
		t.Fatalf("expected repeated switch to refresh recovery window")
	}
	if !sess.ConsumeSwitchPLIBypass() {
		t.Fatalf("expected switch PLI bypass to be available after restart")
	}
	if sess.ConsumeSwitchPLIBypass() {
		t.Fatalf("expected switch PLI bypass to be one-shot")
	}
}

func TestSwitchPLIBypassDoesNotAffectWSKeyframeThrottle(t *testing.T) {
	sess := newBurstTestSession("switch-bypass-isolated")
	now := time.Now()
	sess.LastSipPLISent = now
	sess.StartSwitchVideoRecovery(2*time.Second, 500*time.Millisecond)

	if !sess.ConsumeSwitchPLIBypass() {
		t.Fatalf("expected switch bypass to be available")
	}
	if sess.shouldSendPLIToAsterisk(now.Add(pliForceMinInterval/2), true) {
		t.Fatalf("expected normal forced PLI throttle to remain active")
	}
}

func TestSwitchPLIBypassSendsFirstPLIInsideThrottle(t *testing.T) {
	sess := newBurstTestSession("switch-bypass-send")
	makeSIPVideoRecoveryReady(t, sess)
	now := time.Now()
	sess.LastSipPLISent = now
	sess.LastPLISent = now
	sess.PLISent = 1
	sess.StartSwitchVideoRecovery(2*time.Second, 500*time.Millisecond)

	sess.SendPLIToAsteriskForced("switch")
	firstSent := sess.LastSipPLISent
	firstCount := sess.PLISent
	if firstCount != 2 {
		t.Fatalf("expected first switch PLI to bypass throttle and increment count, got %d", firstCount)
	}

	sess.SendPLIToAsteriskForced("switch")
	if !sess.LastSipPLISent.Equal(firstSent) {
		t.Fatalf("expected repeated switch PLI inside throttle to be skipped")
	}
	if sess.PLISent != firstCount {
		t.Fatalf("expected repeated switch PLI inside throttle not to increment count")
	}
}

func TestSendBrowserRecoveryToAsterisk_SuppressesFreshKeyframeAndSSRCInBurst(t *testing.T) {
	sess := newBurstTestSession("burst-fresh-keyframe-ssrc")
	makeSIPVideoRecoveryReady(t, sess)
	now := time.Now()
	sess.LastKeyframe = now
	sess.LastSipFIRSent = now
	sess.LastSipPLISent = now.Add(-time.Second)
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	if action != "none" {
		t.Fatalf("expected fresh keyframe request to stay quiet while burst window is active, got %s", action)
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

func TestPendingBrowserKeyframe_MarkedWhenSSRCMissing(t *testing.T) {
	sess := newBurstTestSession("pending-missing-ssrc")
	// Ready addr/conn but SSRC=0 so recovery defers
	conn, port := newUDPConn(t)
	t.Cleanup(func() { _ = conn.Close() })
	sess.VideoRTCPConn = conn
	sess.AsteriskVideoAddr = &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
	sess.RemoteVideoSSRC = 0

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	if action != "webrtc" {
		t.Fatalf("expected deferred action=webrtc, got %s", action)
	}
	if !sess.HasPendingBrowserKeyframeRequest() {
		t.Fatalf("expected pending browser keyframe request after missing-ssrc defer")
	}
}

func TestPendingBrowserKeyframe_FlushOnSSRCReady(t *testing.T) {
	sess := newBurstTestSession("pending-flush-ssrc")
	conn, port := newUDPConn(t)
	t.Cleanup(func() { _ = conn.Close() })
	sess.VideoRTCPConn = conn
	sess.AsteriskVideoAddr = &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
	sess.RemoteVideoSSRC = 0
	sess.StartVideoRecoveryBurst("unit-test")

	_ = sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	if !sess.HasPendingBrowserKeyframeRequest() {
		t.Fatalf("expected pending before flush")
	}

	sess.RemoteVideoSSRC = 1234
	flushed := sess.FlushPendingBrowserKeyframeRequest("ssrc-learn")
	if flushed != "both" && flushed != "pli" && flushed != "fir" {
		t.Fatalf("expected SIP-directed flush action, got %s", flushed)
	}
	if sess.HasPendingBrowserKeyframeRequest() {
		t.Fatalf("expected pending cleared after flush")
	}
}

func TestPendingBrowserKeyframe_FlushOnSwitchStart(t *testing.T) {
	sess := newBurstTestSession("pending-flush-switch")
	makeSIPVideoRecoveryReady(t, sess)
	sess.MarkPendingBrowserKeyframeRequest()

	sess.StartSwitchVideoRecovery(5*time.Second, 750*time.Millisecond)
	// StartSwitchVideoRecovery must flush pending when SIP target is ready
	if sess.HasPendingBrowserKeyframeRequest() {
		t.Fatalf("expected pending flushed when switch recovery starts")
	}
}

func TestPendingBrowserKeyframe_ClearedOnResetMediaState(t *testing.T) {
	sess := newBurstTestSession("pending-clear-reset")
	sess.MarkPendingBrowserKeyframeRequest()
	sess.ResetMediaState()
	if sess.HasPendingBrowserKeyframeRequest() {
		t.Fatalf("expected pending cleared on ResetMediaState")
	}
}

func TestFlushPendingBrowserKeyframe_NoopWhenEmpty(t *testing.T) {
	sess := newBurstTestSession("pending-noop")
	makeSIPVideoRecoveryReady(t, sess)
	if got := sess.FlushPendingBrowserKeyframeRequest("ssrc-learn"); got != "none" {
		t.Fatalf("expected none when no pending, got %s", got)
	}
}

func TestShouldStopStartupBrowserPLI(t *testing.T) {
	sess := newBurstTestSession("startup-browser-pli")
	if sess.ShouldStopStartupBrowserPLI() {
		t.Fatal("expected startup PLI to continue before SPS/PPS and uplink IDR")
	}
	sess.CachedSPS = []byte{0x67}
	sess.CachedPPS = []byte{0x68}
	if sess.ShouldStopStartupBrowserPLI() {
		t.Fatal("expected startup PLI to continue until an uplink IDR is forwarded")
	}
	sess.RecordUplinkKeyframe()
	if !sess.ShouldStopStartupBrowserPLI() {
		t.Fatal("expected first-packet PLI to stop after SPS/PPS and uplink IDR")
	}
	if sess.ShouldStopPeriodicBrowserPLI() {
		t.Fatal("expected periodic PLI to continue until the late-join window elapses")
	}
	sess.sipVideoDestReadyAt = time.Now()
	if sess.ShouldStopPeriodicBrowserPLI() {
		t.Fatal("expected periodic PLI to continue while dest-ready window is still open")
	}
	sess.sipVideoDestReadyAt = time.Now().Add(-lateJoinBrowserPLIWindow - time.Millisecond)
	if !sess.ShouldStopPeriodicBrowserPLI() {
		t.Fatal("expected periodic PLI to stop after dest-ready late-join window")
	}
}

func TestHasUplinkKeyframeSinceIgnoresOlderIDR(t *testing.T) {
	sess := newBurstTestSession("uplink-since")
	before := time.Now()
	time.Sleep(2 * time.Millisecond)
	sess.RecordUplinkKeyframe()
	if !sess.HasUplinkKeyframeSince(before) {
		t.Fatal("expected uplink IDR after kick start to count")
	}
	later := time.Now().Add(time.Second)
	if sess.HasUplinkKeyframeSince(later) {
		t.Fatal("expected older uplink IDR not to satisfy a later kick")
	}
}

func TestShouldContinueSwitchFeedbackBurstStopsAfterFirstKeyframe(t *testing.T) {
	sess := newBurstTestSession("switch-burst-stop")
	sess.StartSwitchVideoRecovery(2*time.Second, 20*time.Millisecond)
	if !sess.ShouldContinueSwitchFeedbackBurst(0, 0, false) {
		t.Fatal("expected delayed switch burst before first keyframe")
	}
	sess.MarkSwitchVideoKeyframe(time.Now())
	if sess.ShouldContinueSwitchFeedbackBurst(0, 0, false) {
		t.Fatal("expected delayed switch burst to stop after first keyframe")
	}
}

func TestSIPFIRHonorsMinimumInterval(t *testing.T) {
	sess := newBurstTestSession("sip-fir-throttle")
	makeSIPVideoRecoveryReady(t, sess)
	sess.SendFIRToAsterisk()
	first := sess.LastSipFIRSent
	if first.IsZero() {
		t.Fatal("expected first FIR to send")
	}
	count := sess.PLISent
	sess.SendFIRToAsterisk()
	if !sess.LastSipFIRSent.Equal(first) {
		t.Fatal("expected second FIR inside 1s interval to be skipped")
	}
	if sess.PLISent != count {
		t.Fatal("expected throttled FIR not to increment PLISent")
	}
}
