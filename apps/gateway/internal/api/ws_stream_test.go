package api

import (
	"sync"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

// screenStreamSIPMaker stands in for the SIP side of the auxiliary video
// negotiation.
type screenStreamSIPMaker struct {
	incomingTestSIPCallMaker
	mu          sync.Mutex
	outcome     sip.ScreenStreamOutcome
	openCount   int
	closeCount  int
	closeReason string
	onOpen      func()
	reports     []bool
}

func (m *screenStreamSIPMaker) ReportScreenStreamToPeer(_ *session.Session, ready bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports = append(m.reports, ready)
}

func (m *screenStreamSIPMaker) reported() []bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]bool(nil), m.reports...)
}

func (m *screenStreamSIPMaker) OpenScreenStream(*session.Session) sip.ScreenStreamOutcome {
	m.mu.Lock()
	m.openCount++
	onOpen := m.onOpen
	outcome := m.outcome
	m.mu.Unlock()
	if onOpen != nil {
		onOpen()
	}
	return outcome
}

func (m *screenStreamSIPMaker) CloseScreenStream(_ *session.Session, reason string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCount++
	m.closeReason = reason
	return true
}

func (m *screenStreamSIPMaker) counts() (open, closed int, closeReason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openCount, m.closeCount, m.closeReason
}

type streamTestRig struct {
	srv    *Server
	maker  *screenStreamSIPMaker
	sess   *session.Session
	client *WSClient
}

func newStreamTestRig(t *testing.T) *streamTestRig {
	t.Helper()
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
	maker := &screenStreamSIPMaker{outcome: sip.ScreenStreamOutcome{Status: "ok"}}
	client := &WSClient{sessionID: sess.ID, send: make(chan []byte, 8), multiVideo: true}
	srv := &Server{
		sessionMgr: mgr,
		sipMaker:   maker,
		wsClients:  map[string]*WSClient{sess.ID: client},
	}
	return &streamTestRig{srv: srv, maker: maker, sess: sess, client: client}
}

const (
	offerCameraAndScreen = "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=mid:0\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:1\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:2\r\n"
	offerCameraOnly = "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=mid:0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:1\r\n"
)

func (r *streamTestRig) update(t *testing.T, msg WSMessage) WSMessage {
	t.Helper()
	msg.Type = "stream_update"
	if msg.SessionID == "" {
		msg.SessionID = r.sess.ID
	}
	if msg.Role == "" {
		msg.Role = "screen"
	}
	r.srv.handleWSStreamUpdate(r.client, msg)
	result := readWSTestMessage(t, r.client)
	if result.Type != "stream_update_result" {
		t.Fatalf("expected stream_update_result, got %+v", result)
	}
	if result.Error != "" {
		t.Fatalf("a stream_update answer must never be an error message: %+v", result)
	}
	return result
}

func assertStreamResult(t *testing.T, got WSMessage, status, reason string) {
	t.Helper()
	if got.Status != status || got.Reason != reason {
		t.Fatalf("result status=%q reason=%q, want status=%q reason=%q", got.Status, got.Reason, status, reason)
	}
}

func TestStreamUpdateValidatesRequestBeforeTouchingSIP(t *testing.T) {
	for _, tc := range []struct {
		name   string
		msg    WSMessage
		setup  func(r *streamTestRig)
		reason string
	}{
		{name: "unknown role", msg: WSMessage{Role: "whiteboard", Action: "add", SDP: offerCameraAndScreen}, reason: "unsupported_role"},
		{name: "unknown action", msg: WSMessage{Action: "pause"}, reason: "unsupported_action"},
		{name: "client did not opt in", msg: WSMessage{Action: "add", SDP: offerCameraAndScreen},
			setup: func(r *streamTestRig) { r.client.multiVideo = false }, reason: "multi_video_not_enabled"},
		{name: "public client", msg: WSMessage{Action: "add", SDP: offerCameraAndScreen},
			setup: func(r *streamTestRig) { r.client.publicOnly = true }, reason: "multi_video_not_enabled"},
		{name: "session of another client", msg: WSMessage{Action: "add", SDP: offerCameraAndScreen},
			setup: func(r *streamTestRig) { r.srv.wsClients[r.sess.ID] = &WSClient{sessionID: r.sess.ID} }, reason: "forbidden"},
		{name: "unknown session", msg: WSMessage{Action: "add", SDP: offerCameraAndScreen, SessionID: "missing"},
			setup: func(r *streamTestRig) {
				r.client.ownedSessionIDs = map[string]struct{}{"missing": {}}
			}, reason: "session_not_found"},
		{name: "session not active", msg: WSMessage{Action: "add", SDP: offerCameraAndScreen},
			setup: func(r *streamTestRig) { r.sess.SetState(session.StateReconnecting) }, reason: "not_active"},
		{name: "offer without a second video", msg: WSMessage{Action: "add", SDP: offerCameraOnly}, reason: "invalid_offer"},
		{name: "first share needs an offer", msg: WSMessage{Action: "add"}, reason: "sdp_required"},
		{name: "another renegotiation pending", msg: WSMessage{Action: "add", SDP: offerCameraAndScreen},
			setup: func(r *streamTestRig) {
				if _, ok := r.sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{Source: "sip_reinvite"}); !ok {
					panic("could not start a pending renegotiation")
				}
			}, reason: "busy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newStreamTestRig(t)
			if tc.setup != nil {
				tc.setup(rig)
			}

			assertStreamResult(t, rig.update(t, tc.msg), "failed", tc.reason)

			if open, closed, _ := rig.maker.counts(); open != 0 || closed != 0 {
				t.Fatalf("SIP must not be touched: open=%d close=%d", open, closed)
			}
		})
	}
}

