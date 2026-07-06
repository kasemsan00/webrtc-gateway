package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
)

type trunkOutboundValidation struct {
	trunk    *sip.Trunk
	notFound bool
	reason   string
}

// handleWSoffer handles WebSocket offer messages
func (s *Server) handleWSoffer(client *WSClient, msg WSMessage) {
	// Create or get session
	var sess *session.Session
	var err error

	if msg.SessionID != "" {
		var ok bool
		sess, ok = s.sessionMgr.GetSession(msg.SessionID)
		if !ok {
			s.sendWSError(client, msg.SessionID, "Session not found")
			return
		}
	} else {
		sess, err = s.sessionMgr.CreateSession(s.turnConfig)
		if err != nil {
			s.sendWSError(client, "", fmt.Sprintf("Failed to create session: %v", err))
			return
		}
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
		Name:      "ws_offer_received",
		PayloadID: payloadID,
	})

	// Associate client with session
	client.sessionID = sess.ID
	s.mu.Lock()
	s.wsClients[sess.ID] = client
	s.mu.Unlock()

	// Best-effort: cache H.264 SPS/PPS from Offer SDP (if present) so SIP SDP can include sprop-parameter-sets.
	if sps, pps, ok := session.ExtractH264SpropParameterSets(msg.SDP); ok {
		sess.SetCachedSPSPPS(sps, pps, "ws-offer")
	}

	// Parse and set offer
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  msg.SDP,
	}

	if err := sess.PeerConnection.SetRemoteDescription(offer); err != nil {
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "ws",
			Name:      "webrtc_set_remote_description_err",
			Data:      map[string]interface{}{"error": err.Error()},
		})
		s.sendWSError(client, sess.ID, fmt.Sprintf("Failed to set offer: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "webrtc_set_remote_description_ok",
	})

	// Create answer
	answer, err := sess.PeerConnection.CreateAnswer(nil)
	if err != nil {
		s.sendWSError(client, sess.ID, fmt.Sprintf("Failed to create answer: %v", err))
		return
	}

	// Set local description
	if err := sess.PeerConnection.SetLocalDescription(answer); err != nil {
		s.sendWSError(client, sess.ID, fmt.Sprintf("Failed to set local description: %v", err))
		return
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(sess.PeerConnection)
	<-gatherComplete

	// Send answer with video configuration
	response := WSMessage{
		Type:      "answer",
		SessionID: sess.ID,
		SDP:       sess.PeerConnection.LocalDescription().SDP,
	}
	s.sendWSMessage(client, response)

	answerPayloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sess.ID,
		Timestamp:   time.Now(),
		Kind:        "webrtc_sdp_answer",
		ContentType: "application/sdp",
		BodyText:    response.SDP,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_answer_sent",
		PayloadID: answerPayloadID,
	})

	sess.UpdateState(session.StateConnecting)
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "session_state_changed",
		State:     string(session.StateConnecting),
	})
	s.logSessionSnapshot(ctx, sess, "")
}

// handleWSIce handles trickle ICE candidates from WebRTC clients.
func (s *Server) handleWSIce(client *WSClient, msg WSMessage) {
	if len(msg.Candidate) == 0 || string(msg.Candidate) == "null" {
		return
	}

	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = client.sessionID
	}
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required for ICE candidate")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.sendWSError(client, sessionID, "Session not found")
		return
	}
	if sess.PeerConnection == nil {
		s.sendWSError(client, sessionID, "PeerConnection not available")
		return
	}

	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal(msg.Candidate, &candidate); err != nil {
		s.sendWSError(client, sessionID, fmt.Sprintf("Invalid ICE candidate: %v", err))
		return
	}
	if strings.TrimSpace(candidate.Candidate) == "" {
		return
	}
	if sess.PeerConnection.RemoteDescription() == nil {
		s.sendWSError(client, sessionID, "Remote description not set for ICE candidate")
		return
	}

	if err := sess.PeerConnection.AddICECandidate(candidate); err != nil {
		s.sendWSError(client, sessionID, fmt.Sprintf("Failed to add ICE candidate: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Category:  "ice",
		Name:      "ws_ice_candidate_added",
	})
}

