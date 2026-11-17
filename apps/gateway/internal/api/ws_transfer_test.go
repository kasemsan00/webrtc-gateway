package api

import (
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

type transferSIPStub struct {
	stubSIPCallMaker
	calls chan [2]string // destination, target session ID
	err   error
}

func (s *transferSIPStub) TransferCall(sess *session.Session, destination string, target *session.Session) error {
	targetID := ""
	if target != nil {
		targetID = target.ID
	}
	s.calls <- [2]string{destination, targetID}
	return s.err
}

func transferTestServer(t *testing.T, stub *transferSIPStub) (*Server, *WSClient, *session.Session, *session.Session) {
	t.Helper()
	mgr := newTestSessionManager()
	a, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, stub, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}
	srv.bindClientSession(client, a.ID)
	return srv, client, a, b
}

func awaitTransferProgress(t *testing.T, client *WSClient) WSMessage {
	t.Helper()
	select {
	case raw := <-client.send:
		msgs := readWSMessages(t, singleMessageChan(raw))
		return msgs[0]
	case <-time.After(2 * time.Second):
		t.Fatal("no transfer_progress message")
	}
	return WSMessage{}
}

func singleMessageChan(raw []byte) chan []byte {
	ch := make(chan []byte, 1)
	ch <- raw
	return ch
}

func TestHandleWSTransfer_BlindAccepted(t *testing.T) {
	stub := &transferSIPStub{calls: make(chan [2]string, 1)}
	srv, client, a, _ := transferTestServer(t, stub)

	srv.handleWSTransfer(client, WSMessage{Type: "transfer", SessionID: a.ID, Destination: "1002"})

	if got := <-stub.calls; got != [2]string{"1002", ""} {
		t.Fatalf("TransferCall(%v)", got)
	}
	msg := awaitTransferProgress(t, client)
	if msg.Type != "transfer_progress" || msg.State != transferStateAccepted || msg.StatusCode != 202 {
		t.Fatalf("got %+v", msg)
	}
}

func TestHandleWSTransfer_AttendedNeedsBothCallsOwned(t *testing.T) {
	stub := &transferSIPStub{calls: make(chan [2]string, 1)}
	srv, client, a, b := transferTestServer(t, stub)

	srv.handleWSTransfer(client, WSMessage{Type: "transfer", SessionID: a.ID, TargetSessionID: b.ID})
	if msg := awaitTransferProgress(t, client); msg.State != transferStateFailed {
		t.Fatalf("a target call of another connection must be refused, got %+v", msg)
	}

	srv.bindClientSession(client, b.ID)
	srv.handleWSTransfer(client, WSMessage{Type: "transfer", SessionID: a.ID, TargetSessionID: b.ID})
	if got := <-stub.calls; got != [2]string{"", b.ID} {
		t.Fatalf("TransferCall(%v)", got)
	}
	if msg := awaitTransferProgress(t, client); msg.State != transferStateAccepted {
		t.Fatalf("got %+v", msg)
	}
}

func TestHandleWSTransfer_RejectsForeignCallAndBadArguments(t *testing.T) {
	stub := &transferSIPStub{calls: make(chan [2]string, 1)}
	srv, client, a, b := transferTestServer(t, stub)

	srv.handleWSTransfer(client, WSMessage{Type: "transfer", SessionID: b.ID, Destination: "1002"})
	if msg := awaitTransferProgress(t, client); msg.State != transferStateFailed {
		t.Fatalf("transferring another connection's call must fail, got %+v", msg)
	}
	srv.handleWSTransfer(client, WSMessage{Type: "transfer", SessionID: a.ID})
	if msg := awaitTransferProgress(t, client); msg.State != transferStateFailed {
		t.Fatalf("missing destination must fail, got %+v", msg)
	}
	select {
	case got := <-stub.calls:
		t.Fatalf("no REFER expected, got %v", got)
	default:
	}
}

func TestHandleWSTransfer_ReportsPBXRejection(t *testing.T) {
	stub := &transferSIPStub{calls: make(chan [2]string, 1), err: &sip.TransferRejectedError{StatusCode: 603, Reason: "Declined"}}
	srv, client, a, _ := transferTestServer(t, stub)

	srv.handleWSTransfer(client, WSMessage{Type: "transfer", SessionID: a.ID, Destination: "1002"})
	<-stub.calls
	if msg := awaitTransferProgress(t, client); msg.State != transferStateFailed || msg.StatusCode != 603 {
		t.Fatalf("got %+v", msg)
	}
}

func TestNotifyTransferProgress_MapsSipfragStatus(t *testing.T) {
	stub := &transferSIPStub{calls: make(chan [2]string, 1)}
	srv, client, a, _ := transferTestServer(t, stub)

	srv.NotifyTransferProgress(a.ID, 100, "Trying", false)
	srv.NotifyTransferProgress(a.ID, 200, "OK", true)
	srv.NotifyTransferProgress(a.ID, 486, "Busy Here", true)

	want := []string{transferStateTrying, transferStateCompleted, transferStateFailed}
	for _, state := range want {
		if msg := awaitTransferProgress(t, client); msg.State != state {
			t.Fatalf("want %s, got %+v", state, msg)
		}
	}
}
