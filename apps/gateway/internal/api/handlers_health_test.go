package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

type healthStoreStub struct {
	logstore.LogStore
	health logstore.Health
}

func (s *healthStoreStub) LogStoreHealth() logstore.Health { return s.health }

type fixedHealthProvider struct{ snapshot HealthComponentResponse }

func (p fixedHealthProvider) OperationalHealth() HealthComponentResponse { return p.snapshot }

func TestDashboardDatabaseDisabledReportsDisconnected(t *testing.T) {
	store, err := logstore.New(config.DBConfig{Enable: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	server := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
	recorder := httptest.NewRecorder()
	server.handleDashboard(recorder, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response DashboardResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.DBConnected {
		t.Fatal("database-disabled noop store must not report dbConnected")
	}
}

func TestDetailedHealthIsBoundedAndReportsDisabledDatabase(t *testing.T) {
	store, err := logstore.New(config.DBConfig{Enable: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	server := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
	recorder := httptest.NewRecorder()
	server.handleDetailedHealth(recorder, httptest.NewRequest(http.MethodGet, "/api/health/details", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response DetailedHealthResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := response.Components["database"].State; got != healthDisabled {
		t.Fatalf("database state = %q, want %q", got, healthDisabled)
	}
	if response.Components["database"].Reason != "database_logging_disabled" {
		t.Fatalf("unexpected database reason: %#v", response.Components["database"])
	}
}

func TestPersistenceHealthStates(t *testing.T) {
	tests := []struct {
		name   string
		health logstore.Health
		want   healthState
	}{
		{name: "connected", health: logstore.Health{Enabled: true, Connected: true}, want: healthConnected},
		{name: "degraded drop", health: logstore.Health{Enabled: true, Connected: true, Stats: logstore.QueueHealth{Dropped: 1}}, want: healthDegraded},
		{name: "unavailable", health: logstore.Health{Enabled: true, Connected: false}, want: healthUnavailable},
		{name: "disabled", health: logstore.Health{Enabled: false}, want: healthDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, &healthStoreStub{health: tt.health})
			got, _ := server.persistenceHealth()
			if got.State != tt.want {
				t.Fatalf("state = %q, want %q", got.State, tt.want)
			}
		})
	}

	server := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	got, _ := server.persistenceHealth()
	if got.State != healthUnavailable {
		t.Fatalf("nil store state = %q, want unavailable", got.State)
	}
	unknownServer := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, &apiHandlerLogStoreStub{})
	unknown, _ := unknownServer.persistenceHealth()
	if unknown.State != healthUnknown {
		t.Fatalf("store without reporter state = %q, want unknown", unknown.State)
	}
}

func TestDetailedHealthUsesRegisteredCachedProvider(t *testing.T) {
	store, err := logstore.New(config.DBConfig{Enable: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	server := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
	server.SetOperationalHealthProvider("translator", fixedHealthProvider{snapshot: HealthComponentResponse{State: healthDegraded, Reason: "cached_probe_failed"}})
	recorder := httptest.NewRecorder()
	server.handleDetailedHealth(recorder, httptest.NewRequest(http.MethodGet, "/api/health/details", nil))
	var response DetailedHealthResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := response.Components["translator"]; got.State != healthDegraded || got.Reason != "cached_probe_failed" {
		t.Fatalf("provider snapshot = %#v", got)
	}
}

func TestDetailedHealthRedactsUnsafeProviderReason(t *testing.T) {
	store, err := logstore.New(config.DBConfig{Enable: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	server := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
	server.SetOperationalHealthProvider("push", fixedHealthProvider{snapshot: HealthComponentResponse{State: healthUnavailable, Reason: "postgres://user:token@example.invalid/db"}})
	recorder := httptest.NewRecorder()
	server.handleDetailedHealth(recorder, httptest.NewRequest(http.MethodGet, "/api/health/details", nil))
	if body := recorder.Body.String(); strings.Contains(body, "token") || strings.Contains(body, "postgres") {
		t.Fatalf("detailed health leaked unsafe reason: %s", body)
	}
}

func TestSessionOverviewAllowListsPersistedFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	trunkID := int64(42)
	store := &apiHandlerLogStoreStub{listResult: &logstore.SessionListResult{Items: []*logstore.SessionRecord{{
		SessionID: "session-1", CreatedAt: now, UpdatedAt: now, SIPCallID: "call-1",
		AuthMode: "trunk", TrunkID: &trunkID, TrunkName: "edge", SIPUsername: "1001",
		RTPAudioPort: 10000, RTCPAudioPort: 10001, SIPOpusPT: 111,
		Meta: map[string]interface{}{"password": "must-not-leak", "accountKey": "also-secret"},
	}}}}
	server := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
	recorder := doRequest(t, server.handleGetSessionOverview, http.MethodGet, "/api/sessions/session-1/overview", "", map[string]string{"sessionId": "session-1"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := store.listParams.SessionID; got != "session-1" {
		t.Fatalf("session filter = %q", got)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "must-not-leak") || strings.Contains(body, "also-secret") || strings.Contains(body, "accountKey") {
		t.Fatalf("overview leaked metadata: %s", body)
	}
}

func TestSessionOverviewHandlesLegacyAndUnknownSessions(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store := &apiHandlerLogStoreStub{listResult: &logstore.SessionListResult{Items: []*logstore.SessionRecord{{SessionID: "legacy", CreatedAt: now, UpdatedAt: now}}}}
	server := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
	legacy := doRequest(t, server.handleGetSessionOverview, http.MethodGet, "/api/sessions/legacy/overview", "", map[string]string{"sessionId": "legacy"})
	if legacy.Code != http.StatusOK {
		t.Fatalf("legacy status = %d body=%s", legacy.Code, legacy.Body.String())
	}
	if strings.Contains(legacy.Body.String(), "password") {
		t.Fatalf("legacy response exposed unexpected fields: %s", legacy.Body.String())
	}
	store.listResult = &logstore.SessionListResult{}
	missing := doRequest(t, server.handleGetSessionOverview, http.MethodGet, "/api/sessions/missing/overview", "", map[string]string{"sessionId": "missing"})
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d body=%s", missing.Code, missing.Body.String())
	}
}

func TestSessionOverviewIncludesActiveOverlay(t *testing.T) {
	mgr := newTestSessionManager()
	active, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() { mgr.DeleteSession(active.ID) })
	active.SetState(session.StateActive)
	now := time.Now().UTC()
	store := &apiHandlerLogStoreStub{listResult: &logstore.SessionListResult{Items: []*logstore.SessionRecord{{SessionID: active.ID, CreatedAt: now, UpdatedAt: now}}}}
	server := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, store)
	recorder := doRequest(t, server.handleGetSessionOverview, http.MethodGet, "/api/sessions/"+active.ID+"/overview", "", map[string]string{"sessionId": active.ID})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response SessionOverviewResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.LiveState != string(session.StateActive) {
		t.Fatalf("liveState = %q, want active", response.LiveState)
	}
}
