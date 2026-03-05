package sip

import (
	"testing"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
)

func TestCreateBYERequest_InboundPrefersDialogDomain(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "s1"}
	sess.SetCallInfo("inbound", "sip:00025@203.150.245.42:5060", "sip:1100@203.151.21.121:5060", "call-1")
	sess.SetSIPAuthContext("trunk", "", 1, "203.151.21.121", "1100", "secret", 5060)
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:00025@203.150.245.42:5060>", "198.51.100.20", 5060, 1, nil)

	req, err := srv.createBYERequest(sess)
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	if got := req.Destination(); got != "198.51.100.20:5060" {
		t.Fatalf("expected destination from dialog domain, got %s", got)
	}
	to := req.To()
	if to == nil || to.Address.Host != "203.150.245.42" {
		t.Fatalf("expected To host to preserve remote host, got %#v", to)
	}
}

func TestCreateBYERequest_InboundFallsBackToAuthContext(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "s2"}
	sess.SetCallInfo("inbound", "sip:00025@203.150.245.42:5060", "1100", "call-2")
	sess.SetSIPAuthContext("trunk", "", 1, "203.151.21.121", "1100", "secret", 5060)
	sess.SetSIPDialogState("local-tag", "remote-tag", "", "", 0, 1, nil)

	req, err := srv.createBYERequest(sess)
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	if got := req.Destination(); got != "203.151.21.121:5060" {
		t.Fatalf("expected destination from auth context, got %s", got)
	}
}

func TestCreateBYERequest_InboundFallsBackToRemoteContact(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "s3"}
	sess.SetCallInfo("inbound", "sip:00025@203.150.245.42:5060", "1100", "call-3")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:00025@203.150.245.42:5060>", "", 0, 1, nil)

	req, err := srv.createBYERequest(sess)
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	if got := req.Destination(); got != "203.150.245.42:5060" {
		t.Fatalf("expected destination from remote contact, got %s", got)
	}
}

func TestCreateBYERequest_OutboundKeepsCSeqIncrement(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{Domain: "203.151.21.121", Port: 5060},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "s4"}
	sess.SetCallInfo("outbound", "sip:1100@203.151.21.121:5060", "sip:00025@203.150.245.42:5060", "call-4")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:00025@203.150.245.42:5060>", "203.151.21.121", 5060, 2, nil)

	req, err := srv.createBYERequest(sess)
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	cseq := req.CSeq()
	if cseq == nil || cseq.SeqNo != 3 {
		t.Fatalf("expected outbound BYE CSeq=3, got %#v", cseq)
	}
}
