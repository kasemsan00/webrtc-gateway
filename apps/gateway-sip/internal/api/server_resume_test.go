package api

import (
	"strings"
	"testing"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
)

func newTestSessionManager() *session.Manager {
	cfg := &config.Config{
		RTP: config.RTPConfig{
			BufferSize: 4096,
		},
	}
	return session.NewManager(cfg)
}

func createActiveSession(t *testing.T, mgr *session.Manager) *session.Session {
	t.Helper()

	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetCallInfo("outbound", "1001", "1002", "call-1")
	sess.SetState(session.StateActive)
	return sess
}

func TestHandleWSResume_SessionNotFound(t *testing.T) {
	mgr := newTestSessionManager()
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSResume(client, WSMessage{
		Type:      "resume",
		SessionID: "missing-session",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "resume_failed" {
		t.Fatalf("expected resume_failed, got %s", msgs[0].Type)
	}
	if !strings.Contains(msgs[0].Reason, "not found") {
		t.Fatalf("expected not found reason, got %q", msgs[0].Reason)
	}
}

func TestHandleWSResume_EmptySessionID(t *testing.T) {
	mgr := newTestSessionManager()
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSResume(client, WSMessage{
		Type:      "resume",
		SessionID: "",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "error" {
		t.Fatalf("expected error, got %s", msgs[0].Type)
	}
}

func TestHandleWSResume_StateNotResumable(t *testing.T) {
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)
	sess.SetState(session.StateEnded)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSResume(client, WSMessage{
		Type:      "resume",
		SessionID: sess.ID,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "resume_failed" {
		t.Fatalf("expected resume_failed, got %s", msgs[0].Type)
	}
	if !strings.Contains(msgs[0].Reason, "cannot resume") {
		t.Fatalf("expected cannot resume reason, got %q", msgs[0].Reason)
	}
}

func TestHandleWSResume_SuccessWithoutSDP(t *testing.T) {
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSResume(client, WSMessage{
		Type:      "resume",
		SessionID: sess.ID,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "resumed" {
		t.Fatalf("expected resumed, got %s", msgs[0].Type)
	}
	if msgs[0].SessionID != sess.ID {
		t.Fatalf("expected resumed session %s, got %s", sess.ID, msgs[0].SessionID)
	}
	if client.sessionID != sess.ID {
		t.Fatalf("expected client sessionID to be updated to %s, got %s", sess.ID, client.sessionID)
	}
}

func TestHandleWSResume_WithInvalidSDPFailsRenegotiation(t *testing.T) {
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSResume(client, WSMessage{
		Type:      "resume",
		SessionID: sess.ID,
		SDP:       "invalid sdp",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "resume_failed" {
		t.Fatalf("expected resume_failed, got %s", msgs[0].Type)
	}
	if !strings.Contains(msgs[0].Reason, "Failed to renegotiate") {
		t.Fatalf("expected renegotiation failure reason, got %q", msgs[0].Reason)
	}
}
