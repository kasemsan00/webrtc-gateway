package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
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
	SessionID     string `json:"sessionId"`
	Destination   string `json:"destination"`
	From          string `json:"from,omitempty"`
	TrunkID       int64  `json:"trunkId,omitempty"`
	TrunkPublicID string `json:"trunkPublicId,omitempty"`
}

// CallResponse contains call initiation result
type CallResponse struct {
	SessionID string `json:"sessionId"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
}

// SessionResponse represents session information
type SessionResponse struct {
	ID                 string `json:"id"`
	State              string `json:"state"`
	Direction          string `json:"direction,omitempty"`
	From               string `json:"from,omitempty"`
	To                 string `json:"to,omitempty"`
	SIPCallID          string `json:"sipCallId,omitempty"`
	AuthMode           string `json:"authMode,omitempty"`
	TrunkID            int64  `json:"trunkId,omitempty"`
	TrunkName          string `json:"trunkName,omitempty"`
	SIPUsername        string `json:"sipUsername,omitempty"`
	DurationSec        int64  `json:"durationSec"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
	TranslatorEnabled  bool   `json:"translatorEnabled,omitempty"`
	TranslatorSrcLang  string `json:"translatorSrcLang,omitempty"`
	TranslatorTgtLang  string `json:"translatorTgtLang,omitempty"`
	TranslatorTTSVoice string `json:"translatorTtsVoice,omitempty"`
}

// PublicAccountResponse represents a public SIP account for REST responses
type PublicAccountResponse struct {
	Key                 string `json:"key"`
	Domain              string `json:"domain"`
	Port                int    `json:"port"`
	Username            string `json:"username"`
	IsRegistered        bool   `json:"isRegistered"`
	RefCountActiveCalls int    `json:"refCountActiveCalls"`
	LastUsedAt          string `json:"lastUsedAt"`
	ExpiresAt           string `json:"expiresAt"`
	LastError           string `json:"lastError,omitempty"`
}

// DashboardResponse represents gateway health and summary statistics
type DashboardResponse struct {
	InstanceID       string `json:"instanceId"`
	UptimeSeconds    int64  `json:"uptimeSeconds"`
	ActiveSessions   int    `json:"activeSessions"`
	TotalTrunks      int    `json:"totalTrunks"`
	EnabledTrunks    int    `json:"enabledTrunks"`
	RegisteredTrunks int    `json:"registeredTrunks"`
	PublicAccounts   int    `json:"publicAccounts"`
	WSClients        int    `json:"wsClients"`
	DBConnected      bool   `json:"dbConnected"`
}

type DashboardSummaryMetricsResponse struct {
	PeriodSessions      int     `json:"periodSessions"`
	ActiveSessions      int     `json:"activeSessions"`
	TotalTrunks         int     `json:"totalTrunks"`
	EnabledTrunks       int     `json:"enabledTrunks"`
	RegisteredTrunks    int     `json:"registeredTrunks"`
	PublicAccounts      int     `json:"publicAccounts"`
	SessionDirectoryNow int     `json:"sessionDirectoryNow"`
	WSClients           int     `json:"wsClients"`
	AvgDurationSec      float64 `json:"avgDurationSec"`
	MaxDurationSec      int     `json:"maxDurationSec"`
}