func TestStreamUpdateAddRejectedBySIPLeavesPeerConnectionUntouched(t *testing.T) {
	rig := newStreamTestRig(t)
	rig.maker.outcome = sip.ScreenStreamOutcome{Status: "rejected", Reason: "not_supported"}
	pc := rig.sess.PeerConnection

	result := rig.update(t, WSMessage{Action: "add", SDP: offerCameraAndScreen})

	assertStreamResult(t, result, "rejected", "not_supported")
	if result.SDP != "" || result.Role != "screen" || result.Action != "add" || result.SessionID != rig.sess.ID {
		t.Fatalf("unexpected result fields: %+v", result)
	}
	if rig.sess.PeerConnection != pc || rig.sess.AuxUplinkMid() != "" {
		t.Fatal("a refused share must not change the WebRTC side")
	}
	if _, pending := rig.sess.GetPendingMidCallRenegotiation(); pending {
		t.Fatal("the renegotiation claim must be released")
	}
	if rig.sess.GetState() != session.StateActive {
		t.Fatalf("call state = %s, want active", rig.sess.GetState())
	}
}

func TestStreamUpdateReAddWithoutOfferOnlyRenegotiatesSIP(t *testing.T) {
	rig := newStreamTestRig(t)
	rig.sess.SetAuxUplinkMid("2") // the transceiver from an earlier share

	result := rig.update(t, WSMessage{Action: "add"})

	assertStreamResult(t, result, "ok", "")
	if result.SDP != "" {
		t.Fatalf("a share without an offer must not return an answer, got %d bytes", len(result.SDP))
	}
	if open, _, _ := rig.maker.counts(); open != 1 {
		t.Fatalf("OpenScreenStream calls = %d, want 1", open)
	}
	if rig.sess.AuxUplinkMid() != "2" {
		t.Fatal("the existing auxiliary mid must be kept")
	}
}

func TestStreamUpdateWebRTCFailureClosesSIPStream(t *testing.T) {
	rig := newStreamTestRig(t)
	// The offer names a second video section, but the session's
	// PeerConnection cannot apply this SDP.
	result := rig.update(t, WSMessage{Action: "add", SDP: offerCameraAndScreen})

	assertStreamResult(t, result, "failed", "webrtc_renegotiation_failed")
	open, closed, reason := rig.maker.counts()
	if open != 1 || closed != 1 || reason != "webrtc-renegotiation-failed" {
		t.Fatalf("open=%d close=%d reason=%q, want the SIP stream opened then closed", open, closed, reason)
	}
	if rig.sess.AuxUplinkMid() != "" {
		t.Fatal("the auxiliary mid must be cleared after a failed renegotiation")
	}
	if _, pending := rig.sess.GetPendingMidCallRenegotiation(); pending {
		t.Fatal("the renegotiation claim must be released")
	}
}

func TestStreamUpdateRemove(t *testing.T) {
	rig := newStreamTestRig(t)

	result := rig.update(t, WSMessage{Action: "remove"})

	assertStreamResult(t, result, "ok", "")
	if _, closed, reason := rig.maker.counts(); closed != 1 || reason != "client-remove" {
		t.Fatalf("CloseScreenStream calls = %d reason=%q, want 1 client-remove", closed, reason)
	}
}

func TestStreamUpdateIsSingleFlight(t *testing.T) {
	rig := newStreamTestRig(t)
	rig.sess.SetAuxUplinkMid("2")
	release := make(chan struct{})
	entered := make(chan struct{})
	rig.maker.onOpen = func() {
		close(entered)
		<-release
	}

	rig.srv.handleWSStreamUpdate(rig.client, WSMessage{Type: "stream_update", SessionID: rig.sess.ID, Role: "screen", Action: "add"})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("the SIP negotiation did not start")
	}

	// A second request while the first is still talking to the SIP peer.
	rig.srv.handleWSStreamUpdate(rig.client, WSMessage{Type: "stream_update", SessionID: rig.sess.ID, Role: "screen", Action: "remove"})
	assertStreamResult(t, readWSTestMessage(t, rig.client), "failed", "busy")

	close(release)
	assertStreamResult(t, readWSTestMessage(t, rig.client), "ok", "")
	if open, closed, _ := rig.maker.counts(); open != 1 || closed != 0 {
		t.Fatalf("open=%d close=%d, want only the first request to reach SIP", open, closed)
	}
}

