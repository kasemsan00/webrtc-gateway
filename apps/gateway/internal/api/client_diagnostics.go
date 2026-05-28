package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"k2-gateway/internal/auth"
	"k2-gateway/internal/logstore"
)

const (
	clientDiagnosticsMaxBodyBytes       = 128 * 1024
	clientDiagnosticsMaxEvents          = 100
	clientDiagnosticsMaxStringLength    = 512
	clientDiagnosticsMaxMessageLength   = 1024
	clientDiagnosticsMaxContextJSON     = 8192
	clientDiagnosticsPayloadThreshold   = 2048
	clientDiagnosticsTimestampSkew      = 24 * time.Hour
	clientDiagnosticsRateWindow         = time.Minute
	clientDiagnosticsRateLimitPerWindow = 30
)

type diagnosticRateState struct {
	windowStart time.Time
	count       int
}

type clientDiagnosticsRequest struct {
	ClientTraceID string                  `json:"clientTraceId"`
	AppVersion    string                  `json:"appVersion"`
	Platform      string                  `json:"platform"`
	DeviceIDHash  string                  `json:"deviceIdHash"`
	Events        []clientDiagnosticEvent `json:"events"`
}

type clientDiagnosticEvent struct {
	ID        string                 `json:"id"`
	Timestamp string                 `json:"timestamp"`
	SessionID string                 `json:"sessionId"`
	Source    string                 `json:"source"`
	Level     string                 `json:"level"`
	Name      string                 `json:"name"`
	Message   string                 `json:"message"`
	Context   map[string]interface{} `json:"context"`
}

type clientDiagnosticsResponse struct {
	Status                 string `json:"status"`
	ClientTraceID          string `json:"clientTraceId,omitempty"`
	AcceptedEvents         int    `json:"acceptedEvents"`
	SessionEvents          int    `json:"sessionEvents"`
	NonSessionEvents       int    `json:"nonSessionEvents"`
	StoredSessionEvents    int    `json:"storedSessionEvents"`
	StoredNonSessionEvents int    `json:"storedNonSessionEvents"`
	DroppedEvents          int    `json:"droppedEvents"`
	Persistence            string `json:"persistence"`
}

type clientDiagnosticReadResponse struct {
	ID                int64                  `json:"id"`
	Timestamp         string                 `json:"timestamp"`
	ClientTraceID     string                 `json:"clientTraceId,omitempty"`
	AuthSubject       string                 `json:"authSubject,omitempty"`
	AuthRealm         string                 `json:"authRealm,omitempty"`
	PreferredUsername string                 `json:"preferredUsername,omitempty"`
	Source            string                 `json:"source"`
	Level             string                 `json:"level"`
	Name              string                 `json:"name"`
	AppVersion        string                 `json:"appVersion,omitempty"`
	Platform          string                 `json:"platform,omitempty"`
	DeviceIDHash      string                 `json:"deviceIdHash,omitempty"`
	Data              map[string]interface{} `json:"data,omitempty"`
}

type clientDiagnosticListResponse struct {
	Items    []clientDiagnosticReadResponse `json:"items"`
	Total    int                            `json:"total"`
	Page     int                            `json:"page"`
	PageSize int                            `json:"pageSize"`
}

type sanitizedClientDiagnosticEvent struct {
	ID        string
	Timestamp time.Time
	SessionID string
	Source    string
	Level     string
	Name      string
	Message   string
	Context   map[string]interface{}
}

