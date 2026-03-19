package session

import (
	"testing"
	"time"

	"k2-gateway/internal/observability"
)

func TestManagerCallIDIndex_WritePathLookup(t *testing.T) {
	m := &Manager{
		sessions:        map[string]*Session{},
		callIDToSession: map[string]string{},
		sessionToCallID: map[string]string{},
	}
	s := &Session{ID: "s1", SIPCallID: "call-1"}
	m.sessions[s.ID] = s
	m.UpdateSessionCallID(s.ID, "call-1")

	got, ok := m.GetSessionBySIPCallID("call-1")
	if !ok || got == nil || got.ID != "s1" {
		t.Fatalf("expected session s1 from lookup")
	}
	if idx, ok := m.callIDToSession["call-1"]; !ok || idx != "s1" {
		t.Fatalf("expected index call-1 -> s1, got %q", idx)
	}
}

func TestManagerCallIDIndex_DoesNotBackfillFromSessionScan(t *testing.T) {
	m := &Manager{
		sessions:        map[string]*Session{},
		callIDToSession: map[string]string{},
		sessionToCallID: map[string]string{},
	}
	s := &Session{ID: "s0", SIPCallID: "call-missing-index"}
	m.sessions[s.ID] = s

	got, ok := m.GetSessionBySIPCallID("call-missing-index")
	if ok || got != nil {
		t.Fatalf("expected lookup miss when write-time index is absent")
	}
}

func TestManagerCallIDIndex_CleansStaleMapping(t *testing.T) {
	m := &Manager{
		sessions:        map[string]*Session{},
		callIDToSession: map[string]string{"stale": "missing-session"},
		sessionToCallID: map[string]string{},
	}

	_, ok := m.GetSessionBySIPCallID("stale")
	if ok {
		t.Fatalf("expected stale call-id lookup to fail")
	}
	if _, exists := m.callIDToSession["stale"]; exists {
		t.Fatalf("expected stale mapping to be removed")
	}
}

func TestManagerCallIDIndex_RemovedOnDelete(t *testing.T) {
	m := &Manager{
		sessions:        map[string]*Session{},
		callIDToSession: map[string]string{},
		sessionToCallID: map[string]string{},
	}
	s := &Session{ID: "s2", SIPCallID: "call-2"}
	m.sessions[s.ID] = s
	m.callIDToSession["call-2"] = "s2"
	m.sessionToCallID["s2"] = "call-2"

	m.DeleteSession("s2")

	if _, ok := m.callIDToSession["call-2"]; ok {
		t.Fatalf("expected call-id index to be removed on delete")
	}
	if _, ok := m.sessionToCallID["s2"]; ok {
		t.Fatalf("expected reverse index to be removed on delete")
	}
}

func TestManagerCallIDIndex_HandoffPreservesNewOwnerOnOldDelete(t *testing.T) {
	m := &Manager{
		sessions:        map[string]*Session{},
		callIDToSession: map[string]string{},
		sessionToCallID: map[string]string{},
	}
	oldSess := &Session{ID: "old", SIPCallID: "call-9"}
	newSess := &Session{ID: "new", SIPCallID: "call-9"}
	m.sessions[oldSess.ID] = oldSess
	m.sessions[newSess.ID] = newSess

	m.UpdateSessionCallID(oldSess.ID, "call-9")
	m.UpdateSessionCallID(newSess.ID, "call-9")
	m.DeleteSession(oldSess.ID)

	got, ok := m.GetSessionBySIPCallID("call-9")
	if !ok || got == nil || got.ID != "new" {
		t.Fatalf("expected call-9 to resolve to new session after old delete")
	}
}

func TestDeleteSession_IncrementsCleanupMetricOnWorkerTimeout(t *testing.T) {
	metrics := observability.NewRegistry(nil)
	m := &Manager{
		sessions:        map[string]*Session{},
		callIDToSession: map[string]string{},
		sessionToCallID: map[string]string{},
		metrics:         metrics,
	}

	s := &Session{
		ID:         "s3",
		SIPCallID:  "call-3",
		workerStop: make(chan struct{}),
	}
	s.startWorker(func(stop <-chan struct{}) {
		<-stop
		time.Sleep(3 * time.Second)
	})
	m.sessions[s.ID] = s
	m.callIDToSession["call-3"] = "s3"
	m.sessionToCallID["s3"] = "call-3"

	m.DeleteSession("s3")
	got := metrics.Snapshot().Counters[observability.MetricSessionCleanupErrorTotal]
	if got < 1 {
		t.Fatalf("expected cleanup error metric increment, got %v", got)
	}
}
