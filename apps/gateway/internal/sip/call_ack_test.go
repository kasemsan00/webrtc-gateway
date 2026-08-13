package sip

import (
	"context"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"

	"k2-gateway/internal/session"
)

func TestCompleteOutboundInvite200SetsAsteriskEndpointsFromAnswer(t *testing.T) {
	srv := &Server{publicAddress: "10.10.10.10", sipPort: 5090}
	sess := &session.Session{ID: "ack-first"}
	sess.SetCallInfo("outbound", "0000178668061", "14131", "call-ack-1")

	inviteReq := sip.NewRequest(sip.INVITE, sip.Uri{User: "14131", Host: "sipclient.ttrs.or.th"})
	inviteReq.AppendHeader(sip.NewHeader("Via", "SIP/2.0/TCP 10.10.10.10:5090;branch=z9hG4bK-invite"))
	inviteReq.AppendHeader(sip.NewHeader("From", "<sip:0000178668061@sipclient.ttrs.or.th>;tag=from-tag"))
	inviteReq.AppendHeader(sip.NewHeader("To", "<sip:14131@sipclient.ttrs.or.th>"))
	inviteReq.AppendHeader(sip.NewHeader("Call-ID", "call-ack-1"))
	inviteReq.AppendHeader(sip.NewHeader("CSeq", "2 INVITE"))
	inviteReq.SetDestination("203.150.245.39:5060")
	inviteReq.SetTransport("TCP")

	res := sip.NewResponseFromRequest(inviteReq, 200, "OK", []byte(strings.Join([]string{
		"v=0",
		"o=- 0 0 IN IP4 203.150.245.41",
		"s=Asterisk",
		"c=IN IP4 203.150.245.41",
		"t=0 0",
		"m=audio 20000 RTP/AVP 111",
		"a=rtpmap:111 opus/48000/2",
		"m=video 20002 RTP/AVP 96",
		"a=rtpmap:96 H264/90000",
		"",
	}, "\r\n")))
	res.AppendHeader(sip.NewHeader("Contact", "<sip:14131@203.150.245.41:5060>"))
	if to := res.To(); to != nil {
		if to.Params == nil {
			to.Params = sip.NewParams()
		}
		to.Params.Add("tag", "as5934b3c2")
	}

	if err := srv.completeOutboundInvite200(context.Background(), inviteReq, res, sess, sipAuthParams{
		Domain: "sipclient.ttrs.or.th",
		Port:   5060,
	}, 2, true); err != nil {
		t.Fatalf("completeOutboundInvite200: %v", err)
	}

	if sess.GetState() != session.StateActive {
		t.Fatalf("expected active after 200, got %s", sess.GetState())
	}
	if sess.AsteriskAudioAddr == nil || sess.AsteriskAudioAddr.Port != 20000 {
		t.Fatalf("expected audio endpoint from SDP before DB logging, got %#v", sess.AsteriskAudioAddr)
	}
	if sess.SIPOpusPT != 111 {
		t.Fatalf("expected opus PT 111 from authenticated answer, got %d", sess.SIPOpusPT)
	}
}

func TestHandleUnhandledSIPResponseIgnoresNonInvite(t *testing.T) {
	srv := &Server{}
	srv.handleUnhandledSIPResponse(nil)

	res := sip.NewResponse(200, "OK")
	res.AppendHeader(sip.NewHeader("CSeq", "102 BYE"))
	srv.handleUnhandledSIPResponse(res)
}
