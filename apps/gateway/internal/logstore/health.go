package logstore

import (
	"context"
	"time"
)

// QueueHealth contains bounded, non-sensitive persistence queue measurements.
// Counters are cumulative for the current process lifetime.
type QueueHealth struct {
	Depth    int    `json:"depth"`
	Capacity int    `json:"capacity"`
	Accepted uint64 `json:"accepted"`
	Dropped  uint64 `json:"dropped"`
}

// Health is a cached persistence readiness snapshot. It never includes DSNs,
// credentials, query data, or any other database configuration.
type Health struct {
	Enabled       bool        `json:"enabled"`
	Connected     bool        `json:"connected"`
	LastSuccessAt *time.Time  `json:"lastSuccessAt,omitempty"`
	Events        QueueHealth `json:"events"`
	Stats         QueueHealth `json:"stats"`
}

// HealthReporter is intentionally separate from LogStore. Existing callers and
// tests that only implement LogStore remain source-compatible.
type HealthReporter interface {
	LogStoreHealth() Health
}

// SessionOverviewReader is separate from LogStore to preserve compatibility
// with existing lightweight test and integration implementations.
type SessionOverviewReader interface {
	GetSession(ctx context.Context, sessionID string) (*SessionRecord, error)
}
