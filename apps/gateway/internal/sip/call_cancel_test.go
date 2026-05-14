package sip

import (
	"testing"

	"github.com/emiago/sipgo/sip"
)

func TestCreateCancelRequestFromInviteCopiesDialogCriticalHeaders(t *testing.T) {
	inviteReq := sip.NewRequest(sip.INVITE, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	inviteReq.SipVersion = "SIP/2.0"
	inviteReq.AppendHeader(sip.NewHeader("Via", "SIP/2.0/TCP 10.0.0.1:5060;branch=z9hG4bK-test"))
	inviteReq.AppendHeader(sip.NewHeader("Route", "<sip:proxy.example.com;lr>"))
	inviteReq.AppendHeader(sip.NewHeader("From", "<sip:1001@example.com>;tag=from-tag"))
	inviteReq.AppendHeader(sip.NewHeader("To", "<sip:1002@example.com>"))
	inviteReq.AppendHeader(sip.NewHeader("Call-ID", "call-123"))
	inviteReq.AppendHeader(sip.NewHeader("CSeq", "42 INVITE"))
	inviteReq.SetTransport("tcp")
	inviteReq.SetSource("10.0.0.1:5060")
	inviteReq.SetDestination("sip.example.com:5060")

	cancelReq, err := createCancelRequestFromInvite(inviteReq)
	if err != nil {
		t.Fatalf("createCancelRequestFromInvite failed: %v", err)
	}

	if cancelReq.Method != sip.CANCEL {
		t.Fatalf("expected CANCEL method, got %s", cancelReq.Method)
	}
	if got := cancelReq.CallID().Value(); got != "call-123" {
		t.Fatalf("expected Call-ID copied, got %q", got)
	}
	if got := cancelReq.From().Value(); got != inviteReq.From().Value() {
		t.Fatalf("expected From copied, got %q", got)
	}
	if got := cancelReq.To().Value(); got != inviteReq.To().Value() {
		t.Fatalf("expected To copied, got %q", got)
	}
	gotBranch, ok := cancelReq.Via().Params.Get("branch")
	if !ok || gotBranch != "z9hG4bK-test" {
		t.Fatalf("expected Via branch copied, got %q", gotBranch)
	}
	if got := cancelReq.CSeq().SeqNo; got != 42 {
		t.Fatalf("expected CSeq number copied, got %d", got)
	}
	if got := cancelReq.CSeq().MethodName; got != sip.CANCEL {
		t.Fatalf("expected CSeq method CANCEL, got %s", got)
	}
	if len(cancelReq.GetHeaders("Route")) != 1 {
		t.Fatalf("expected Route header copied")
	}
}