func (s *Server) handleClientDiagnostics(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		log.Printf("🧾 [ClientDiagnostics] upload disabled: logStore unavailable")
		s.respondJSON(w, http.StatusAccepted, clientDiagnosticsResponse{
			Status:        "disabled",
			Persistence:   "unavailable",
			DroppedEvents: 0,
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, clientDiagnosticsMaxBodyBytes)
	defer r.Body.Close()

	var req clientDiagnosticsRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		log.Printf("🧾 [ClientDiagnostics] rejected invalid body: %v", err)
		s.respondError(w, http.StatusBadRequest, "Invalid diagnostics request body")
		return
	}

	claims, _ := AuthClaimsFromContext(r.Context())
	clientTraceID := truncateString(strings.TrimSpace(req.ClientTraceID), clientDiagnosticsMaxStringLength)
	rateKey := clientDiagnosticsRateKey(r, claimsSubject(claims))
	if !s.allowClientDiagnostics(rateKey, time.Now()) {
		log.Printf("🧾 [ClientDiagnostics] rate limited: subject=%s trace=%s events=%d", claimsSubject(claims), clientTraceID, len(req.Events))
		s.respondError(w, http.StatusTooManyRequests, "Diagnostics upload rate limit exceeded")
		return
	}

	events, err := sanitizeClientDiagnosticsRequest(req, time.Now())
	if err != nil {
		log.Printf("🧾 [ClientDiagnostics] rejected invalid payload: subject=%s trace=%s events=%d reason=%v", claimsSubject(claims), clientTraceID, len(req.Events), err)
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	sessionEvents, nonSessionEvents, persistence, err := s.persistClientDiagnostics(r.Context(), req, events, claims)
	if err != nil {
		log.Printf("🧾 [ClientDiagnostics] persist failed: subject=%s trace=%s events=%d session=%d nonSession=%d persistence=%s error=%v", claimsSubject(claims), clientTraceID, len(events), sessionEvents, nonSessionEvents, persistence, err)
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to store diagnostics: %v", err))
		return
	}

	status := "ok"
	if persistence == "disabled" {
		status = "disabled"
	}
	log.Printf("🧾 [ClientDiagnostics] accepted: subject=%s trace=%s events=%d session=%d nonSession=%d persistence=%s", claimsSubject(claims), clientTraceID, len(events), sessionEvents, nonSessionEvents, persistence)
	s.respondJSON(w, http.StatusAccepted, clientDiagnosticsResponse{
		Status:                 status,
		ClientTraceID:          clientTraceID,
		AcceptedEvents:         len(events),
		SessionEvents:          sessionEvents,
		NonSessionEvents:       nonSessionEvents,
		StoredSessionEvents:    sessionEvents,
		StoredNonSessionEvents: nonSessionEvents,
		DroppedEvents:          0,
		Persistence:            persistence,
	})
}

func (s *Server) handleListClientDiagnostics(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))
	result, err := s.logStore.ListClientDiagnostics(r.Context(), logstore.ClientDiagnosticListParams{
		Page:          page,
		PageSize:      pageSize,
		ClientTraceID: q.Get("clientTraceId"),
		AuthSubject:   q.Get("authSubject"),
		Source:        q.Get("source"),
		Level:         q.Get("level"),
		Name:          q.Get("name"),
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list client diagnostics: %v", err))
		return
	}

	items := make([]clientDiagnosticReadResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, clientDiagnosticReadResponse{
			ID:                item.ID,
			Timestamp:         item.Timestamp.Format(time.RFC3339Nano),
			ClientTraceID:     item.ClientTraceID,
			AuthSubject:       item.AuthSubject,
			AuthRealm:         item.AuthRealm,
			PreferredUsername: item.PreferredUsername,
			Source:            item.Source,
			Level:             item.Level,
			Name:              item.Name,
			AppVersion:        item.AppVersion,
			Platform:          item.Platform,
			DeviceIDHash:      item.DeviceIDHash,
			Data:              item.Data,
		})
	}

	s.respondJSON(w, http.StatusOK, clientDiagnosticListResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
}

func (s *Server) handleListClientDiagnosticSessionEvents(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}
	sessionID := mux.Vars(r)["sessionId"]
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
		Category:  "client",
		Name:      q.Get("name"),
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list client diagnostic events: %v", err))
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

func (s *Server) handleListClientDiagnosticSessionPayloads(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}
	sessionID := mux.Vars(r)["sessionId"]
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
		Kind:      "client_diagnostics_batch",
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list client diagnostic payloads: %v", err))
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

func (s *Server) handleGetClientDiagnosticPayload(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}
	payloadID, err := strconv.ParseInt(mux.Vars(r)["payloadId"], 10, 64)
	if err != nil || payloadID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid payload ID")
		return
	}
	p, err := s.logStore.GetPayload(r.Context(), payloadID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("Payload not found: %v", err))
		return
	}
	if p.Kind != "client_diagnostics_batch" {
		s.respondError(w, http.StatusNotFound, "Client diagnostics payload not found")
		return
	}
	s.respondJSON(w, http.StatusOK, payloadResponseFrom(p))
}

