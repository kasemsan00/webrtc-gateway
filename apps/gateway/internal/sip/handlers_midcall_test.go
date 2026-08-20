package sip

import (
	"testing"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

type midCallServerTxStub struct {
	responses []*sip.Response
	onCancel  sip.FnTxCancel
}

func (tx *midCallServerTxStub) Respond(res *sip.Response) error {
	tx.responses = append(tx.responses, res)
	return nil
}
func (tx *midCallServerTxStub) Terminate() {}
func (tx *midCallServerTxStub) OnTerminate(_ sip.FnTxTerminate) bool {
	return false
}
func (tx *midCallServerTxStub) Done() <-chan struct{} { return make(chan struct{}) }
func (tx *midCallServerTxStub) Err() error            { return nil }
func (tx *midCallServerTxStub) Acks() <-chan *sip.Request {
	return make(chan *sip.Request)
}
func (tx *midCallServerTxStub) OnCancel(fn sip.FnTxCancel) bool {
	tx.onCancel = fn
	return true
}

type midCallStateNotifierStub struct {
	calls     int
	sessionID string
	state     session.SessionState
}

func (n *midCallStateNotifierStub) NotifySessionState(sessionID string, state session.SessionState) {
	n.calls++
	n.sessionID = sessionID
	n.state = state
}

type midCallRenegotiationNotifierStub struct {
	calls      int
	sessionID  string
	renegID    string
	validation session.MidCallSDPValidation
}

func (n *midCallRenegotiationNotifierStub) NotifyMidCallRenegotiation(sessionID string, renegotiation session.MidCallRenegotiationSnapshot, validation session.MidCallSDPValidation) {
	n.calls++
	n.sessionID = sessionID
	n.renegID = renegotiation.ID
	n.validation = validation
}

func TestHandleINVITE_ActiveReinviteHoldRespondsOKAndUpdatesMediaState(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-mid-hold")
	notifier := &midCallStateNotifierStub{}
	srv.stateNotifier = notifier
	req := newMidCallInviteRequest("call-mid-hold", "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendonly\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=sendonly\r\n")
	tx := &midCallServerTxStub{}

	srv.handleINVITE(req, tx)

	assertResponseCodes(t, tx, []int{100, 200})
	state := sess.GetMidCallMediaState()
	if state.AudioDirection != "sendonly" || state.VideoDirection != "sendonly" {
		t.Fatalf("unexpected mid-call media state: %+v", state)
	}
	if !state.HasActiveVideo {
		t.Fatalf("expected active video to remain true on sendonly hold")
	}
	if notifier.calls != 1 || notifier.sessionID != sess.ID || notifier.state != session.StateActive {
		t.Fatalf("expected active state notification, got calls=%d sessionID=%s state=%s", notifier.calls, notifier.sessionID, notifier.state)
	}
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatalf("expected pending renegotiation to be cleared after successful fast path")
	}
}

func TestHandleINVITE_ActiveReinviteUnsupportedVideoResponds488(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-mid-vp8")
	req := newMidCallInviteRequest("call-mid-vp8", "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\nm=video 4002 RTP/AVP 97\r\na=rtpmap:97 VP8/90000\r\n")
	tx := &midCallServerTxStub{}

	srv.handleINVITE(req, tx)

	assertResponseCodes(t, tx, []int{100, 488})
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatalf("expected no pending renegotiation after rejected SDP")
	}
}

func TestHandleINVITE_ActiveReinvitePendingResponds491(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-mid-glare")
	if _, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		ID:       "existing",
		Source:   "sip_update",
		Method:   "UPDATE",
		OfferSDP: "v=0\r\n",
	}); !ok {
		t.Fatalf("expected test setup renegotiation to start")
	}
	req := newMidCallInviteRequest("call-mid-glare", "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv\r\n")
	tx := &midCallServerTxStub{}

	srv.handleINVITE(req, tx)

	assertResponseCodes(t, tx, []int{100, 491})
	pending, ok := sess.GetPendingMidCallRenegotiation()
	if !ok || pending.ID != "existing" {
		t.Fatalf("expected original pending renegotiation to remain, got %+v ok=%v", pending, ok)
	}
}

