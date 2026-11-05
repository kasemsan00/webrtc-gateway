package telemetry

import (
	"context"
	"errors"
	"time"
)

// LogSIPTransaction writes the bounded sip.transaction.completed event. The raw
// error is never logged; only a normalized outcome and reason.
func LogSIPTransaction(ctx context.Context, started time.Time, statusCode int, err error) {
	outcome, reason := classifyError(ctx, err, "internal_error")
	_ = Log(ctx, LogEvent{
		Severity:  SeverityInfo,
		Component: "sip",
		Name:      "sip.transaction.completed",
		Outcome:   outcome,
		Reason:    reason,
		Measurements: []Measurement{
			DurationMilliseconds("duration_ms", time.Since(started)),
			Int64Measurement("status_code", int64(statusCode)),
		},
	})
}

// LogDependency writes the bounded dependency completion event without the raw
// error, endpoint, request, response, token, device, or transcript.
func LogDependency(ctx context.Context, dependency string, started time.Time, err error) {
	outcome, reason := classifyError(ctx, err, "dependency_unavailable")
	_ = Log(ctx, LogEvent{
		Severity:     SeverityInfo,
		Component:    dependencyComponent(dependency),
		Name:         dependencyEventName(dependency),
		Outcome:      outcome,
		Reason:       reason,
		Measurements: []Measurement{DurationMilliseconds("duration_ms", time.Since(started))},
	})
}

// LogPersistenceDrop records that a persistence queue was full and a record
// was discarded.
func LogPersistenceDrop(ctx context.Context) {
	_ = Log(ctx, LogEvent{Severity: SeverityWarn, Component: "persistence", Name: "persistence.record.dropped", Outcome: "dropped", Reason: "queue_full"})
}

func classifyError(ctx context.Context, err error, failureReason string) (outcome, reason string) {
	if err == nil {
		return "success", "none"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout", "deadline_exceeded"
	}
	return "failure", failureReason
}

func dependencyComponent(dependency string) string {
	switch dependency {
	case "translator":
		return "translator"
	case "push_fcm", "push_apns", "push_ttrs":
		return "push"
	}
	return "gateway"
}

func dependencyEventName(dependency string) string {
	switch dependency {
	case "translator":
		return "translator.request.completed"
	case "push_fcm", "push_apns", "push_ttrs":
		return "push.request.completed"
	}
	return "dependency.request.completed"
}
