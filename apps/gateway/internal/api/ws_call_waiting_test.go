package api

import "testing"

// A /ws phone that opted into call waiting keeps being offered calls while
// it is on one; a phone that did not, or the public socket, is still busy.
func TestCallWaitingOptInOnMobileSocket(t *testing.T) {
	srv := &Server{wsConnections: map[*WSClient]struct{}{}, wsClients: map[string]*WSClient{}}

	mobile := &WSClient{}
	srv.handleWSClientState(mobile, WSMessage{Type: "client_state", Availability: "busy", CallState: "active", MultiCall: true, ActiveCalls: 1})
	if !mobile.multiCall || mobile.activeCalls != 1 {
		t.Fatalf("expected /ws client to opt into multi-call, got multiCall=%v activeCalls=%d", mobile.multiCall, mobile.activeCalls)
	}
	if !isClientAvailableForIncoming(mobile) {
		t.Fatal("a busy multi-call phone must still be offered a waiting call")
	}

	single := &WSClient{}
	srv.handleWSClientState(single, WSMessage{Type: "client_state", Availability: "busy", CallState: "active"})
	if isClientAvailableForIncoming(single) {
		t.Fatal("a busy single-call phone must be treated as busy")
	}

	public := &WSClient{publicOnly: true}
	srv.handleWSClientState(public, WSMessage{Type: "client_state", Availability: "busy", CallState: "active", MultiCall: true})
	if public.multiCall {
		t.Fatal("the unauthenticated public socket must not opt into multi-call")
	}

	unavailable := &WSClient{}
	srv.handleWSClientState(unavailable, WSMessage{Type: "client_state", Availability: "unavailable", MultiCall: true})
	if isClientAvailableForIncoming(unavailable) {
		t.Fatal("unavailable turns calls away even in multi-call mode")
	}
}