func TestHandleINVITE_InDialogUnknownCallResponds481(t *testing.T) {
	cfg := &config.Config{SIP: config.SIPConfig{MidCallRenegotiationEnable: true}, RTP: config.RTPConfig{BufferSize: 1500}}
	mgr := session.NewManager(cfg)
	srv := &Server{
		config:        cfg.SIP,
		rtpConfig:     cfg.RTP,
		sessionMgr:    mgr,
		publicAddress: "127.0.0.1",
		sipPort:       5060,
	}
	req := newMidCallInviteRequest("unknown-dialog", "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\n")
	tx := &midCallServerTxStub{}

	srv.handleINVITE(req, tx)

	assertResponseCodes(t, tx, []int{481})
}

func TestHandleINVITE_ActiveReinviteMalformedSDPResponds488(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-mid-malformed")
	req := newMidCallInviteRequest("call-mid-malformed", "v=0\r\nm=audio nope RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\n")
	tx := &midCallServerTxStub{}

	srv.handleINVITE(req, tx)

	assertResponseCodes(t, tx, []int{100, 488})
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatalf("expected no pending renegotiation after malformed SDP")
	}
}

func TestHandleINVITE_ActiveReinviteVideoRemovedKeepsAudioAndRequestsClientRenegotiation(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-mid-video-remove")
	notifier := &midCallRenegotiationNotifierStub{}
	srv.midCallNotifier = notifier
	sess.ApplyMidCallSDPValidation(session.MidCallSDPValidation{
		Accept:         true,
		Audio:          session.MidCallMedia{Direction: "sendrecv"},
		Video:          session.MidCallMedia{Direction: "sendrecv"},
		HasActiveVideo: true,
	})
	req := newMidCallInviteRequest("call-mid-video-remove", "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=inactive\r\n")
	tx := &midCallServerTxStub{}

	srv.handleINVITE(req, tx)

	assertResponseCodes(t, tx, []int{100, 200})
	state := sess.GetMidCallMediaState()
	if state.AudioDirection != "sendrecv" || state.HasActiveVideo {
		t.Fatalf("expected audio preserved and video inactive, got %+v", state)
	}
	if notifier.calls != 1 || notifier.sessionID != sess.ID || !notifier.validation.RequiresClientRenegotiation {
		t.Fatalf("expected client renegotiation notification, got %+v", notifier)
	}
	if pending, ok := sess.GetPendingMidCallRenegotiation(); !ok || pending.ID != notifier.renegID {
		t.Fatalf("expected pending renegotiation to wait for client answer, got %+v ok=%v", pending, ok)
	}
}

func TestHandleINVITE_ActiveReinviteVideoAddedRequestsClientRenegotiation(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-mid-video-add")
	notifier := &midCallRenegotiationNotifierStub{}
	srv.midCallNotifier = notifier
	sess.ApplyMidCallSDPValidation(session.MidCallSDPValidation{
		Accept:         true,
		Audio:          session.MidCallMedia{Direction: "sendrecv"},
		HasActiveVideo: false,
	})
	req := newMidCallInviteRequest("call-mid-video-add", "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=sendrecv\r\n")
	tx := &midCallServerTxStub{}

	srv.handleINVITE(req, tx)

	assertResponseCodes(t, tx, []int{100, 200})
	if notifier.calls != 1 || notifier.sessionID != sess.ID || !notifier.validation.HasActiveVideo {
		t.Fatalf("expected video add client renegotiation notification, got %+v", notifier)
	}
	if _, ok := sess.GetPendingMidCallRenegotiation(); !ok {
		t.Fatal("expected pending renegotiation to wait for client answer")
	}
}

func TestHandleUPDATE_ActiveHoldRespondsOKAndUpdatesMediaState(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-update-hold")
	req := newMidCallUpdateRequest("call-update-hold", "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendonly\r\n")
	tx := &midCallServerTxStub{}

	srv.handleUPDATE(req, tx)

	assertResponseCodes(t, tx, []int{200})
	state := sess.GetMidCallMediaState()
	if state.AudioDirection != "sendonly" {
		t.Fatalf("unexpected media state after UPDATE hold: %+v", state)
	}
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatalf("expected UPDATE fast path to clear pending renegotiation")
	}
}

