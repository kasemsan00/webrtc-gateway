package sip

import (
	"testing"

	"github.com/emiago/sipgo/sip"
)

func ackTestRequest(t *testing.T, toTag string, routes ...string) *sip.Request {
	t.Helper()
	var uri sip.Uri
	if err := sip.ParseUri("sip:9999@203.150.245.41:5060", &uri); err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	req := sip.NewRequest(sip.INVITE, uri)
	for _, route := range routes {
		req.AppendHeader(sip.NewHeader("Route", route))
	}
	req.AppendHeader(sip.NewHeader("From", "<sip:1001@pbx>;tag=local"))
	to := "<sip:9999@pbx>"
	if toTag != "" {
		to += ";tag=" + toTag
	}
	req.AppendHeader(sip.NewHeader("To", to))
	req.AppendHeader(sip.NewHeader("Call-ID", "call-1"))
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 7, MethodName: sip.INVITE})
	return req
}

// The 200 to a hold re-INVITE carries no Record-Route. Its ACK must still follow
// the dialog's route set, or the proxy drops it and Asterisk hangs up the call.
func TestBuildAckForReinviteUsesDialogRouteSet(t *testing.T) {
	s := &Server{publicAddress: "203.151.21.121", sipPort: 5090}
	reinvite := ackTestRequest(t, "remote", "<sip:203.150.245.39;transport=tcp;lr>", "<sip:203.150.245.39;lr>")
	res := sip.NewResponseFromRequest(reinvite, 200, "OK", nil)

	ack := s.buildAckForInvite(reinvite, res)

	routes := ack.GetHeaders("Route")
	if len(routes) != 2 {
		t.Fatalf("ACK for a re-INVITE 200 must carry the dialog's 2 Route headers, got %d:\n%s", len(routes), ack.String())
	}
	if routes[0].Value() != "<sip:203.150.245.39;transport=tcp;lr>" {
		t.Fatalf("Route order must match the re-INVITE, got %q first", routes[0].Value())
	}
	if cseq := ack.CSeq(); cseq == nil || cseq.SeqNo != 7 || cseq.MethodName != sip.ACK {
		t.Fatalf("ACK must reuse the re-INVITE CSeq number, got %v", ack.CSeq())
	}
}

// A dialog-creating INVITE still takes its route set from the 200's Record-Route.
func TestBuildAckForInitialInviteUsesRecordRoute(t *testing.T) {
	s := &Server{publicAddress: "203.151.21.121", sipPort: 5090}
	invite := ackTestRequest(t, "", "<sip:outbound-proxy;lr>")
	res := sip.NewResponseFromRequest(invite, 200, "OK", nil)
	res.AppendHeader(sip.NewHeader("Record-Route", "<sip:proxy-b;lr>"))
	res.AppendHeader(sip.NewHeader("Record-Route", "<sip:proxy-a;lr>"))

	ack := s.buildAckForInvite(invite, res)

	routes := ack.GetHeaders("Route")
	if len(routes) != 2 || routes[0].Value() != "<sip:proxy-a;lr>" || routes[1].Value() != "<sip:proxy-b;lr>" {
		t.Fatalf("initial ACK must use the reversed Record-Route set, got:\n%s", ack.String())
	}
}