func (s *Server) validateTrunkReadyForOutboundCall(ctx context.Context, trunkID int64) trunkOutboundValidation {
	if s.trunkManager == nil {
		return trunkOutboundValidation{reason: "Trunk manager not available"}
	}

	trunk, err := s.trunkManager.GetTrunkByIDFromDB(ctx, trunkID)
	if err != nil || trunk == nil {
		return trunkOutboundValidation{
			notFound: true,
			reason:   fmt.Sprintf("Trunk %d not found", trunkID),
		}
	}

	if trunk.LeaseOwner == nil || strings.TrimSpace(*trunk.LeaseOwner) == "" || trunk.LeaseUntil == nil || trunk.LeaseUntil.Before(time.Now()) {
		return trunkOutboundValidation{reason: "Trunk lease not active"}
	}

	leaseOwner := strings.TrimSpace(*trunk.LeaseOwner)
	if leaseOwner != s.gatewayConfig.InstanceID {
		return trunkOutboundValidation{
			reason: fmt.Sprintf("Trunk is owned by another gateway instance (%s)", leaseOwner),
		}
	}

	return trunkOutboundValidation{trunk: trunk}
}

// handleWSCall handles WebSocket call messages
func (s *Server) handleWSCall(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}

	if msg.Destination == "" {
		s.sendWSError(client, msg.SessionID, "Destination required")
		return
	}

	ctx := context.Background()

	// Determine SIP auth mode: public vs trunk
	var authMode string
	var accountKey string
	var trunkID int64
	var trunkPublicID string
	useResolvedConnectionTrunk := msg.TrunkID == 0 &&
		strings.TrimSpace(msg.TrunkPublicID) == "" &&
		strings.TrimSpace(msg.SIPDomain) == "" &&
		strings.TrimSpace(msg.SIPUsername) == "" &&
		strings.TrimSpace(msg.SIPPassword) == "" &&
		client != nil &&
		client.trunkResolved &&
		client.resolvedTrunkID > 0

	if client != nil && client.publicOnly {
		if msg.TrunkID > 0 || strings.TrimSpace(msg.TrunkPublicID) != "" || useResolvedConnectionTrunk {
			s.sendWSError(client, msg.SessionID, "Trunk calls require authenticated WebSocket")
			return
		}
		if strings.TrimSpace(msg.SIPDomain) == "" ||
			strings.TrimSpace(msg.SIPUsername) == "" ||
			strings.TrimSpace(msg.SIPPassword) == "" {
			s.sendWSError(client, msg.SessionID, "Public SIP credentials required on public WebSocket")
			return
		}
	}

	if msg.TrunkID > 0 || msg.TrunkPublicID != "" || useResolvedConnectionTrunk {
		// Trunk mode: use trunk from DB
		authMode = "trunk"
		if s.trunkManager == nil {
			s.sendWSError(client, msg.SessionID, "Trunk manager not available")
			return
		}
		if useResolvedConnectionTrunk {
			trunkID = client.resolvedTrunkID
		} else if msg.TrunkID > 0 {
			trunkID = msg.TrunkID
		} else {
			normalized, ok := sip.NormalizeTrunkPublicID(msg.TrunkPublicID)
			if !ok {
				s.sendWSError(client, msg.SessionID, "Invalid trunkPublicId")
				return
			}
			trunkPublicID = normalized
			resolvedID, found := s.trunkManager.GetTrunkIDByPublicID(trunkPublicID)
			if !found {
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk %s not found", trunkPublicID))
				return
			}
			trunkID = resolvedID
		}

		if !client.trunkResolved || client.resolvedTrunkID != trunkID {
			s.sendWSError(client, msg.SessionID, "Trunk must be resolved before placing call")
			return
		}

		validation := s.validateTrunkReadyForOutboundCall(ctx, trunkID)
		if validation.notFound {
			s.sendWSError(client, msg.SessionID, validation.reason)
			return
		}
		if validation.reason != "" {
			client.trunkResolved = false
			client.resolvedTrunkID = 0
			s.sendWSMessage(client, WSMessage{Type: "trunk_not_ready", Reason: validation.reason})
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not ready: %s", validation.reason))
			return
		}
		if validation.trunk != nil {
			trunkPublicID = validation.trunk.PublicID
		}

		// Set trunk auth context in session
		sess.SetSIPAuthContext("trunk", "", trunkID, "", "", "", 0)

		// Require authenticated user to use a trunk.
		if s.tokenVerifier != nil && client.authClaims == nil {
			s.sendWSError(client, msg.SessionID, "Token authentication required to use trunk")
			return
		}

		// Record who is using this trunk.
		if client.authClaims != nil && s.trunkManager != nil {
			username := client.authClaims.PreferredUsername
			if err := s.trunkManager.SetTrunkInUseBy(ctx, trunkID, &username); err != nil {
				log.Printf("⚠️ [WS Call] Failed to set in_use_by for trunk %d: %v", trunkID, err)
			}
		}

		log.Printf(
			"📞 [WS Call] Session %s using trunk mode (trunkId=%d, trunkPublicId=%s)",
			sess.ID, trunkID, trunkPublicID,
		)

	} else if msg.SIPDomain != "" && msg.SIPUsername != "" && msg.SIPPassword != "" {
		// Public mode: register and bind credentials to session
		authMode = "public"

		// Use port as-is from client (0 means "not specified" for hostname domains)
		// This allows DNS SRV resolution for hostnames without explicit port
		port := msg.SIPPort

		// Guard against identity switches on an existing public session.
		// Public username/domain changes require a new offer/session.
		existingMode, _, _, existingDomain, existingUsername, _, _ := sess.GetSIPAuthContext()
		if existingMode == "public" && (existingUsername != "" || existingDomain != "") {
			if existingUsername != msg.SIPUsername || !strings.EqualFold(existingDomain, msg.SIPDomain) {
				const errMsg = "Public SIP identity changed (username/domain). Send a new offer to create a new session."
				s.logEvent(&logstore.Event{
					Timestamp: time.Now(),
					SessionID: sess.ID,
					Category:  "ws",
					Name:      "ws_call_rejected_identity_change",
					Data: map[string]interface{}{
						"existingSipUsername": existingUsername,
						"existingSipDomain":   existingDomain,
						"newSipUsername":      msg.SIPUsername,
						"newSipDomain":        msg.SIPDomain,
					},
				})
				s.sendWSError(client, msg.SessionID, errMsg)
				return
			}
		}

		// Acquire and register account
		if s.publicRegistry != nil {
			var err error
			accountKey, err = s.publicRegistry.AcquireAndRegister(ctx, msg.SIPDomain, msg.SIPUsername, msg.SIPPassword, port)
			if err != nil {
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to register SIP account: %v", err))
				return
			}

			// Increment ref count for active call
			s.publicRegistry.IncrementRefCount(accountKey)
		}

		// Set public auth context in session
		sess.SetSIPAuthContext("public", accountKey, 0, msg.SIPDomain, msg.SIPUsername, msg.SIPPassword, port)

		log.Printf("📞 [WS Call] Session %s using public mode (account=%s, port=%d)", sess.ID, accountKey, port)

	} else {
		// Legacy mode: use global/dynamic SIP credentials (backward compatibility)
		authMode = ""
		log.Printf("📞 [WS Call] Session %s using legacy mode (global credentials)", sess.ID)
	}

	// Set call info
	sess.SetCallInfo("outbound", msg.From, msg.Destination, "")

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_call_request",
		Data: map[string]interface{}{
			"destination":   msg.Destination,
			"from":          msg.From,
			"authMode":      authMode,
			"accountKey":    accountKey,
			"trunkId":       trunkID,
			"trunkPublicId": trunkPublicID,
		},
	})
	s.logSessionSnapshot(ctx, sess, "")

	// Upsert session directory entry
	if s.logStore != nil && s.gatewayConfig.InstanceID != "" && s.gatewayConfig.PublicWSURL != "" {
		ttl := 7200 // 2 hours default
		if err := s.logStore.UpsertSessionDirectory(ctx, sess.ID, s.gatewayConfig.InstanceID, s.gatewayConfig.PublicWSURL, ttl); err != nil {
			log.Printf("⚠️ Failed to upsert session directory for %s: %v", sess.ID, err)
		}
	}

	// Make SIP call asynchronously so the read loop can process hangup while
	// the outbound INVITE is still pending and translate it to SIP CANCEL.
	if s.sipMaker != nil {
		go s.runWSCall(client, msg, sess, authMode, accountKey)
	}

	// Send state update
	response := WSMessage{
		Type:      "state",
		SessionID: sess.ID,
		State:     string(sess.GetState()),
	}
	s.sendWSMessage(client, response)
}

