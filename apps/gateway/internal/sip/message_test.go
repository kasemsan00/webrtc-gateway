package sip

import (
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

func TestCreateInDialogMessageRequestPreservesOutboundDialogIdentityAndNextHop(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "message-outbound"}
	sess.SetCallInfo(
		"outbound",
		"sip:agent-1001@sip.example.com",
		"sip:linphone-2002@sip.example.com",
		"dialog-call-id",
	)
	sess.SetSIPAuthContext("trunk", "", 42, "", "", "", 0)
	sess.SetSIPDialogState(
		"local-tag",
		"remote-tag",
		"<sip:device-contact@192.0.2.55:5070;transport=tcp>",
		"198.51.100.20",
		5060,
		7,
		[]string{"<sip:198.51.100.20:5060;lr>"},
	)
	sess.SetHeld(true)

	req, err := srv.createInDialogMessageRequest(sess, "hello linphone", "text/plain;charset=UTF-8")
	if err != nil {
		t.Fatalf("createInDialogMessageRequest: %v", err)
	}

	if req.Method != sip.MESSAGE {
		t.Fatalf("expected MESSAGE, got %s", req.Method)
	}
	if req.Recipient.User != "device-contact" || req.Recipient.Host != "192.0.2.55" || req.Recipient.Port != 5070 {
		t.Fatalf("request URI must use remote Contact, got %s", req.Recipient.String())
	}
	if got := req.Destination(); got != "198.51.100.20:5060" {
		t.Fatalf("network destination must use dialog next hop, got %s", got)
	}
	assertDialogMessageFrom(t, req.From(), "agent-1001", "sip.example.com", "local-tag")
	assertDialogMessageTo(t, req.To(), "linphone-2002", "sip.example.com", "remote-tag")
	if callID := req.CallID(); callID == nil || callID.Value() != "dialog-call-id" {
		t.Fatalf("unexpected Call-ID: %#v", callID)
	}
	if cseq := req.CSeq(); cseq == nil || cseq.SeqNo != 8 || cseq.MethodName != sip.MESSAGE {
		t.Fatalf("unexpected MESSAGE CSeq: %#v", cseq)
	}
	if got := len(req.GetHeaders("Route")); got != 1 {
		t.Fatalf("expected dialog Route header, got %d", got)
	}
	if contact := req.Contact(); contact == nil || contact.Address.User != "agent-1001" {
		t.Fatalf("Contact must use local dialog identity, got %#v", contact)
	}
	if body := string(req.Body()); body != "hello linphone" {
		t.Fatalf("unexpected body %q", body)
	}
	wire := req.String()
	if !strings.HasPrefix(wire, "MESSAGE sip:device-contact@192.0.2.55:5070;transport=tcp SIP/2.0") {
		t.Fatalf("serialized request has wrong request line: %s", wire)
	}
	if !strings.Contains(wire, "CSeq: 8 MESSAGE") || strings.Contains(wire, "CSeq: 8 BYE") {
		t.Fatalf("serialized request has wrong CSeq: %s", wire)
	}
}

func TestCreateInDialogMessageRequestSwapsInboundDialogParties(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "message-inbound"}
	sess.SetCallInfo(
		"inbound",
		"sip:linphone-2002@sip.example.com",
		"sip:agent-1001@sip.example.com",
		"incoming-dialog-call-id",
	)
	sess.SetSIPDialogState(
		"local-tag",
		"remote-tag",
		"<sip:linphone-device@192.0.2.60:5060>",
		"198.51.100.21",
		5060,
		3,
		nil,
	)

	req, err := srv.createInDialogMessageRequest(sess, "inbound dialog", "")
	if err != nil {
		t.Fatalf("createInDialogMessageRequest: %v", err)
	}

	assertDialogMessageFrom(t, req.From(), "agent-1001", "sip.example.com", "local-tag")
	assertDialogMessageTo(t, req.To(), "linphone-2002", "sip.example.com", "remote-tag")
	if got := req.Destination(); got != "198.51.100.21:5060" {
		t.Fatalf("network destination must use inbound dialog next hop, got %s", got)
	}
	contentType := req.GetHeaders("Content-Type")
	if len(contentType) != 1 || contentType[0].Value() != "text/plain;charset=UTF-8" {
		t.Fatalf("unexpected default Content-Type: %#v", contentType)
	}
}

func TestCreateInDialogMessageRequestRequiresCompleteDialog(t *testing.T) {
	srv := &Server{}
	if _, err := srv.createInDialogMessageRequest(nil, "hello", "text/plain"); err == nil {
		t.Fatal("expected nil session error")
	}

	sess := &session.Session{ID: "message-invalid"}
	if _, err := srv.createInDialogMessageRequest(sess, "hello", "text/plain"); err == nil {
		t.Fatal("expected missing dialog state error")
	}
	sess.SetSIPDialogState("local-tag", "remote-tag", "", "198.51.100.20", 5060, 1, nil)
	if _, err := srv.createInDialogMessageRequest(sess, "hello", "text/plain"); err == nil || !strings.Contains(err.Error(), "remote contact") {
		t.Fatalf("expected missing remote Contact error, got %v", err)
	}
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:2002@192.0.2.55>", "198.51.100.20", 5060, 1, nil)
	if _, err := srv.createInDialogMessageRequest(sess, "hello", "text/plain"); err == nil || !strings.Contains(err.Error(), "Call-ID") {
		t.Fatalf("expected missing Call-ID error, got %v", err)
	}
}

