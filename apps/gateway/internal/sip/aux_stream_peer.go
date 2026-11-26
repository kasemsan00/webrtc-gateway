package sip

import (
	"fmt"
	"strings"
	"time"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

// The two legs of a call are separate sessions with separate SIP dialogs, and
// Asterisk answers each leg on its own. The leg that shares a screen is told
// "accepted" before, and regardless of whether, the other leg can receive the
// stream. This file is how the receiving leg reports back.
//
// Asterisk is a B2BUA and does not reliably pass an in-dialog MESSAGE from
// one leg to the other. When both legs are on this gateway the report is
// therefore made directly between the two sessions. Otherwise it goes out as
// a PBX-routed MESSAGE, the route chat already uses between instances.

// AuxVideoReceiver is implemented by the API server. It tells the SIP side
// whether a session's client can show a second video stream, and starts the
// client-side renegotiation once the SIP side of that stream is open.
type AuxVideoReceiver interface {
	CanReceiveAuxVideo(sessionID string) bool
	NotifyAuxVideoOffered(sessionID string)
}

const (
	screenStreamReadyBody       = "@screen:ready"
	screenStreamUnsupportedBody = "@screen:unsupported"
	screenStreamMessagePrefix   = "@screen:"

	// How long the sharing leg waits for the far end to confirm.
	defaultScreenConfirmTimeout        = 3 * time.Second
	defaultScreenKeyframeFollowupDelay = 1100 * time.Millisecond
)

func (s *Server) canReceiveAuxVideo(sess *session.Session) bool {
	receiver, ok := s.midCallNotifier.(AuxVideoReceiver)
	return ok && receiver.CanReceiveAuxVideo(sess.ID)
}

// startAuxVideoClientRenegotiation runs after the SIP answer that opened a
// received auxiliary stream went out. The client then adds the stream on its
// side; ReportScreenStreamToPeer follows when it is done.
func (s *Server) startAuxVideoClientRenegotiation(sess *session.Session) {
	if receiver, ok := s.midCallNotifier.(AuxVideoReceiver); ok {
		receiver.NotifyAuxVideoOffered(sess.ID)
	}
}

// bridgedGatewayPeer returns the other leg of the call when it is a session
// on this gateway.
func (s *Server) bridgedGatewayPeer(sess *session.Session) *session.Session {
	if s.sessionMgr == nil || sess == nil {
		return nil
	}
	return session.FindBridgedGatewayPeer(s.sessionMgr.ListSessions(), sess)
}

// ReportScreenStreamToPeer tells the sharing leg whether this (receiving) leg
// shows its screen stream.
func (s *Server) ReportScreenStreamToPeer(sess *session.Session, ready bool) {
	if sess == nil {
		return
	}
	if peer := s.bridgedGatewayPeer(sess); peer != nil {
		fmt.Printf("[%s] aux_stream report ready=%v to bridged session %s\n", sess.ID, ready, peer.ID)
		s.ConfirmScreenStream(peer, ready)
		return
	}
	body := screenStreamUnsupportedBody
	if ready {
		body = screenStreamReadyBody
	}
	if err := s.sendScreenControlMessage(sess, body); err != nil {
		// The sharing leg times out and closes the stream.
		fmt.Printf("[%s] aux_stream report ready=%v via MESSAGE failed: %v\n", sess.ID, ready, err)
		return
	}
	fmt.Printf("[%s] aux_stream report ready=%v via PBX MESSAGE\n", sess.ID, ready)
}

func sessionScreenControlParties(sess *session.Session) (destination, from string, err error) {
	destination, from, err = sessionMessageParties(sess)
	if err != nil {
		return "", "", err
	}
	direction, _, _, _ := sess.GetCallInfo()
	if strings.EqualFold(strings.TrimSpace(direction), "outbound") {
		if agent := sess.ChatMessageDestination(); agent != "" {
			destination = agent
		}
	}
	return destination, from, nil
}

func (s *Server) sendScreenControlMessage(sess *session.Session, body string) error {
	if sess == nil {
		return fmt.Errorf("session is required")
	}
	destination, from, err := sessionScreenControlParties(sess)
	if err != nil {
		return err
	}
	params, err := s.resolveSessionSIPParams(sess)
	if err != nil {
		return fmt.Errorf("resolve SIP message route: %w", err)
	}
	return s.sendMessageWithParams(sess.ID, destination, from, body, "text/plain;charset=UTF-8", params)
}

// ConfirmScreenStream applies the far end's report to a sharing session. A
// ready report keeps the stream and tells the client; anything else closes
// the local uplink. Reports for a session that is not sharing are ignored.
func (s *Server) ConfirmScreenStream(sess *session.Session, ready bool) {
	if sess == nil {
		return
	}
	aux := sess.AuxVideo()
	if aux == nil || !aux.HasUplink() {
		return
	}
	if !ready {
		s.closeScreenStreamForPeer(sess, aux, "peer_not_supported")
		return
	}
	if !aux.MarkPeerConfirmed() {
		return
	}
	fmt.Printf("[%s] aux_stream confirmed by far end\n", sess.ID)
	s.logAuxStreamEvent(sess, "sip_aux_stream_confirmed", nil)
	// The far end has just attached its track and needs a whole picture.
	// Replay any recently cached uplink IDR immediately so a static screen
	// that already emitted its first frame before confirmation still renders
	// even if the browser ignores PLI until pixels change.
	aux.ReplayCachedIDR("peer-ready")
	aux.RequestKeyframeFromBrowserForced("peer-ready")
	s.scheduleAuxKeyframeFollowup(sess, aux)
	s.notifyAuxVideoState(sess.ID, true, "", "local")
}

func (s *Server) scheduleAuxKeyframeFollowup(sess *session.Session, aux *session.AuxVideo) {
	if s == nil || sess == nil || aux == nil {
		return
	}
	delay := s.screenKeyframeFollowupDelay
	if delay <= 0 {
		delay = defaultScreenKeyframeFollowupDelay
	}
	time.AfterFunc(delay, func() {
		if sess.AuxVideo() == aux {
			aux.RequestKeyframeFromBrowser("peer-ready-followup")
		}
	})
	time.AfterFunc(delay*2, func() {
		if sess.AuxVideo() == aux && aux.Stats().UplinkForwarded == 0 {
			aux.RequestKeyframeFromBrowser("peer-ready-stalled")
		}
	})
}

// armScreenStreamConfirmation closes a shared stream that the far end has not
// confirmed in time: a far end that cannot take a second video stream stays
// silent, and without this the sharer would see "sharing" while nobody
// receives it.
func (s *Server) armScreenStreamConfirmation(sess *session.Session, aux *session.AuxVideo) {
	timeout := s.screenConfirmTimeout
	if timeout <= 0 {
		timeout = defaultScreenConfirmTimeout
	}
	time.AfterFunc(timeout, func() {
		if aux.PeerConfirmed() || sess.AuxVideo() != aux || !aux.HasUplink() {
			return
		}
		fmt.Printf("[%s] aux_stream not confirmed by far end within %s\n", sess.ID, timeout)
		s.closeScreenStreamForPeer(sess, aux, "peer_not_supported")
	})
}

// closeScreenStreamForPeer ends a shared stream's local uplink for a reason
// the client did not ask for, updates or closes its m-line toward the SIP
// peer and tells the client.
func (s *Server) closeScreenStreamForPeer(sess *session.Session, aux *session.AuxVideo, reason string) {
	if sess.AuxVideo() != aux || !aux.HasUplink() {
		return
	}
	// The closing re-INVITE must not overlap another mid-call negotiation on
	// this dialog. Media stops either way.
	claim, claimed := session.MidCallRenegotiationSnapshot{}, false
	for attempt := 0; attempt < 10 && !claimed; attempt++ {
		claim, claimed = sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
			Source: "aux_stream_close", Method: "INVITE", StartedAt: time.Now(),
		})
		if !claimed {
			time.Sleep(200 * time.Millisecond)
		}
	}
	closed := s.CloseScreenStream(sess, reason)
	if claimed {
		sess.CompleteMidCallRenegotiation(claim.ID, "")
	}
	if !closed {
		return
	}
	s.logAuxStreamEvent(sess, "sip_aux_stream_closed", map[string]interface{}{"reason": reason, "direction": "local"})
	s.notifyAuxVideoState(sess.ID, false, reason, "local")
}

