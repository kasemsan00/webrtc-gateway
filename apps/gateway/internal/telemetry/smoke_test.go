package telemetry_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"webrtc-sip-gateway/internal/telemetry"
)

func TestSmokeSignalsCarryResourceIdentityAndOmitSeededSecrets(t *testing.T) {
	var output bytes.Buffer
	logger := telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format: telemetry.LogFormatJSON,
		Resource: telemetry.ResourceIdentity{
			ServiceName: "webrtc-sip-gateway", ServiceVersion: "test",
			Environment: "staging-test", GatewayInstanceID: "instance-smoke",
		},
		Sanitizer: telemetry.NewSanitizer(256),
	})
	telemetry.SetStructuredLogger(logger)
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(recorder))
	_, span := provider.Tracer("smoke").Start(context.Background(), "call.setup")
	telemetry.SetSpanCorrelation(span, telemetry.Correlation{SessionID: "safe-session"})
	telemetry.EndSpan(span, "failure", "export_failed", 0)
	_ = logger.Log(context.Background(), telemetry.LogEvent{
		Severity: telemetry.SeverityInfo, Component: "call", Name: "call.completed",
		Outcome: "failure", Reason: "export_failed", Correlation: telemetry.Correlation{SessionID: "safe-session"},
		Measurements: []telemetry.Measurement{telemetry.DurationMilliseconds("duration_ms", 12*time.Millisecond)},
	})
	_ = logger.Log(context.Background(), telemetry.LogEvent{
		Severity: telemetry.SeverityWarn, Component: "telemetry", Name: "telemetry.export.failed",
		Outcome: "failure", Reason: "export_failed",
	})

	text := output.String()
	for _, want := range []string{
		`"service.name":"webrtc-sip-gateway"`, `"deployment.environment":"staging-test"`,
		`"service.instance.id":"instance-smoke"`, `"session.id":"safe-session"`,
		`"event.name":"call.completed"`, `"event.name":"telemetry.export.failed"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	for _, forbidden := range []string{"Bearer secret-token", "postgres://", "sip:alice", "password=s3cret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("smoke telemetry leaked %q: %s", forbidden, text)
		}
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "call.setup" {
		t.Fatalf("unexpected spans: %#v", spans)
	}
	foundSession := false
	for _, attr := range spans[0].Attributes() {
		if string(attr.Key) == "session.id" && attr.Value.AsString() == "safe-session" {
			foundSession = true
		}
		emitted := attr.Value.Emit()
		if strings.Contains(emitted, "secret-token") || strings.Contains(emitted, "password") {
			t.Fatalf("span leaked secret attribute %s=%s", attr.Key, emitted)
		}
	}
	if !foundSession {
		t.Fatal("sampled call span omitted session correlation")
	}
}
