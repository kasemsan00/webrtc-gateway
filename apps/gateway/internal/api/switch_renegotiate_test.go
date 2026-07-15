package api

import (
	"testing"
	"time"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
)

type switchRenegotiateWSClient struct {
	send chan []byte
}

func TestStartSwitchVideoRenegotiationDisabledByConfig(t *testing.T) {
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)
	server := &Server{
		sessionMgr: mgr,
		runtimeConfig: &config.Config{
			SIP: config.SIPConfig{
				SwitchVideoRenegotiateEnable: false,
				MidCallRenegotiationEnable:   true,
			},
		},
	}

	server.StartSwitchVideoRenegotiation(sess.ID, 3)
	if sess.SwitchVideoRenegotiateGeneration != 0 {
		t.Fatalf("expected no claim when disabled, got generation=%d", sess.SwitchVideoRenegotiateGeneration)
	}
}

func TestNotifySwitchVideoRenegotiationSendsAgentSwitchOffer(t *testing.T) {
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)
	client := &WSClient{sessionID: sess.ID, send: make(chan []byte, 2)}
	server := &Server{
		sessionMgr: mgr,
		wsClients:  map[string]*WSClient{sess.ID: client},
	}

	server.NotifySwitchVideoRenegotiation(sess.ID, session.MidCallRenegotiationSnapshot{
		ID:       "reneg-switch-1",
		Source:   session.MidCallRenegotiationSourceSwitchGateRelease,
		OfferSDP: "v=0\r\noffer",
		Timeout:  time.Second,
	})

	msg := readWSTestMessage(t, client)
	if msg.Type != "renegotiate" {
		t.Fatalf("expected renegotiate, got %s", msg.Type)
	}
	if msg.RenegotiationID != "reneg-switch-1" || msg.Reason != "agent_switch" || msg.SDP != "v=0\r\noffer" || !msg.RequiresAnswer {
		t.Fatalf("unexpected renegotiate payload: %#v", msg)
	}
}

func TestHandleWSRenegotiateAnswerAppliesSwitchSourceBeforeComplete(t *testing.T) {
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)
	pending, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		ID:       "reneg-switch-apply",
		Source:   session.MidCallRenegotiationSourceSwitchGateRelease,
		Method:   "WS",
		OfferSDP: "offer-sdp",
	})
	if !ok {
		t.Fatal("expected pending renegotiation")
	}

	server := &Server{sessionMgr: mgr}
	client := &WSClient{sessionID: sess.ID, send: make(chan []byte, 2)}

	server.handleWSMessage(client, []byte(`{"type":"renegotiate_answer","sessionId":"`+sess.ID+`","renegotiationId":"`+pending.ID+`","sdp":"answer-sdp","status":"ok"}`))

	msg := readWSTestMessage(t, client)
	if msg.Type != "renegotiate_result" || msg.Status != "failed" {
		t.Fatalf("expected failed result without peer connection, got %#v", msg)
	}
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatal("expected pending renegotiation cleared after failed apply")
	}
}
