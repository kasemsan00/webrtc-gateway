package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"k2-gateway/internal/logstore"
)

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