type DashboardSummarySeriesPointResponse struct {
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

type DashboardSummaryStateResponse struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

type DashboardSummaryTrunkResponse struct {
	TrunkKey  string `json:"trunkKey"`
	TrunkName string `json:"trunkName"`
	Count     int    `json:"count"`
}

type DashboardSummaryDirectionResponse struct {
	Direction string `json:"direction"`
	Count     int    `json:"count"`
}

type DashboardSummaryResponse struct {
	Period     string                                `json:"period"`
	AnchorDate string                                `json:"anchorDate"`
	Timezone   string                                `json:"timezone"`
	RangeStart string                                `json:"rangeStart"`
	RangeEnd   string                                `json:"rangeEnd"`
	Metrics    DashboardSummaryMetricsResponse       `json:"metrics"`
	Series     []DashboardSummarySeriesPointResponse `json:"series"`
	States     []DashboardSummaryStateResponse       `json:"states"`
	Directions []DashboardSummaryDirectionResponse   `json:"directions"`
	TopTrunks  []DashboardSummaryTrunkResponse       `json:"topTrunks"`
}

// WSClientResponse represents a connected WebSocket client
type WSClientResponse struct {
	SessionID   string `json:"sessionId"`
	ConnectedAt string `json:"connectedAt"`
}

// DTMFRequest represents a DTMF request
type DTMFRequest struct {
	Digits string `json:"digits"`
}

// SwitchRequest represents a REST switch trigger request.
type SwitchRequest struct {
	SessionID     string `json:"sessionId"`
	QueueNumber   string `json:"queueNumber"`
	AgentUsername string `json:"agentUsername"`
}

// SwitchResponse contains switch trigger result.
type SwitchResponse struct {
	Status        string `json:"status"`
	SessionID     string `json:"sessionId"`
	QueueNumber   string `json:"queueNumber"`
	AgentUsername string `json:"agentUsername"`
	AutoMode      bool   `json:"autoMode,omitempty"`
}

// TrunkResponse represents a SIP trunk entry for REST responses
type TrunkResponse struct {
	ID                 int64    `json:"id"`
	PublicID           string   `json:"public_id"`
	PublicIDCompat     string   `json:"publicId"`
	Name               string   `json:"name"`
	Domain             string   `json:"domain"`
	Port               int      `json:"port"`
	Username           string   `json:"username"`
	Transport          string   `json:"transport"`
	Enabled            bool     `json:"enabled"`
	IsDefault          bool     `json:"isDefault"`
	ActiveCallCount    int      `json:"activeCallCount"`
	ActiveDestinations []string `json:"activeDestinations,omitempty"`
	LeaseOwner         string   `json:"leaseOwner,omitempty"`
	LeaseUntil         string   `json:"leaseUntil,omitempty"`
	LastRegisteredAt   string   `json:"lastRegisteredAt,omitempty"`
	IsRegistered       bool     `json:"isRegistered"`
	LastError          string   `json:"lastError,omitempty"`
	InUseBy            *string  `json:"inUseBy,omitempty"`
	CreatedAt          string   `json:"createdAt"`
	UpdatedAt          string   `json:"updatedAt"`
}

type UpdateTrunkRequest struct {
	Name      *string `json:"name"`
	Domain    *string `json:"domain"`
	Port      *int    `json:"port"`
	Username  *string `json:"username"`
	Password  *string `json:"password"`
	Transport *string `json:"transport"`
	Enabled   *bool   `json:"enabled"`
	IsDefault *bool   `json:"isDefault"`
	UpdatedBy *string `json:"updatedBy"`
}

// CreateTrunkRequest represents a request to create a new trunk
type CreateTrunkRequest struct {
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Transport string `json:"transport"`
	Enabled   *bool  `json:"enabled"`
	IsDefault *bool  `json:"isDefault"`
}

// TrunkListResponse represents a paginated list of trunks
type TrunkListResponse struct {
	Items    []TrunkResponse `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

type TrunkStreamEvent struct {
	Type    string `json:"type"`
	TrunkID *int64 `json:"trunkId,omitempty"`
	At      string `json:"at"`
}

type SessionStreamEvent struct {
	Type      string  `json:"type"`
	SessionID *string `json:"sessionId,omitempty"`
	At        string  `json:"at"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

func (s *Server) notifyTrunkListChanged(eventType string, trunkID *int64) {
	payload, err := json.Marshal(TrunkStreamEvent{
		Type:    eventType,
		TrunkID: trunkID,
		At:      time.Now().Format(time.RFC3339Nano),
	})
	if err != nil {
		return
	}
	s.broadcastTrunkStream(payload)
}

func (s *Server) notifySessionListChanged(eventType string, sessionID *string) {
	payload, err := json.Marshal(SessionStreamEvent{
		Type:      eventType,
		SessionID: sessionID,
		At:        time.Now().Format(time.RFC3339Nano),
	})
	if err != nil {
		return
	}
	s.broadcastSessionStream(payload)
}

func (s *Server) writeSSE(w http.ResponseWriter, eventName string, payload []byte) error {
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func (s *Server) handleTrunkStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	id, ch := s.subscribeTrunkStream()
	defer s.unsubscribeTrunkStream(id)

	connectedPayload, _ := json.Marshal(TrunkStreamEvent{
		Type: "connected",
		At:   time.Now().Format(time.RFC3339Nano),
	})
	if err := s.writeSSE(w, "connected", connectedPayload); err != nil {
		return
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			if err := s.writeSSE(w, "trunk", payload); err != nil {
				return
			}
		case <-heartbeat.C:
			heartbeatPayload, _ := json.Marshal(TrunkStreamEvent{
				Type: "heartbeat",
				At:   time.Now().Format(time.RFC3339Nano),
			})
			if err := s.writeSSE(w, "heartbeat", heartbeatPayload); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleSessionStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	id, ch := s.subscribeSessionStream()
	defer s.unsubscribeSessionStream(id)

	connectedPayload, _ := json.Marshal(SessionStreamEvent{
		Type: "connected",
		At:   time.Now().Format(time.RFC3339Nano),
	})
	if err := s.writeSSE(w, "connected", connectedPayload); err != nil {
		return
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			if err := s.writeSSE(w, "session", payload); err != nil {
				return
			}
		case <-heartbeat.C:
			heartbeatPayload, _ := json.Marshal(SessionStreamEvent{
				Type: "heartbeat",
				At:   time.Now().Format(time.RFC3339Nano),
			})
			if err := s.writeSSE(w, "heartbeat", heartbeatPayload); err != nil {
				return
			}
		}
	}
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

// handleCreateTrunk creates a new SIP trunk
func (s *Server) handleCreateTrunk(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	var req CreateTrunkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		s.respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	domain := strings.TrimSpace(req.Domain)
	if domain == "" {
		s.respondError(w, http.StatusBadRequest, "domain is required")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		s.respondError(w, http.StatusBadRequest, "username is required")
		return
	}
	password := strings.TrimSpace(req.Password)
	if password == "" {
		s.respondError(w, http.StatusBadRequest, "password is required")
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		req.Port = 5060
	}
	transport := strings.ToLower(strings.TrimSpace(req.Transport))
	if transport != "tcp" && transport != "udp" {
		transport = "tcp"
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	isDefault := false
	if req.IsDefault != nil {
		isDefault = *req.IsDefault
	}

	payload := sip.CreateTrunkPayload{
		Name:      name,
		Domain:    domain,
		Port:      req.Port,
		Username:  username,
		Password:  password,
		Transport: transport,
		Enabled:   enabled,
		IsDefault: isDefault,
	}

	trunk, err := s.trunkManager.CreateTrunk(r.Context(), payload)
	if err != nil {
		switch {
		case errors.Is(err, sip.ErrTrunkValidation):
			s.respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, sip.ErrTrunkConflict):
			s.respondError(w, http.StatusConflict, err.Error())
		default:
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create trunk: %v", err))
		}
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunk_created",
		Data: map[string]interface{}{
			"trunkId":  trunk.ID,
			"name":     trunk.Name,
			"domain":   trunk.Domain,
			"username": trunk.Username,
		},
	})
	createdID := trunk.ID
	s.notifyTrunkListChanged("created", &createdID)

	s.respondJSON(w, http.StatusCreated, trunkResponseFrom(trunk, 0, nil))
}

// handleListTrunks returns trunks with pagination, search, and time filtering
func (s *Server) handleListTrunks(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	q := r.URL.Query()

	// Parse pagination
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	// Parse filters
	trunkID, _ := strconv.ParseInt(q.Get("trunkId"), 10, 64)
	trunkPublicID := q.Get("trunkPublicId")
	if trunkPublicID != "" {
		normalized, ok := sip.NormalizeTrunkPublicID(trunkPublicID)
		if !ok {
			s.respondError(w, http.StatusBadRequest, "Invalid trunk public ID")
			return
		}
		trunkPublicID = normalized
	}
	search := q.Get("search")

	var createdAfter, createdBefore *time.Time
	if v := q.Get("createdAfter"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			createdAfter = &t
		}
	}
	if v := q.Get("createdBefore"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			createdBefore = &t
		}
	}

	// Parse sort parameters
	sortBy := q.Get("sortBy")
	sortDir := q.Get("sortDir")

	params := sip.TrunkListParams{
		Page:          page,
		PageSize:      pageSize,
		TrunkID:       trunkID,
		TrunkPublicID: trunkPublicID,
		Search:        search,
		CreatedAfter:  createdAfter,
		CreatedBefore: createdBefore,
		SortBy:        sortBy,
		SortDir:       sortDir,
	}

	result, err := s.trunkManager.ListTrunks(r.Context(), params)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list trunks: %v", err))
		return
	}

	// Build active-call count per trunk from in-memory sessions
	callInfoByTrunk := s.collectActiveCallsByTrunk()

	items := make([]TrunkResponse, 0, len(result.Items))
	for _, trunk := range result.Items {
		active := callInfoByTrunk[trunk.ID]
		items = append(items, trunkResponseFrom(trunk, active.Count, active.Destinations))
	}

	s.respondJSON(w, http.StatusOK, TrunkListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	})
}

