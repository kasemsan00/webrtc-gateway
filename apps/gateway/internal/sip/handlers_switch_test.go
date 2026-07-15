package sip

import (
	"testing"
	"time"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
)

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

	srv := &Server{
		config:     cfg.SIP,
		rtpConfig:  cfg.RTP,
		sessionMgr: mgr,
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

	srv := &Server{
		config:     cfg.SIP,
		rtpConfig:  cfg.RTP,
		sessionMgr: mgr,
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
