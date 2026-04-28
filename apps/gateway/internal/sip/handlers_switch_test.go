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
			SwitchPLIDelayMS:             0,
			SwitchVideoBlackoutEnabled:   true,
			SwitchVideoBlackoutMS:        700,
			SwitchVideoBlackoutMaxWaitMS: 2000,
			VideoRecoveryBurstEnabled:    true,
			VideoRecoveryBurstWindowMS:   12000,
			VideoRecoveryBurstIntervalMS: 800,
			VideoRecoveryBurstStaleMS:    1200,
			VideoRecoveryBurstFIRStaleMS: 2500,
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
	if sess.SwitchVideoBlackoutUntil.IsZero() {
		t.Fatalf("expected @switch blackout window to be set")
	}
	if sess.SwitchVideoBlackoutMaxWait.IsZero() {
		t.Fatalf("expected @switch blackout max-wait window to be set")
	}
}