// handleGetTrunk returns a single trunk by ID with active call count
func (s *Server) handleGetTrunk(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]
	trunkID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || trunkID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid trunk ID")
		return
	}

	trunk, err := s.trunkManager.GetTrunkByIDFromDB(r.Context(), trunkID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("Trunk not found: %v", err))
		return
	}

	callInfoByTrunk := s.collectActiveCallsByTrunk()
	active := callInfoByTrunk[trunk.ID]
	s.respondJSON(w, http.StatusOK, trunkResponseFrom(trunk, active.Count, active.Destinations))
}

// handleUpdateTrunk updates a trunk by ID with partial patch semantics.
func (s *Server) handleUpdateTrunk(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]
	trunkID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || trunkID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid trunk ID")
		return
	}

	var req UpdateTrunkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validation + normalization
	if req.Name != nil {
		v := strings.TrimSpace(*req.Name)
		if v == "" {
			s.respondError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		req.Name = &v
	}
	if req.Domain != nil {
		v := strings.TrimSpace(*req.Domain)
		if v == "" {
			s.respondError(w, http.StatusBadRequest, "domain cannot be empty")
			return
		}
		req.Domain = &v
	}
	if req.Username != nil {
		v := strings.TrimSpace(*req.Username)
		if v == "" {
			s.respondError(w, http.StatusBadRequest, "username cannot be empty")
			return
		}
		req.Username = &v
	}
	if req.Port != nil && (*req.Port < 1 || *req.Port > 65535) {
		s.respondError(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if req.Transport != nil {
		v := strings.ToLower(strings.TrimSpace(*req.Transport))
		if v != "tcp" && v != "udp" {
			s.respondError(w, http.StatusBadRequest, "transport must be tcp or udp")
			return
		}
		req.Transport = &v
	}
	if req.Password != nil {
		trimmed := strings.TrimSpace(*req.Password)
		if trimmed == "" {
			// Optional replace: empty means no password change.
			req.Password = nil
		} else {
			req.Password = &trimmed
		}
	}

	callInfoByTrunk := s.collectActiveCallsByTrunk()
	activeCallCount := callInfoByTrunk[trunkID].Count
	if activeCallCount > 0 {
		if req.Enabled != nil && !*req.Enabled {
			s.respondError(w, http.StatusConflict, "cannot disable trunk while active calls exist")
			return
		}
		if req.Domain != nil || req.Port != nil || req.Username != nil || req.Transport != nil {
			s.respondError(w, http.StatusConflict, "cannot update domain/port/username/transport while active calls exist")
			return
		}
	}

	patch := sip.TrunkUpdatePatch{
		Name:      req.Name,
		Domain:    req.Domain,
		Port:      req.Port,
		Username:  req.Username,
		Password:  req.Password,
		Transport: req.Transport,
		Enabled:   req.Enabled,
		IsDefault: req.IsDefault,
		UpdatedBy: req.UpdatedBy,
	}

	trunk, err := s.trunkManager.UpdateTrunk(r.Context(), trunkID, patch)
	if err != nil {
		switch {
		case errors.Is(err, sip.ErrTrunkValidation):
			s.respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, sip.ErrTrunkNotFound):
			s.respondError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, sip.ErrTrunkConflict):
			s.respondError(w, http.StatusConflict, err.Error())
		default:
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update trunk: %v", err))
		}
		return
	}

	eventData := map[string]interface{}{
		"trunkId": trunkID,
	}
	if req.UpdatedBy != nil {
		eventData["updatedBy"] = *req.UpdatedBy
	}
	if req.Enabled != nil {
		eventData["enabled"] = *req.Enabled
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunk_updated",
		Data:      eventData,
	})
	updatedID := trunk.ID
	s.notifyTrunkListChanged("updated", &updatedID)

	callInfoByTrunk = s.collectActiveCallsByTrunk()
	active := callInfoByTrunk[trunk.ID]
	s.respondJSON(w, http.StatusOK, trunkResponseFrom(trunk, active.Count, active.Destinations))
}

