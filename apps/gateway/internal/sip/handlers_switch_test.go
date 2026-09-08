package sip

import (
	"sync"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

type recordingSwitchRenegotiateStarter struct {
	mu         sync.Mutex
	n          int
	sessionID  string
	generation int
}

func (r *recordingSwitchRenegotiateStarter) StartSwitchVideoRenegotiation(sessionID string, generation int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	r.sessionID = sessionID
	r.generation = generation
}

func (r *recordingSwitchRenegotiateStarter) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *recordingSwitchRenegotiateStarter) lastID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

func (r *recordingSwitchRenegotiateStarter) lastGen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.generation
}

func TestHandleSwitchMessage_StartsVideoRecoveryBurst(t *testing.T) {
	cfg := &config.Config{
		SIP: config.SIPConfig{
			VideoAUNormalizeEnabled:        true,
			SwitchPLIDelayMS:               0,
			SwitchVideoTransitionMode:      config.SIPSwitchVideoTransitionPreserve,
			SwitchVideoBlackoutEnabled:     true,
			SwitchVideoBlackoutMS:          300,
			SwitchVideoBlackoutMaxWaitMS:   1200,
			SwitchVideoRecoveryWindowMS:    5000,
			SwitchVideoRecoveryStableMS:    750,
			SwitchDuplicateDebounceEnabled: true,
			SwitchDuplicateDebounceMS:      60000,
			VideoRecoveryBurstEnabled:      true,
			VideoRecoveryBurstWindowMS:     12000,
			VideoRecoveryBurstIntervalMS:   800,
			VideoRecoveryBurstStaleMS:      1200,
			VideoRecoveryBurstFIRStaleMS:   2500,
		},
		RTP: config.RTPConfig{BufferSize: 1500},
	}

	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() {
		mgr.DeleteSession(sess.ID)
	})

	sess.SetCallInfo("outbound", "sip:0900200002@example.com", "1002", "call-1")
	sess.SetState(session.StateActive)
	sess.CacheSIPSPS([]byte{0x67, 0x64, 0x00, 0x28})
	sess.CacheSIPPPS([]byte{0x68, 0xee, 0x3c, 0x80})

	baseInterval := 3 * time.Second
	baseStale := 5 * time.Second
	baseFIRStale := 10 * time.Second

	_, _, _, active := sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFIRStale)
	if active {
		t.Fatalf("expected burst policy inactive before @switch")
	}

	starter := &recordingSwitchRenegotiateStarter{}
	srv := &Server{
		config:                     cfg.SIP,
		rtpConfig:                  cfg.RTP,
		sessionMgr:                 mgr,
		switchRenegotiationStarter: starter,
	}

	srv.handleSwitchMessage("@switch:14131|00025", "sip:0900200002@example.com")

	_, _, _, active = sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFIRStale)
	if !active {
		t.Fatalf("expected burst policy active after @switch")
	}
	if !sess.ShouldUseVideoRTCPFallback() {
		t.Fatalf("expected RTCP fallback active during @switch burst")
	}
	if sess.VideoRecoveryBurstLastReason != "switch" {
		t.Fatalf("expected burst reason switch, got %q", sess.VideoRecoveryBurstLastReason)
	}
	if sess.VideoRecoveryBurstUntil.IsZero() {
		t.Fatalf("expected burst window to be set after @switch")
	}
	if !sess.IsSwitchVideoRecoveryActive() {
		t.Fatalf("expected switch recovery state to be active after @switch")
	}
	if sess.SwitchVideoRecoveryStableWindow != 750*time.Millisecond {
		t.Fatalf("expected switch stable window from config, got %s", sess.SwitchVideoRecoveryStableWindow)
	}
	if sess.SwitchVideoTransitionMode != config.SIPSwitchVideoTransitionPreserve {
		t.Fatalf("expected @switch preserve transition mode, got %q", sess.SwitchVideoTransitionMode)
	}
	if sess.SwitchVideoBlackoutUntil.IsZero() {
		t.Fatalf("expected @switch transition window to be set")
	}
	if sess.SwitchVideoBlackoutMaxWait.IsZero() {
		t.Fatalf("expected @switch transition max-wait window to be set")
	}
	if !sess.SwitchVideoGateActive {
		t.Fatalf("expected complete-IDR gate active after accepted @switch")
	}
	if sess.SwitchVideoGateGeneration != sess.GetSwitchGeneration() {
		t.Fatalf("gate generation = %d, switch generation = %d", sess.SwitchVideoGateGeneration, sess.GetSwitchGeneration())
	}
	if _, _, ok := sess.GetSIPCachedSPSPPS(); ok {
		t.Fatalf("expected accepted switch to clear stale SIP-side parameter sets")
	}
	if sess.SwitchSPSPPSInjectRemaining != 3 {
		t.Fatalf("expected uplink SPS/PPS inject armed to 3, got %d", sess.SwitchSPSPPSInjectRemaining)
	}

	deadline := time.Now().Add(time.Second)
	for starter.calls() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if starter.calls() != 1 || starter.lastID() != sess.ID || starter.lastGen() != sess.GetSwitchGeneration() {
		t.Fatalf("expected @switch renegotiate start session=%s generation=%d calls=%d last=%s/%d",
			sess.ID, sess.GetSwitchGeneration(), starter.calls(), starter.lastID(), starter.lastGen())
	}
}