func TestHandleUPDATE_PendingResponds491(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-update-glare")
	if _, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		ID:     "existing",
		Source: "sip_reinvite",
		Method: "INVITE",
	}); !ok {
		t.Fatal("expected pending renegotiation setup")
	}
	req := newMidCallUpdateRequest("call-update-glare", "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\n")
	tx := &midCallServerTxStub{}

	srv.handleUPDATE(req, tx)

	assertResponseCodes(t, tx, []int{491})
}

func TestHandleUPDATE_UnsupportedSDPResponds488(t *testing.T) {
	srv, _ := newMidCallTestServer(t, "call-update-vp8")
	req := newMidCallUpdateRequest("call-update-vp8", "v=0\r\nm=audio 4000 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n")
	tx := &midCallServerTxStub{}

	srv.handleUPDATE(req, tx)

	assertResponseCodes(t, tx, []int{488})
}

func TestHandleUPDATE_WithoutSDPRespondsOK(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-update-refresh")
	req := newMidCallUpdateRequest("call-update-refresh", "")
	tx := &midCallServerTxStub{}

	srv.handleUPDATE(req, tx)

	assertResponseCodes(t, tx, []int{200})
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatal("expected UPDATE without SDP not to create pending renegotiation")
	}
}

func newMidCallTestServer(t *testing.T, callID string) (*Server, *session.Session) {
	t.Helper()
	cfg := &config.Config{
		SIP: config.SIPConfig{MidCallRenegotiationEnable: true},
		RTP: config.RTPConfig{BufferSize: 1500},
	}
	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() {
		mgr.DeleteSession(sess.ID)
	})
	sess.SetCallInfo("inbound", "sip:1001@example.com", "sip:1002@example.com", callID)
	sess.SetState(session.StateActive)
	sess.RTPPort = 4000
	sess.VideoRTPPort = 4002
	sess.ApplyMidCallSDPValidation(session.MidCallSDPValidation{
		Accept:         true,
		Audio:          session.MidCallMedia{Direction: "sendrecv"},
		Video:          session.MidCallMedia{Direction: "sendrecv"},
		HasActiveVideo: true,
	})

	return &Server{
		config:        cfg.SIP,
		rtpConfig:     cfg.RTP,
		sessionMgr:    mgr,
		publicAddress: "127.0.0.1",
		sipPort:       5060,
	}, sess
}

func newMidCallInviteRequest(callID, body string) *sip.Request {
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	req.AppendHeader(sip.NewHeader("From", "<sip:1001@example.com>;tag=from-tag"))
	req.AppendHeader(sip.NewHeader("To", "<sip:1002@example.com>;tag=to-tag"))
	req.AppendHeader(sip.NewHeader("CSeq", "2 INVITE"))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.SetBody([]byte(body))
	return req
}

func newMidCallUpdateRequest(callID, body string) *sip.Request {
	req := sip.NewRequest(sip.UPDATE, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	req.AppendHeader(sip.NewHeader("From", "<sip:1001@example.com>;tag=from-tag"))
	req.AppendHeader(sip.NewHeader("To", "<sip:1002@example.com>;tag=to-tag"))
	req.AppendHeader(sip.NewHeader("CSeq", "3 UPDATE"))
	if body != "" {
		req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
		req.SetBody([]byte(body))
	}
	return req
}

func assertResponseCodes(t *testing.T, tx *midCallServerTxStub, want []int) {
	t.Helper()
	if len(tx.responses) != len(want) {
		got := make([]int, 0, len(tx.responses))
		for _, res := range tx.responses {
			if res != nil {
				got = append(got, res.StatusCode)
			}
		}
		t.Fatalf("response count = %d codes=%v, want %v", len(tx.responses), got, want)
	}
	for i, wantCode := range want {
		if tx.responses[i] == nil {
			t.Fatalf("response %d is nil", i)
		}
		if tx.responses[i].StatusCode != wantCode {
			t.Fatalf("response %d status = %d, want %d", i, tx.responses[i].StatusCode, wantCode)
		}
	}
}
