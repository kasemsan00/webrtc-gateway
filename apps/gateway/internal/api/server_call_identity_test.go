package api

import (
	"strings"
	"testing"
	"time"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
)

type stubSIPCallMaker struct {
	makeCallCount int
	lastDest      string
	lastFrom      string
	lastSessionID string
}

func (s *stubSIPCallMaker) MakeCall(destination, from string, sess *session.Session) error {
	s.makeCallCount++
	s.lastDest = destination
	s.lastFrom = from
	if sess != nil {
		s.lastSessionID = sess.ID
	}
	return nil
}

func (s *stubSIPCallMaker) Hangup(sess *session.Session) error                    { return nil }
func (s *stubSIPCallMaker) SendDTMF(sess *session.Session, digits string) error   { return nil }
func (s *stubSIPCallMaker) AcceptCall(sess *session.Session) error                { return nil }
func (s *stubSIPCallMaker) RejectCall(sess *session.Session, reason string) error { return nil }
func (s *stubSIPCallMaker) SendMessage(destination, from, body, contentType string) error {
	return nil
}
func (s *stubSIPCallMaker) SendMessageToSession(sess *session.Session, body, contentType string) error {
	return nil
}
func (s *stubSIPCallMaker) TriggerSwitchMessage(body, callerURI string) error {
	return nil
}

func TestHandleWSCallRejectsPublicIdentityChange(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("public", "userA@example.com", 0, "example.com", "userA", "old-secret", 5060)

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1001",
		From:        "callerA",
		SIPDomain:   "example.com",
		SIPUsername: "userB",
		SIPPassword: "new-secret",
		SIPPort:     5060,
	})

	if sipMaker.makeCallCount != 0 {
		t.Fatalf("expected MakeCall not to be called, got %d", sipMaker.makeCallCount)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 websocket message, got %d", len(msgs))
	}
	if msgs[0].Type != "error" {
		t.Fatalf("expected error message, got %s", msgs[0].Type)
	}
	if !strings.Contains(msgs[0].Error, "Public SIP identity changed") {
		t.Fatalf("unexpected error message: %q", msgs[0].Error)
	}
}

func TestHandleWSCallAllowsSamePublicIdentity(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("public", "userA@example.com", 0, "example.com", "userA", "old-secret", 5060)

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1002",
		From:        "callerA",
		SIPDomain:   "example.com",
		SIPUsername: "userA",
		SIPPassword: "new-secret",
		SIPPort:     5060,
	})

	if sipMaker.makeCallCount != 1 {
		t.Fatalf("expected MakeCall to be called once, got %d", sipMaker.makeCallCount)
	}
	if sipMaker.lastSessionID != sess.ID {
		t.Fatalf("expected MakeCall session %s, got %s", sess.ID, sipMaker.lastSessionID)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 websocket message, got %d", len(msgs))
	}
	if msgs[0].Type != "state" {
		t.Fatalf("expected state message, got %s", msgs[0].Type)
	}
}

func TestHandleWSCall_AllowsIdentityChangeForNonPublicMode(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("trunk", "", 1, "example.com", "userA", "old-secret", 5060)

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1003",
		From:        "callerA",
		SIPDomain:   "example.com",
		SIPUsername: "userB",
		SIPPassword: "new-secret",
		SIPPort:     5060,
	})

	if sipMaker.makeCallCount != 1 {
		t.Fatalf("expected MakeCall to be called once, got %d", sipMaker.makeCallCount)
	}
}

func TestHandleWSCallRejectsTrunkCallWhenNotResolved(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a", LeaseOwner: &owner, LeaseUntil: &future},
		},
		byPublicID: map[string]int64{
			"8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a": 42,
		},
	}

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, mgr, sipMaker, nil, trunkMgr, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1004",
		TrunkID:     42,
	})

	if sipMaker.makeCallCount != 0 {
		t.Fatalf("expected MakeCall not to be called, got %d", sipMaker.makeCallCount)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 websocket message, got %d", len(msgs))
	}
	if msgs[0].Type != "error" {
		t.Fatalf("expected error message, got %s", msgs[0].Type)
	}
	if !strings.Contains(msgs[0].Error, "Trunk must be resolved before placing call") {
		t.Fatalf("unexpected error message: %q", msgs[0].Error)
	}
}

func TestHandleWSCallRejectsTrunkCallWhenLeaseNotActive(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	owner := "gw-1"
	past := time.Now().Add(-1 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a", LeaseOwner: &owner, LeaseUntil: &past},
		},
		byPublicID: map[string]int64{
			"8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a": 42,
		},
	}

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, mgr, sipMaker, nil, trunkMgr, nil)
	client := &WSClient{
		send:            make(chan []byte, 8),
		trunkResolved:   true,
		resolvedTrunkID: 42,
	}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1005",
		TrunkID:     42,
	})

	if sipMaker.makeCallCount != 0 {
		t.Fatalf("expected MakeCall not to be called, got %d", sipMaker.makeCallCount)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 websocket messages, got %d", len(msgs))
	}
	if msgs[0].Type != "trunk_not_ready" {
		t.Fatalf("expected first message trunk_not_ready, got %s", msgs[0].Type)
	}
	if msgs[0].Reason != "Trunk lease not active" {
		t.Fatalf("unexpected trunk_not_ready reason: %q", msgs[0].Reason)
	}
	if msgs[1].Type != "error" {
		t.Fatalf("expected second message error, got %s", msgs[1].Type)
	}
	if !strings.Contains(msgs[1].Error, "Trunk not ready: Trunk lease not active") {
		t.Fatalf("unexpected error message: %q", msgs[1].Error)
	}
	if client.trunkResolved {
		t.Fatalf("expected trunkResolved to be reset to false")
	}
	if client.resolvedTrunkID != 0 {
		t.Fatalf("expected resolvedTrunkID reset to 0, got %d", client.resolvedTrunkID)
	}
}
