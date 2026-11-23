package logstore

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/telemetry"
)

type stubBatchResults struct {
	execErrs []error
	execIdx  int
	closed   bool
}

func (r *stubBatchResults) Exec() (pgconn.CommandTag, error) {
	idx := r.execIdx
	r.execIdx++
	if idx < len(r.execErrs) && r.execErrs[idx] != nil {
		return pgconn.CommandTag{}, r.execErrs[idx]
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (r *stubBatchResults) Query() (pgx.Rows, error) { return nil, nil }
func (r *stubBatchResults) QueryRow() pgx.Row        { return nil }
func (r *stubBatchResults) Close() error {
	r.closed = true
	return nil
}
func (r *stubBatchResults) Tx() pgx.Tx { return nil }

func drainCopyRows(t *testing.T, rowSrc pgx.CopyFromSource) [][]any {
	t.Helper()
	var rows [][]any
	for rowSrc.Next() {
		vals, err := rowSrc.Values()
		if err != nil {
			t.Fatalf("rowSrc.Values() error = %v", err)
		}
		rowCopy := make([]any, len(vals))
		copy(rowCopy, vals)
		rows = append(rows, rowCopy)
	}
	if err := rowSrc.Err(); err != nil {
		t.Fatalf("rowSrc.Err() = %v", err)
	}
	return rows
}

func TestBatchInsertEventsUsesCopyFromWithoutFallback(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	payloadID := int64(77)

	var (
		gotTable  pgx.Identifier
		gotCols   []string
		gotRows   [][]any
		batchCall int
	)

	store := &logStore{
		copyFromHook: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
			gotTable = tableName
			gotCols = append([]string(nil), columnNames...)
			gotRows = drainCopyRows(t, rowSrc)
			return int64(len(gotRows)), nil
		},
		sendBatchHook: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
			batchCall++
			return &stubBatchResults{}
		},
	}

	events := []*Event{
		{
			Timestamp:     now,
			SessionID:     "sess-1",
			Category:      "sip",
			Name:          "sip_invite_sent",
			SIPMethod:     "INVITE",
			SIPStatusCode: 200,
			SIPCallID:     "cid-1",
			State:         "active",
			PayloadID:     &payloadID,
			Data:          map[string]interface{}{"k": "v"},
		},
		nil,
		{
			Timestamp: now.Add(time.Second),
			SessionID: "sess-2",
			Category:  "ws",
			Name:      "ws_connected",
		},
	}

	if err := store.batchInsertEvents(context.Background(), events); err != nil {
		t.Fatalf("batchInsertEvents() error = %v", err)
	}
	if batchCall != 0 {
		t.Fatalf("expected CopyFrom success to skip pgx.Batch fallback, got %d fallback calls", batchCall)
	}
	if !reflect.DeepEqual(gotTable, pgx.Identifier{"call_events"}) {
		t.Fatalf("table = %v, want [call_events]", gotTable)
	}
	if !reflect.DeepEqual(gotCols, callEventsColumns) {
		t.Fatalf("columns = %v, want %v", gotCols, callEventsColumns)
	}
	if len(gotRows) != 2 {
		t.Fatalf("copied rows = %d, want 2", len(gotRows))
	}
	if gotRows[0][1] != "sess-1" || gotRows[0][4] != "INVITE" || gotRows[0][5] != 200 {
		t.Fatalf("unexpected row[0]: %#v", gotRows[0])
	}
	if string(gotRows[0][9].([]byte)) != `{"k":"v"}` {
		t.Fatalf("row[0] json = %q, want %q", string(gotRows[0][9].([]byte)), `{"k":"v"}`)
	}
}

func TestBatchInsertEventsFallsBackToBatchOnCopyFromError(t *testing.T) {
	now := time.Now().UTC()

	t.Run("fallback succeeds", func(t *testing.T) {
		var batchLen int
		res := &stubBatchResults{}
		store := &logStore{
			copyFromHook: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
				return 0, errors.New("copy protocol error")
			},
			sendBatchHook: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
				batchLen = b.Len()
				return res
			},
		}

		err := store.batchInsertEvents(context.Background(), []*Event{
			{Timestamp: now, SessionID: "s1", Category: "sip", Name: "ev1"},
			{Timestamp: now, SessionID: "s2", Category: "sip", Name: "ev2"},
		})
		if err != nil {
			t.Fatalf("batchInsertEvents() fallback error = %v", err)
		}
		if batchLen != 2 {
			t.Fatalf("fallback batch len = %d, want 2", batchLen)
		}
		if res.execIdx != 2 || !res.closed {
			t.Fatalf("expected 2 Exec calls and Close, got execIdx=%d closed=%v", res.execIdx, res.closed)
		}
	})

	t.Run("fallback reports failing row index", func(t *testing.T) {
		res := &stubBatchResults{
			execErrs: []error{nil, errors.New("bad row constraint")},
		}
		store := &logStore{
			copyFromHook: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
				return 0, errors.New("copy failed")
			},
			sendBatchHook: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
				return res
			},
		}

		err := store.batchInsertEvents(context.Background(), []*Event{
			{Timestamp: now, SessionID: "s1", Category: "sip", Name: "ev1"},
			{Timestamp: now, SessionID: "s2", Category: "sip", Name: "ev2"},
		})
		if err == nil || !strings.Contains(err.Error(), "failed to insert event 1") || !strings.Contains(err.Error(), "bad row constraint") {
			t.Fatalf("unexpected fallback error: %v", err)
		}
		if !res.closed {
			t.Fatal("expected batchResults.Close() to be called on error")
		}
	})
}

