package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStructuredLoggerGolden(t *testing.T) {
	t.Parallel()

	for _, format := range []LogFormat{LogFormatJSON, LogFormatText} {
		format := format
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			logger := NewStructuredLogger(&output, StructuredLogConfig{
				Format: format,
				Resource: ResourceIdentity{
					ServiceName:       "webrtc-sip-gateway",
					ServiceVersion:    "1.2.3",
					Environment:       "test",
					GatewayInstanceID: "gateway-a",
				},
				Sanitizer: NewSanitizer(256),
				Now: func() time.Time {
					return time.Date(2026, 9, 7, 4, 5, 6, 123456789, time.FixedZone("ICT", 7*60*60))
				},
			})
			err := logger.Log(context.Background(), LogEvent{
				Severity:  SeverityInfo,
				Component: "session",
				Name:      "call.completed",
				Outcome:   "success",
				Reason:    "normal",
				Correlation: Correlation{
					SessionID:         "session-123",
					CallCorrelationID: "corr-456",
					TraceID:           "0123456789abcdef0123456789abcdef",
					SpanID:            "0123456789abcdef",
					TrunkID:           17,
				},
				Measurements: []Measurement{
					Int64Measurement("packets", 42),
					DurationMilliseconds("duration_ms", 1500*time.Millisecond),
				},
			})
			if err != nil {
				t.Fatalf("Log: %v", err)
			}

			goldenPath := filepath.Join("testdata", "structured_log."+string(format)+".golden")
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file: %v", err)
			}
			if output.String() != string(want) {
				t.Fatalf("output mismatch\nwant: %s\n got: %s", want, output.String())
			}

			if format == LogFormatJSON {
				var decoded map[string]any
				if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
					t.Fatalf("collector cannot consume JSON record: %v", err)
				}
				for _, key := range []string{"timestamp", "severity", "service.name", "deployment.environment", "service.instance.id", "component", "event.name"} {
					if _, ok := decoded[key]; !ok {
						t.Errorf("structured record lacks stable field %q", key)
					}
				}
			}
		})
	}
}

func TestStructuredLoggerSanitizesBeforeSerialization(t *testing.T) {
	t.Parallel()

	fixtures := []string{
		"Bearer structured-auth-secret",
		"postgres://gateway:structured-dsn-secret@db.example/gateway",
		"push-token-secret-fixture",
		"transcript-secret-fixture",
		"203.0.113.91",
	}
	var output bytes.Buffer
	logger := NewStructuredLogger(&output, StructuredLogConfig{
		Format:    LogFormatJSON,
		Resource:  ResourceIdentity{ServiceName: "gateway", GatewayInstanceID: fixtures[4]},
		Sanitizer: NewSanitizer(256),
		Now:       func() time.Time { return time.Unix(0, 0) },
	})
	err := logger.Log(context.Background(), LogEvent{
		Severity:  SeverityError,
		Component: "push",
		Name:      "push.failed",
		Outcome:   "error",
		Reason:    "Authorization: " + fixtures[0] + " dsn=" + fixtures[1],
		Correlation: Correlation{
			SessionID:         "session-safe",
			CallCorrelationID: "sip:private-user@example.test",
		},
		Measurements: []Measurement{
			{Name: "push_token", Value: fixtures[2]},
			{Name: "transcript", Value: fixtures[3]},
			{Name: "invalid\nname", Value: 9},
			Int64Measurement("attempt", 1),
		},
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	for _, fixture := range fixtures {
		if strings.Contains(output.String(), fixture) {
			t.Errorf("structured record contains raw fixture %q: %s", fixture, output.String())
		}
	}

	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode structured record: %v", err)
	}
	measurements, ok := decoded["measurements"].(map[string]any)
	if !ok {
		t.Fatalf("measurements has type %T", decoded["measurements"])
	}
	if _, present := measurements["push_token"]; present {
		t.Error("sensitive measurement was serialized")
	}
	if _, present := measurements["invalid\nname"]; present {
		t.Error("invalid measurement name was serialized")
	}
	if measurements["attempt"] != float64(1) {
		t.Errorf("safe typed measurement = %#v, want 1", measurements["attempt"])
	}
}

func TestStructuredLoggerRetainsWriterAndIsNilSafe(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := NewStructuredLogger(&output, StructuredLogConfig{Format: LogFormatText})
	if err := logger.Log(context.Background(), LogEvent{Name: "gateway.started"}); err != nil {
		t.Fatalf("write existing writer: %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("existing local/stdout writer received no record")
	}

	var nilLogger *StructuredLogger
	if err := nilLogger.Log(context.Background(), LogEvent{}); err != nil {
		t.Fatalf("nil logger returned error: %v", err)
	}
}