// handleScreenStreamMessage applies a far-end report that arrived as a SIP
// MESSAGE. An in-dialog MESSAGE names its session by Call-ID. A PBX-routed
// one is matched to the one sharing session of the addressed user that is
// still waiting; with none or several it is dropped and the wait times out.
func (s *Server) handleScreenStreamMessage(body, callID, toURI string) {
	ready := strings.HasPrefix(body, screenStreamReadyBody)
	if !ready && !strings.HasPrefix(body, screenStreamUnsupportedBody) {
		fmt.Printf("aux_stream unknown control MESSAGE %q ignored\n", body)
		return
	}
	if s.sessionMgr == nil {
		return
	}
	if callID != "" {
		if sess, ok := s.sessionMgr.GetSessionBySIPCallID(callID); ok && sess != nil {
			s.ConfirmScreenStream(sess, ready)
			return
		}
	}
	user := extractUsernameFromURI(toURI)
	if user == "" {
		return
	}
	var waiting []*session.Session
	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil || sess.GetState() != session.StateActive {
			continue
		}
		aux := sess.AuxVideo()
		if aux == nil || !aux.HasUplink() || aux.PeerConfirmed() {
			continue
		}
		if sessionLocalSIPUser(sess) == user {
			waiting = append(waiting, sess)
		}
	}
	if len(waiting) != 1 {
		fmt.Printf("aux_stream control MESSAGE for user %s matches %d sharing sessions, ignored\n", user, len(waiting))
		return
	}
	s.ConfirmScreenStream(waiting[0], ready)
}

