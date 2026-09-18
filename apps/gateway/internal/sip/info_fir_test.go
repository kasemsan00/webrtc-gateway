package sip

import (
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

func TestCreateInDialogInfoRequestUsesRFC5168PictureFastUpdate(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "info-outbound"}
	sess.SetCallInfo(
		"outbound",
		"sip:agent-1001@sip.example.com",
		"sip:linphone-2002@sip.example.com",
		"dialog-call-id",
	)
	sess.SetSIPDialogState(
		"local-tag",
		"remote-tag",
		"<sip:device-contact@192.0.2.55:5070;transport=tcp>",
		"198.51.100.20",
		5060,
		7,
		[]string{"<sip:198.51.100.20:5060;lr>"},
	)

	req, err := srv.createInDialogInfoRequest(sess, pictureFastUpdateXML, pictureFastUpdateContentType)
	if err != nil {
		t.Fatalf("createInDialogInfoRequest: %v", err)
	}

	if req.Method != sip.INFO {
		t.Fatalf("expected INFO, got %s", req.Method)
	}
	if req.Recipient.User != "device-contact" || req.Recipient.Host != "192.0.2.55" || req.Recipient.Port != 5070 {
		t.Fatalf("request URI must use remote Contact, got %s", req.Recipient.String())
	}
	if got := req.Destination(); got != "198.51.100.20:5060" {
		t.Fatalf("network destination must use dialog next hop, got %s", got)
	}
	if cseq := req.CSeq(); cseq == nil || cseq.SeqNo != 8 || cseq.MethodName != sip.INFO {
		t.Fatalf("unexpected INFO CSeq: %#v", cseq)
	}
	contentType := req.GetHeaders("Content-Type")
	if len(contentType) != 1 || contentType[0].Value() != pictureFastUpdateContentType {
		t.Fatalf("unexpected Content-Type: %#v", contentType)
	}
	if body := string(req.Body()); !strings.Contains(body, "picture_fast_update") {
		t.Fatalf("expected RFC 5168 picture_fast_update body, got %q", body)
	}
	wire := req.String()
	if !strings.HasPrefix(wire, "INFO sip:device-contact@192.0.2.55:5070;transport=tcp SIP/2.0") {
		t.Fatalf("serialized request has wrong request line: %s", wire)
	}
	if !strings.Contains(wire, "CSeq: 8 INFO") || strings.Contains(wire, "CSeq: 8 BYE") {
		t.Fatalf("serialized request has wrong CSeq: %s", wire)
	}
}

func TestCreateInDialogInfoRequestRequiresCompleteDialog(t *testing.T) {
	srv := &Server{}
	if _, err := srv.createInDialogInfoRequest(nil, pictureFastUpdateXML, pictureFastUpdateContentType); err == nil {
		t.Fatal("expected nil session error")
	}

	sess := &session.Session{ID: "info-invalid"}
	if _, err := srv.createInDialogInfoRequest(sess, pictureFastUpdateXML, pictureFastUpdateContentType); err == nil {
		t.Fatal("expected missing dialog state error")
	}
	sess.SetSIPDialogState("local-tag", "remote-tag", "", "198.51.100.20", 5060, 1, nil)
	if _, err := srv.createInDialogInfoRequest(sess, pictureFastUpdateXML, pictureFastUpdateContentType); err == nil || !strings.Contains(err.Error(), "remote contact") {
		t.Fatalf("expected missing remote Contact error, got %v", err)
	}
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:2002@192.0.2.55>", "198.51.100.20", 5060, 1, nil)
	if _, err := srv.createInDialogInfoRequest(sess, pictureFastUpdateXML, pictureFastUpdateContentType); err == nil || !strings.Contains(err.Error(), "Call-ID") {
		t.Fatalf("expected missing Call-ID error, got %v", err)
	}
}

func TestHandleINFOPictureFastUpdateRespondsOK(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-info-fir")
	req := sip.NewRequest(sip.INFO, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", "call-info-fir"))
	req.AppendHeader(sip.NewHeader("Content-Type", pictureFastUpdateContentType))
	req.SetBody([]byte(pictureFastUpdateXML))
	tx := &midCallServerTxStub{}

	srv.handleINFO(req, tx)

	assertResponseCodes(t, tx, []int{200})
	if sess.GetState() != session.StateActive {
		t.Fatalf("expected INFO not to change call state, got %s", sess.GetState())
	}
}

func TestHandleINFOKeepAliveRespondsOKWithoutMediaAction(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-info-keepalive")
	req := sip.NewRequest(sip.INFO, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", "call-info-keepalive"))
	tx := &midCallServerTxStub{}

	srv.handleINFO(req, tx)

	assertResponseCodes(t, tx, []int{200})
	if sess.GetState() != session.StateActive {
		t.Fatalf("expected keep-alive INFO not to change call state, got %s", sess.GetState())
	}
}

func TestIsPictureFastUpdateBody(t *testing.T) {
	if !isPictureFastUpdateBody(pictureFastUpdateXML) {
		t.Fatal("expected RFC 5168 XML to match")
	}
	if isPictureFastUpdateBody("") || isPictureFastUpdateBody("dtmf-relay") {
		t.Fatal("expected non-FIR INFO bodies to be ignored")
	}
}
