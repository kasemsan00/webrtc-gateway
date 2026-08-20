package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

// handleOffer processes WebRTC offer and returns answer
func (s *Server) handleOffer(w http.ResponseWriter, r *http.Request) {
	var req OfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.SDP == "" {
		s.respondError(w, http.StatusBadRequest, "SDP is required")
		return
	}

	// Create or get session
	var sess *session.Session
	var err error

	if req.SessionID != "" {
		var ok bool
		sess, ok = s.sessionMgr.GetSession(req.SessionID)
		if !ok {
			s.respondError(w, http.StatusNotFound, "Session not found")
			return
		}
	} else {
		sess, err = s.sessionMgr.CreateSession(s.turnConfig)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create session: %v", err))
			return
		}
	}

	ctx := r.Context()
	payloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sess.ID,
		Timestamp:   time.Now(),
		Kind:        "webrtc_sdp_offer",
		ContentType: "application/sdp",
		BodyText:    req.SDP,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "rest",
		Name:      "rest_offer_received",
		PayloadID: payloadID,
	})

	// Best-effort: cache H.264 SPS/PPS from Offer SDP (if present) so SIP SDP can include sprop-parameter-sets.
	if sps, pps, ok := session.ExtractH264SpropParameterSets(req.SDP); ok {
		sess.SetCachedSPSPPS(sps, pps, "rest-offer")
	}

	// Parse and set offer
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  req.SDP,
	}

	if err := sess.PeerConnection.SetRemoteDescription(offer); err != nil {
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "rest",
			Name:      "webrtc_set_remote_description_err",
			Data:      map[string]interface{}{"error": err.Error()},
		})
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to set offer: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "rest",
		Name:      "webrtc_set_remote_description_ok",
	})

	videoPacketizationMode := sess.GetSIPVideoPacketizationMode()
	if err := session.PreferWebRTCH264PacketizationMode(sess.PeerConnection, req.SDP, videoPacketizationMode); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to select H264 packetization mode: %v", err))
		return
	}
	fmt.Printf("[%s] 🎬 WebRTC H264 answer restricted to packetization-mode=%d (SIP leg)\n", sess.ID, videoPacketizationMode)

	// Create answer
	answer, err := sess.PeerConnection.CreateAnswer(nil)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create answer: %v", err))
		return
	}

	// Set local description
	if err := sess.PeerConnection.SetLocalDescription(answer); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to set local description: %v", err))
		return
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(sess.PeerConnection)
	<-gatherComplete

	sess.UpdateState(session.StateConnecting)
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "rest",
		Name:      "session_state_changed",
		State:     string(session.StateConnecting),
	})
	s.logSessionSnapshot(ctx, sess, "")

	// Return answer
	response := OfferResponse{
		SDP:       sess.PeerConnection.LocalDescription().SDP,
		SessionID: sess.ID,
	}
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
		Category:  "rest",
		Name:      "rest_answer_sent",
		PayloadID: answerPayloadID,
	})
	s.respondJSON(w, http.StatusOK, response)
}

