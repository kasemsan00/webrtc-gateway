package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"webrtc-sip-gateway/internal/telemetry"
)

func TestTelemetryHTTPMiddlewareUsesRouteTemplateAndOmitsRequestContent(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256),
	}))
	server := &Server{}
	router := mux.NewRouter()
	router.Use(server.telemetryHTTPMiddleware)
	router.HandleFunc("/api/items/{itemId}", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	}).Methods(http.MethodPost)

	request := httptest.NewRequest(http.MethodPost, "/api/items/private-item?access_token=query-secret", strings.NewReader("body-secret"))
	request.Header.Set("Authorization", "Bearer header-secret")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d", recorder.Code)
	}
	text := output.String()
	if !strings.Contains(text, `"event.name":"http.request.completed"`) || !strings.Contains(text, `"reason":"forbidden"`) {
		t.Fatalf("missing bounded HTTP event: %s", text)
	}
	for _, forbidden := range []string{"private-item", "query-secret", "header-secret", "body-secret", "access_token", "Authorization"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("HTTP telemetry leaked %q: %s", forbidden, text)
		}
	}
}
