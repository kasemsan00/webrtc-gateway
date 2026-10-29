package api

import (
	"context"
	"errors"
	"log"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

// Prepared incoming offers (`offer` with `prepare: true`).
//
// A presented client negotiates a PeerConnection (ICE+DTLS) for a ringing
// incoming session without claiming it. The call is claimed only by
// `accept`, which session.AcceptPreparedIncoming decides atomically. The
// gateway closes every prepared PC that did not win; see
// internal/session/prepared_pc.go.

func (s *Server) webrtcPrepareTTL() time.Duration {
	if s.config.WebRTCPrepareTTL <= 0 {
		return config.DefaultWebRTCPrepareTTL
	}
	return s.config.WebRTCPrepareTTL
}

func (s *Server) handleWSPrepareOffer(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Incoming sessionId required for prepare offer")
		return
	}
	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}
	if sess.GetState() != session.StateIncoming {
		s.sendWSMessage(client, WSMessage{Type: "state", SessionID: sess.ID, State: string(sess.GetState())})
		return
	}
	// Only a connection the call was presented to may prepare for it.
	if !s.clientHasPendingIncoming(client, sess.ID) {
		s.sendWSError(client, sess.ID, "Incoming call was not presented to this connection")
		return
	}

	ctx := context.Background()
	payloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sess.ID,
		Timestamp:   time.Now(),
		Kind:        "webrtc_sdp_offer",
		ContentType: "application/sdp",
		BodyText:    msg.SDP,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_prepare_offer_received",
		PayloadID: payloadID,
	})

	sessionID := sess.ID
	onClosed := func(reason string) {
		s.sendWSMessage(client, WSMessage{Type: "cancel", SessionID: sessionID, Reason: reason})
	}
	ttl := s.webrtcPrepareTTL()
	answerSDP, err := sess.PrepareIncomingAnswer(wsClientClaimID(client), msg.SDP, s.turnConfig, s.config.DebugTURN, ttl, onClosed)
	if err != nil {
		log.Printf("[%s] ⚠️ [Prepare] prepare offer failed client=%s: %v", sessionID, client.clientID, err)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sessionID,
			Category:  "ws",
			Name:      "ws_prepare_offer_failed",
			Data:      map[string]interface{}{"error": err.Error()},
		})
		switch {
		case errors.Is(err, session.ErrPrepareAnsweredElsewhere):
			s.sendIncomingAnsweredElsewhere(client, sessionID)
		case errors.Is(err, session.ErrPrepareNotIncoming), errors.Is(err, session.ErrPreparedNotFound):
			// The gateway already sent cancel if the call left ringing.
			s.sendWSMessage(client, WSMessage{Type: "state", SessionID: sessionID, State: string(sess.GetState())})
		default:
			s.sendWSError(client, sessionID, err.Error())
		}
		return
	}

	s.sendWSMessage(client, WSMessage{Type: "answer", SessionID: sessionID, SDP: answerSDP, Prepare: true})
	answerPayloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sessionID,
		Timestamp:   time.Now(),
		Kind:        "webrtc_sdp_answer",
		ContentType: "application/sdp",
		BodyText:    answerSDP,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Category:  "ws",
		Name:      "ws_prepare_answer_sent",
		PayloadID: answerPayloadID,
		Data:      map[string]interface{}{"ttlMs": ttl.Milliseconds()},
	})
	log.Printf("[%s] ✅ [Prepare] prepared PC answered client=%s ttl=%s", sessionID, client.clientID, ttl)
}

// acceptPreparedIncoming completes accept for a client that holds a prepared
// PC. The session decides the race and installs the PC; the call keeps the
// incoming sessionId.
func (s *Server) acceptPreparedIncoming(client *WSClient, incomingSess *session.Session, msg WSMessage) {
	clientID := wsClientClaimID(client)
	losers, err := incomingSess.AcceptPreparedIncoming(clientID)
	if err != nil {
		log.Printf("⚠️ [Accept] prepared accept lost session=%s client=%s: %v", msg.SessionID, client.clientID, err)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: incomingSess.ID,
			Category:  "ws",
			Name:      "ws_incoming_action_result",
			Data: map[string]interface{}{
				"incomingAction": "sending_accept",
				"prepared":       true,
				"result":         err.Error(),
				"sessionId":      msg.SessionID,
			},
		})
		switch {
		case errors.Is(err, session.ErrPrepareAnsweredElsewhere):
			s.sendIncomingAnsweredElsewhere(client, msg.SessionID)
		case errors.Is(err, session.ErrPreparedNotFound):
			s.sendWSError(client, msg.SessionID, "Prepared WebRTC session closed; send a new offer with prepare=true")
		default:
			s.sendWSMessage(client, WSMessage{Type: "state", SessionID: msg.SessionID, State: string(incomingSess.GetState())})
		}
		return
	}
	s.completeIncomingAccept(client, incomingSess, incomingSess, msg, losers)
	incomingSess.KickMediaAfterPreparedAccept()
}

func (s *Server) clientHasPreparedPC(client *WSClient, sessionID string) bool {
	if client == nil || s.sessionMgr == nil {
		return false
	}
	sess, ok := s.sessionMgr.GetSession(sessionID)
	return ok && sess != nil && sess.HasPreparedPeerConnection(wsClientClaimID(client))
}

// dropClientPreparedPeerConnections closes every prepared PC a disconnected
// WebSocket left behind. The ringing call itself is not affected.
func (s *Server) dropClientPreparedPeerConnections(client *WSClient) {
	if client == nil || s.sessionMgr == nil {
		return
	}
	owner := wsClientClaimID(client)
	for _, sess := range s.sessionMgr.ListSessions() {
		if sess != nil && sess.DropPreparedPeerConnection(owner) {
			log.Printf("[%s] 🧊 [Prepare] closed prepared PC on WebSocket disconnect client=%s", sess.ID, client.clientID)
		}
	}
}