// handleRefreshTrunks triggers a trunk reload from the database
func (s *Server) handleRefreshTrunks(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	if err := s.trunkManager.RefreshTrunks(); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to refresh trunks: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunks_refreshed",
	})
	s.notifyTrunkListChanged("refreshed", nil)

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

type trunkActiveCallInfo struct {
	Count        int
	Destinations []string
}

// collectActiveCallsByTrunk returns a map of trunkID -> active call summary.
func (s *Server) collectActiveCallsByTrunk() map[int64]trunkActiveCallInfo {
	infoByTrunk := make(map[int64]trunkActiveCallInfo)
	for _, sess := range s.sessionMgr.ListSessions() {
		state := sess.GetState()
		if state == session.StateEnded {
			continue
		}
		mode, _, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
		if mode == "trunk" && trunkID > 0 {
			activeInfo := infoByTrunk[trunkID]
			activeInfo.Count++
			_, _, to, _ := sess.GetCallInfo()
			if destination := strings.TrimSpace(to); destination != "" {
				activeInfo.Destinations = append(activeInfo.Destinations, destination)
			}
			infoByTrunk[trunkID] = activeInfo
		}
	}
	return infoByTrunk
}

// handleTrunkUnregister unregisters a trunk (force)
func (s *Server) handleTrunkRegister(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]
	trunkID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || trunkID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid trunk ID")
		return
	}

	if err := s.trunkManager.RegisterTrunk(trunkID, true); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to register trunk: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunk_registered",
		Data:      map[string]interface{}{"trunkId": trunkID, "force": true},
	})
	s.notifyTrunkListChanged("registered", &trunkID)

	s.respondJSON(w, http.StatusOK, map[string]interface{}{"trunkId": trunkID, "status": "registered"})
}

// handleTrunkUnregister unregisters a trunk (force)
func (s *Server) handleTrunkUnregister(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]
	trunkID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || trunkID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid trunk ID")
		return
	}

	if err := s.trunkManager.UnregisterTrunk(trunkID, true); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to unregister trunk: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunk_unregistered",
		Data:      map[string]interface{}{"trunkId": trunkID, "force": true},
	})
	s.notifyTrunkListChanged("unregistered", &trunkID)

	s.respondJSON(w, http.StatusOK, map[string]interface{}{"trunkId": trunkID, "status": "unregistered"})
}

