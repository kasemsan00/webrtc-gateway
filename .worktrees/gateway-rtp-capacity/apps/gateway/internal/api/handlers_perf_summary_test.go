package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"k2-gateway/internal/auth"
	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/observability"
)

type perfSummaryTokenVerifierStub struct{}

func (perfSummaryTokenVerifierStub) VerifyToken(context.Context, string) (*auth.VerifiedClaims, error) {
	return &auth.VerifiedClaims{}, nil
}

func TestHandlePerfSummary_ReturnsSnapshot(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/perf-summary", nil)
	srv := &Server{
		metrics: observability.NewRegistry(nil),
	}

	srv.handlePerfSummary(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", rr.Code)
	}

	assertJSONHasKeys(t, rr.Body.Bytes(), "windows", "counters", "timersMsP95", "runtime")
}

func TestHandlePerfSummary_RequiresAuthWhenEnabled(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/perf-summary", nil)
	srv := &Server{
		metrics:       observability.NewRegistry(nil),
		tokenVerifier: perfSummaryTokenVerifierStub{},
	}
	handler := srv.authMiddleware(http.HandlerFunc(srv.handlePerfSummary))

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d", rr.Code)
	}
}

func assertJSONHasKeys(t *testing.T, body []byte, keys ...string) {
	t.Helper()
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	for _, key := range keys {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q", key)
		}
	}
}

func TestServerStartsSnapshotLogger(t *testing.T) {
	srv := NewServer(
		config.APIConfig{Port: 0, EnableREST: false, EnableWS: false},
		config.TURNConfig{},
		config.GatewayConfig{},
		nil,
		nil,
		nil,
		nil,
		mustNoopLogStore(t),
	)
	var started atomic.Int32
	var stopped atomic.Int32
	srv.snapshotStarter = func() func() {
		started.Add(1)
		return func() { stopped.Add(1) }
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	if started.Load() != 1 {
		t.Fatalf("expected snapshot starter to run once, got %d", started.Load())
	}
	// Start returns only after deferred cleanup runs.
	if stopped.Load() != 1 {
		t.Fatalf("expected snapshot stopper to run once, got %d", stopped.Load())
	}

}

func mustNoopLogStore(t *testing.T) logstore.LogStore {
	t.Helper()
	store, err := logstore.New(config.DBConfig{Enable: false})
	if err != nil {
		t.Fatalf("failed to create noop logstore: %v", err)
	}
	return store
}
