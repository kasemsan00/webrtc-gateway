package push

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/telemetry"
)

func TestTTRSFetchTelemetryOmitsTokenURLAndBody(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256),
	}))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "user-secret") {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"status":"ok","data":[{"user_id":"user-secret","token":"push-token-secret","mobile_device":"device-secret"}]}`))
			return
		}
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	client := &TTRSClient{baseURL: server.URL, httpClient: server.Client()}
	entries, err := client.FetchNotifications(context.Background(), "user-secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Token != "push-token-secret" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	text := output.String()
	if !strings.Contains(text, `"event.name":"push.request.completed"`) || !strings.Contains(text, `"outcome":"success"`) {
		t.Fatalf("missing bounded TTRS telemetry: %s", text)
	}
	for _, forbidden := range []string{"push-token-secret", "device-secret", "user-secret", "token=", "Authorization"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("TTRS telemetry leaked %q: %s", forbidden, text)
		}
	}
}

func TestTTRSFetchTimeoutUsesNormalizedOutcome(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256),
	}))
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()
	client := &TTRSClient{baseURL: server.URL, httpClient: &http.Client{Timeout: time.Hour}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.FetchNotifications(ctx, "user-secret")
	if err == nil {
		t.Fatal("expected timeout")
	}
	text := output.String()
	if !strings.Contains(text, `"outcome":"timeout"`) || !strings.Contains(text, `"reason":"deadline_exceeded"`) {
		t.Fatalf("missing timeout telemetry: %s", text)
	}
	if strings.Contains(text, "user-secret") {
		t.Fatalf("timeout telemetry leaked user id: %s", text)
	}
}
