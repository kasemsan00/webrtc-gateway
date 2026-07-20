package api

import (
	"testing"

	"k2-gateway/internal/config"
)

func TestNotifyRemoteMedia_EmitsOnceShape(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), sessionID: sess.ID}
	srv.mu.Lock()
	srv.wsClients[sess.ID] = client
	srv.mu.Unlock()

	srv.NotifyRemoteMedia(sess.ID, "video", "remote", "receiving")

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 media message, got %d (%+v)", len(msgs), msgs)
	}
	if msgs[0].Type != "media" || msgs[0].Kind != "video" || msgs[0].Direction != "remote" || msgs[0].State != "receiving" {
		t.Fatalf("unexpected media payload: %+v", msgs[0])
	}
	if msgs[0].SessionID != sess.ID {
		t.Fatalf("sessionId=%s", msgs[0].SessionID)
	}
}

func TestNotifyRemoteMedia_NoClientIsNonFatal(t *testing.T) {
	mgr := newTestSessionManager()
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	// Must not panic when no WS client is bound.
	srv.NotifyRemoteMedia("missing-session", "video", "remote", "receiving")
}

func TestSessionRemoteVideoReadyDedupeWithNotify(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), sessionID: sess.ID}
	srv.mu.Lock()
	srv.wsClients[sess.ID] = client
	srv.mu.Unlock()

	if !sess.TryMarkRemoteVideoReady(true) {
		t.Fatal("first mark failed")
	}
	srv.NotifyRemoteMedia(sess.ID, "video", "remote", "receiving")
	if sess.TryMarkRemoteVideoReady(true) {
		t.Fatal("second mark should fail")
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "media" {
		t.Fatalf("expected single media emit, got %+v", msgs)
	}
}