func sanitizeClientDiagnosticsRequest(req clientDiagnosticsRequest, now time.Time) ([]sanitizedClientDiagnosticEvent, error) {
	if len(req.Events) == 0 {
		return nil, fmt.Errorf("events are required")
	}
	if len(req.Events) > clientDiagnosticsMaxEvents {
		return nil, fmt.Errorf("too many events")
	}

	events := make([]sanitizedClientDiagnosticEvent, 0, len(req.Events))
	for _, event := range req.Events {
		name := sanitizeLabel(event.Name)
		source := sanitizeLabel(event.Source)
		level := sanitizeLevel(event.Level)
		if name == "" {
			return nil, fmt.Errorf("event name is required")
		}
		if source == "" {
			return nil, fmt.Errorf("event source is required")
		}
		if level == "" {
			return nil, fmt.Errorf("invalid event level")
		}

		context := sanitizeDiagnosticValue(event.Context)
		contextMap, _ := context.(map[string]interface{})
		if contextMap == nil {
			contextMap = map[string]interface{}{}
		}
		if contextBytes, err := json.Marshal(contextMap); err == nil && len(contextBytes) > clientDiagnosticsMaxContextJSON {
			return nil, fmt.Errorf("event context is too large")
		}

		events = append(events, sanitizedClientDiagnosticEvent{
			ID:        truncateString(strings.TrimSpace(event.ID), clientDiagnosticsMaxStringLength),
			Timestamp: normalizeClientDiagnosticTimestamp(event.Timestamp, now),
			SessionID: truncateString(strings.TrimSpace(event.SessionID), clientDiagnosticsMaxStringLength),
			Source:    source,
			Level:     level,
			Name:      name,
			Message:   truncateString(strings.TrimSpace(event.Message), clientDiagnosticsMaxMessageLength),
			Context:   contextMap,
		})
	}
	return events, nil
}

func (s *Server) persistClientDiagnostics(ctx context.Context, req clientDiagnosticsRequest, events []sanitizedClientDiagnosticEvent, claims *auth.VerifiedClaims) (int, int, string, error) {
	authSubject, authRealm, preferredUsername := diagnosticAuthMetadata(claims)
	clientTraceID := truncateString(strings.TrimSpace(req.ClientTraceID), clientDiagnosticsMaxStringLength)
	appVersion := truncateString(strings.TrimSpace(req.AppVersion), clientDiagnosticsMaxStringLength)
	platform := truncateString(strings.TrimSpace(req.Platform), clientDiagnosticsMaxStringLength)
	deviceIDHash := truncateString(strings.TrimSpace(req.DeviceIDHash), clientDiagnosticsMaxStringLength)

	sessionGroups := make(map[string][]sanitizedClientDiagnosticEvent)
	nonSessionEvents := make([]sanitizedClientDiagnosticEvent, 0)
	for _, event := range events {
		if event.SessionID == "" {
			nonSessionEvents = append(nonSessionEvents, event)
			continue
		}
		sessionGroups[event.SessionID] = append(sessionGroups[event.SessionID], event)
	}

	persistence := "stored"
	sessionCount := 0
	for sessionID, grouped := range sessionGroups {
		var payloadID *int64
		payloadBody, _ := json.Marshal(map[string]interface{}{
			"clientTraceId": clientTraceID,
			"appVersion":    appVersion,
			"platform":      platform,
			"deviceIdHash":  deviceIDHash,
			"events":        diagnosticEventsPayload(grouped),
		})
		if len(payloadBody) >= clientDiagnosticsPayloadThreshold {
			id, err := s.logStore.StorePayload(ctx, &logstore.PayloadRecord{
				SessionID:   sessionID,
				Timestamp:   time.Now(),
				Kind:        "client_diagnostics_batch",
				ContentType: "application/json",
				BodyText:    string(payloadBody),
				Parsed: map[string]interface{}{
					"clientTraceId": clientTraceID,
					"eventCount":    len(grouped),
				},
			})
			if err != nil && !errors.Is(err, logstore.ErrDisabled) {
				return sessionCount, len(nonSessionEvents), persistence, err
			}
			if id > 0 {
				payloadID = &id
			} else if errors.Is(err, logstore.ErrDisabled) {
				persistence = "disabled"
			}
		}

		for _, event := range grouped {
			data := eventSummaryData(event, clientTraceID, appVersion, platform, deviceIDHash)
			if payloadID == nil {
				data["context"] = event.Context
			}
			s.logEvent(&logstore.Event{
				Timestamp: event.Timestamp,
				SessionID: sessionID,
				Category:  "client",
				Name:      event.Name,
				PayloadID: payloadID,
				Data:      data,
			})
			sessionCount++
		}
	}

	for _, event := range nonSessionEvents {
		err := s.logStore.StoreClientDiagnostic(ctx, &logstore.ClientDiagnosticRecord{
			Timestamp:         event.Timestamp,
			ClientTraceID:     clientTraceID,
			AuthSubject:       authSubject,
			AuthRealm:         authRealm,
			PreferredUsername: preferredUsername,
			Source:            event.Source,
			Level:             event.Level,
			Name:              event.Name,
			AppVersion:        appVersion,
			Platform:          platform,
			DeviceIDHash:      deviceIDHash,
			Data:              eventSummaryData(event, clientTraceID, appVersion, platform, deviceIDHash),
		})
		if errors.Is(err, logstore.ErrDisabled) {
			persistence = "disabled"
			continue
		}
		if err != nil {
			return sessionCount, len(nonSessionEvents), persistence, err
		}
	}

	return sessionCount, len(nonSessionEvents), persistence, nil
}

