package sip

import (
	"net/url"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/session"
)

func transferTestSession(id, direction, from, to, callID string) *session.Session {
	sess := &session.Session{ID: id, State: session.StateActive}
	sess.SetCallInfo(direction, from, to, callID)
	sess.SetSIPAuthContext("public", "acct", 0, "pbx.example.com", "1100", "secret", 5060)
	sess.SetSIPDialogState("local-"+id, "remote-"+id, "<sip:x@127.0.0.1>", "pbx.example.com", 5060, 1, nil)
	return sess
}

func TestBuildReferTo_Blind(t *testing.T) {
	srv := &Server{}
	sess := transferTestSession("a", "outbound", "1100", "14131", "call-a")

	got, err := srv.buildReferTo(sess, "1002", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "<sip:1002@pbx.example.com>" {
		t.Fatalf("Refer-To = %q", got)
	}
	if _, err := srv.buildReferTo(sess, "10 02\r\nX: y", nil); err == nil {
		t.Fatal("expected an invalid destination to be rejected")
	}
}

// Attended transfer: Replaces names the target dialog as the PBX sees it,
// to-tag = the PBX's tag (our remote tag), from-tag = ours.
func TestBuildReferTo_AttendedUsesTargetDialog(t *testing.T) {
	srv := &Server{}
	sess := transferTestSession("a", "outbound", "1100", "14131", "call-a")
	target := transferTestSession("b", "inbound", "sip:2001@pbx.example.com", "1100", "call-b@10.0.0.1")

	got, err := srv.buildReferTo(sess, "", target)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "<sip:2001@pbx.example.com?Replaces="
	if !strings.HasPrefix(got, prefix) || !strings.HasSuffix(got, ">") {
		t.Fatalf("Refer-To = %q", got)
	}
	replaces, err := url.QueryUnescape(strings.TrimSuffix(strings.TrimPrefix(got, prefix), ">"))
	if err != nil {
		t.Fatal(err)
	}
	if replaces != "call-b@10.0.0.1;to-tag=remote-b;from-tag=local-b" {
		t.Fatalf("Replaces = %q", replaces)
	}
	if _, err := srv.buildReferTo(sess, "", sess); err == nil {
		t.Fatal("transferring a call to itself must fail")
	}
}

func TestParseSipfragStatus(t *testing.T) {
	cases := map[string]int{
		"SIP/2.0 100 Trying\r\n":        100,
		"SIP/2.0 200 OK":                200,
		"SIP/2.0 486 Busy Here\r\nX: y": 486,
	}
	for body, want := range cases {
		code, _, ok := parseSipfragStatus([]byte(body))
		if !ok || code != want {
			t.Fatalf("%q: got %d ok=%v, want %d", body, code, ok, want)
		}
	}
	if _, _, ok := parseSipfragStatus([]byte("garbage")); ok {
		t.Fatal("expected garbage to be rejected")
	}
}

type transferNotifierStub struct {
	midCallRenegotiationNotifierStub
	codes  []int
	finals []bool
}

func (n *transferNotifierStub) NotifyTransferProgress(_ string, code int, _ string, final bool) {
	n.codes = append(n.codes, code)
	n.finals = append(n.finals, final)
}

func TestHandleNOTIFY_ReportsTransferProgress(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-transfer")
	notifier := &transferNotifierStub{}
	srv.midCallNotifier = notifier

	for _, body := range []string{"SIP/2.0 100 Trying", "SIP/2.0 200 OK"} {
		req := sip.NewRequest(sip.NOTIFY, sip.Uri{User: "1100", Host: "gw"})
		req.AppendHeader(sip.NewHeader("Call-ID", "call-transfer"))
		req.AppendHeader(sip.NewHeader("Event", "refer;id=7"))
		req.AppendHeader(sip.NewHeader("Content-Type", "message/sipfrag"))
		req.SetBody([]byte(body))
		tx := &midCallServerTxStub{}
		srv.handleNOTIFY(req, tx)
		assertResponseCodes(t, tx, []int{200})
	}
	_ = sess

	if len(notifier.codes) != 2 || notifier.codes[0] != 100 || notifier.codes[1] != 200 {
		t.Fatalf("progress codes = %v", notifier.codes)
	}
	if notifier.finals[0] || !notifier.finals[1] {
		t.Fatalf("finals = %v", notifier.finals)
	}
}

func TestHandleNOTIFY_UnknownDialogGets481(t *testing.T) {
	srv, _ := newMidCallTestServer(t, "call-known")
	req := sip.NewRequest(sip.NOTIFY, sip.Uri{User: "1100", Host: "gw"})
	req.AppendHeader(sip.NewHeader("Call-ID", "call-unknown"))
	tx := &midCallServerTxStub{}

	srv.handleNOTIFY(req, tx)

	assertResponseCodes(t, tx, []int{481})
}
