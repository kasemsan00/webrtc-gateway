package api

import (
	"context"
	"testing"
	"time"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
)

func TestHandleWSCall_AckIsConnectingWhenDialing(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	// ICE may already be connected; call progress must still start as connecting.
	sess.SetState(session.StateConnecting)

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1002",
		From:        "callerA",
		SIPDomain:   "example.com",
		SIPUsername: "userA",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	waitForMakeCallCount(t, sipMaker, 1)
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 websocket message, got %d (%+v)", len(msgs), msgs)
	}
	if msgs[0].Type != "state" || msgs[0].State != string(session.StateConnecting) {
		t.Fatalf("expected connecting ack, got %+v", msgs[0])
	}
	if got := sess.GetState(); got != session.StateConnecting {
		t.Fatalf("session state=%s want connecting", got)
	}
}

func TestNotifySessionState_RingingEmitsStateAndRinging(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), sessionID: sess.ID}
	srv.mu.Lock()
	srv.wsClients[sess.ID] = client
	srv.mu.Unlock()

	srv.NotifySessionStateWithReason(sess.ID, session.StateRinging, "sip-180")

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 2 {
		t.Fatalf("expected state+ringing, got %d (%+v)", len(msgs), msgs)
	}
	if msgs[0].Type != "state" || msgs[0].State != "ringing" {
		t.Fatalf("expected state ringing, got %+v", msgs[0])
	}
	if msgs[1].Type != "ringing" || msgs[1].SessionID != sess.ID {
		t.Fatalf("expected type ringing, got %+v", msgs[1])
	}
}

func TestNotifySessionState_ActiveOnlyState(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), sessionID: sess.ID}
	srv.mu.Lock()
	srv.wsClients[sess.ID] = client
	srv.mu.Unlock()

	srv.NotifySessionStateWithReason(sess.ID, session.StateActive, "sip-200")

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "state" || msgs[0].State != "active" {
		t.Fatalf("expected single active state, got %+v", msgs)
	}
}

func TestNotifySessionState_IncludesReason(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), sessionID: sess.ID}
	srv.mu.Lock()
	srv.wsClients[sess.ID] = client
	srv.mu.Unlock()

	srv.NotifySessionStateWithReason(sess.ID, session.StateEnded, "ice_failed_pre_sip")

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Reason != "ice_failed_pre_sip" {
		t.Fatalf("expected terminal reason, got %+v", msgs)
	}
}

func TestRunWSCall_ICEFailureBeforeInviteEndsWithoutGenericError(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetTerminalReason("ice_failed")
	maker := &stubSIPCallMaker{makeCallErr: context.Canceled}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, maker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), sessionID: sess.ID}
	srv.mu.Lock()
	srv.wsClients[sess.ID] = client
	srv.mu.Unlock()

	srv.runWSCall(client, WSMessage{SessionID: sess.ID, Destination: "1002"}, sess, "", "")

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "state" || msgs[0].State != "ended" || msgs[0].Reason != "ice_failed_pre_sip" {
		t.Fatalf("expected reasoned terminal state without generic error, got %+v", msgs)
	}
}

func TestHandleWSCall_BindsClientBeforeImmediatePreSIPFailure(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetTerminalReason("ice_failed")
	maker := &stubSIPCallMaker{makeCallErr: context.Canceled}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, maker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1002",
		SIPDomain:   "example.com",
		SIPUsername: "userA",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	deadline := time.Now().Add(time.Second)
	var msgs []WSMessage
	for len(msgs) < 2 && time.Now().Before(deadline) {
		msgs = append(msgs, readWSMessages(t, client.send)...)
		if len(msgs) < 2 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if len(msgs) != 2 {
		t.Fatalf("expected connecting then ended, got %+v", msgs)
	}
	if msgs[0].State != "connecting" || msgs[1].State != "ended" || msgs[1].Reason != "ice_failed_pre_sip" {
		t.Fatalf("unexpected progress ordering: %+v", msgs)
	}
}

func TestOutboundCallAckState_NeverReportsActiveFromConnecting(t *testing.T) {
	if got := session.OutboundCallAckState(session.StateConnecting); got != session.StateConnecting {
		t.Fatalf("got %s", got)
	}
	if got := session.OutboundCallAckState(session.StateNew); got != session.StateConnecting {
		t.Fatalf("got %s", got)
	}
}