// sessionLocalSIPUser is the SIP user this gateway session acts as.
func sessionLocalSIPUser(sess *session.Session) string {
	if _, _, _, _, username, _, _ := sess.GetSIPAuthContext(); strings.TrimSpace(username) != "" {
		return strings.TrimSpace(username)
	}
	direction, from, to, _ := sess.GetCallInfo()
	if strings.EqualFold(strings.TrimSpace(direction), "inbound") {
		return extractUsernameFromURI(to)
	}
	return extractUsernameFromURI(from)
}

// relayAuxKeyframeToBridgedPeer asks the sharing leg's client for a keyframe
// directly. The receiving leg also sends RTCP to the SIP peer, but whether
// that reaches the sharing leg for a second video stream is up to the PBX.
func (s *Server) relayAuxKeyframeToBridgedPeer(sess *session.Session) {
	peer := s.bridgedGatewayPeer(sess)
	if peer == nil {
		return
	}
	if aux := peer.AuxVideo(); aux != nil && aux.HasUplink() {
		aux.RequestKeyframeFromBrowser("bridged-peer-request")
	}
}

// closeAuxVideoForSwitch ends the auxiliary stream when the call's far end is
// switched to another agent. The new agent has not agreed to receive it, so
// the user shares again.
func (s *Server) closeAuxVideoForSwitch(sess *session.Session) {
	aux := sess.AuxVideo()
	if aux == nil {
		return
	}
	hadUplink := aux.HasUplink()
	hadDownlink := aux.HasDownlink()
	if hadUplink {
		if hadDownlink {
			aux.StopDownlink()
		}
		s.closeScreenStreamForPeer(sess, aux, "agent_switch")
		if hadDownlink {
			s.notifyAuxVideoState(sess.ID, false, "agent_switch", "remote")
		}
		return
	}
	if sess.CloseAuxVideo("agent-switch") {
		s.notifyAuxVideoState(sess.ID, false, "agent_switch", "remote")
	}
}

// logAuxStreamEvent records an auxiliary stream event for the session.
func (s *Server) logAuxStreamEvent(sess *session.Session, name string, data map[string]interface{}) {
	_, _, _, callID := sess.GetCallInfo()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      name,
		SIPCallID: callID,
		Data:      data,
	})
}
