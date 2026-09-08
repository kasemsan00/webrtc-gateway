package telemetry

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var allowedHTTPMethods = map[string]struct{}{
	"GET": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {}, "OPTIONS": {}, "HEAD": {},
}

// StartHTTPSpan uses only the reviewed route template. Callers must never pass
// request URLs or query strings as route.
func StartHTTPSpan(ctx context.Context, method, route string) (context.Context, trace.Span) {
	if _, ok := allowedHTTPMethods[method]; !ok {
		method = "OTHER"
	}
	if route == "" || len(route) > 128 || route[0] != '/' {
		route = "unmatched"
	}
	return tracer().Start(ctx, method+" "+route,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("component", "http"),
			attribute.String("http.request.method", method),
			attribute.String("http.route", route),
		),
	)
}

func StartWebSocketSpan(ctx context.Context, messageType string) (context.Context, trace.Span) {
	messageType = normalizeWSMessageType(messageType)
	return tracer().Start(ctx, "websocket."+messageType,
		trace.WithAttributes(attribute.String("component", "websocket"), attribute.String("websocket.message_type", messageType)),
	)
}

func StartSIPSpan(ctx context.Context, method string) (context.Context, trace.Span) {
	method = normalizeSIPMethod(method)
	return tracer().Start(ctx, "sip."+method,
		trace.WithAttributes(attribute.String("component", "sip"), attribute.String("sip.method", method)),
	)
}

func StartCallSpan(ctx context.Context, phase string) (context.Context, trace.Span) {
	phase = normalizeCallPhase(phase)
	return tracer().Start(ctx, "call."+phase,
		trace.WithAttributes(attribute.String("component", "call")),
	)
}

func StartDependencySpan(ctx context.Context, dependency string) (context.Context, trace.Span) {
	dependency = normalizeDependency(dependency)
	return tracer().Start(ctx, "dependency."+dependency,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("component", "dependency"), attribute.String("dependency.name", dependency)),
	)
}

// SetSpanCorrelation attaches reviewed log/trace identifiers. Callers must never
// pass SIP identities, Call-IDs, numbers, or addresses.
func SetSpanCorrelation(span trace.Span, correlation Correlation) {
	if span == nil || !span.IsRecording() {
		return
	}
	if correlation.SessionID != "" {
		span.SetAttributes(attribute.String("session.id", correlation.SessionID))
	}
	if correlation.CallCorrelationID != "" {
		span.SetAttributes(attribute.String("call.correlation_id", correlation.CallCorrelationID))
	}
	if correlation.TrunkID > 0 {
		span.SetAttributes(attribute.Int64("trunk.id", correlation.TrunkID))
	}
}

func SetCallDirection(span trace.Span, direction string) {
	if span == nil || !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.String("call.direction", normalizeDirection(direction)))
}

func EndSpan(span trace.Span, outcome, reason string, statusCode int) {
	if span == nil {
		return
	}
	outcome, reason = normalizeOutcome(outcome), normalizeReason(reason)
	span.SetAttributes(attribute.String("outcome", outcome), attribute.String("reason", reason))
	if statusCode > 0 {
		span.SetAttributes(attribute.Int("http.response.status_code", statusCode))
	}
	if outcome == "success" {
		span.SetStatus(codes.Ok, "")
	} else {
		span.SetStatus(codes.Error, reason)
	}
	span.End()
}

// EndDependency records a bounded dependency result without attaching the raw
// error, endpoint, request, response, token, device, or transcript.
func EndDependency(ctx context.Context, span trace.Span, dependency string, started time.Time, err error) {
	outcome, reason := "success", "none"
	if err != nil {
		outcome, reason = "failure", "dependency_unavailable"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			outcome, reason = "timeout", "deadline_exceeded"
		}
	}
	RecordDependency(ctx, dependency, outcome, reason, time.Since(started))
	EndSpan(span, outcome, reason, 0)
	_ = Log(ctx, LogEvent{Severity: SeverityInfo, Component: dependencyComponent(dependency), Name: dependencyEventName(dependency), Outcome: outcome, Reason: reason, Measurements: []Measurement{DurationMilliseconds("duration_ms", time.Since(started))}})
}

func EndSIP(ctx context.Context, span trace.Span, method string, started time.Time, statusCode int, err error) {
	outcome, reason := "success", "none"
	if err != nil {
		outcome, reason = "failure", "internal_error"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			outcome, reason = "timeout", "deadline_exceeded"
		}
	}
	RecordSIPTransaction(ctx, method, statusCode, outcome, time.Since(started))
	EndSpan(span, outcome, reason, 0)
	_ = Log(ctx, LogEvent{Severity: SeverityInfo, Component: "sip", Name: "sip.transaction.completed", Outcome: outcome, Reason: reason, Measurements: []Measurement{DurationMilliseconds("duration_ms", time.Since(started)), Int64Measurement("status_code", int64(statusCode))}})
}

func dependencyComponent(dependency string) string {
	if dependency == "translator" {
		return "translator"
	}
	if dependency == "push_fcm" || dependency == "push_apns" || dependency == "push_ttrs" {
		return "push"
	}
	return "gateway"
}

func dependencyEventName(dependency string) string {
	if dependency == "translator" {
		return "translator.request.completed"
	}
	if dependency == "push_fcm" || dependency == "push_apns" || dependency == "push_ttrs" {
		return "push.request.completed"
	}
	return "dependency.request.completed"
}

func tracer() trace.Tracer {
	runtime := currentRuntime.Load()
	if runtime == nil {
		return trace.NewNoopTracerProvider().Tracer(instrumentationName)
	}
	return runtime.Tracer()
}