func (s *Server) runWSCall(client *WSClient, msg WSMessage, sess *session.Session, authMode, accountKey string) {
	if err := s.sipMaker.MakeCall(msg.Destination, msg.From, sess); err != nil {
		if sess.IsPendingCancelRequested() {
			log.Printf("[%s] Suppressing MakeCall error after local CANCEL: %v", sess.ID, err)
			return
		}
		if s.sessionMgr != nil {
			if _, ok := s.sessionMgr.GetSession(sess.ID); !ok {
				log.Printf("[%s] Suppressing MakeCall error after session cleanup: %v", sess.ID, err)
				return
			}
		}

		if authMode == "public" && accountKey != "" && s.publicRegistry != nil {
			s.publicRegistry.DecrementRefCount(accountKey)
		}

		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "ws",
			Name:      "ws_call_failed",
			Data:      map[string]interface{}{"error": err.Error()},
		})
		s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to make call: %v", err))
	}
}

// handleWSHangup handles WebSocket hangup messages
func (s *Server) handleWSHangup(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}

	ctx := context.Background()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_hangup_request",
	})
	s.logSessionSnapshot(ctx, sess, "ws_hangup")

	// Send state=ended to browser IMMEDIATELY so the frontend knows the
	// call is ending. This must happen BEFORE Hangup() because Hangup()
	// blocks waiting for the SIP BYE response (up to 10 s timeout).
	response := WSMessage{
		Type:      "state",
		SessionID: msg.SessionID,
		State:     string(session.StateEnded),
	}
	s.sendWSMessage(client, response)

	state := sess.GetState()
	hasDialog := sess.HasDialogState()
	action := "end"
	switch {
	case state == session.StateIncoming:
		action = "reject"
	case hasDialog || state == session.StateActive:
		action = "bye"
	case state == session.StateConnecting || state == session.StateRinging:
		action = "cancel"
	}
	if !sess.TryBeginTerminalAction(action) {
		log.Printf("[%s] Duplicate hangup ignored (requested=%s winner=%s)", msg.SessionID, action, sess.GetTerminalAction())
		return
	}
	s.logTerminalAction(sess, action, 0, msg.Reason, "client")

	// Send the SIP method appropriate for the current signaling state.
	if s.sipMaker != nil {
		switch {
		case state == session.StateIncoming:
			if err := s.sipMaker.RejectCall(sess, "busy"); err != nil {
				log.Printf("[%s] Reject-on-hangup error: %v", msg.SessionID, err)
			}
			s.incrementIncomingCounter("incoming_rejected")
		case hasDialog || state == session.StateActive:
			if err := s.sipMaker.Hangup(sess); err != nil {
				log.Printf("[%s] Hangup error: %v", msg.SessionID, err)
			}
			s.incrementIncomingCounter("bye")
		case state == session.StateConnecting || state == session.StateRinging:
			if err := s.sipMaker.CancelPendingCall(sess); err != nil {
				log.Printf("[%s] Cancel pending call error: %v", msg.SessionID, err)
			}
			s.incrementIncomingCounter("outgoing_canceled")
		default:
			sess.UpdateState(session.StateEnded)
		}
	} else {
		sess.UpdateState(session.StateEnded)
	}

	// Decrement public account refcount if applicable (before deleting session)
	authMode, accountKey, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
	if authMode == "public" && accountKey != "" && s.publicRegistry != nil {
		s.publicRegistry.DecrementRefCount(accountKey)
	}

	// Clear in_use_by for trunk sessions.
	if authMode == "trunk" && trunkID > 0 && s.trunkManager != nil {
		if err := s.trunkManager.SetTrunkInUseBy(context.Background(), trunkID, nil); err != nil {
			log.Printf("⚠️ [WS Hangup] Failed to clear in_use_by for trunk %d: %v", trunkID, err)
		}
	}

	// Delete session after BYE is sent
	s.sessionMgr.DeleteSession(msg.SessionID)
}

// handleWSDTMF handles WebSocket DTMF messages
func (s *Server) handleWSDTMF(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_dtmf_request",
		Data:      map[string]interface{}{"digits": msg.Digits},
	})

	if s.sipMaker != nil {
		if err := s.sipMaker.SendDTMF(sess, msg.Digits); err != nil {
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to send DTMF: %v", err))
			return
		}
	}
}