func TestStreamUpdateDispatchRespectsEndpointPolicy(t *testing.T) {
	t.Run("agent socket needs session access", func(t *testing.T) {
		rig := newStreamTestRig(t)
		agent := newAgentWSClient()
		agent.multiVideo = true

		rig.srv.handleWSMessage(agent, []byte(`{"type":"stream_update","sessionId":"`+rig.sess.ID+`","role":"screen","action":"remove"}`))

		if msg := readWSTestMessage(t, agent); msg.Type != "error" {
			t.Fatalf("expected the agent policy to reject, got %+v", msg)
		}
		if _, closed, _ := rig.maker.counts(); closed != 0 {
			t.Fatal("SIP must not be touched")
		}
	})
	t.Run("agent socket with access is dispatched", func(t *testing.T) {
		rig := newStreamTestRig(t)
		agent := newAgentWSClient()
		agent.multiVideo = true
		agent.ownedSessionIDs[rig.sess.ID] = struct{}{}

		rig.srv.handleWSMessage(agent, []byte(`{"type":"stream_update","sessionId":"`+rig.sess.ID+`","role":"screen","action":"remove"}`))

		assertStreamResult(t, readWSTestMessage(t, agent), "ok", "")
	})
	t.Run("public socket is refused", func(t *testing.T) {
		rig := newStreamTestRig(t)
		rig.client.publicOnly = true

		rig.srv.handleWSMessage(rig.client, []byte(`{"type":"stream_update","sessionId":"`+rig.sess.ID+`","role":"screen","action":"remove"}`))

		if msg := readWSTestMessage(t, rig.client); msg.Type != "error" {
			t.Fatalf("expected the public policy to reject, got %+v", msg)
		}
		if _, closed, _ := rig.maker.counts(); closed != 0 {
			t.Fatal("SIP must not be touched")
		}
	})
}

func TestClientStateOptsInToMultiVideoExceptOnPublicSocket(t *testing.T) {
	srv := &Server{}
	client := &WSClient{send: make(chan []byte, 2)}
	srv.handleWSClientState(client, WSMessage{Type: "client_state", MultiVideo: true})
	if !client.multiVideo {
		t.Fatal("client_state with multiVideo must opt the connection in")
	}
	srv.handleWSClientState(client, WSMessage{Type: "client_state"})
	if !client.multiVideo {
		t.Fatal("a later client_state without the field must not opt out")
	}

	public := &WSClient{send: make(chan []byte, 2), publicOnly: true}
	srv.handleWSClientState(public, WSMessage{Type: "client_state", MultiVideo: true})
	if public.multiVideo {
		t.Fatal("a public connection must not be able to opt in")
	}
}

func TestNotifyAuxVideoStateOnlyReachesOptedInClient(t *testing.T) {
	rig := newStreamTestRig(t)

	rig.srv.NotifyAuxVideoState(rig.sess.ID, false, "peer_closed", "remote")

	msg := readWSTestMessage(t, rig.client)
	if msg.Type != "stream_state" || msg.SessionID != rig.sess.ID || msg.Role != "screen" ||
		msg.Direction != "remote" || msg.Active == nil || *msg.Active || msg.Reason != "peer_closed" {
		t.Fatalf("unexpected stream_state: %+v", msg)
	}

	rig.client.multiVideo = false
	rig.srv.NotifyAuxVideoState(rig.sess.ID, false, "peer_closed", "remote")
	assertNoWSTestMessage(t, rig.client)
}

func TestStreamUpdateAcceptsThreeVideoOfferForDualScreenShare(t *testing.T) {
	rig := newStreamTestRig(t)
	rig.maker.outcome = sip.ScreenStreamOutcome{Status: "rejected", Reason: "not_supported"}

	offerDual := "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=mid:0\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:1\r\na=sendrecv\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:2\r\na=recvonly\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:3\r\na=sendonly\r\n"

	// Validation must accept the 3-video dual-screen offer and reach SIP.
	result := rig.update(t, WSMessage{Action: "add", SDP: offerDual})
	assertStreamResult(t, result, "rejected", "not_supported")
	if open, _, _ := rig.maker.counts(); open != 1 {
		t.Fatalf("OpenScreenStream calls = %d, want 1", open)
	}
}