// handleCall initiates an outbound call
func (s *Server) handleCall(w http.ResponseWriter, r *http.Request) {
	var req CallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.SessionID == "" {
		s.respondError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	if req.Destination == "" {
		s.respondError(w, http.StatusBadRequest, "Destination is required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(req.SessionID)
	if !ok {
		s.respondError(w, http.StatusNotFound, "Session not found")
		return
	}

	// Set call info
	sess.SetCallInfo("outbound", req.From, req.Destination, "")

	ctx := r.Context()

	// Trunk mode: resolve trunk, enforce auth, and record who is using it.
	if req.TrunkID > 0 || req.TrunkPublicID != "" {
		if s.trunkManager == nil {
			s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
			return
		}

		var trunkID int64
		if req.TrunkID > 0 {
			trunkID = req.TrunkID
			if _, found := s.trunkManager.GetTrunkByID(trunkID); !found {
				s.respondError(w, http.StatusNotFound, fmt.Sprintf("Trunk %d not found", trunkID))
				return
			}
		} else {
			normalized, ok := sip.NormalizeTrunkPublicID(req.TrunkPublicID)
			if !ok {
				s.respondError(w, http.StatusBadRequest, "Invalid trunkPublicId")
				return
			}
			resolved, found := s.trunkManager.GetTrunkIDByPublicID(normalized)
			if !found {
				s.respondError(w, http.StatusNotFound, fmt.Sprintf("Trunk %s not found", normalized))
				return
			}
			trunkID = resolved
		}

		validation := s.validateTrunkReadyForOutboundCall(ctx, trunkID)
		if validation.notFound {
			s.respondError(w, http.StatusNotFound, validation.reason)
			return
		}
		if validation.reason != "" {
			s.respondError(w, http.StatusConflict, fmt.Sprintf("Trunk not ready: %s", validation.reason))
			return
		}

		// Require an authenticated user to use a trunk.
		claims, hasClaims := AuthClaimsFromContext(ctx)
		if s.tokenVerifier != nil && !hasClaims {
			s.respondError(w, http.StatusUnauthorized, "Token authentication required to use trunk")
			return
		}

		// Record who is using this trunk.
		if hasClaims {
			username := claims.PreferredUsername
			if err := s.trunkManager.SetTrunkInUseBy(ctx, trunkID, &username); err != nil {
				log.Printf("⚠️ [REST Call] Failed to set in_use_by for trunk %d: %v", trunkID, err)
			}
		}

		sess.SetSIPAuthContext("trunk", "", trunkID, "", "", "", 0)
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "rest",
		Name:      "rest_call_request",
		Data:      map[string]interface{}{"destination": req.Destination, "from": req.From},
	})
	s.logSessionSnapshot(ctx, sess, "")

	// Make SIP call
	if s.sipMaker != nil {
		if err := s.sipMaker.MakeCall(req.Destination, req.From, sess); err != nil {
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "rest",
				Name:      "rest_call_failed",
				Data:      map[string]interface{}{"error": err.Error()},
			})
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to make call: %v", err))
			return
		}
	}

	response := CallResponse{
		SessionID: sess.ID,
		State:     string(sess.GetState()),
		Message:   "Call initiated",
	}
	s.respondJSON(w, http.StatusOK, response)
}

// handleHangup terminates a call
func (s *Server) handleHangup(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]

	if sessionID == "" {
		s.respondError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.respondError(w, http.StatusNotFound, "Session not found")
		return
	}

	// Send SIP BYE
	if s.sipMaker != nil {
		s.sipMaker.Hangup(sess)
	}

	ctx := r.Context()

	// Clear in_use_by for trunk sessions.
	authMode, _, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
	if authMode == "trunk" && trunkID > 0 && s.trunkManager != nil {
		if err := s.trunkManager.SetTrunkInUseBy(ctx, trunkID, nil); err != nil {
			log.Printf("⚠️ [REST Hangup] Failed to clear in_use_by for trunk %d: %v", trunkID, err)
		}
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "rest",
		Name:      "rest_hangup_request",
	})
	s.logSessionSnapshot(ctx, sess, "rest_hangup")

	// Delete session
	s.sessionMgr.DeleteSession(sessionID)

	response := CallResponse{
		SessionID: sessionID,
		State:     string(session.StateEnded),
		Message:   "Call ended",
	}
	s.respondJSON(w, http.StatusOK, response)
}