func TestHandleSwitchMessage_IgnoresDuplicateTargetInsideDebounce(t *testing.T) {
	cfg := &config.Config{
		SIP: config.SIPConfig{
			VideoAUNormalizeEnabled:        true,
			SwitchPLIDelayMS:               0,
			SwitchVideoTransitionMode:      config.SIPSwitchVideoTransitionPreserve,
			SwitchVideoBlackoutEnabled:     true,
			SwitchVideoBlackoutMS:          300,
			SwitchVideoBlackoutMaxWaitMS:   1200,
			SwitchVideoRecoveryWindowMS:    5000,
			SwitchVideoRecoveryStableMS:    750,
			SwitchDuplicateDebounceEnabled: true,
			SwitchDuplicateDebounceMS:      60000,
			VideoRecoveryBurstEnabled:      true,
			VideoRecoveryBurstWindowMS:     12000,
			VideoRecoveryBurstIntervalMS:   800,
			VideoRecoveryBurstStaleMS:      1200,
			VideoRecoveryBurstFIRStaleMS:   2500,
		},
		RTP: config.RTPConfig{BufferSize: 1500},
	}

	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() {
		mgr.DeleteSession(sess.ID)
	})

	sess.SetCallInfo("outbound", "sip:0900200002@example.com", "1002", "call-1")
	sess.SetState(session.StateActive)
	sess.SetRemoteVideoSSRC(1234)

	starter := &recordingSwitchRenegotiateStarter{}
	srv := &Server{
		config:                     cfg.SIP,
		rtpConfig:                  cfg.RTP,
		sessionMgr:                 mgr,
		switchRenegotiationStarter: starter,
	}

	srv.handleSwitchMessage("@switch:14131|00025", "sip:0900200002@example.com")
	firstUntil := sess.VideoRecoveryBurstUntil
	firstGeneration := sess.GetSwitchGeneration()
	gateGeneration := sess.SwitchVideoGateGeneration
	gateStartedAt := sess.SwitchVideoGateStartedAt
	gateLeaseNonce := sess.SwitchVideoGateLeaseNonce
	gateReservation := sess.SwitchVideoGateReservation
	gateBaseline := sess.SwitchVideoGateFeedbackBaseline
	gateRejected := sess.SwitchVideoGateRejectedCount

	srv.handleSwitchMessage("@switch:14131|00025", "sip:0900200002@example.com")

	if sess.GetSwitchGeneration() != firstGeneration {
		t.Fatalf("expected duplicate target to keep generation %d, got %d", firstGeneration, sess.GetSwitchGeneration())
	}
	if !sess.VideoRecoveryBurstUntil.Equal(firstUntil) {
		t.Fatalf("expected duplicate target not to restart recovery burst")
	}
	if sess.SwitchDuplicateCount != 1 {
		t.Fatalf("expected duplicate count 1, got %d", sess.SwitchDuplicateCount)
	}
	if sess.SwitchVideoGateGeneration != gateGeneration || !sess.SwitchVideoGateStartedAt.Equal(gateStartedAt) ||
		sess.SwitchVideoGateLeaseNonce != gateLeaseNonce || sess.SwitchVideoGateReservation != gateReservation ||
		sess.SwitchVideoGateFeedbackBaseline != gateBaseline || sess.SwitchVideoGateRejectedCount != gateRejected {
		t.Fatalf("duplicate mutated gate: generation=%d started=%s nonce=%d reservation=%d baseline=%d rejected=%d",
			sess.SwitchVideoGateGeneration, sess.SwitchVideoGateStartedAt, sess.SwitchVideoGateLeaseNonce,
			sess.SwitchVideoGateReservation, sess.SwitchVideoGateFeedbackBaseline, sess.SwitchVideoGateRejectedCount)
	}
	if starter.calls() != 1 {
		t.Fatalf("expected one renegotiate start for first @switch, got %d", starter.calls())
	}
}