func trunkResponseFrom(trunk *sip.Trunk, activeCallCount int, activeDestinations []string) TrunkResponse {
	isRegistered := trunk.LastRegisteredAt != nil
	if trunk.LastError != nil && *trunk.LastError != "" {
		isRegistered = false
	}

	response := TrunkResponse{
		ID:                 trunk.ID,
		PublicID:           trunk.PublicID,
		PublicIDCompat:     trunk.PublicID,
		Name:               trunk.Name,
		Domain:             trunk.Domain,
		Port:               trunk.Port,
		Username:           trunk.Username,
		Transport:          trunk.Transport,
		Enabled:            trunk.Enabled,
		IsDefault:          trunk.IsDefault,
		ActiveCallCount:    activeCallCount,
		ActiveDestinations: append([]string(nil), activeDestinations...),
		LeaseUntil:         formatOptionalTime(trunk.LeaseUntil),
		LastRegisteredAt:   formatOptionalTime(trunk.LastRegisteredAt),
		IsRegistered:       isRegistered,
		CreatedAt:          trunk.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          trunk.UpdatedAt.Format(time.RFC3339),
	}
	if trunk.LeaseOwner != nil {
		response.LeaseOwner = *trunk.LeaseOwner
	}
	if trunk.LastError != nil {
		response.LastError = *trunk.LastError
	}
	if trunk.InUseBy != nil {
		response.InUseBy = trunk.InUseBy
	}
	return response
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

// SessionHistoryResponse represents a call session entry for REST responses
type SessionHistoryResponse struct {
	SessionID  string `json:"sessionId"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
	EndedAt    string `json:"endedAt,omitempty"`
	Direction  string `json:"direction"`
	FromURI    string `json:"fromUri"`
	ToURI      string `json:"toUri"`
	SIPCallID  string `json:"sipCallId"`
	FinalState string `json:"finalState"`
	EndReason  string `json:"endReason"`
}

// SessionHistoryListResponse represents a paginated list of call sessions
type SessionHistoryListResponse struct {
	Items    []SessionHistoryResponse `json:"items"`
	Total    int                      `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"pageSize"`
}

// handleListSessionHistory returns call sessions from DB with pagination, search, and time filtering
func (s *Server) handleListSessionHistory(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	q := r.URL.Query()

	// Parse pagination
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	// Parse filters
	search := q.Get("search")
	direction := q.Get("direction")
	state := q.Get("state")
	sessionID := q.Get("sessionId")

	var createdAfter, createdBefore *time.Time
	if v := q.Get("createdAfter"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			createdAfter = &t
		}
	}
	if v := q.Get("createdBefore"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			createdBefore = &t
		}
	}

	params := logstore.SessionListParams{
		Page:          page,
		PageSize:      pageSize,
		SessionID:     sessionID,
		Direction:     direction,
		State:         state,
		Search:        search,
		CreatedAfter:  createdAfter,
		CreatedBefore: createdBefore,
	}

	result, err := s.logStore.ListSessions(r.Context(), params)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list sessions: %v", err))
		return
	}

	items := make([]SessionHistoryResponse, 0, len(result.Items))
	for _, sess := range result.Items {
		items = append(items, SessionHistoryResponse{
			SessionID:  sess.SessionID,
			CreatedAt:  sess.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  sess.UpdatedAt.Format(time.RFC3339),
			EndedAt:    formatOptionalTime(sess.EndedAt),
			Direction:  sess.Direction,
			FromURI:    sess.FromURI,
			ToURI:      sess.ToURI,
			SIPCallID:  sess.SIPCallID,
			FinalState: sess.FinalState,
			EndReason:  sess.EndReason,
		})
	}

	s.respondJSON(w, http.StatusOK, SessionHistoryListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	})
}

// --- Session Detail Handlers ---

// EventResponse represents a call event entry for REST responses
type EventResponse struct {
	ID            int64                  `json:"id"`
	Timestamp     string                 `json:"timestamp"`
	SessionID     string                 `json:"sessionId"`
	Category      string                 `json:"category"`
	Name          string                 `json:"name"`
	SIPMethod     string                 `json:"sipMethod,omitempty"`
	SIPStatusCode int                    `json:"sipStatusCode,omitempty"`
	SIPCallID     string                 `json:"sipCallId,omitempty"`
	State         string                 `json:"state,omitempty"`
	PayloadID     *int64                 `json:"payloadId,omitempty"`
	Data          map[string]interface{} `json:"data,omitempty"`
}

// EventListResponse represents a paginated list of events
type EventListResponse struct {
	Items    []EventResponse `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

// handleListSessionEvents returns events for a specific session
func (s *Server) handleListSessionEvents(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		s.respondError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	result, err := s.logStore.ListEvents(r.Context(), logstore.EventListParams{
		Page:      page,
		PageSize:  pageSize,
		SessionID: sessionID,
		Category:  q.Get("category"),
		Name:      q.Get("name"),
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list events: %v", err))
		return
	}

	items := make([]EventResponse, 0, len(result.Items))
	for _, ev := range result.Items {
		items = append(items, EventResponse{
			ID:            ev.ID,
			Timestamp:     ev.Timestamp.Format(time.RFC3339Nano),
			SessionID:     ev.SessionID,
			Category:      ev.Category,
			Name:          ev.Name,
			SIPMethod:     ev.SIPMethod,
			SIPStatusCode: ev.SIPStatusCode,
			SIPCallID:     ev.SIPCallID,
			State:         ev.State,
			PayloadID:     ev.PayloadID,
			Data:          ev.Data,
		})
	}

	s.respondJSON(w, http.StatusOK, EventListResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
}

// PayloadResponse represents a payload entry for REST responses
type PayloadResponse struct {
	PayloadID    int64                  `json:"payloadId"`
	Timestamp    string                 `json:"timestamp"`
	SessionID    string                 `json:"sessionId"`
	Kind         string                 `json:"kind"`
	ContentType  string                 `json:"contentType,omitempty"`
	BodyText     string                 `json:"bodyText,omitempty"`
	BodyBytesB64 string                 `json:"bodyBytesB64,omitempty"`
	Parsed       map[string]interface{} `json:"parsed,omitempty"`
}

// PayloadListResponse represents a paginated list of payloads
type PayloadListResponse struct {
	Items    []PayloadResponse `json:"items"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
}

func payloadResponseFrom(p *logstore.PayloadReadRecord) PayloadResponse {
	return PayloadResponse{
		PayloadID:    p.PayloadID,
		Timestamp:    p.Timestamp.Format(time.RFC3339Nano),
		SessionID:    p.SessionID,
		Kind:         p.Kind,
		ContentType:  p.ContentType,
		BodyText:     p.BodyText,
		BodyBytesB64: p.BodyBytesB64,
		Parsed:       p.Parsed,
	}
}

// handleListSessionPayloads returns payloads for a specific session
func (s *Server) handleListSessionPayloads(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		s.respondError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	result, err := s.logStore.ListPayloads(r.Context(), logstore.PayloadListParams{
		Page:      page,
		PageSize:  pageSize,
		SessionID: sessionID,
		Kind:      q.Get("kind"),
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list payloads: %v", err))
		return
	}

	items := make([]PayloadResponse, 0, len(result.Items))
	for _, p := range result.Items {
		items = append(items, payloadResponseFrom(p))
	}

	s.respondJSON(w, http.StatusOK, PayloadListResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
}

// handleGetPayload returns a single payload by ID
func (s *Server) handleGetPayload(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	vars := mux.Vars(r)
	payloadID, err := strconv.ParseInt(vars["payloadId"], 10, 64)
	if err != nil || payloadID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid payload ID")
		return
	}

	p, err := s.logStore.GetPayload(r.Context(), payloadID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("Payload not found: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, payloadResponseFrom(p))
}

// DialogResponse represents a dialog entry for REST responses
type DialogResponse struct {
	ID            int64    `json:"id"`
	SessionID     string   `json:"sessionId"`
	Timestamp     string   `json:"timestamp"`
	SIPCallID     string   `json:"sipCallId,omitempty"`
	FromTag       string   `json:"fromTag,omitempty"`
	ToTag         string   `json:"toTag,omitempty"`
	RemoteContact string   `json:"remoteContact,omitempty"`
	CSeq          int      `json:"cseq"`
	RouteSet      []string `json:"routeSet,omitempty"`
}

// DialogListResponse represents a paginated list of dialogs
type DialogListResponse struct {
	Items    []DialogResponse `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

// handleListSessionDialogs returns dialog snapshots for a specific session
func (s *Server) handleListSessionDialogs(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		s.respondError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	result, err := s.logStore.ListDialogs(r.Context(), logstore.DialogListParams{
		Page:      page,
		PageSize:  pageSize,
		SessionID: sessionID,
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list dialogs: %v", err))
		return
	}

	items := make([]DialogResponse, 0, len(result.Items))
	for _, d := range result.Items {
		items = append(items, DialogResponse{
			ID:            d.ID,
			SessionID:     d.SessionID,
			Timestamp:     d.Timestamp.Format(time.RFC3339Nano),
			SIPCallID:     d.SIPCallID,
			FromTag:       d.FromTag,
			ToTag:         d.ToTag,
			RemoteContact: d.RemoteContact,
			CSeq:          d.CSeq,
			RouteSet:      d.RouteSet,
		})
	}

	s.respondJSON(w, http.StatusOK, DialogListResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
}

// StatsResponse represents a stats entry for REST responses
type StatsResponse struct {
	ID             int64                  `json:"id"`
	Timestamp      string                 `json:"timestamp"`
	SessionID      string                 `json:"sessionId"`
	PLISent        int                    `json:"pliSent"`
	PLIResponse    int                    `json:"pliResponse"`
	LastPLISentAt  string                 `json:"lastPliSentAt,omitempty"`
	LastKeyframeAt string                 `json:"lastKeyframeAt,omitempty"`
	AudioRTCPRR    int                    `json:"audioRtcpRr"`
	AudioRTCPSR    int                    `json:"audioRtcpSr"`
	VideoRTCPRR    int                    `json:"videoRtcpRr"`
	VideoRTCPSR    int                    `json:"videoRtcpSr"`
	Data           map[string]interface{} `json:"data,omitempty"`
}

// StatsListResponse represents a paginated list of stats
type StatsListResponse struct {
	Items    []StatsResponse `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

// handleListSessionStats returns stats for a specific session
func (s *Server) handleListSessionStats(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		s.respondError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	result, err := s.logStore.ListStats(r.Context(), logstore.StatsListParams{
		Page:      page,
		PageSize:  pageSize,
		SessionID: sessionID,
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list stats: %v", err))
		return
	}

	items := make([]StatsResponse, 0, len(result.Items))
	for _, st := range result.Items {
		items = append(items, StatsResponse{
			ID:             st.ID,
			Timestamp:      st.Timestamp.Format(time.RFC3339Nano),
			SessionID:      st.SessionID,
			PLISent:        st.PLISent,
			PLIResponse:    st.PLIResponse,
			LastPLISentAt:  formatOptionalTime(st.LastPLISentAt),
			LastKeyframeAt: formatOptionalTime(st.LastKeyframeAt),
			AudioRTCPRR:    st.AudioRTCPRR,
			AudioRTCPSR:    st.AudioRTCPSR,
			VideoRTCPRR:    st.VideoRTCPRR,
			VideoRTCPSR:    st.VideoRTCPSR,
			Data:           st.Data,
		})
	}

	s.respondJSON(w, http.StatusOK, StatsListResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
}

// --- Ops Handlers ---

// GatewayInstanceResponse represents a gateway instance entry
type GatewayInstanceResponse struct {
	InstanceID string `json:"instanceId"`
	WSURL      string `json:"wsUrl"`
	ExpiresAt  string `json:"expiresAt"`
	UpdatedAt  string `json:"updatedAt"`
	IsExpired  bool   `json:"isExpired"`
}

// GatewayInstanceListResponse represents a paginated list of instances
type GatewayInstanceListResponse struct {
	Items    []GatewayInstanceResponse `json:"items"`
	Total    int                       `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"pageSize"`
}

// handleListGatewayInstances returns gateway instances
func (s *Server) handleListGatewayInstances(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	result, err := s.logStore.ListGatewayInstances(r.Context(), logstore.GatewayInstanceListParams{
		Page:     page,
		PageSize: pageSize,
		Search:   q.Get("search"),
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list instances: %v", err))
		return
	}

	now := time.Now()
	items := make([]GatewayInstanceResponse, 0, len(result.Items))
	for _, gi := range result.Items {
		items = append(items, GatewayInstanceResponse{
			InstanceID: gi.InstanceID,
			WSURL:      gi.WSURL,
			ExpiresAt:  gi.ExpiresAt.Format(time.RFC3339),
			UpdatedAt:  gi.UpdatedAt.Format(time.RFC3339),
			IsExpired:  gi.ExpiresAt.Before(now),
		})
	}

	s.respondJSON(w, http.StatusOK, GatewayInstanceListResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
}

// SessionDirectoryResponse represents a session directory entry
type SessionDirectoryResponse struct {
	SessionID       string `json:"sessionId"`
	OwnerInstanceID string `json:"ownerInstanceId"`
	WSURL           string `json:"wsUrl"`
	ExpiresAt       string `json:"expiresAt"`
	UpdatedAt       string `json:"updatedAt"`
	IsExpired       bool   `json:"isExpired"`
}

// SessionDirectoryListResponse represents a paginated list of session directory entries
type SessionDirectoryListResponse struct {
	Items    []SessionDirectoryResponse `json:"items"`
	Total    int                        `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"pageSize"`
}

// handleListSessionDirectory returns session directory entries
func (s *Server) handleListSessionDirectory(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	result, err := s.logStore.ListSessionDirectory(r.Context(), logstore.SessionDirectoryListParams{
		Page:     page,
		PageSize: pageSize,
		Search:   q.Get("search"),
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list session directory: %v", err))
		return
	}

	now := time.Now()
	items := make([]SessionDirectoryResponse, 0, len(result.Items))
	for _, sd := range result.Items {
		items = append(items, SessionDirectoryResponse{
			SessionID:       sd.SessionID,
			OwnerInstanceID: sd.OwnerInstanceID,
			WSURL:           sd.WSURL,
			ExpiresAt:       sd.ExpiresAt.Format(time.RFC3339),
			UpdatedAt:       sd.UpdatedAt.Format(time.RFC3339),
			IsExpired:       sd.ExpiresAt.Before(now),
		})
	}

	s.respondJSON(w, http.StatusOK, SessionDirectoryListResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
}

// handleListPublicAccounts returns all registered public SIP accounts
func (s *Server) handleListPublicAccounts(w http.ResponseWriter, r *http.Request) {
	if s.publicRegistry == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Public account registry not available")
		return
	}

	accounts := s.publicRegistry.ListAccounts()
	response := make([]PublicAccountResponse, 0, len(accounts))

	for _, acc := range accounts {
		// Note: acc fields might be subject to race conditions, but ListAccounts
		// already provides a snapshot under the registry mutex
		item := PublicAccountResponse{
			Key:                 acc.Key,
			Domain:              acc.Domain,
			Port:                acc.Port,
			Username:            acc.Username,
			IsRegistered:        acc.IsRegistered,
			RefCountActiveCalls: acc.RefCountActiveCalls,
			LastUsedAt:          acc.LastUsedAt.Format(time.RFC3339),
			ExpiresAt:           acc.ExpiresAt.Format(time.RFC3339),
			LastError:           acc.LastError,
		}
		response = append(response, item)
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleListWSClients returns all connected WebSocket clients
func (s *Server) handleListWSClients(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	clients := make([]WSClientResponse, 0, len(s.wsClients))
	for sessionID, client := range s.wsClients {
		clients = append(clients, WSClientResponse{
			SessionID:   sessionID,
			ConnectedAt: client.ConnectedAt.Format(time.RFC3339),
		})
	}
	s.mu.RUnlock()

	s.respondJSON(w, http.StatusOK, clients)
}

// handleDashboard returns gateway health and summary statistics
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Count active sessions
	activeSessions := 0
	if s.sessionMgr != nil {
		activeSessions = len(s.sessionMgr.ListSessions())
	}

	// Count trunks
	totalTrunks := 0
	enabledTrunks := 0
	registeredTrunks := 0
	if s.trunkManager != nil {
		trunks := s.trunkManager.ListOwnedTrunks()
		totalTrunks = len(trunks)
		for _, trunk := range trunks {
			if trunk.Enabled {
				enabledTrunks++
			}
			if trunk.LastRegisteredAt != nil {
				registeredTrunks++
			}
		}
	}

	// Count public accounts
	publicAccounts := 0
	if s.publicRegistry != nil {
		publicAccounts = len(s.publicRegistry.ListAccounts())
	}

	// Count WS clients
	s.mu.RLock()
	wsClients := len(s.wsClients)
	s.mu.RUnlock()

	// Check DB connection
	dbConnected := s.logStore != nil

	// Calculate uptime
	uptime := int64(time.Since(s.startTime).Seconds())

	response := DashboardResponse{
		InstanceID:       s.gatewayConfig.InstanceID,
		UptimeSeconds:    uptime,
		ActiveSessions:   activeSessions,
		TotalTrunks:      totalTrunks,
		EnabledTrunks:    enabledTrunks,
		RegisteredTrunks: registeredTrunks,
		PublicAccounts:   publicAccounts,
		WSClients:        wsClients,
		DBConnected:      dbConnected,
	}

	s.respondJSON(w, http.StatusOK, response)
}

func dashboardSummaryLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Bangkok")
	if err == nil {
		return location
	}

	// Fallback when tzdata is unavailable in runtime images.
	return time.FixedZone("Asia/Bangkok", 7*60*60)
}

func parseDashboardSummaryRange(period, anchorDate string) (time.Time, time.Time, string, error) {
	location := dashboardSummaryLocation()

	nowInLocation := time.Now().In(location)
	if anchorDate == "" {
		anchorDate = nowInLocation.Format("2006-01-02")
	}

	anchor, err := time.ParseInLocation("2006-01-02", anchorDate, location)
	if err != nil {
		return time.Time{}, time.Time{}, "", fmt.Errorf("anchorDate must be YYYY-MM-DD")
	}

	anchor = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, location)

	var startLocal time.Time
	var endLocal time.Time

	switch period {
	case "day":
		startLocal = anchor
		endLocal = startLocal.AddDate(0, 0, 1)
	case "month":
		startLocal = time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, location)
		endLocal = startLocal.AddDate(0, 1, 0)
	case "year":
		startLocal = time.Date(anchor.Year(), time.January, 1, 0, 0, 0, 0, location)
		endLocal = startLocal.AddDate(1, 0, 0)
	default:
		return time.Time{}, time.Time{}, "", fmt.Errorf("period must be day, month, or year")
	}

	return startLocal.UTC(), endLocal.UTC(), anchor.Format("2006-01-02"), nil
}

// handleDashboardSummary returns aggregate dashboard data for a selected period.
func (s *Server) handleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	query := r.URL.Query()
	period := strings.ToLower(strings.TrimSpace(query.Get("period")))
	if period == "" {
		period = "day"
	}

	rangeStartUTC, rangeEndUTC, normalizedAnchor, err := parseDashboardSummaryRange(period, strings.TrimSpace(query.Get("anchorDate")))
	if err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	summary, err := s.logStore.GetDashboardSummary(r.Context(), logstore.DashboardSummaryParams{
		Period:        period,
		RangeStartUTC: rangeStartUTC,
		RangeEndUTC:   rangeEndUTC,
		Timezone:      "Asia/Bangkok",
		TopTrunks:     10,
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load dashboard summary: %v", err))
		return
	}

	activeSessions := 0
	if s.sessionMgr != nil {
		activeSessions = len(s.sessionMgr.ListSessions())
	}

	totalTrunks := 0
	enabledTrunks := 0
	registeredTrunks := 0
	if s.trunkManager != nil {
		trunks := s.trunkManager.ListOwnedTrunks()
		totalTrunks = len(trunks)
		for _, trunk := range trunks {
			if trunk.Enabled {
				enabledTrunks++
			}
			if trunk.LastRegisteredAt != nil {
				registeredTrunks++
			}
		}
	}

	publicAccounts := 0
	if s.publicRegistry != nil {
		publicAccounts = len(s.publicRegistry.ListAccounts())
	}

	s.mu.RLock()
	wsClients := len(s.wsClients)
	s.mu.RUnlock()

	series := make([]DashboardSummarySeriesPointResponse, 0, len(summary.Series))
	for _, point := range summary.Series {
		series = append(series, DashboardSummarySeriesPointResponse{
			Bucket: point.Bucket,
			Count:  point.Count,
		})
	}

	states := make([]DashboardSummaryStateResponse, 0, len(summary.States))
	for _, state := range summary.States {
		states = append(states, DashboardSummaryStateResponse{
			State: state.State,
			Count: state.Count,
		})
	}

	topTrunks := make([]DashboardSummaryTrunkResponse, 0, len(summary.TopTrunks))
	for _, trunk := range summary.TopTrunks {
		topTrunks = append(topTrunks, DashboardSummaryTrunkResponse{
			TrunkKey:  trunk.TrunkKey,
			TrunkName: trunk.TrunkName,
			Count:     trunk.Count,
		})
	}

	directions := make([]DashboardSummaryDirectionResponse, 0, len(summary.Directions))
	for _, dir := range summary.Directions {
		directions = append(directions, DashboardSummaryDirectionResponse{
			Direction: dir.Direction,
			Count:     dir.Count,
		})
	}

	s.respondJSON(w, http.StatusOK, DashboardSummaryResponse{
		Period:     period,
		AnchorDate: normalizedAnchor,
		Timezone:   "Asia/Bangkok",
		RangeStart: rangeStartUTC.Format(time.RFC3339),
		RangeEnd:   rangeEndUTC.Format(time.RFC3339),
		Metrics: DashboardSummaryMetricsResponse{
			PeriodSessions:      summary.TotalSessions,
			ActiveSessions:      activeSessions,
			TotalTrunks:         totalTrunks,
			EnabledTrunks:       enabledTrunks,
			RegisteredTrunks:    registeredTrunks,
			PublicAccounts:      publicAccounts,
			SessionDirectoryNow: summary.SessionDirectoryCount,
			WSClients:           wsClients,
			AvgDurationSec:      summary.AvgDurationSec,
			MaxDurationSec:      summary.MaxDurationSec,
		},
		Series:     series,
		States:     states,
		Directions: directions,
		TopTrunks:  topTrunks,
	})
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

// handleUserTrunkHeartbeat checks if a trunk is assigned to the authenticated user
// and refreshes its updated_at timestamp. If no trunk is found, logs an alert.
func (s *Server) handleUserTrunkHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, hasClaims := AuthClaimsFromContext(ctx)
	if !hasClaims {
		s.respondError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	preferredUsername := claims.PreferredUsername
	if preferredUsername == "" {
		s.respondError(w, http.StatusBadRequest, "Token missing preferred_username claim")
		return
	}

	trunk, err := s.trunkManager.FindTrunkByInUseBy(ctx, preferredUsername)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to lookup trunk: %v", err))
		return
	}

	if trunk == nil {
		log.Printf("⚠️ [User Trunk] Trunk does not exist for user %s (sub=%s), alert!", preferredUsername, claims.Subject)
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("No trunk assigned to user %s", preferredUsername))
		return
	}

	// Refresh updated_at by re-setting in_use_by to the same value.
	if err := s.trunkManager.SetTrunkInUseBy(ctx, trunk.ID, &preferredUsername); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to refresh trunk: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"trunkId":   trunk.ID,
		"publicId":  trunk.PublicID,
		"name":      trunk.Name,
		"domain":    trunk.Domain,
		"port":      trunk.Port,
		"username":  trunk.Username,
		"inUseBy":   preferredUsername,
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// handleWSTranslate enables S2S speech translation for a session.
func (s *Server) handleWSTranslate(client *WSClient, msg WSMessage) {
	if s.translatorClient == nil {
		s.sendWSError(client, msg.SessionID, "Translator not available")
		return
	}

	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = client.sessionID
	}
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.sendWSError(client, sessionID, "Session not found")
		return
	}

	srcLang := msg.SourceLang
	if srcLang == "" {
		srcLang = s.translatorCfg.SourceLang
	}
	tgtLang := msg.TargetLang
	if tgtLang == "" {
		tgtLang = s.translatorCfg.TargetLang
	}
	ttsVoice := msg.TTSVoice
	if ttsVoice == "" {
		ttsVoice = s.translatorCfg.TTSVoice
	}

	sess.SetTranslator(s.translatorClient, srcLang, tgtLang, ttsVoice)
	sess.EnableTranslator()

	log.Printf("[%s] 🎤 Translation enabled via WS: %s → %s (voice: %s)",
		sessionID, srcLang, tgtLang, ttsVoice)

	s.sendWSMessage(client, WSMessage{
		Type:       "translate",
		SessionID:  sessionID,
		State:      "enabled",
		SourceLang: srcLang,
		TargetLang: tgtLang,
		TTSVoice:   ttsVoice,
	})
}

// handleWSTranslateStop disables S2S speech translation for a session.
func (s *Server) handleWSTranslateStop(client *WSClient, msg WSMessage) {
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = client.sessionID
	}
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.sendWSError(client, sessionID, "Session not found")
		return
	}

	sess.DisableTranslator()

	log.Printf("[%s] 🎤 Translation disabled via WS", sessionID)

	s.sendWSMessage(client, WSMessage{
		Type:      "translate_stop",
		SessionID: sessionID,
		State:     "disabled",
	})
}
