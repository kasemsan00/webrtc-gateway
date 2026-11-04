package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/telemetry"
)

func TestVersionIsPublicAndReturnsBuildFields(t *testing.T) {
	srv := &Server{config: config.APIConfig{EnableREST: true}}
	srv.SetAdminPassword("admin-secret") // REST auth on
	router := srv.newRouter()

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/api/version without token = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	for _, key := range []string{"version", "commit", "branch", "buildTime", "dirty", "goVersion"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing %q in %s", key, rr.Body.String())
		}
	}
	if _, ok := body["dirty"].(bool); !ok {
		t.Fatalf("dirty is not a bool: %s", rr.Body.String())
	}
	if body["goVersion"] != runtime.Version() {
		t.Fatalf("goVersion = %v", body["goVersion"])
	}

	// Control: the rest of /api still needs a token.
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/health/details", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("/api/health/details without token = %d, want 401", rr.Code)
	}
}

func TestVersionAvailableWithRESTDisabled(t *testing.T) {
	srv := &Server{}
	rr := httptest.NewRecorder()
	srv.newRouter().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/api/version with REST disabled = %d, want 200", rr.Code)
	}
}

func TestVersionIsExcludedFromHTTPTelemetry(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256),
	}))
	srv := &Server{config: config.APIConfig{EnableREST: true}}
	srv.SetAdminPassword("admin-secret")
	router := srv.newRouter()

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if strings.Contains(output.String(), "http.request.completed") {
		t.Fatalf("/api/version was traced: %s", output.String())
	}
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if !strings.Contains(output.String(), "http.request.completed") {
		t.Fatal("control route was not traced; the exemption test proves nothing")
	}
}
