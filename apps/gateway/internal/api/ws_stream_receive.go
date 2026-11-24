package api

import (
	"fmt"
	"strings"
	"time"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

// Receiving side of the auxiliary (screen) video stream: the far end shares
// its screen, the SIP side of this session has accepted the second m=video,
// and the client now has to add a receive-only video section.

// CanReceiveAuxVideo reports whether the session's client opted in to a
// second video stream. The SIP side asks before accepting the peer's offer.
func (s *Server) CanReceiveAuxVideo(sessionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	client := s.wsClients[sessionID]
	return client != nil && client.multiVideo && !client.publicOnly
}

// NotifyAuxVideoOffered runs once the SIP side of a received screen stream is
// open. On the first share of a call the client is asked to renegotiate: it
// adds a receive-only video section and offers, as in agent_switch, but the
// offer is applied on the live PeerConnection. A later share reuses that
// section, so the stream is attached at once and the client is only told.
func (s *Server) NotifyAuxVideoOffered(sessionID string) {
	if s.sessionMgr == nil {
		return
	}
	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok || sess == nil {
		return
	}
	s.mu.RLock()
	client := s.wsClients[sessionID]
	s.mu.RUnlock()
	if client == nil {
		s.abortReceivedAuxStream(sess, "client-missing")
		return
	}

	if sess.HasNegotiatedAuxDownlink() {
		if err := sess.AttachAuxDownlinkTrack(); err != nil {
			fmt.Printf("[%s] stream_added reattach failed: %v\n", sessionID, err)
			s.abortReceivedAuxStream(sess, "reattach-failed")
			return
		}
		s.NotifyAuxVideoState(sessionID, true, "")
		s.reportScreenStream(sess, true)
		return
	}

	// The send-only transceiver must exist before the client's offer is
	// applied, so the new section pairs with it.
	if err := sess.PrepareAuxDownlinkTrack(); err != nil {
		fmt.Printf("[%s] stream_added prepare failed: %v\n", sessionID, err)
		s.abortReceivedAuxStream(sess, "prepare-failed")
		return
	}
	started, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		Source:    session.MidCallRenegotiationSourceStreamAdded,
		Method:    "INVITE",
		StartedAt: time.Now(),
		Timeout:   defaultMidCallRenegotiationTimeout,
	})
	if !ok {
		fmt.Printf("[%s] stream_added skipped: another renegotiation is pending\n", sessionID)
		s.abortReceivedAuxStream(sess, "renegotiation-busy")
		return
	}
	s.scheduleMidCallRenegotiationTimeout(sessionID, started.ID, defaultMidCallRenegotiationTimeout)
	s.sendWSMessage(client, WSMessage{
		Type:            "renegotiate",
		SessionID:       sessionID,
		RenegotiationID: started.ID,
		Reason:          "stream_added",
		Role:            streamRoleScreen,
		MediaDirection:  "recvonly",
		HasVideo:        "true",
		RequiresAnswer:  true,
		Status:          "pending",
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Category:  "ws",
		Name:      "ws_stream_added_renegotiate",
		Data:      map[string]interface{}{"renegotiationId": started.ID, "role": streamRoleScreen},
	})
}

// handleStreamAddedAnswer completes the client side of a received screen
// stream: the client's offer is applied in place and the stream is attached
// to the track that offer paired with.
func (s *Server) handleStreamAddedAnswer(client *WSClient, sess *session.Session, pending session.MidCallRenegotiationSnapshot, msg WSMessage, status string) {
	result := func(resultStatus, reason, sdp string) {
		s.sendWSMessage(client, WSMessage{
			Type:            "renegotiate_result",
			SessionID:       sess.ID,
			RenegotiationID: pending.ID,
			Status:          resultStatus,
			Reason:          reason,
			SDP:             sdp,
		})
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "ws",
			Name:      "ws_midcall_renegotiation_answer",
			Data: map[string]interface{}{
				"renegotiationId": pending.ID,
				"status":          resultStatus,
				"reason":          reason,
				"source":          pending.Source,
			},
		})
	}

	switch status {
	case "ok":
	case "failed":
		if !sess.CompleteMidCallRenegotiation(pending.ID, "") {
			s.sendWSError(client, sess.ID, "Renegotiation not pending or mismatched")
			return
		}
		result("failed", msg.Reason, "")
		s.abortReceivedAuxStream(sess, "client-failed")
		return
	default:
		s.sendWSError(client, sess.ID, "Unsupported renegotiation answer status")
		return
	}

	if strings.TrimSpace(msg.SDP) == "" {
		// The contract for stream_added is a client offer.
		sess.CompleteMidCallRenegotiation(pending.ID, "")
		result("failed", "sdp_required", "")
		s.abortReceivedAuxStream(sess, "missing-sdp")
		return
	}
	answerSDP, err := sess.AnswerClientOfferInPlace(msg.SDP)
	if err == nil {
		err = sess.AttachAuxDownlinkTrack()
	}
	// The claim is released with Complete in every case: Fail would also
	// abort a make-before-break switch, which this renegotiation is not.
	sess.CompleteMidCallRenegotiation(pending.ID, answerSDP)
	if err != nil {
		fmt.Printf("[%s] stream_added renegotiation failed: %v\n", sess.ID, err)
		result("failed", err.Error(), "")
		s.abortReceivedAuxStream(sess, "answer-failed")
		return
	}
	result("ok", "", answerSDP)
	s.reportScreenStream(sess, true)
}

func (s *Server) abortReceivedAuxStream(sess *session.Session, reason string) {
	if sess != nil {
		sess.CloseAuxVideo(reason)
	}
	s.reportScreenStream(sess, false)
}

// reportScreenStream tells the sharing leg whether this session shows its
// screen. It runs off the caller's goroutine: the report can involve SIP I/O.
func (s *Server) reportScreenStream(sess *session.Session, ready bool) {
	controller, ok := s.sipMaker.(sipScreenStreamController)
	if !ok || controller == nil || sess == nil {
		return
	}
	go controller.ReportScreenStreamToPeer(sess, ready)
}

// handleAuxAfterResume acts on an auxiliary stream that could not move to the
// PeerConnection built for a resume, because the client's offer had no second
// video section. A sharing session closes its SIP m-line; a receiving session
// tells the sharing leg, which then closes it.
func (s *Server) handleAuxAfterResume(sess *session.Session) {
	outcome := sess.TakeAuxResumeOutcome()
	if !outcome.Lost {
		return
	}
	controller, ok := s.sipMaker.(sipScreenStreamController)
	if !ok || controller == nil {
		return
	}
	fmt.Printf("[%s] aux_stream lost on resume sending=%v\n", sess.ID, outcome.Sending)
	go func() {
		if outcome.Sending {
			controller.CloseScreenStream(sess, "resume")
		} else {
			controller.ReportScreenStreamToPeer(sess, false)
		}
		s.NotifyAuxVideoState(sess.ID, false, "resume")
	}()
}
