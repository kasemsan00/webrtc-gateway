package sip

import (
	"testing"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

func byeTestSession(id string) *session.Session {
	sess := &session.Session{ID: id}
	sess.SetCallInfo("outbound", "1100", "1200", "call-"+id)
	sess.SetSIPAuthContext("trunk", "", 7, "127.0.0.1", "1100", "secret", 5060)
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:1200@127.0.0.1:5060>", "127.0.0.1", 5060, 1, nil)
	return sess
}

func TestCreateBYERequest_UsesUDPTrunkTransport(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
		trunkManager:  &TrunkManager{trunks: map[int64]*Trunk{7: {ID: 7, Domain: "127.0.0.1", Port: 5060, Transport: "udp"}}},
	}

	req, err := srv.createBYERequest(byeTestSession("udp"))
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	if got := req.Transport(); got != "UDP" {
		t.Fatalf("expected UDP transport, got %q", got)
	}
	if via := req.Via(); via == nil || via.Transport != "UDP" {
		t.Fatalf("expected UDP Via, got %#v", via)
	}
	if c := req.Contact(); c == nil || c.Address.UriParams.GetOr("transport", "") != "udp" {
		t.Fatalf("expected Contact transport=udp, got %#v", c)
	}
}

func TestCreateBYERequest_DefaultsToTCPWithoutTrunk(t *testing.T) {
	srv := &Server{config: config.SIPConfig{}, publicAddress: "10.10.10.10", sipPort: 5090}

	req, err := srv.createBYERequest(byeTestSession("tcp"))
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}
	if got := req.Transport(); got != "TCP" {
		t.Fatalf("expected TCP transport, got %q", got)
	}
}

func TestValidateSIPUserPart(t *testing.T) {
	for _, ok := range []string{"1002", "+66812345678", "*97#", "agent.1_a-b"} {
		if err := validateSIPUserPart(ok); err != nil {
			t.Fatalf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1002\r\nX-Injected: 1", "10 02", "a<b", "a\"b", "a@b", "a:b", "a\x00b"} {
		if err := validateSIPUserPart(bad); err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
	}
}

func TestCreateInviteRequest_RejectsHeaderInjection(t *testing.T) {
	srv := &Server{publicAddress: "10.10.10.10", sipPort: 5090}
	params := sipAuthParams{Domain: "127.0.0.1", Port: 5060, Username: "1100", Transport: "tcp"}

	if _, err := srv.createInviteRequestWithParams("1002\r\nX-Evil: 1", "1100", nil, params); err == nil {
		t.Fatal("expected CR/LF in destination to be rejected")
	}
	if _, err := srv.createInviteRequestWithParams("1002", "1100\r\nX-Evil: 1", nil, params); err == nil {
		t.Fatal("expected CR/LF in from to be rejected")
	}
	if _, err := srv.createInviteRequestWithParams("1002", "1100", nil, params); err != nil {
		t.Fatalf("plain destination should build: %v", err)
	}
}
