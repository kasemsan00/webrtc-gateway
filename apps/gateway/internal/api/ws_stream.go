package api

import (
	"fmt"
	"strings"
	"time"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

// Client-originated auxiliary (screen) video stream: stream_update add/remove.
//
// Every answer is a stream_update_result. A failure never goes out as a
// generic "error" carrying the sessionId: older SDKs treat that as a terminal
// call failure and close the PeerConnection, and a refused screen share must
// leave the call as it was.

const (
	streamRoleScreen   = "screen"
	streamActionAdd    = "add"
	streamActionRemove = "remove"

	streamStatusOK     = "ok"
	streamStatusFailed = "failed"
)

// sipScreenStreamController negotiates the auxiliary video stream with the
// SIP peer (internal/sip/aux_stream.go).
type sipScreenStreamController interface {
	OpenScreenStream(sess *session.Session) sip.ScreenStreamOutcome
	CloseScreenStream(sess *session.Session, reason string) bool
	// ReportScreenStreamToPeer tells the sharing leg whether this session,
	// the receiving leg, shows its stream.
	ReportScreenStreamToPeer(sess *session.Session, ready bool)
}

func (s *Server) handleWSStreamUpdate(client *WSClient, msg WSMessage) {
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" && client != nil && !client.isAgentPresence() {
		// /ws clients may rely on their single current session, as they do
		// for renegotiate_answer.
		sessionID = client.sessionID
	}
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	action := strings.ToLower(strings.TrimSpace(msg.Action))

	reply := func(status, reason, sdp string) {
		s.sendWSMessage(client, WSMessage{
			Type:      "stream_update_result",
			SessionID: sessionID,
			Role:      role,
			Action:    action,
			Status:    status,
			Reason:    reason,
			SDP:       sdp,
		})
		fmt.Printf("[%s] stream_update_result role=%s action=%s status=%s reason=%s sdpBytes=%d\n",
			sessionID, role, action, status, reason, len(sdp))
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sessionID,
			Category:  "ws",
			Name:      "ws_stream_update",
			Data: map[string]interface{}{
				"role":   role,
				"action": action,
				"status": status,
				"reason": reason,
			},
		})
	}

	switch {
	case sessionID == "":
		reply(streamStatusFailed, "session_required", "")
		return
	case role != streamRoleScreen:
		reply(streamStatusFailed, "unsupported_role", "")
		return
	case action != streamActionAdd && action != streamActionRemove:
		reply(streamStatusFailed, "unsupported_action", "")
		return
	}

	s.mu.RLock()
	multiVideo := client.multiVideo && !client.publicOnly
	s.mu.RUnlock()
	if !multiVideo {
		reply(streamStatusFailed, "multi_video_not_enabled", "")
		return
	}
	if !s.clientOwnsSession(client, sessionID) {
		reply(streamStatusFailed, "forbidden", "")
		return
	}
	if s.sessionMgr == nil {
		reply(streamStatusFailed, "not_available", "")
		return
	}
	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok || sess == nil {
		reply(streamStatusFailed, "session_not_found", "")
		return
	}
	controller, ok := s.sipMaker.(sipScreenStreamController)
	if !ok || controller == nil {
		reply(streamStatusFailed, "not_available", "")
		return
	}
	if sess.GetState() != session.StateActive {
		reply(streamStatusFailed, "not_active", "")
		return
	}

	// The WebRTC side changes only on the first share of a call, when the
	// client's offer adds the second video section. Later shares reuse that
	// section and carry no SDP.
	offerSDP := strings.TrimSpace(msg.SDP)
	auxMid := ""
	if action == streamActionAdd {
		if offerSDP != "" {
			mid, ok := session.AuxVideoMidFromOffer(msg.SDP)
			if !ok {
				reply(streamStatusFailed, "invalid_offer", "")
				return
			}
			auxMid = mid
		} else if sess.AuxUplinkMid() == "" {
			reply(streamStatusFailed, "sdp_required", "")
			return
		}
	}

	started, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		Source:    "client_stream_" + action,
		Method:    "INVITE",
		StartedAt: time.Now(),
	})
	if !ok {
		reply(streamStatusFailed, "busy", "")
		return
	}
	finish := func(status, reason, sdp string) {
		// Clear the claim first so the client can act on the result at once.
		sess.CompleteMidCallRenegotiation(started.ID, "")
		reply(status, reason, sdp)
	}

	// The SIP round trip can take seconds (and a glare retry more). It must
	// not hold up this connection's other messages.
	go func() {
		if action == streamActionRemove {
			controller.CloseScreenStream(sess, "client-remove")
			finish(streamStatusOK, "", "")
			return
		}

		// SIP first: a peer that refuses the stream leaves the
		// PeerConnection untouched.
		outcome := controller.OpenScreenStream(sess)
		if outcome.Status != streamStatusOK {
			finish(outcome.Status, outcome.Reason, "")
			return
		}
		if offerSDP == "" {
			finish(streamStatusOK, "", "")
			return
		}

		// The mid is set before the offer is applied so the new track is
		// routed to the auxiliary pipeline, never to the camera's.
		sess.SetAuxUplinkMid(auxMid)
		answerSDP, err := sess.AnswerClientOfferInPlace(msg.SDP)
		if err != nil {
			fmt.Printf("[%s] stream_update in-place renegotiation failed: %v\n", sessionID, err)
			sess.SetAuxUplinkMid("")
			controller.CloseScreenStream(sess, "webrtc-renegotiation-failed")
			finish(streamStatusFailed, "webrtc_renegotiation_failed", "")
			return
		}
		finish(streamStatusOK, "", answerSDP)
	}()
}

// NotifyAuxVideoState tells the session's client that its auxiliary video
// stream started or stopped for a reason other than its own request. Only
// clients that opted in with multiVideo are told.
func (s *Server) NotifyAuxVideoState(sessionID string, active bool, reason string) {
	s.mu.RLock()
	client := s.wsClients[sessionID]
	optedIn := client != nil && client.multiVideo
	s.mu.RUnlock()
	if !optedIn {
		return
	}
	value := active
	s.sendWSMessage(client, WSMessage{
		Type:      "stream_state",
		SessionID: sessionID,
		Role:      streamRoleScreen,
		Active:    &value,
		Reason:    reason,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Category:  "ws",
		Name:      "ws_stream_state",
		Data: map[string]interface{}{
			"role":   streamRoleScreen,
			"active": active,
			"reason": reason,
		},
	})
}
