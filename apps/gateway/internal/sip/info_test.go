package sip

import (
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

type infoDTMFStub struct {
	sessionID string
	digit     string
	calls     int
}

func (s *infoDTMFStub) NotifyDTMF(sessionID, digit string) {
	s.calls++
	s.sessionID = sessionID
	s.digit = digit
}

func TestFormatSIPInfoReceivedIsOneLineWithOrigin(t *testing.T) {
	body := strings.Repeat("A", 40) + "\r\n<picture_fast_update/>\r\n" + strings.Repeat("B", 250)
	line := formatSIPInfoReceived(
		"iS5acYB7munf",
		"application/media_control+xml",
		body,
		"203.150.245.41:5060",
		"<sip:0979045047@sipagent.ttrs.or.th>;tag=remote-tag",
		"<sip:00025@203.151.21.121>;tag=local-tag",
		"call-info-1",
		"FPBX-15.0.37.4",
	)
	if strings.Contains(line, "\n") || strings.Contains(line, "\r") {
		t.Fatalf("log is not one line: %q", line)
	}
	for _, want := range []string{
		"sip_info received",
		"session=iS5acYB7munf",
		"content_type=application/media_control+xml",
		"source=203.150.245.41:5060",
		"from=<sip:0979045047@sipagent.ttrs.or.th>;tag=remote-tag",
		"to=<sip:00025@203.151.21.121>;tag=local-tag",
		"call_id=call-info-1",
		"user_agent=FPBX-15.0.37.4",
		"picture_fast_update",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("log %q missing %q", line, want)
		}
	}
	bodyField := line[strings.Index(line, "body=")+len("body=") : strings.Index(line, " source=")]
	if len(bodyField) != 200 {
		t.Fatalf("body field length %d, want 200: %q", len(bodyField), bodyField)
	}
}

func TestHandleINFO481WithoutDialog(t *testing.T) {
	srv, _ := newMidCallTestServer(t, "call-info-missing")
	req := newInfoRequest("unknown-call", "application/media_control+xml", "<picture_fast_update/>")
	tx := &midCallServerTxStub{}
	srv.handleINFO(req, tx)
	assertResponseCodes(t, tx, []int{481})

	req = newInfoRequest("call-info-missing", "application/media_control+xml", "<picture_fast_update/>")
	tx = &midCallServerTxStub{}
	srv.handleINFO(req, tx)
	assertResponseCodes(t, tx, []int{481})
}

func TestHandleINFOPictureFastUpdateResetsPLI(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-info-fir")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:1001@example.com>", "example.com", 5060, 1, nil)
	req := newInfoRequest("call-info-fir", "application/media_control+xml", "<?xml version=\"1.0\"?>\n<picture_fast_update/>")
	req.SetSource("203.150.245.41:5060")
	req.AppendHeader(sip.NewHeader("User-Agent", "FPBX-15.0.37.4"))
	tx := &midCallServerTxStub{}

	before := time.Now()
	srv.handleINFO(req, tx)

	assertResponseCodes(t, tx, []int{200})
	wait := sess.ProactiveBrowserPLIWait(30*time.Second, before)
	if wait < 1500*time.Millisecond || wait > 2500*time.Millisecond {
		t.Fatalf("proactive wait after INFO = %s, want about 2s", wait)
	}
	if !sess.SIPInfoKeyframePending() {
		t.Fatal("INFO picture_fast_update did not arm the next uplink IDR")
	}
}

func TestHandleINFOForwardsDTMFRelay(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-info-dtmf")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:1001@example.com>", "example.com", 5060, 1, nil)
	notifier := &infoDTMFStub{}
	srv.dtmfNotifier = notifier
	req := newInfoRequest("call-info-dtmf", "application/dtmf-relay", "Signal=5\r\nDuration=160\r\n")
	tx := &midCallServerTxStub{}

	srv.handleINFO(req, tx)

	assertResponseCodes(t, tx, []int{200})
	if notifier.calls != 1 || notifier.digit != "5" || notifier.sessionID != sess.ID {
		t.Fatalf("dtmf notify = %+v", notifier)
	}
}

func TestHandleINFORejectsWrongDialogTags(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-info-tags")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:1001@example.com>", "example.com", 5060, 1, nil)
	req := newInfoRequest("call-info-tags", "application/dtmf", "1")
	req.RemoveHeader("From")
	req.RemoveHeader("To")
	req.AppendHeader(sip.NewHeader("From", "<sip:1001@example.com>;tag=other"))
	req.AppendHeader(sip.NewHeader("To", "<sip:1002@example.com>;tag=local-tag"))
	tx := &midCallServerTxStub{}

	srv.handleINFO(req, tx)

	assertResponseCodes(t, tx, []int{481})
}

func newInfoRequest(callID, contentType, body string) *sip.Request {
	req := sip.NewRequest(sip.INFO, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	req.AppendHeader(sip.NewHeader("From", "<sip:1001@example.com>;tag=remote-tag"))
	req.AppendHeader(sip.NewHeader("To", "<sip:1002@example.com>;tag=local-tag"))
	req.AppendHeader(sip.NewHeader("CSeq", "4 INFO"))
	if contentType != "" {
		req.AppendHeader(sip.NewHeader("Content-Type", contentType))
	}
	if body != "" {
		req.SetBody([]byte(body))
	}
	return req
}
