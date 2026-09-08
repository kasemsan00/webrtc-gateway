package config

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

var observabilityEnvKeys = []string{
	"OTEL_ENABLE", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_PROTOCOL",
	"OTEL_EXPORTER_OTLP_HEADERS", "OTEL_SERVICE_NAME", "OTEL_DEPLOYMENT_ENVIRONMENT",
	"OTEL_RESOURCE_ATTRIBUTES", "OTEL_METRIC_EXPORT_INTERVAL_MS", "OTEL_TRACES_ENABLE",
	"OTEL_TRACES_SAMPLER_ARG", "OTEL_BSP_MAX_QUEUE_SIZE", "OTEL_BSP_MAX_EXPORT_BATCH_SIZE",
	"OTEL_BSP_SCHEDULE_DELAY_MS", "OTEL_EXPORTER_OTLP_TIMEOUT_MS", "OTEL_SHUTDOWN_TIMEOUT_MS",
}

func clearObservabilityEnv(t *testing.T) {
	t.Helper()
	for _, key := range observabilityEnvKeys {
		t.Setenv(key, "")
	}
}

func TestObservabilityDefaultsDisabled(t *testing.T) {
	clearObservabilityEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Observability
	if got.Enable || got.Endpoint != "http://localhost:4318" || got.Protocol != "http/protobuf" {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if got.ServiceName != "webrtc-sip-gateway" || got.Environment != "unspecified" || got.TraceSampleRatio != 0.05 {
		t.Fatalf("unexpected identity/sampling defaults: %+v", got)
	}
	if got.MaxQueueSize != 2048 || got.MaxExportBatchSize != 512 || got.MetricsIntervalMS != 10000 {
		t.Fatalf("unexpected bounded defaults: %+v", got)
	}
}

func TestObservabilityEnabledProtocols(t *testing.T) {
	for _, protocol := range []string{"http/protobuf", "grpc"} {
		t.Run(protocol, func(t *testing.T) {
			clearObservabilityEnv(t)
			t.Setenv("OTEL_ENABLE", "true")
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if !cfg.Observability.Enable || cfg.Observability.Protocol != protocol {
				t.Fatalf("unexpected config: %+v", cfg.Observability)
			}
		})
	}
}

func TestObservabilityInvalidEnabledConfig(t *testing.T) {
	tests := []struct{ key, value, contains string }{
		{"OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4318", "ENDPOINT"},
		{"OTEL_EXPORTER_OTLP_PROTOCOL", "json", "PROTOCOL"},
		{"OTEL_METRIC_EXPORT_INTERVAL_MS", "999", "INTERVAL"},
		{"OTEL_TRACES_SAMPLER_ARG", "1.1", "SAMPLER"},
		{"OTEL_BSP_MAX_QUEUE_SIZE", "0", "QUEUE"},
		{"OTEL_BSP_MAX_EXPORT_BATCH_SIZE", "4096", "BATCH"},
		{"OTEL_BSP_SCHEDULE_DELAY_MS", "0", "DELAY"},
		{"OTEL_EXPORTER_OTLP_TIMEOUT_MS", "0", "TIMEOUT"},
		{"OTEL_SHUTDOWN_TIMEOUT_MS", "0", "SHUTDOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			clearObservabilityEnv(t)
			t.Setenv("OTEL_ENABLE", "true")
			t.Setenv(tt.key, tt.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestObservabilityMalformedValuesFailFast(t *testing.T) {
	for _, key := range []string{"OTEL_ENABLE", "OTEL_TRACES_ENABLE", "OTEL_METRIC_EXPORT_INTERVAL_MS", "OTEL_BSP_MAX_QUEUE_SIZE", "OTEL_TRACES_SAMPLER_ARG"} {
		t.Run(key, func(t *testing.T) {
			clearObservabilityEnv(t)
			t.Setenv("OTEL_ENABLE", "true")
			t.Setenv(key, "not-a-valid-value")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("expected fail-fast error naming %s, got %v", key, err)
			}
		})
	}
}

func TestPublicObservabilityViewOmitsSecretsAndEndpoint(t *testing.T) {
	cfg := &Config{Observability: ObservabilityConfig{
		Enable: true, Endpoint: "https://private-collector.example:4318", Headers: "Authorization=secret-token",
		ResourceAttributes: "private=value", Protocol: "http/protobuf", ServiceName: "gateway",
		Environment: "prod", MaxQueueSize: 4, MaxExportBatchSize: 2,
	}}
	encoded, err := json.Marshal(cfg.PublicView())
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"private-collector", "secret-token", "private=value", "headers", "resourceAttributes", `"endpoint"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public view leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"endpointConfigured":true`) {
		t.Fatalf("public view should report endpoint presence: %s", text)
	}
}

func TestObservabilityStartupSummaryOmitsEndpointHeadersAndAttributes(t *testing.T) {
	cfg := &Config{Observability: ObservabilityConfig{
		Enable: true, Endpoint: "https://private-collector.example:4318", Headers: "Authorization=secret-token",
		ResourceAttributes: "credential=private-value", Protocol: "http/protobuf", ServiceName: "gateway", Environment: "prod",
		TracesEnable: true, TraceSampleRatio: 0.05, MaxQueueSize: 4, MaxExportBatchSize: 2,
	}}
	temporary, err := os.CreateTemp(t.TempDir(), "display-*.log")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = temporary
	cfg.Display()
	os.Stdout = previous
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(temporary.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	encoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"private-collector", "secret-token", "private-value", "Authorization"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("startup summary leaked %q: %s", forbidden, text)
		}
	}
}

func TestSensitiveDiagnosticDefaultsRemainOff(t *testing.T) {
	clearObservabilityEnv(t)
	for _, key := range []string{"DEBUG_SIP_MESSAGE", "DEBUG_SIP_INVITE", "DEBUG_TURN", "DB_LOG_FULL_SIP", "PUSH_DEBUG_TTRS_TOKEN"} {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SIP.DebugSIPMessage || cfg.SIP.DebugSIPInvite || cfg.API.DebugTURN || cfg.DB.LogFullSIP {
		t.Fatalf("sensitive diagnostics enabled by default: sipMessage=%v sipInvite=%v turn=%v fullSIP=%v", cfg.SIP.DebugSIPMessage, cfg.SIP.DebugSIPInvite, cfg.API.DebugTURN, cfg.DB.LogFullSIP)
	}
}

func TestSensitiveDiagnosticExplicitDebugModeHasResidualExposure(t *testing.T) {
	clearObservabilityEnv(t)
	for _, key := range []string{"DEBUG_SIP_MESSAGE", "DEBUG_SIP_INVITE", "DEBUG_TURN", "DB_LOG_FULL_SIP"} {
		t.Setenv(key, "true")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SIP.DebugSIPMessage || !cfg.SIP.DebugSIPInvite || !cfg.API.DebugTURN || !cfg.DB.LogFullSIP {
		t.Fatalf("explicit debug toggles were not honored: %+v %+v %+v", cfg.SIP, cfg.API, cfg.DB)
	}
	// These modes intentionally retain local diagnostic exposure; the
	// observability contract prohibits routing their raw records centrally.
}