func TestHandleSwitchMessage_MalformedForcePathDoesNotStartVideoGate(t *testing.T) {
	cfg := &config.Config{
		SIP: config.SIPConfig{
			VideoAUNormalizeEnabled:     true,
			SwitchPLIDelayMS:            0,
			SwitchVideoRecoveryWindowMS: 5000,
			SwitchVideoRecoveryStableMS: 750,
		},
		RTP: config.RTPConfig{BufferSize: 1500},
	}
	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
	sess.SetCallInfo("outbound", "sip:0900200002@example.com", "1002", "call-1")
	sess.SetState(session.StateActive)

	srv := &Server{config: cfg.SIP, rtpConfig: cfg.RTP, sessionMgr: mgr}
	srv.handleSwitchMessage("not-a-switch", "sip:0900200002@example.com")

	if sess.SwitchVideoGateActive || sess.SwitchVideoGateGeneration != 0 || sess.GetSwitchGeneration() != 0 {
		t.Fatalf("malformed force path started gate: active=%v gateGeneration=%d switchGeneration=%d",
			sess.SwitchVideoGateActive, sess.SwitchVideoGateGeneration, sess.GetSwitchGeneration())
	}
}

func TestHandleSwitchMessage_StaleGenerationAbortsBeforeRecoveryFeedbackOrHold(t *testing.T) {
	cfg := switchStaleHandlerTestConfig(true)
	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
	sess.SetCallInfo("outbound", "sip:0900200002@example.com", "1002", "call-1")
	sess.SetState(session.StateActive)

	accepted := make(chan struct{})
	resume := make(chan struct{})
	stages := make(chan string, 16)
	srv := &Server{config: cfg.SIP, rtpConfig: cfg.RTP, sessionMgr: mgr}
	srv.switchHandlerTestHook = func(stage string, decision session.SwitchTargetDecision) {
		if stage == "accepted" && decision.Generation == 1 {
			close(accepted)
			<-resume
			return
		}
		stages <- stage
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleSwitchMessage("@switch:14131|00025", "sip:0900200002@example.com")
	}()
	<-accepted

	newer, activation := sess.PrepareAndActivateSwitchVideoTarget("14131", "00026", time.Now(), time.Minute, true)
	if newer.Generation != 2 || !activation.Active {
		t.Fatalf("failed to accept newer switch: decision=%+v activation=%+v", newer, activation)
	}
	sess.CacheSIPSPS([]byte{0x67, 0x64, 0x00, 0x29})
	sess.CacheSIPPPS([]byte{0x68, 0xee, 0x3c, 0x81})
	gateStartedAt := sess.SwitchVideoGateStartedAt
	close(resume)
	<-done

	select {
	case stage := <-stages:
		t.Fatalf("stale handler entered stage %q", stage)
	default:
	}
	if sess.IsSwitchVideoRecoveryActive() || !sess.SwitchVideoBlackoutUntil.IsZero() {
		t.Fatalf("stale handler mutated recovery/hold state")
	}
	if sess.SwitchVideoGateGeneration != newer.Generation || !sess.SwitchVideoGateStartedAt.Equal(gateStartedAt) {
		t.Fatalf("stale handler mutated newer gate")
	}
	if _, _, ok := sess.GetSIPCachedSPSPPS(); !ok {
		t.Fatalf("stale handler cleared newer SIP parameter sets")
	}
}

func TestHandleSwitchMessage_ResetEpochAbortsBeforeRecoveryFeedbackOrHold(t *testing.T) {
	cfg := switchStaleHandlerTestConfig(true)
	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
	sess.SetCallInfo("outbound", "sip:0900200002@example.com", "1002", "call-1")
	sess.SetState(session.StateActive)

	accepted := make(chan struct{})
	resume := make(chan struct{})
	stages := make(chan string, 16)
	srv := &Server{config: cfg.SIP, rtpConfig: cfg.RTP, sessionMgr: mgr}
	srv.switchHandlerTestHook = func(stage string, decision session.SwitchTargetDecision) {
		if stage == "accepted" {
			close(accepted)
			<-resume
			return
		}
		stages <- stage
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleSwitchMessage("@switch:14131|00025", "sip:0900200002@example.com")
	}()
	<-accepted

	sess.ResetMediaState()
	sess.CacheSIPSPS([]byte{0x67, 0x64, 0x00, 0x2a})
	sess.CacheSIPPPS([]byte{0x68, 0xee, 0x3c, 0x82})
	close(resume)
	<-done

	select {
	case stage := <-stages:
		t.Fatalf("reset-invalidated handler entered stage %q", stage)
	default:
	}
	if sess.IsSwitchVideoRecoveryActive() || !sess.SwitchVideoBlackoutUntil.IsZero() || sess.SwitchVideoGateActive {
		t.Fatalf("reset-invalidated handler mutated new-call media state")
	}
	if _, _, ok := sess.GetSIPCachedSPSPPS(); !ok {
		t.Fatalf("reset-invalidated handler cleared new-call SIP parameter sets")
	}
}

