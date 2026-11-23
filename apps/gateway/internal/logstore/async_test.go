package logstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
)

func TestWithDefaultTimeout(t *testing.T) {
	t.Run("adds timeout to background context", func(t *testing.T) {
		ctx, cancel := withDefaultTimeout(context.Background(), 2*time.Second)
		defer cancel()

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected deadline to be set on background context")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > 2*time.Second+100*time.Millisecond {
			t.Fatalf("unexpected remaining deadline: %s", remaining)
		}
	})

	t.Run("respects shorter caller deadline", func(t *testing.T) {
		parent, parentCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer parentCancel()
		parentDeadline, _ := parent.Deadline()

		ctx, cancel := withDefaultTimeout(parent, 3*time.Second)
		defer cancel()

		gotDeadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected deadline to be preserved")
		}
		if !gotDeadline.Equal(parentDeadline) {
			t.Fatalf("deadline = %v, want %v", gotDeadline, parentDeadline)
		}
	})

	t.Run("handles nil context safely", func(t *testing.T) {
		ctx, cancel := withDefaultTimeout(nil, time.Second)
		defer cancel()
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("expected deadline on nil context")
		}
	})
}

func TestEffectivePoolMaxConns(t *testing.T) {
	cases := []struct {
		in   int
		want int32
	}{
		{in: 0, want: int32(config.DefaultDBMaxConns)},
		{in: -10, want: int32(config.DefaultDBMaxConns)},
		{in: 1, want: int32(config.MinDBMaxConns)},
		{in: 3, want: int32(config.MinDBMaxConns)},
		{in: 25, want: 25},
		{in: 64, want: 64},
		{in: 999, want: int32(config.MaxDBMaxConns)},
	}
	for _, tc := range cases {
		if got := effectivePoolMaxConns(tc.in); got != tc.want {
			t.Fatalf("effectivePoolMaxConns(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestBuildListSessionsQueriesOmitsTrunkJoinOnCountWhenNoSearch(t *testing.T) {
	countSQL, dataSQL, countArgs, dataArgs := buildListSessionsQueries(SessionListParams{
		Page:      1,
		PageSize:  20,
		Direction: "outbound",
	})
	if strings.Contains(countSQL, "LEFT JOIN sip_trunks") {
		t.Fatalf("expected countSQL without search to omit LEFT JOIN sip_trunks, got:\n%s", countSQL)
	}
	if !strings.Contains(dataSQL, "LEFT JOIN sip_trunks") {
		t.Fatalf("expected dataSQL to include LEFT JOIN sip_trunks, got:\n%s", dataSQL)
	}
	if !strings.Contains(dataSQL, "cs.trunk_id") {
		t.Fatalf("expected dataSQL join to prefer typed cs.trunk_id, got:\n%s", dataSQL)
	}
	if len(countArgs) != 1 || len(dataArgs) != 3 {
		t.Fatalf("unexpected arg counts: count=%d data=%d", len(countArgs), len(dataArgs))
	}

	searchCountSQL, _, searchCountArgs, _ := buildListSessionsQueries(SessionListParams{
		Page:     1,
		PageSize: 20,
		Search:   "1001",
	})
	if !strings.Contains(searchCountSQL, "LEFT JOIN sip_trunks") {
		t.Fatalf("expected countSQL with search to include LEFT JOIN sip_trunks, got:\n%s", searchCountSQL)
	}
	if len(searchCountArgs) != 4 {
		t.Fatalf("expected 4 search count args, got %d", len(searchCountArgs))
	}
}

func TestNoopStoreResolveTrunkDetailsByCredentials(t *testing.T) {
	store, err := New(config.DBConfig{Enable: false})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	resolver, ok := store.(TrunkDetailsResolver)
	if !ok {
		t.Fatal("expected noop store to implement TrunkDetailsResolver")
	}
	trunkID, publicID, resolvedUsername, leaseOwner, leaseUntil, found, err := resolver.ResolveTrunkDetailsByCredentials(context.Background(), "sip.example.com", 5060, "1001", "secret")
	if err != nil || found || trunkID != 0 || publicID != "" || resolvedUsername != "" || leaseOwner != nil || leaseUntil != nil {
		t.Fatalf("unexpected noop ResolveTrunkDetailsByCredentials result: id=%d pub=%q user=%q found=%v err=%v", trunkID, publicID, resolvedUsername, found, err)
	}
}

func TestStorePayloadAndLogEventAsyncLinksPayloadIDAndDrainsOnStop(t *testing.T) {
	store := &logStore{
		config: config.DBConfig{
			BatchSize:       10,
			BatchIntervalMS: 50,
		},
	}

	var nextPayloadID atomic.Int64
	var mu sync.Mutex
	var insertedPayloads []*PayloadRecord
	var insertedEvents []*Event

	store.storePayloadHook = func(ctx context.Context, payload *PayloadRecord) (int64, error) {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			t.Error("expected bounded deadline in async payload store")
		}
		id := nextPayloadID.Add(1)
		mu.Lock()
		insertedPayloads = append(insertedPayloads, payload)
		mu.Unlock()
		return id, nil
	}
	store.batchEventsHook = func(ctx context.Context, events []*Event) error {
		mu.Lock()
		for _, ev := range events {
			insertedEvents = append(insertedEvents, cloneEvent(ev))
		}
		mu.Unlock()
		return nil
	}

	store.startAsyncWorkers()
	store.wg.Add(1)
	go store.eventBatchWorker()

	for i := 0; i < 5; i++ {
		store.StorePayloadAndLogEventAsync(
			&PayloadRecord{
				SessionID: "sess-1",
				Timestamp: time.Now(),
				Kind:      "webrtc_sdp_offer",
				BodyText:  "v=0",
			},
			&Event{
				Timestamp: time.Now(),
				SessionID: "sess-1",
				Category:  "sdp",
				Name:      "webrtc_offer",
			},
		)
	}

	// Stop immediately: phase 1 must drain payload worker -> enqueue events,
	// and phase 2 must drain eventBatchWorker so all 5 linked events persist.
	if err := store.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(insertedPayloads) != 5 {
		t.Fatalf("insertedPayloads = %d, want 5", len(insertedPayloads))
	}
	if len(insertedEvents) != 5 {
		t.Fatalf("insertedEvents = %d, want 5", len(insertedEvents))
	}
	for i, ev := range insertedEvents {
		if ev.PayloadID == nil || *ev.PayloadID <= 0 {
			t.Fatalf("event[%d] missing linked PayloadID: %#v", i, ev)
		}
	}
	if store.lastSuccessNS.Load() == 0 {
		t.Fatal("expected lastSuccessNS to be updated after successful async writes/batch flushes")
	}
}

func TestStorePayloadAndLogEventAsyncQueuesEventOnPayloadErrorOrFullQueue(t *testing.T) {
	store := &logStore{
		config:       config.DBConfig{BatchSize: 10, BatchIntervalMS: 50},
		payloadQueue: make(chan payloadEventJob, 1),
		eventQueue:   make(chan *Event, 10),
	}

	store.storePayloadHook = func(ctx context.Context, payload *PayloadRecord) (int64, error) {
		return 0, errors.New("db unavailable")
	}
	var mu sync.Mutex
	var events []*Event
	store.batchEventsHook = func(ctx context.Context, batch []*Event) error {
		mu.Lock()
		for _, ev := range batch {
			events = append(events, cloneEvent(ev))
		}
		mu.Unlock()
		return nil
	}

	// Fill payloadQueue before starting workers so the second call hits full-queue fallback.
	store.StorePayloadAndLogEventAsync(
		&PayloadRecord{SessionID: "s1", Kind: "sdp_1"},
		&Event{SessionID: "s1", Category: "sdp", Name: "ev_1"},
	)
	store.StorePayloadAndLogEventAsync(
		&PayloadRecord{SessionID: "s1", Kind: "sdp_2"},
		&Event{SessionID: "s1", Category: "sdp", Name: "ev_2"},
	)

	store.startAsyncWorkers()
	store.wg.Add(1)
	go store.eventBatchWorker()

	if err := store.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected both timeline events to be persisted despite payload failure/drop, got %d", len(events))
	}
	for _, ev := range events {
		if ev.PayloadID != nil {
			t.Fatalf("expected nil PayloadID on failed/dropped payload, got %d", *ev.PayloadID)
		}
	}
}

func TestSessionCoalescerCoalescesAndProtectsTerminalState(t *testing.T) {
	c := newSessionCoalescer(16)
	now := time.Now()
	endedAt := now.Add(5 * time.Second)
	trunkID := int64(42)

	// Rapid updates for same session coalesce in place.
	c.enqueue(&SessionRecord{
		SessionID:  "sess-A",
		CreatedAt:  now,
		UpdatedAt:  now,
		Direction:  "outbound",
		FromURI:    "sip:1001@example.com",
		FinalState: "connecting",
		TrunkID:    &trunkID,
	})
	c.enqueue(&SessionRecord{
		SessionID:  "sess-A",
		UpdatedAt:  now.Add(time.Second),
		ToURI:      "sip:2002@example.com",
		SIPCallID:  "call-123",
		FinalState: "active",
	})
	c.enqueue(&SessionRecord{
		SessionID:  "sess-A",
		UpdatedAt:  endedAt,
		EndedAt:    &endedAt,
		FinalState: "ended",
		EndReason:  "hangup",
	})
	// Out-of-order stale non-ended snapshot MUST NOT overwrite queued ended snapshot.
	c.enqueue(&SessionRecord{
		SessionID:  "sess-A",
		UpdatedAt:  endedAt.Add(time.Second),
		FinalState: "active",
		EndReason:  "",
	})

	rec, ok := c.pop()
	if !ok || rec == nil {
		t.Fatal("expected coalesced record")
	}
	if rec.FinalState != "ended" || rec.EndReason != "hangup" || rec.EndedAt == nil {
		t.Fatalf("terminal state was not preserved: state=%q reason=%q endedAt=%v", rec.FinalState, rec.EndReason, rec.EndedAt)
	}
	if rec.Direction != "outbound" || rec.FromURI != "sip:1001@example.com" || rec.ToURI != "sip:2002@example.com" || rec.SIPCallID != "call-123" {
		t.Fatalf("coalescing lost earlier populated fields: %#v", rec)
	}
	if rec.TrunkID == nil || *rec.TrunkID != 42 {
		t.Fatalf("coalescing lost TrunkID: %#v", rec.TrunkID)
	}
	if _, more := c.pop(); more {
		t.Fatal("expected only one coalesced record in queue")
	}

	// Stale non-ended snapshot arriving AFTER ended was popped/flushed must also be ignored.
	c.enqueue(&SessionRecord{
		SessionID:  "sess-A",
		UpdatedAt:  endedAt.Add(2 * time.Second),
		FinalState: "ringing",
	})
	if stale, ok := c.pop(); ok {
		t.Fatalf("expected stale non-ended snapshot after ended flush to be dropped, got %#v", stale)
	}
}

func TestAsyncSessionDialogSessionDirAndStatsDrainOnStop(t *testing.T) {
	store := &logStore{
		config: config.DBConfig{
			BatchSize:       100,
			BatchIntervalMS: 60000, // Large interval to verify Stop() actually drains queues
		},
	}

	var mu sync.Mutex
	var sessions []*SessionRecord
	var dialogs []*DialogRecord
	var dirWrites []string
	var statsBatches int

	store.upsertSessionHook = func(ctx context.Context, sess *SessionRecord) error {
		mu.Lock()
		sessions = append(sessions, cloneSessionRecord(sess))
		mu.Unlock()
		return nil
	}
	store.upsertDialogHook = func(ctx context.Context, dialog *DialogRecord) error {
		mu.Lock()
		dialogs = append(dialogs, cloneDialogRecord(dialog))
		mu.Unlock()
		return nil
	}
	store.upsertSessionDirHook = func(ctx context.Context, sessionID, ownerInstanceID, wsURL string, ttlSeconds int) error {
		mu.Lock()
		dirWrites = append(dirWrites, sessionID+":"+ownerInstanceID)
		mu.Unlock()
		return nil
	}
	store.batchStatsHook = func(ctx context.Context, stats []*StatsRecord) error {
		mu.Lock()
		statsBatches += len(stats)
		mu.Unlock()
		return nil
	}

	store.startAsyncWorkers()
	store.wg.Add(1)
	go store.statsBatchWorker()

	ended := time.Now()
	store.UpsertSessionAsync(&SessionRecord{SessionID: "s-1", Direction: "inbound", FinalState: "ringing"})
	store.UpsertSessionAsync(&SessionRecord{SessionID: "s-1", Direction: "inbound", FinalState: "ended", EndedAt: &ended, EndReason: "bye"})
	store.UpsertDialogAsync(&DialogRecord{SessionID: "s-1", SIPCallID: "cid-1", RouteSet: []string{"<sip:a>"}})
	store.UpsertSessionDirectoryAsync("s-1", "gw-1", "wss://gw-1/ws", 3600)
	store.RecordStats(&StatsRecord{SessionID: "s-1", Timestamp: time.Now(), PLISent: 2})
	store.RecordStats(&StatsRecord{SessionID: "s-1", Timestamp: time.Now(), PLISent: 3})

	if err := store.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	// Calling Stop() twice must be safe (no panic on closed channels).
	if err := store.Stop(); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sessions) == 0 || sessions[len(sessions)-1].FinalState != "ended" {
		t.Fatalf("expected final session state ended, got %#v", sessions)
	}
	if len(dialogs) != 1 || dialogs[0].SIPCallID != "cid-1" {
		t.Fatalf("expected 1 dialog persisted, got %#v", dialogs)
	}
	if len(dirWrites) != 1 || dirWrites[0] != "s-1:gw-1" {
		t.Fatalf("expected 1 session_directory write, got %#v", dirWrites)
	}
	if statsBatches != 2 {
		t.Fatalf("expected 2 stats records drained on Stop, got %d", statsBatches)
	}
	if store.lastSuccessNS.Load() == 0 {
		t.Fatal("expected lastSuccessNS to be updated")
	}
}
