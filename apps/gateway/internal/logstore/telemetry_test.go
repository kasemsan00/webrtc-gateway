package logstore

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"webrtc-sip-gateway/internal/telemetry"
)

func TestFailedBatchTelemetryUsesBoundedFailureWithoutChangingBatchResult(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256)}))
	recordPersistenceBatchTelemetry("events", time.Now().Add(-time.Millisecond), 12, errors.New("postgres://user:secret@private.invalid/db"))
	text := output.String()
	if !strings.Contains(text, `"event.name":"persistence.batch.completed"`) || !strings.Contains(text, `"outcome":"failure"`) || !strings.Contains(text, `"reason":"dependency_unavailable"`) {
		t.Fatalf("missing bounded failed-batch telemetry: %s", text)
	}
	for _, forbidden := range []string{"postgres://", "secret", "private.invalid"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("failed-batch telemetry leaked %q: %s", forbidden, text)
		}
	}
}

func TestFullQueueDropTelemetryUsesCatalogEventWithoutChangingDropNewest(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256)}))
	store := &logStore{
		pool:       &pgxpool.Pool{},
		statsQueue: make(chan *StatsRecord, 1),
		eventQueue: make(chan *Event, 1),
	}
	store.statsQueue <- &StatsRecord{SessionID: "first"}
	store.RecordStats(&StatsRecord{SessionID: "dropped-secret-session"})
	if got := (<-store.statsQueue).SessionID; got != "first" {
		t.Fatalf("full queue must preserve earlier sample, got %q", got)
	}
	text := output.String()
	if !strings.Contains(text, `"event.name":"persistence.record.dropped"`) || !strings.Contains(text, `"reason":"queue_full"`) {
		t.Fatalf("missing drop event: %s", text)
	}
	if strings.Contains(text, "dropped-secret-session") {
		t.Fatalf("drop telemetry leaked session identifier: %s", text)
	}
}
