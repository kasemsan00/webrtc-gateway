package api

import (
	"testing"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
)

type incomingTestSIPCallMaker struct {
	acceptCount int
	rejectCount int
	lastReject  string
}

func (s *incomingTestSIPCallMaker) MakeCall(destination, from string, sess *session.Session) error {
	return nil
}
func (s *incomingTestSIPCallMaker) Hangup(sess *session.Session) error                  { return nil }
func (s *incomingTestSIPCallMaker) SendDTMF(sess *session.Session, digits string) error { return nil }
func (s *incomingTestSIPCallMaker) AcceptCall(sess *session.Session) error {
	s.acceptCount++
	return nil
}
func (s *incomingTestSIPCallMaker) RejectCall(sess *session.Session, reason string) error {
	s.rejectCount++
	s.lastReject = reason
	return nil
}
func (s *incomingTestSIPCallMaker) SendMessage(destination, from, body, contentType string) error {
	return nil
}
func (s *incomingTestSIPCallMaker) SendMessageToSession(sess *session.Session, body, contentType string) error {
	return nil
}

func TestHandleWSAccept_FirstAcceptWins(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-1")

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, sipMaker, nil, nil, nil)

	client1 := &WSClient{send: make(chan []byte, 8)}
	client2 := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSAccept(client1, WSMessage{
		Type:      "accept",
		SessionID: incomingSess.ID,
	})

	msgs1 := readWSMessages(t, client1.send)
	if len(msgs1) != 1 {
		t.Fatalf("expected 1 message for first accept, got %d", len(msgs1))
	}
	if msgs1[0].Type != "state" || msgs1[0].State != "active" {
		t.Fatalf("expected active state for first accept, got type=%s state=%s", msgs1[0].Type, msgs1[0].State)
	}

	srv.handleWSAccept(client2, WSMessage{
		Type:      "accept",
		SessionID: incomingSess.ID,
	})

	msgs2 := readWSMessages(t, client2.send)
	if len(msgs2) != 1 {
		t.Fatalf("expected 1 message for second accept, got %d", len(msgs2))
	}
	if msgs2[0].Type != "error" {
		t.Fatalf("expected error for second accept, got %s", msgs2[0].Type)
	}
	if msgs2[0].Error != "Call already accepted by another client" {
		t.Fatalf("unexpected second accept error: %q", msgs2[0].Error)
	}

	if sipMaker.acceptCount != 1 {
		t.Fatalf("expected AcceptCall once, got %d", sipMaker.acceptCount)
	}
}

func TestHandleWSReject_DefaultReasonAndDeletesSession(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-2")

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSReject(client, WSMessage{
		Type:      "reject",
		SessionID: incomingSess.ID,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for reject, got %d", len(msgs))
	}
	if msgs[0].Type != "state" {
		t.Fatalf("expected state message, got %s", msgs[0].Type)
	}
	if msgs[0].State != string(session.StateEnded) {
		t.Fatalf("expected ended state, got %s", msgs[0].State)
	}
	if sipMaker.rejectCount != 1 {
		t.Fatalf("expected RejectCall once, got %d", sipMaker.rejectCount)
	}
	if sipMaker.lastReject != "busy" {
		t.Fatalf("expected default reject reason busy, got %q", sipMaker.lastReject)
	}
	if _, ok := mgr.GetSession(incomingSess.ID); ok {
		t.Fatalf("expected session %s to be deleted after reject", incomingSess.ID)
	}
}

func TestNotifyIncomingCall_BroadcastPayload(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, nil, nil, nil, nil, nil)
	clientA := &WSClient{sessionID: "a", send: make(chan []byte, 8)}
	clientB := &WSClient{sessionID: "b", send: make(chan []byte, 8)}

	srv.mu.Lock()
	srv.wsClients["a"] = clientA
	srv.wsClients["b"] = clientB
	srv.mu.Unlock()

	srv.NotifyIncomingCall("incoming-123", "sip:alice@example.com", "sip:bob@example.com")

	msgsA := readWSMessages(t, clientA.send)
	msgsB := readWSMessages(t, clientB.send)
	if len(msgsA) != 1 || len(msgsB) != 1 {
		t.Fatalf("expected one incoming message per client, got A=%d B=%d", len(msgsA), len(msgsB))
	}

	for i, msg := range []WSMessage{msgsA[0], msgsB[0]} {
		if msg.Type != "incoming" {
			t.Fatalf("client msg %d expected incoming, got %s", i, msg.Type)
		}
		if msg.SessionID != "incoming-123" {
			t.Fatalf("client msg %d expected session incoming-123, got %s", i, msg.SessionID)
		}
		if msg.From != "sip:alice@example.com" || msg.To != "sip:bob@example.com" {
			t.Fatalf("client msg %d unexpected from/to: from=%s to=%s", i, msg.From, msg.To)
		}
	}
}