func TestSessionMessagePartiesUseSelectedAgentInsteadOfDynamicQueue(t *testing.T) {
	sess := &session.Session{ID: "queue-call"}
	sess.SetCallInfo(
		"inbound",
		`"Android Reviewer" <sip:1429900148716@203.150.245.41>`,
		"sip:00025@203.151.21.121:5090",
		"queue-call-id",
	)

	destination, from, err := sessionMessageParties(sess)
	if err != nil {
		t.Fatalf("sessionMessageParties: %v", err)
	}
	if destination != "sip:1429900148716@203.150.245.41" || from != "00025" {
		t.Fatalf("MESSAGE parties = %s <- %s, want full caller URI sip:1429900148716@203.150.245.41 <- agent 00025", destination, from)
	}
}

func TestResolveMessageRecipientTreatsNumericDestinationAsUser(t *testing.T) {
	for _, destination := range []string{"1429900148716", "sip:1429900148716"} {
		recipient, err := resolveMessageRecipient(destination, "sipagent.ttrs.or.th", 5060)
		if err != nil {
			t.Fatalf("resolveMessageRecipient(%q): %v", destination, err)
		}
		if recipient.User != "1429900148716" || recipient.Host != "sipagent.ttrs.or.th" || recipient.Port != 5060 {
			t.Fatalf("numeric recipient %q parsed as %+v, want user at PBX domain", destination, recipient)
		}
	}
}

func TestResolveMessageRecipientPreservesFullURI(t *testing.T) {
	recipient, err := resolveMessageRecipient("sip:2002@192.0.2.20:5070;transport=tcp", "sipagent.ttrs.or.th", 5060)
	if err != nil {
		t.Fatalf("resolveMessageRecipient: %v", err)
	}
	if recipient.User != "2002" || recipient.Host != "192.0.2.20" || recipient.Port != 5070 {
		t.Fatalf("full URI changed unexpectedly: %+v", recipient)
	}
}

func TestSessionMessagePartiesSupportOutboundCalls(t *testing.T) {
	sess := &session.Session{ID: "outbound-call"}
	sess.SetCallInfo("outbound", "sip:00025@example.com", "sip:2002@example.com", "outbound-call-id")

	destination, from, err := sessionMessageParties(sess)
	if err != nil {
		t.Fatalf("sessionMessageParties: %v", err)
	}
	if destination != "sip:2002@example.com" || from != "00025" {
		t.Fatalf("MESSAGE parties = %s <- %s, want full remote URI sip:2002@example.com <- agent 00025", destination, from)
	}
}

func TestResolveMessageRecipientPreservesDisplayNameURI(t *testing.T) {
	recipient, err := resolveMessageRecipient(
		`"Android Reviewer" <sip:1429900148716@caller.example:5070;transport=tcp>`,
		"proxy.example",
		5060,
	)
	if err != nil {
		t.Fatalf("resolveMessageRecipient: %v", err)
	}
	if recipient.User != "1429900148716" || recipient.Host != "caller.example" || recipient.Port != 5070 {
		t.Fatalf("remote call URI changed unexpectedly: %+v", recipient)
	}
}

func TestResolveMessageNextHopUsesProxyIndependentlyOfRemoteURI(t *testing.T) {
	recipient, err := resolveMessageRecipient("sip:1429900148716@caller.example:5070", "203.150.245.41", 5060)
	if err != nil {
		t.Fatalf("resolveMessageRecipient: %v", err)
	}
	nextHop, err := resolveMessageNextHop("203.150.245.41")
	if err != nil {
		t.Fatalf("resolveMessageNextHop: %v", err)
	}
	if recipient.Host != "caller.example" || recipient.Port != 5070 {
		t.Fatalf("Request-URI must preserve active call peer, got %s", recipient.String())
	}
	if nextHop != "203.150.245.41" {
		t.Fatalf("network next hop must use Asterisk proxy, got %s", nextHop)
	}
}

func assertDialogMessageFrom(
	t *testing.T,
	header *sip.FromHeader,
	wantUser string,
	wantHost string,
	wantTag string,
) {
	t.Helper()
	if header == nil || header.Address.User != wantUser || header.Address.Host != wantHost {
		t.Fatalf("From must preserve dialog identity %s@%s, got %#v", wantUser, wantHost, header)
	}
	if tag, ok := header.Params.Get("tag"); !ok || tag != wantTag {
		t.Fatalf("From must preserve dialog tag %q, got %#v", wantTag, header.Params)
	}
}

func assertDialogMessageTo(
	t *testing.T,
	header *sip.ToHeader,
	wantUser string,
	wantHost string,
	wantTag string,
) {
	t.Helper()
	if header == nil || header.Address.User != wantUser || header.Address.Host != wantHost {
		t.Fatalf("To must preserve dialog identity %s@%s, got %#v", wantUser, wantHost, header)
	}
	if tag, ok := header.Params.Get("tag"); !ok || tag != wantTag {
		t.Fatalf("To must preserve dialog tag %q, got %#v", wantTag, header.Params)
	}
}