func (s *Server) allowClientDiagnostics(key string, now time.Time) bool {
	if key == "" {
		key = "unknown"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diagnosticLimits == nil {
		s.diagnosticLimits = make(map[string]*diagnosticRateState)
	}
	state := s.diagnosticLimits[key]
	if state == nil || now.Sub(state.windowStart) >= clientDiagnosticsRateWindow {
		s.diagnosticLimits[key] = &diagnosticRateState{windowStart: now, count: 1}
		return true
	}
	if state.count >= clientDiagnosticsRateLimitPerWindow {
		return false
	}
	state.count++
	return true
}

func sanitizeDiagnosticValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			cleanKey := truncateString(strings.TrimSpace(key), clientDiagnosticsMaxStringLength)
			if cleanKey == "" {
				continue
			}
			if isSensitiveDiagnosticKey(cleanKey) {
				out[cleanKey] = "[redacted]"
				continue
			}
			out[cleanKey] = sanitizeDiagnosticValue(item)
		}
		return out
	case []interface{}:
		limit := len(v)
		if limit > 20 {
			limit = 20
		}
		out := make([]interface{}, 0, limit)
		for i := 0; i < limit; i++ {
			out = append(out, sanitizeDiagnosticValue(v[i]))
		}
		return out
	case string:
		return truncateString(v, clientDiagnosticsMaxStringLength)
	case float64, bool, nil:
		return v
	default:
		return truncateString(fmt.Sprint(v), clientDiagnosticsMaxStringLength)
	}
}

func isSensitiveDiagnosticKey(key string) bool {
	lower := strings.ToLower(key)
	sensitiveParts := []string{
		"token", "password", "secret", "credential", "authorization",
		"access_token", "refresh_token", "id_token", "sippassword",
		"pntoken", "pn_token", "sdp", "sipmessage",
	}
	for _, part := range sensitiveParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

func normalizeClientDiagnosticTimestamp(raw string, now time.Time) time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return now.UTC()
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return now.UTC()
	}
	parsed = parsed.UTC()
	if parsed.Before(now.Add(-clientDiagnosticsTimestampSkew)) || parsed.After(now.Add(clientDiagnosticsTimestampSkew)) {
		return now.UTC()
	}
	return parsed
}

func sanitizeLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info", "warn", "error":
		return strings.ToLower(strings.TrimSpace(level))
	default:
		return ""
	}
}

func sanitizeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = truncateString(value, clientDiagnosticsMaxStringLength)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	return b.String()
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func eventSummaryData(event sanitizedClientDiagnosticEvent, clientTraceID, appVersion, platform, deviceIDHash string) map[string]interface{} {
	data := map[string]interface{}{
		"clientTraceId": clientTraceID,
		"source":        event.Source,
		"level":         event.Level,
		"message":       event.Message,
		"appVersion":    appVersion,
		"platform":      platform,
		"deviceIdHash":  deviceIDHash,
	}
	if event.SessionID != "" {
		data["sessionId"] = event.SessionID
	}
	if event.ID != "" {
		data["eventId"] = event.ID
	}
	return data
}

func diagnosticEventsPayload(events []sanitizedClientDiagnosticEvent) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(events))
	for _, event := range events {
		out = append(out, map[string]interface{}{
			"timestamp": event.Timestamp.Format(time.RFC3339Nano),
			"id":        event.ID,
			"sessionId": event.SessionID,
			"source":    event.Source,
			"level":     event.Level,
			"name":      event.Name,
			"message":   event.Message,
			"context":   event.Context,
		})
	}
	return out
}

func clientDiagnosticsRateKey(r *http.Request, subject string) string {
	if subject != "" {
		return "sub:" + subject
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return "ip:" + host
	}
	return "ip:" + r.RemoteAddr
}

func claimsSubject(claims *auth.VerifiedClaims) string {
	if claims == nil {
		return ""
	}
	return strings.TrimSpace(claims.Subject)
}

func diagnosticAuthMetadata(claims *auth.VerifiedClaims) (string, string, string) {
	if claims == nil {
		return "", "", ""
	}
	return strings.TrimSpace(claims.Subject), string(claims.Realm), strings.TrimSpace(claims.PreferredUsername)
}