func TestBatchInsertStatsUsesCopyFromAndFallback(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	lastPLI := now.Add(-2 * time.Second)
	lastKF := now.Add(-1 * time.Second)

	t.Run("CopyFrom happy path", func(t *testing.T) {
		var (
			gotTable  pgx.Identifier
			gotCols   []string
			gotRows   [][]any
			batchCall int
		)
		store := &logStore{
			copyFromHook: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
				gotTable = tableName
				gotCols = append([]string(nil), columnNames...)
				gotRows = drainCopyRows(t, rowSrc)
				return int64(len(gotRows)), nil
			},
			sendBatchHook: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
				batchCall++
				return &stubBatchResults{}
			},
		}

		stats := []*StatsRecord{
			{
				Timestamp:      now,
				SessionID:      "sess-stats-1",
				PLISent:        4,
				PLIResponse:    3,
				LastPLISentAt:  &lastPLI,
				LastKeyframeAt: &lastKF,
				AudioRTCPRR:    10,
				AudioRTCPSR:    11,
				VideoRTCPRR:    20,
				VideoRTCPSR:    21,
				Data:           map[string]interface{}{"jitter": 3},
			},
		}

		if err := store.batchInsertStats(context.Background(), stats); err != nil {
			t.Fatalf("batchInsertStats() error = %v", err)
		}
		if batchCall != 0 {
			t.Fatalf("expected CopyFrom success to skip fallback, got %d calls", batchCall)
		}
		if !reflect.DeepEqual(gotTable, pgx.Identifier{"call_stats"}) {
			t.Fatalf("table = %v, want [call_stats]", gotTable)
		}
		if !reflect.DeepEqual(gotCols, callStatsColumns) {
			t.Fatalf("columns = %v, want %v", gotCols, callStatsColumns)
		}
		if len(gotRows) != 1 {
			t.Fatalf("copied rows = %d, want 1", len(gotRows))
		}
		if gotRows[0][1] != "sess-stats-1" || gotRows[0][2] != 4 || gotRows[0][9] != 21 {
			t.Fatalf("unexpected row[0]: %#v", gotRows[0])
		}
		if string(gotRows[0][10].([]byte)) != `{"jitter":3}` {
			t.Fatalf("row[0] json = %q, want %q", string(gotRows[0][10].([]byte)), `{"jitter":3}`)
		}
	})

	t.Run("CopyFrom error falls back to pgx.Batch", func(t *testing.T) {
		var batchLen int
		res := &stubBatchResults{
			execErrs: []error{errors.New("stats constraint error")},
		}
		store := &logStore{
			copyFromHook: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
				return 0, errors.New("copy stats failed")
			},
			sendBatchHook: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
				batchLen = b.Len()
				return res
			},
		}

		err := store.batchInsertStats(context.Background(), []*StatsRecord{
			{Timestamp: now, SessionID: "sess-stats-bad"},
		})
		if err == nil || !strings.Contains(err.Error(), "failed to insert stats 0") || !strings.Contains(err.Error(), "stats constraint error") {
			t.Fatalf("unexpected stats fallback error: %v", err)
		}
		if batchLen != 1 || !res.closed {
			t.Fatalf("expected fallback batch len=1 and closed=true, got len=%d closed=%v", batchLen, res.closed)
		}
	})
}

func TestBatchWorkersUpdateLastSuccessAndTelemetryWithCopyFrom(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format:    telemetry.LogFormatJSON,
		Sanitizer: telemetry.NewSanitizer(256),
	}))

	var mu sync.Mutex
	var copiedTables []string

	store := &logStore{
		config: config.DBConfig{
			BatchSize:       10,
			BatchIntervalMS: 25,
		},
		copyFromHook: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
			rows := drainCopyRows(t, rowSrc)
			mu.Lock()
			copiedTables = append(copiedTables, tableName.Sanitize())
			mu.Unlock()
			return int64(len(rows)), nil
		},
	}
	store.ensureAsyncQueues()
	store.wg.Add(2)
	go store.eventBatchWorker()
	go store.statsBatchWorker()

	store.eventQueue <- &Event{Timestamp: time.Now(), SessionID: "s1", Category: "ws", Name: "open"}
	store.statsQueue <- &StatsRecord{Timestamp: time.Now(), SessionID: "s1", PLISent: 1}

	if err := store.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if store.lastSuccessNS.Load() == 0 {
		t.Fatal("expected lastSuccessNS to be updated after CopyFrom batch flushes")
	}

	mu.Lock()
	tables := append([]string(nil), copiedTables...)
	mu.Unlock()
	if len(tables) != 2 {
		t.Fatalf("expected 2 CopyFrom calls (events + stats), got %v", tables)
	}
	text := output.String()
	if strings.Count(text, `"event.name":"persistence.batch.completed"`) < 2 || !strings.Contains(text, `"outcome":"success"`) {
		t.Fatalf("expected persistence.batch.completed success telemetry for both batches, got: %s", text)
	}
}