func TestHandleSwitchMessage_NormalizationDisabledKeepsLegacyRecovery(t *testing.T) {
	cfg := switchStaleHandlerTestConfig(false)
	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
	sess.SetCallInfo("outbound", "sip:0900200002@example.com", "1002", "call-1")
	sess.SetState(session.StateActive)

	srv := &Server{config: cfg.SIP, rtpConfig: cfg.RTP, sessionMgr: mgr}
	srv.handleSwitchMessage("@switch:14131|00025", "sip:0900200002@example.com")

	if sess.SwitchVideoGateActive {
		t.Fatalf("normalization-disabled legacy path activated gate")
	}
	if !sess.IsSwitchVideoRecoveryActive() {
		t.Fatalf("normalization-disabled legacy path skipped recovery")
	}
}

func TestHandleSwitchMessage_StaleFeedbackTokenStopsFIRBurst(t *testing.T) {
	cfg := switchStaleHandlerTestConfig(true)
	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
	sess.SetCallInfo("outbound", "sip:0900200002@example.com", "1002", "call-1")
	sess.SetState(session.StateActive)

	paused := make(chan struct{})
	resume := make(chan struct{})
	stages := make(chan string, 16)
	srv := &Server{config: cfg.SIP, rtpConfig: cfg.RTP, sessionMgr: mgr}
	srv.switchHandlerTestHook = func(stage string, decision session.SwitchTargetDecision) {
		if stage == "fir-send" && decision.Generation == 1 {
			close(paused)
			<-resume
			return
		}
		stages <- stage
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleSwitchMessage("@switch:14131|00025", "sip:0900200002@example.com")
	}()
	<-paused

	newer, activation := sess.PrepareAndActivateSwitchVideoTarget("14131", "00026", time.Now(), time.Minute, true)
	if newer.Generation != 2 || !activation.Active {
		t.Fatalf("failed to accept newer switch: decision=%+v activation=%+v", newer, activation)
	}
	close(resume)
	<-done

	for {
		select {
		case stage := <-stages:
			if stage == "pli-burst" {
				t.Fatalf("stale FIR feedback token did not stop remaining bursts")
			}
		default:
			return
		}
	}
}

func TestHandleSwitchMessage_SkipsBurstAfterKeyframeRecovered(t *testing.T) {
	cfg := switchStaleHandlerTestConfig(true)
	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
	sess.SetCallInfo("outbound", "sip:0900200002@example.com", "1002", "call-1")
	sess.SetState(session.StateActive)

	stages := make(chan string, 16)
	srv := &Server{config: cfg.SIP, rtpConfig: cfg.RTP, sessionMgr: mgr}
	srv.switchHandlerTestHook = func(stage string, decision session.SwitchTargetDecision) {
		if stage == "fir-burst" {
			sess.MarkSwitchVideoKeyframe(time.Now())
		}
		stages <- stage
	}
	srv.handleSwitchMessage("@switch:14131|00025", "sip:0900200002@example.com")

	sawFIRBurst := false
	for {
		select {
		case stage := <-stages:
			if stage == "fir-burst" {
				sawFIRBurst = true
			}
			if stage == "fir-send" || stage == "pli-burst" || stage == "pli-send" {
				t.Fatalf("recovered switch still sent delayed burst stage=%s", stage)
			}
		default:
			if !sawFIRBurst {
				t.Fatal("expected fir-burst hook before skip")
			}
			return
		}
	}
}

func switchStaleHandlerTestConfig(normalize bool) *config.Config {
	return &config.Config{
		SIP: config.SIPConfig{
			VideoAUNormalizeEnabled:        normalize,
			SwitchPLIDelayMS:               0,
			SwitchVideoTransitionMode:      config.SIPSwitchVideoTransitionPreserve,
			SwitchVideoBlackoutEnabled:     true,
			SwitchVideoBlackoutMS:          300,
			SwitchVideoBlackoutMaxWaitMS:   1200,
			SwitchVideoRecoveryWindowMS:    5000,
			SwitchVideoRecoveryStableMS:    750,
			SwitchDuplicateDebounceEnabled: true,
			SwitchDuplicateDebounceMS:      60000,
			VideoRecoveryBurstEnabled:      true,
			VideoRecoveryBurstWindowMS:     12000,
			VideoRecoveryBurstIntervalMS:   800,
			VideoRecoveryBurstStaleMS:      1200,
			VideoRecoveryBurstFIRStaleMS:   2500,
		},
		RTP: config.RTPConfig{BufferSize: 1500},
	}
}
