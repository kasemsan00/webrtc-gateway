package sip

import (
	"net"
	"testing"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/session"
)

const holdOfferSDP = "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendonly\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=sendonly\r\n"

// The client's network dropped (reconnecting) while Asterisk puts the call on
// hold. The re-INVITE must still be answered or Asterisk ends the call.
func TestHandleINVITE_ReinviteWhileReconnectingIsAnswered(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-reconnecting-hold")
	sess.SetState(session.StateReconnecting)
	notifier := &midCallStateNotifierStub{}
	srv.stateNotifier = notifier
	tx := &midCallServerTxStub{}

	srv.handleINVITE(newMidCallInviteRequest("call-reconnecting-hold", holdOfferSDP), tx)

	assertResponseCodes(t, tx, []int{100, 200})
	if got := sess.GetMidCallMediaState().AudioDirection; got != "sendonly" {
		t.Fatalf("expected hold applied, audio direction=%q", got)
	}
	if tx.onCancel != nil {
		t.Fatal("a re-INVITE must not register the incoming-call CANCEL handler")
	}
	if notifier.calls != 0 {
		t.Fatalf("a reconnecting call must not be announced active, got %d notifications (%s)", notifier.calls, notifier.state)
	}
}

func TestHandleINVITE_ReinviteFollowsMovedAsteriskMedia(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-media-moved")
	sess.SetAsteriskEndpoints(&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 10000}, &net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 10002})
	offer := "v=0\r\nc=IN IP4 10.0.0.9\r\nm=audio 20000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv\r\nm=video 20002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=sendrecv\r\n"
	tx := &midCallServerTxStub{}

	srv.handleINVITE(newMidCallInviteRequest("call-media-moved", offer), tx)

	assertResponseCodes(t, tx, []int{100, 200})
	_, audio, _ := sess.GetAudioRTPInfo()
	if audio == nil || audio.String() != "10.0.0.9:20000" {
		t.Fatalf("expected audio moved to 10.0.0.9:20000, got %v", audio)
	}
}

func TestUpdateAsteriskEndpointsFromOffer_IgnoresHoldAddress(t *testing.T) {
	sess := &session.Session{ID: "hold-address"}
	orig := &net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 10000}
	sess.SetAsteriskEndpoints(orig, nil)

	if sess.UpdateAsteriskEndpointsFromOffer(net.ParseIP("0.0.0.0"), 20000, 0) {
		t.Fatal("c=0.0.0.0 hold must not move the media destination")
	}
	if sess.UpdateAsteriskEndpointsFromOffer(net.ParseIP("10.0.0.5"), 10000, 0) {
		t.Fatal("an unchanged address must not count as moved")
	}
	_, audio, _ := sess.GetAudioRTPInfo()
	if audio.String() != orig.String() {
		t.Fatalf("expected %s kept, got %s", orig, audio)
	}
}

// Without a database there is no trunk manager. A new INVITE must still get
// a final response instead of timing out at the caller.
func TestHandleINVITE_NewCallWithoutTrunkManagerGets503(t *testing.T) {
	srv, _ := newMidCallTestServer(t, "call-existing")
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", "call-new-no-trunks"))
	req.AppendHeader(sip.NewHeader("From", "<sip:1001@example.com>;tag=from-tag"))
	req.AppendHeader(sip.NewHeader("To", "<sip:1002@example.com>"))
	req.AppendHeader(sip.NewHeader("CSeq", "1 INVITE"))
	tx := &midCallServerTxStub{}

	srv.handleINVITE(req, tx)

	assertResponseCodes(t, tx, []int{503})
}

func TestHandleINVITE_ReinviteForEndedSessionGets481(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-ended-reinvite")
	sess.SetState(session.StateEnded)
	tx := &midCallServerTxStub{}

	srv.handleINVITE(newMidCallInviteRequest("call-ended-reinvite", holdOfferSDP), tx)

	assertResponseCodes(t, tx, []int{481})
}

func TestHandleINVITE_ReinviteBeforeDialogGets491(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-ringing-reinvite")
	sess.SetState(session.StateRinging)
	tx := &midCallServerTxStub{}

	srv.handleINVITE(newMidCallInviteRequest("call-ringing-reinvite", holdOfferSDP), tx)

	assertResponseCodes(t, tx, []int{491})
}
