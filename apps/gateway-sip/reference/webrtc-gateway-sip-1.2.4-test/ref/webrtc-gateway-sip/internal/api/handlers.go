package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
)

// OfferRequest represents a WebRTC offer request
type OfferRequest struct {
	SDP       string `json:"sdp"`
	SessionID string `json:"sessionId,omitempty"`
}

// OfferResponse contains the WebRTC answer
type OfferResponse struct {
	SDP       string `json:"sdp"`
	SessionID string `json:"sessionId"`
}

// CallRequest represents an outbound call request
type CallRequest struct {
	SessionID   string `json:"sessionId"`
	Destination string `json:"destination"`
	From        string `json:"from,omitempty"`
}

// CallResponse contains call initiation result
type CallResponse struct {
	SessionID string `json:"sessionId"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
}

// SessionResponse represents session information
type SessionResponse struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Direction string `json:"direction,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// DTMFRequest represents a DTMF request
type DTMFRequest struct {
	Digits string `json:"digits"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

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
		direction, from, to, _ := sess.GetCallInfo()
		response[i] = SessionResponse{
			ID:        sess.ID,
			State:     string(sess.GetState()),
			Direction: direction,
			From:      from,
			To:        to,
			CreatedAt: sess.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: sess.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
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

	direction, from, to, _ := sess.GetCallInfo()
	response := SessionResponse{
		ID:        sess.ID,
		State:     string(sess.GetState()),
		Direction: direction,
		From:      from,
		To:        to,
		CreatedAt: sess.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: sess.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
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

// respondJSON sends a JSON response
func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError sends an error response
func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	s.respondJSON(w, status, ErrorResponse{Error: message})
}
