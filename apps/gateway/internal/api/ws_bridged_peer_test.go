package api

import (
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

// A caller with two calls (a queue call answered by an agent, and a second line to
// another number) must not lose the agent's leg when the second line hangs up.
func TestHandleWSHangup_SecondLineKeepsOtherCallsAgent(t *testing.T) {
	mgr := newTestSessionManager()
	newSess := func(dir, from, to, sipUser string) *session.Session {
		sess, err := mgr.CreateSession(config.TURNConfig{})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		sess.SetCallInfo(dir, from, to, sess.ID+"-call")
		sess.SetSIPAuthContext("trunk", "", 1, "sipclient.ttrs.or.th", sipUser, "secret", 5060)
		sess.SetState(session.StateActive)
		sess.SetSIPDialogState(sess.ID+"-from", sess.ID+"-to", "<sip:x@example.com>", "example.com", 5060, 1, nil)
		return sess
	}
	queueCall := newSess("outbound", "sip:1429900148716@sipclient.ttrs.or.th", "sip:14131@sipclient.ttrs.or.th", "1429900148716")
	queueCall.SwitchTargetAgent = "00025"
	secondLine := newSess("outbound", "sip:1429900148716@sipclient.ttrs.or.th", "sip:9999@sipclient.ttrs.or.th", "1429900148716")
	agentLeg := newSess("inbound", "sip:1429900148716@sipclient.ttrs.or.th", "sip:00025@sipagent.ttrs.or.th", "00025")

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	caller := &WSClient{sessionID: secondLine.ID, send: make(chan []byte, 16), ConnectedAt: time.Now()}
	srv.mu.Lock()
	srv.wsClients[secondLine.ID] = caller
	srv.wsClients[queueCall.ID] = caller
	srv.wsConnections[caller] = struct{}{}
	srv.mu.Unlock()

	srv.handleWSHangup(caller, WSMessage{Type: "hangup", SessionID: secondLine.ID})

	if sipMaker.hangupCount != 1 {
		t.Fatalf("expected a BYE on the second line only, got %d", sipMaker.hangupCount)
	}
	if _, ok := mgr.GetSession(agentLeg.ID); !ok {
		t.Fatalf("the queue call's agent leg must survive the second line's hangup")
	}
	if _, ok := mgr.GetSession(queueCall.ID); !ok {
		t.Fatalf("the held queue call must survive the second line's hangup")
	}
	if peer := srv.findBridgedAgentSession(queueCall); peer == nil || peer.ID != agentLeg.ID {
		t.Fatalf("the @switch agent still pairs with its own call, got %v", peer)
	}
}

func TestFindBridgedGatewayPeer_AmbiguousCallerIsNotPaired(t *testing.T) {
	mgr := newTestSessionManager()
	mk := func(dir, to string) *session.Session {
		sess, _ := mgr.CreateSession(config.TURNConfig{})
		sess.SetCallInfo(dir, "sip:1001@pbx", to, sess.ID)
		sess.SetState(session.StateActive)
		return sess
	}
	first := mk("outbound", "sip:14131@pbx")
	agent := mk("inbound", "sip:00025@pbx")

	if got := session.FindBridgedGatewayPeer(mgr.ListSessions(), first); got == nil || got.ID != agent.ID {
		t.Fatalf("one call per caller still pairs with its agent, got %v", got)
	}
	if got := session.FindBridgedGatewayPeer(mgr.ListSessions(), agent); got == nil || got.ID != first.ID {
		t.Fatalf("the agent still pairs back with its caller, got %v", got)
	}

	second := mk("outbound", "sip:9999@pbx")
	if got := session.FindBridgedGatewayPeer(mgr.ListSessions(), second); got != nil {
		t.Fatalf("a second call from the same caller must not claim the agent, got %s", got.ID)
	}
	if got := session.FindBridgedGatewayPeer(mgr.ListSessions(), agent); got != nil {
		t.Fatalf("with two calls from the caller the agent's peer is ambiguous, got %s", got.ID)
	}
}
