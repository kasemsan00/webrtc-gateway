package api

import (
	"encoding/json"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

func TestNotifySessionState_TrunkModeEmitsTrunkStreamOnConnecting(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("trunk", "", 42, "example.com", "u1", "p1", 5060)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	_, ch := srv.subscribeTrunkStream()

	srv.NotifySessionStateWithReason(sess.ID, session.StateConnecting, "outbound-dial")

	var payload []byte
	select {
	case payload = <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected trunk stream event")
	}

	var ev TrunkStreamEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		t.Fatalf("decode trunk event: %v", err)
	}
	if ev.Type != "session_updated" {
		t.Fatalf("event type=%q want session_updated", ev.Type)
	}
	if ev.TrunkID == nil || *ev.TrunkID != 42 {
		t.Fatalf("trunkId=%v want 42", ev.TrunkID)
	}
}
