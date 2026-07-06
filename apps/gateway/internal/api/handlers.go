package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"k2-gateway/internal/logstore"
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

type DashboardSummaryTerminalOutcomeResponse struct {
	Outcome       string `json:"outcome"`
	Direction     string `json:"direction"`
	SIPStatusCode int    `json:"sipStatusCode"`
	Count         int    `json:"count"`
}

type DashboardSummaryTerminalTrunkResponse struct {
	TrunkKey  string `json:"trunkKey"`
	TrunkName string `json:"trunkName"`
	Outcome   string `json:"outcome"`
	Count     int    `json:"count"`
}

type DashboardSummaryResponse struct {
	Period           string                                    `json:"period"`
	AnchorDate       string                                    `json:"anchorDate"`
	Timezone         string                                    `json:"timezone"`
	RangeStart       string                                    `json:"rangeStart"`
	RangeEnd         string                                    `json:"rangeEnd"`
	Metrics          DashboardSummaryMetricsResponse           `json:"metrics"`
	Series           []DashboardSummarySeriesPointResponse     `json:"series"`
	States           []DashboardSummaryStateResponse           `json:"states"`
	Directions       []DashboardSummaryDirectionResponse       `json:"directions"`
	TopTrunks        []DashboardSummaryTrunkResponse           `json:"topTrunks"`
	TerminalOutcomes []DashboardSummaryTerminalOutcomeResponse `json:"terminalOutcomes"`
	TerminalTrunks   []DashboardSummaryTerminalTrunkResponse   `json:"terminalTrunks"`
}

// WSClientResponse represents a connected WebSocket client
type WSClientResponse struct {
	SessionID             string `json:"sessionId"`
	ConnectedAt           string `json:"connectedAt"`
	TrunkResolved         bool   `json:"trunkResolved"`
	ResolvedTrunkID       int64  `json:"resolvedTrunkId,omitempty"`
	ResolvedTrunkPublicID string `json:"resolvedTrunkPublicId,omitempty"`
	Availability          string `json:"availability,omitempty"`
	CallState             string `json:"callState,omitempty"`
	AuthSubject           string `json:"authSubject,omitempty"`
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

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
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
	endReason := q.Get("endReason")
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
		EndReason:     endReason,
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
	sipStatusCode, _ := strconv.Atoi(q.Get("sipStatusCode"))

	result, err := s.logStore.ListEvents(r.Context(), logstore.EventListParams{
		Page:          page,
		PageSize:      pageSize,
		SessionID:     sessionID,
		Category:      q.Get("category"),
		Name:          q.Get("name"),
		SIPStatusCode: sipStatusCode,
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
		resp := WSClientResponse{
			SessionID:       sessionID,
			ConnectedAt:     client.ConnectedAt.Format(time.RFC3339),
			TrunkResolved:   client.trunkResolved,
			ResolvedTrunkID: client.resolvedTrunkID,
			Availability:    client.availability,
			CallState:       client.callState,
		}
		if client.authClaims != nil {
			resp.AuthSubject = client.authClaims.Subject
		}
		clients = append(clients, resp)
	}
	s.mu.RUnlock()

	if s.trunkManager != nil {
		for idx := range clients {
			if clients[idx].ResolvedTrunkID <= 0 {
				continue
			}
			if trunkRaw, ok := s.trunkManager.GetTrunkByID(clients[idx].ResolvedTrunkID); ok {
				if trunk, ok := trunkRaw.(*sip.Trunk); ok && trunk.PublicID != "" {
					clients[idx].ResolvedTrunkPublicID = trunk.PublicID
				}
			}
		}
	}

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

	terminalOutcomes := make([]DashboardSummaryTerminalOutcomeResponse, 0, len(summary.TerminalOutcomes))
	for _, outcome := range summary.TerminalOutcomes {
		terminalOutcomes = append(terminalOutcomes, DashboardSummaryTerminalOutcomeResponse{
			Outcome:       outcome.Outcome,
			Direction:     outcome.Direction,
			SIPStatusCode: outcome.SIPStatusCode,
			Count:         outcome.Count,
		})
	}

	terminalTrunks := make([]DashboardSummaryTerminalTrunkResponse, 0, len(summary.TerminalTrunks))
	for _, trunk := range summary.TerminalTrunks {
		terminalTrunks = append(terminalTrunks, DashboardSummaryTerminalTrunkResponse{
			TrunkKey:  trunk.TrunkKey,
			TrunkName: trunk.TrunkName,
			Outcome:   trunk.Outcome,
			Count:     trunk.Count,
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
		Series:           series,
		States:           states,
		Directions:       directions,
		TopTrunks:        topTrunks,
		TerminalOutcomes: terminalOutcomes,
		TerminalTrunks:   terminalTrunks,
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
