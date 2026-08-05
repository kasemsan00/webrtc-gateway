package sip

import (
	"strconv"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
)

func TestCreateHoldRequestBuildsInDialogInviteWithMonotonicCSeq(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{Domain: "203.0.113.20", Port: 5060},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "hold-1"}
	sess.SetCallInfo("outbound", "sip:1001@203.0.113.20:5060", "sip:2002@203.0.113.20:5060", "call-hold-1")
	sess.SetSIPDialogState(
		"local-tag",
		"remote-tag",
		"<sip:2002@203.0.113.20:5060>",
		"203.0.113.20",
		5060,
		7,
		nil,
	)
	sess.SetRTPConnection(nil, 4000)

	hold, err := srv.createHoldRequest(sess, true)
	if err != nil {
		t.Fatalf("createHoldRequest hold: %v", err)
	}
	assertHoldInvite(t, hold, 8, "inactive")

	resume, err := srv.createHoldRequest(sess, false)
	if err != nil {
		t.Fatalf("createHoldRequest resume: %v", err)
	}
	assertHoldInvite(t, resume, 9, "sendrecv")
}

func TestCreateHoldRequestRequiresReadyAudioEndpoint(t *testing.T) {
	srv := &Server{}
	_, err := srv.createHoldRequest(&session.Session{ID: "hold-no-media"}, true)
	if err == nil || !strings.Contains(err.Error(), "audio RTP endpoint") {
		t.Fatalf("expected audio endpoint error, got %v", err)
	}
}

func assertHoldInvite(t *testing.T, req *sip.Request, wantCSeq uint32, wantDirection string) {
	t.Helper()
	if req.Method != sip.INVITE {
		t.Fatalf("expected INVITE, got %s", req.Method)
	}
	if cseq := req.CSeq(); cseq == nil || cseq.SeqNo != wantCSeq || cseq.MethodName != sip.INVITE {
		t.Fatalf("unexpected CSeq: %#v", cseq)
	}
	wire := req.String()
	if !strings.HasPrefix(wire, "INVITE ") {
		t.Fatalf("serialized request must start with INVITE: %s", wire)
	}
	wantWireCSeq := "CSeq: " + strconv.FormatUint(uint64(wantCSeq), 10) + " INVITE"
	if !strings.Contains(wire, wantWireCSeq) {
		t.Fatalf("serialized request missing %q: %s", wantWireCSeq, wire)
	}
	if strings.Contains(wire, "CSeq: "+strconv.FormatUint(uint64(wantCSeq), 10)+" BYE") {
		t.Fatalf("serialized hold request retained BYE CSeq: %s", wire)
	}
	if got := len(req.GetHeaders("Content-Type")); got != 1 {
		t.Fatalf("expected one Content-Type header, got %d", got)
	}
	body := string(req.Body())
	if !strings.Contains(body, "a="+wantDirection) {
		t.Fatalf("expected a=%s in SDP: %s", wantDirection, body)
	}
	if wantDirection == "inactive" && strings.Contains(body, "a=sendrecv") {
		t.Fatalf("held SDP must not retain sendrecv: %s", body)
	}
}