// handleListSessions returns all active sessions
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.sessionMgr.ListSessions()

	response := make([]SessionResponse, len(sessions))
	for i, sess := range sessions {
		direction, from, to, sipCallID := sess.GetCallInfo()
		authMode, _, trunkID, _, sipUsername, _, _ := sess.GetSIPAuthContext()
		snap := sess.Snapshot()

		durationSec := int64(time.Since(sess.CreatedAt).Seconds())

		var trunkName string
		if authMode == "trunk" && trunkID > 0 && s.trunkManager != nil {
			if trunk, err := s.trunkManager.GetTrunkByIDFromDB(r.Context(), trunkID); err == nil {
				trunkName = trunk.Name
			}
		}

		response[i] = SessionResponse{
			ID:                 sess.ID,
			State:              string(sess.GetState()),
			Direction:          direction,
			From:               from,
			To:                 to,
			SIPCallID:          sipCallID,
			AuthMode:           authMode,
			TrunkID:            trunkID,
			TrunkName:          trunkName,
			SIPUsername:        sipUsername,
			DurationSec:        durationSec,
			CreatedAt:          sess.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:          sess.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			TranslatorEnabled:  snap.TranslatorEnabled,
			TranslatorSrcLang:  snap.TranslatorSrcLang,
			TranslatorTgtLang:  snap.TranslatorTgtLang,
			TranslatorTTSVoice: snap.TranslatorTTSVoice,
		}
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleGetSession returns a specific session
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]

	if sessionID == "" {
		s.respondError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.respondError(w, http.StatusNotFound, "Session not found")
		return
	}

	direction, from, to, sipCallID := sess.GetCallInfo()
	authMode, _, trunkID, _, sipUsername, _, _ := sess.GetSIPAuthContext()
	snap := sess.Snapshot()

	durationSec := int64(time.Since(sess.CreatedAt).Seconds())

	var trunkName string
	if authMode == "trunk" && trunkID > 0 && s.trunkManager != nil {
		if trunk, err := s.trunkManager.GetTrunkByIDFromDB(r.Context(), trunkID); err == nil {
			trunkName = trunk.Name
		}
	}

	response := SessionResponse{
		ID:                 sess.ID,
		State:              string(sess.GetState()),
		Direction:          direction,
		From:               from,
		To:                 to,
		SIPCallID:          sipCallID,
		AuthMode:           authMode,
		TrunkID:            trunkID,
		TrunkName:          trunkName,
		SIPUsername:        sipUsername,
		DurationSec:        durationSec,
		CreatedAt:          sess.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:          sess.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		TranslatorEnabled:  snap.TranslatorEnabled,
		TranslatorSrcLang:  snap.TranslatorSrcLang,
		TranslatorTgtLang:  snap.TranslatorTgtLang,
		TranslatorTTSVoice: snap.TranslatorTTSVoice,
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleDTMF sends DTMF tones
func (s *Server) handleDTMF(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]

	if sessionID == "" {
		s.respondError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	var req DTMFRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Digits == "" {
		s.respondError(w, http.StatusBadRequest, "Digits are required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.respondError(w, http.StatusNotFound, "Session not found")
		return
	}

	if s.sipMaker != nil {
		if err := s.sipMaker.SendDTMF(sess, req.Digits); err != nil {
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to send DTMF: %v", err))
			return
		}
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "rest",
		Name:      "rest_dtmf_request",
		Data:      map[string]interface{}{"digits": req.Digits},
	})

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleSwitch triggers @switch media-recovery behavior for a session.
func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request) {
	if s.sipMaker == nil {
		s.respondError(w, http.StatusServiceUnavailable, "SIP call maker not available")
		return
	}

	var req SwitchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	req.SessionID = strings.TrimSpace(req.SessionID)
	req.QueueNumber = strings.TrimSpace(req.QueueNumber)
	req.AgentUsername = strings.TrimSpace(req.AgentUsername)

	if req.SessionID == "" {
		s.respondError(w, http.StatusBadRequest, "Session ID is required")
		return
	}
	if (req.QueueNumber == "") != (req.AgentUsername == "") {
		s.respondError(w, http.StatusBadRequest, "queueNumber and agentUsername must be provided together")
		return
	}

	sess, ok := s.sessionMgr.GetSession(req.SessionID)
	if !ok {
		s.respondError(w, http.StatusNotFound, "Session not found")
		return
	}

	_, fromField, _, _ := sess.GetCallInfo()
	callerURI := strings.TrimSpace(fromField)
	if callerURI == "" {
		s.respondError(w, http.StatusBadRequest, "Session caller identifier is missing")
		return
	}

	autoMode := req.QueueNumber == "" && req.AgentUsername == ""
	if autoMode {
		req.QueueNumber = "force send PLI"
		req.AgentUsername = "force send PLI"
	}

	body := fmt.Sprintf("@switch:%s|%s", req.QueueNumber, req.AgentUsername)
	if err := s.sipMaker.TriggerSwitchMessage(body, callerURI); err != nil {
		log.Printf("⚠️ Failed to trigger switch via REST: session=%s caller=%s err=%v", req.SessionID, callerURI, err)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "rest",
			Name:      "rest_switch_trigger_failed",
			Data: map[string]interface{}{
				"callerURI": callerURI,
				"error":     err.Error(),
			},
		})
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to trigger switch: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "rest",
		Name:      "rest_switch_request",
		Data: map[string]interface{}{
			"queueNumber":   req.QueueNumber,
			"agentUsername": req.AgentUsername,
			"callerURI":     callerURI,
			"autoMode":      autoMode,
		},
	})

	s.respondJSON(w, http.StatusAccepted, SwitchResponse{
		Status:        "accepted",
		SessionID:     req.SessionID,
		QueueNumber:   req.QueueNumber,
		AgentUsername: req.AgentUsername,
		AutoMode:      autoMode,
	})
}
