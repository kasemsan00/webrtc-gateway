package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"k2-gateway/internal/auth"
	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
)

type diagnosticsLogStoreStub struct {
	logstore.LogStore
	events      []*logstore.Event
	payloads    []*logstore.PayloadRecord
	diagnostics []*logstore.ClientDiagnosticRecord
	listResult  *logstore.ClientDiagnosticListResult
	listParams  logstore.ClientDiagnosticListParams
	eventResult *logstore.EventListResult
	eventParams logstore.EventListParams
	getPayload  *logstore.PayloadReadRecord
	disabled    bool
}

func (s *diagnosticsLogStoreStub) LogEvent(event *logstore.Event) {
	s.events = append(s.events, event)
}

func (s *diagnosticsLogStoreStub) StorePayload(_ context.Context, payload *logstore.PayloadRecord) (int64, error) {
	s.payloads = append(s.payloads, payload)
	return int64(len(s.payloads)), nil
}

func (s *diagnosticsLogStoreStub) StoreClientDiagnostic(_ context.Context, diagnostic *logstore.ClientDiagnosticRecord) error {
	if s.disabled {
		return logstore.ErrDisabled
	}
	s.diagnostics = append(s.diagnostics, diagnostic)
	return nil
}

func (s *diagnosticsLogStoreStub) ListClientDiagnostics(_ context.Context, params logstore.ClientDiagnosticListParams) (*logstore.ClientDiagnosticListResult, error) {
	s.listParams = params
	if s.listResult != nil {
		return s.listResult, nil
	}
	return &logstore.ClientDiagnosticListResult{Items: []*logstore.ClientDiagnosticRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}

func (s *diagnosticsLogStoreStub) ListEvents(_ context.Context, params logstore.EventListParams) (*logstore.EventListResult, error) {
	s.eventParams = params
	if s.eventResult != nil {
		return s.eventResult, nil
	}
	return &logstore.EventListResult{Items: []*logstore.EventRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}

func (s *diagnosticsLogStoreStub) ListPayloads(_ context.Context, params logstore.PayloadListParams) (*logstore.PayloadListResult, error) {
	return &logstore.PayloadListResult{Items: []*logstore.PayloadReadRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}

func (s *diagnosticsLogStoreStub) GetPayload(_ context.Context, payloadID int64) (*logstore.PayloadReadRecord, error) {
	if s.getPayload != nil {
		return s.getPayload, nil
	}
	return nil, errors.New("not found")
}

func TestSanitizeClientDiagnosticsRequestRedactsSensitiveValues(t *testing.T) {
	t.Parallel()

	events, err := sanitizeClientDiagnosticsRequest(clientDiagnosticsRequest{
		Events: []clientDiagnosticEvent{
			{
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
				Source:    "gateway",
				Level:     "info",
				Name:      "ws.connect",
				Context: map[string]interface{}{
					"accessToken": "secret-token",
					"nested": map[string]interface{}{
						"sipPassword": "secret-password",
						"safe":        "kept",
					},
				},
			},
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("sanitize failed: %v", err)
	}

	ctx := events[0].Context
	if got := ctx["accessToken"]; got != "[redacted]" {
		t.Fatalf("expected accessToken redacted, got %#v", got)
	}
	nested := ctx["nested"].(map[string]interface{})
	if got := nested["sipPassword"]; got != "[redacted]" {
		t.Fatalf("expected sipPassword redacted, got %#v", got)
	}
	if got := nested["safe"]; got != "kept" {
		t.Fatalf("expected safe field kept, got %#v", got)
	}
}

func TestHandleClientDiagnosticsPersistsSessionAndNonSessionEvents(t *testing.T) {
	t.Parallel()

	store := &diagnosticsLogStoreStub{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
	body := `{
		"clientTraceId":"trace-1",
		"appVersion":"1.2.3",
		"platform":"ios",
		"deviceIdHash":"device-hash",
		"events":[
			{"id":"evt-session-1","timestamp":"2026-05-25T10:00:00Z","sessionId":"sess-1","source":"gateway","level":"info","name":"ws.connected","message":"connected","context":{"token":"secret","state":"ok"}},
			{"id":"evt-global-1","timestamp":"2026-05-25T10:00:01Z","source":"app","level":"warn","name":"app.boot","message":"booted","context":{"safe":"yes"}}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/client-diagnostics", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuthClaims(req, &auth.VerifiedClaims{
		Subject:           "user-1",
		Realm:             auth.TokenRealmUser,
		PreferredUsername: "alice",
	})
	rr := httptest.NewRecorder()

	srv.handleClientDiagnostics(rr, req)

	var resp clientDiagnosticsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rr.Code != http.StatusAccepted || resp.AcceptedEvents != 2 || resp.SessionEvents != 1 || resp.NonSessionEvents != 1 {
		t.Fatalf("unexpected response status=%d resp=%+v body=%s", rr.Code, resp, rr.Body.String())
	}
	if resp.ClientTraceID != "trace-1" || resp.StoredSessionEvents != 1 || resp.StoredNonSessionEvents != 1 || resp.DroppedEvents != 0 {
		t.Fatalf("expected additive response fields populated, got %+v", resp)
	}
	if len(store.events) != 1 {
		t.Fatalf("expected one call event, got %d", len(store.events))
	}
	if store.events[0].Category != "client" || store.events[0].SessionID != "sess-1" {
		t.Fatalf("unexpected call event: %+v", store.events[0])
	}
	if got := store.events[0].Data["context"].(map[string]interface{})["token"]; got != "[redacted]" {
		t.Fatalf("expected session context redacted, got %#v", got)
	}
	if got := store.events[0].Data["eventId"]; got != "evt-session-1" {
		t.Fatalf("expected session event id stored, got %#v", got)
	}
	if len(store.diagnostics) != 1 {
		t.Fatalf("expected one non-session diagnostic, got %d", len(store.diagnostics))
	}
	if store.diagnostics[0].AuthSubject != "user-1" || store.diagnostics[0].PreferredUsername != "alice" {
		t.Fatalf("expected auth metadata persisted, got %+v", store.diagnostics[0])
	}
	if got := store.diagnostics[0].Data["eventId"]; got != "evt-global-1" {
		t.Fatalf("expected non-session event id stored, got %#v", got)
	}
}

func TestHandleClientDiagnosticsRejectsInvalidPayloads(t *testing.T) {
	t.Parallel()

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, &diagnosticsLogStoreStub{})
	req := httptest.NewRequest(http.MethodPost, "/api/client-diagnostics", strings.NewReader(`{"events":[{"source":"app","level":"verbose","name":"bad"}]}`))
	req = withAuthClaims(req, &auth.VerifiedClaims{Subject: "user-1"})
	rr := httptest.NewRecorder()

	srv.handleClientDiagnostics(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleClientDiagnosticsRateLimitRejectsClearly(t *testing.T) {
	t.Parallel()

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, &diagnosticsLogStoreStub{})
	body := `{"clientTraceId":"trace-rate","events":[{"source":"app","level":"info","name":"app.boot"}]}`
	for i := 0; i < clientDiagnosticsRateLimitPerWindow; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/client-diagnostics", strings.NewReader(body))
		req = withAuthClaims(req, &auth.VerifiedClaims{Subject: "rate-user"})
		rr := httptest.NewRecorder()
		srv.handleClientDiagnostics(rr, req)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("expected warmup request %d accepted, got %d body=%s", i, rr.Code, rr.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/client-diagnostics", strings.NewReader(body))
	req = withAuthClaims(req, &auth.VerifiedClaims{Subject: "rate-user"})
	rr := httptest.NewRecorder()
	srv.handleClientDiagnostics(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleClientDiagnosticsDisabledStoreReturnsClearResponse(t *testing.T) {
	t.Parallel()

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, &diagnosticsLogStoreStub{disabled: true})
	req := httptest.NewRequest(http.MethodPost, "/api/client-diagnostics", strings.NewReader(`{"events":[{"source":"app","level":"info","name":"app.boot"}]}`))
	req = withAuthClaims(req, &auth.VerifiedClaims{Subject: "user-1"})
	rr := httptest.NewRecorder()

	srv.handleClientDiagnostics(rr, req)

	var resp clientDiagnosticsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rr.Code != http.StatusAccepted || resp.Persistence != "disabled" {
		t.Fatalf("expected disabled accepted response, status=%d resp=%+v", rr.Code, resp)
	}
}

func TestClientDiagnosticsRouteUsesAuthMiddleware(t *testing.T) {
	t.Parallel()

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, &diagnosticsLogStoreStub{})
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, _ auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw == "valid-token" {
				return &auth.VerifiedClaims{Subject: "user-1"}, nil
			}
			return nil, errors.New("invalid")
		},
	})
	router := mux.NewRouter()
	api := router.PathPrefix("/api").Subrouter()
	api.Use(srv.authMiddleware)
	api.HandleFunc("/client-diagnostics", srv.handleClientDiagnostics).Methods("POST")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/client-diagnostics", strings.NewReader(`{"events":[{"source":"app","level":"info","name":"app.boot"}]}`)))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing token 401, got %d", rr.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/client-diagnostics", strings.NewReader(`{"events":[{"source":"app","level":"info","name":"app.boot"}]}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected valid token accepted, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestListClientDiagnosticsDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	store := &diagnosticsLogStoreStub{
		listResult: &logstore.ClientDiagnosticListResult{
			Items: []*logstore.ClientDiagnosticRecord{
				{
					ID:            7,
					Timestamp:     time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC),
					ClientTraceID: "trace-1",
					AuthSubject:   "user-1",
					Source:        "app",
					Level:         "info",
					Name:          "app.boot",
					Data:          map[string]interface{}{"safe": "yes"},
				},
			},
			Total:    1,
			Page:     1,
			PageSize: 20,
		},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
	router := mux.NewRouter()
	router.HandleFunc("/api/client-diagnostics", srv.handleListClientDiagnostics).Methods("GET")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/client-diagnostics?clientTraceId=trace-1", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 without auth, got %d body=%s", rr.Code, rr.Body.String())
	}
	if store.listParams.ClientTraceID != "trace-1" {
		t.Fatalf("expected trace filter forwarded, got %+v", store.listParams)
	}
	var resp clientDiagnosticListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Name != "app.boot" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestListClientDiagnosticSessionEventsReturnsClientCategory(t *testing.T) {
	t.Parallel()

	store := &diagnosticsLogStoreStub{
		eventResult: &logstore.EventListResult{
			Items: []*logstore.EventRecord{
				{
					ID:        9,
					Timestamp: time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC),
					SessionID: "sess-1",
					Category:  "client",
					Name:      "incoming.received",
					Data:      map[string]interface{}{"clientTraceId": "trace-1"},
				},
			},
			Total:    1,
			Page:     1,
			PageSize: 20,
		},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
	rr := doRequest(t, srv.handleListClientDiagnosticSessionEvents, http.MethodGet, "/api/client-diagnostics/sessions/sess-1/events?page=1&pageSize=20", "", map[string]string{"sessionId": "sess-1"})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if store.eventParams.SessionID != "sess-1" || store.eventParams.Category != "client" {
		t.Fatalf("expected client event query params, got %+v", store.eventParams)
	}
	var resp EventListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Name != "incoming.received" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestGetClientDiagnosticPayloadOnlyReturnsDiagnosticPayload(t *testing.T) {
	t.Parallel()

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, &diagnosticsLogStoreStub{
		getPayload: &logstore.PayloadReadRecord{
			PayloadID: 1,
			Timestamp: time.Now().UTC(),
			SessionID: "s-1",
			Kind:      "webrtc_sdp_offer",
		},
	})
	rr := doRequest(t, srv.handleGetClientDiagnosticPayload, http.MethodGet, "/api/client-diagnostics/payloads/1", "", map[string]string{"payloadId": "1"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected non-diagnostic payload hidden, got %d body=%s", rr.Code, rr.Body.String())
	}
}
