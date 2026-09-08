package telemetry

import (
	"sync/atomic"
	"time"
)

// QueueHealth is a bounded process-local snapshot. It contains no endpoint,
// credentials, payloads, or exporter error strings.
type QueueHealth struct {
	Depth    uint64
	Capacity uint64
	Accepted uint64
	Dropped  uint64
	Exported uint64
	Failed   uint64
}

// HealthSnapshot is safe to expose through the authenticated operational
// health adapter. State and Reason use bounded vocabularies.
type HealthSnapshot struct {
	State         string
	Reason        string
	LastSuccessAt *time.Time
	LastFailureAt *time.Time
	Queue         QueueHealth
}

type healthTracker struct {
	enabled     bool
	capacity    uint64
	depth       atomic.Uint64
	accepted    atomic.Uint64
	dropped     atomic.Uint64
	exported    atomic.Uint64
	failed      atomic.Uint64
	lastSuccess atomic.Int64
	lastFailure atomic.Int64
}

func newHealthTracker(enabled bool, capacity int) *healthTracker {
	tracker := &healthTracker{enabled: enabled}
	if capacity > 0 {
		tracker.capacity = uint64(capacity)
	}
	return tracker
}

func (h *healthTracker) snapshot() HealthSnapshot {
	if h == nil || !h.enabled {
		return HealthSnapshot{State: "disabled", Reason: "telemetry_disabled"}
	}
	snapshot := HealthSnapshot{
		State: "unknown", Reason: "export_not_observed",
		Queue: QueueHealth{
			Depth: h.depth.Load(), Capacity: h.capacity, Accepted: h.accepted.Load(),
			Dropped: h.dropped.Load(), Exported: h.exported.Load(), Failed: h.failed.Load(),
		},
	}
	lastSuccessValue := h.lastSuccess.Load()
	lastFailureValue := h.lastFailure.Load()
	if lastSuccessValue > 0 {
		at := time.Unix(0, lastSuccessValue).UTC()
		snapshot.LastSuccessAt = &at
		snapshot.State = "connected"
		snapshot.Reason = ""
	}
	if lastFailureValue > 0 {
		at := time.Unix(0, lastFailureValue).UTC()
		snapshot.LastFailureAt = &at
		if snapshot.LastSuccessAt == nil {
			snapshot.State = "unavailable"
		} else if lastFailureValue > lastSuccessValue {
			snapshot.State = "degraded"
		}
		if lastFailureValue > lastSuccessValue {
			snapshot.Reason = "export_failed"
		}
	}
	if snapshot.Queue.Dropped > 0 {
		snapshot.State = "degraded"
		snapshot.Reason = "queue_dropping"
	}
	return snapshot
}

func (h *healthTracker) recordAccepted(depth int) {
	h.accepted.Add(1)
	h.setDepth(depth)
}

func (h *healthTracker) recordDropped(depth int) {
	h.dropped.Add(1)
	h.setDepth(depth)
}

func (h *healthTracker) recordExported(count int) {
	if count > 0 {
		h.exported.Add(uint64(count))
	}
	h.lastSuccess.Store(time.Now().UTC().UnixNano())
}

func (h *healthTracker) recordFailed() {
	h.failed.Add(1)
	h.lastFailure.Store(time.Now().UTC().UnixNano())
}

func (h *healthTracker) setDepth(depth int) {
	if depth < 0 {
		depth = 0
	}
	h.depth.Store(uint64(depth))
}
