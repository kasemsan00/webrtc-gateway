package telemetry

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type instruments struct {
	startedAt time.Time
	health    *healthTracker

	processStarts         metric.Int64Counter
	telemetryRecords      metric.Int64Counter
	callsActive           metric.Int64UpDownCounter
	callsStarted          metric.Int64Counter
	callsCompleted        metric.Int64Counter
	callSetupDuration     metric.Float64Histogram
	callDuration          metric.Float64Histogram
	wsConnectionsActive   metric.Int64UpDownCounter
	wsMessages            metric.Int64Counter
	sipTransactions       metric.Int64Counter
	sipDuration           metric.Float64Histogram
	persistenceDropped    metric.Int64Counter
	persistenceBatch      metric.Float64Histogram
	dependencyRequests    metric.Int64Counter
	dependencyDuration    metric.Float64Histogram
	mediaPacketLoss       metric.Float64Histogram
	mediaJitter           metric.Float64Histogram
	mediaRTT              metric.Float64Histogram
	mediaRecovery         metric.Int64Counter
	persistenceEventsUtil atomic.Uint64
	persistenceStatsUtil  atomic.Uint64
}

var currentRuntime atomic.Pointer[Runtime]

func setDefaultRuntime(runtime *Runtime) {
	if runtime != nil {
		currentRuntime.Store(runtime)
	}
}

func defaultInstruments() *instruments {
	runtime := currentRuntime.Load()
	if runtime == nil || !runtime.enabled {
		return nil
	}
	return runtime.instruments
}

func newInstruments(meter metric.Meter, health *healthTracker) (*instruments, error) {
	result := &instruments{startedAt: time.Now(), health: health}
	var err error
	if result.processStarts, err = meter.Int64Counter("gateway.process.starts", metric.WithUnit("{start}")); err != nil {
		return nil, err
	}
	if result.telemetryRecords, err = meter.Int64Counter("gateway.telemetry.records", metric.WithUnit("{record}")); err != nil {
		return nil, err
	}
	if result.callsActive, err = meter.Int64UpDownCounter("gateway.calls.active", metric.WithUnit("{call}")); err != nil {
		return nil, err
	}
	if result.callsStarted, err = meter.Int64Counter("gateway.calls.started", metric.WithUnit("{call}")); err != nil {
		return nil, err
	}
	if result.callsCompleted, err = meter.Int64Counter("gateway.calls.completed", metric.WithUnit("{call}")); err != nil {
		return nil, err
	}
	if result.callSetupDuration, err = meter.Float64Histogram("gateway.calls.setup.duration", metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if result.callDuration, err = meter.Float64Histogram("gateway.calls.duration", metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if result.wsConnectionsActive, err = meter.Int64UpDownCounter("gateway.websocket.connections.active", metric.WithUnit("{connection}")); err != nil {
		return nil, err
	}
	if result.wsMessages, err = meter.Int64Counter("gateway.websocket.messages", metric.WithUnit("{message}")); err != nil {
		return nil, err
	}
	if result.sipTransactions, err = meter.Int64Counter("gateway.sip.transactions", metric.WithUnit("{transaction}")); err != nil {
		return nil, err
	}
	if result.sipDuration, err = meter.Float64Histogram("gateway.sip.transaction.duration", metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if result.persistenceDropped, err = meter.Int64Counter("gateway.persistence.records.dropped", metric.WithUnit("{record}")); err != nil {
		return nil, err
	}
	if result.persistenceBatch, err = meter.Float64Histogram("gateway.persistence.batch.duration", metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if result.dependencyRequests, err = meter.Int64Counter("gateway.dependency.requests", metric.WithUnit("{request}")); err != nil {
		return nil, err
	}
	if result.dependencyDuration, err = meter.Float64Histogram("gateway.dependency.request.duration", metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if result.mediaPacketLoss, err = meter.Float64Histogram("gateway.media.packet_loss", metric.WithUnit("1")); err != nil {
		return nil, err
	}
	if result.mediaJitter, err = meter.Float64Histogram("gateway.media.jitter", metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if result.mediaRTT, err = meter.Float64Histogram("gateway.media.rtt", metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if result.mediaRecovery, err = meter.Int64Counter("gateway.media.recovery.requests", metric.WithUnit("{request}")); err != nil {
		return nil, err
	}

	uptime, err := meter.Float64ObservableGauge("gateway.process.uptime", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	queue, err := meter.Float64ObservableGauge("gateway.telemetry.queue.utilization", metric.WithUnit("1"))
	if err != nil {
		return nil, err
	}
	persistenceQueue, err := meter.Float64ObservableGauge("gateway.persistence.queue.utilization", metric.WithUnit("1"))
	if err != nil {
		return nil, err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		observer.ObserveFloat64(uptime, time.Since(result.startedAt).Seconds())
		snapshot := health.snapshot()
		utilization := 0.0
		if snapshot.Queue.Capacity > 0 {
			utilization = float64(snapshot.Queue.Depth) / float64(snapshot.Queue.Capacity)
		}
		observer.ObserveFloat64(queue, utilization)
		observer.ObserveFloat64(persistenceQueue, float64(result.persistenceEventsUtil.Load())/1_000_000, attrs(attribute.String("component", "events")))
		observer.ObserveFloat64(persistenceQueue, float64(result.persistenceStatsUtil.Load())/1_000_000, attrs(attribute.String("component", "stats")))
		return nil
	}, uptime, queue, persistenceQueue)
	return result, err
}

func attrs(values ...attribute.KeyValue) metric.MeasurementOption {
	return metric.WithAttributes(values...)
}

func RecordGatewayStart(ctx context.Context) {
	if instruments := defaultInstruments(); instruments != nil {
		instruments.processStarts.Add(ctx, 1)
	}
}

func RecordCallStarted(ctx context.Context, direction string) {
	if instruments := defaultInstruments(); instruments != nil {
		direction = normalizeDirection(direction)
		option := attrs(attribute.String("direction", direction))
		instruments.callsStarted.Add(ctx, 1, option)
		instruments.callsActive.Add(ctx, 1, option)
	}
}

func RecordCallCompleted(ctx context.Context, direction, outcome, reason string, setup, duration time.Duration) {
	if instruments := defaultInstruments(); instruments != nil {
		direction, outcome, reason = normalizeDirection(direction), normalizeOutcome(outcome), normalizeReason(reason)
		base := attrs(attribute.String("direction", direction), attribute.String("outcome", outcome))
		instruments.callsActive.Add(ctx, -1, attrs(attribute.String("direction", direction)))
		instruments.callsCompleted.Add(ctx, 1, attrs(attribute.String("direction", direction), attribute.String("outcome", outcome), attribute.String("reason", reason)))
		if setup >= 0 {
			instruments.callSetupDuration.Record(ctx, setup.Seconds(), base)
		}
		if duration >= 0 {
			instruments.callDuration.Record(ctx, duration.Seconds(), base)
		}
	}
}

func RecordCallSetup(ctx context.Context, direction, outcome string, duration time.Duration) {
	if instruments := defaultInstruments(); instruments != nil {
		direction, outcome = normalizeDirection(direction), normalizeOutcome(outcome)
		instruments.callSetupDuration.Record(ctx, maxDuration(duration).Seconds(), attrs(attribute.String("direction", direction), attribute.String("outcome", outcome)))
	}
}

func RecordWebSocketConnection(ctx context.Context, connected bool) {
	if instruments := defaultInstruments(); instruments != nil {
		delta := int64(-1)
		if connected {
			delta = 1
		}
		instruments.wsConnectionsActive.Add(ctx, delta)
	}
}

func RecordWebSocketMessage(ctx context.Context, messageType, outcome string) {
	if instruments := defaultInstruments(); instruments != nil {
		instruments.wsMessages.Add(ctx, 1, attrs(attribute.String("websocket.message_type", normalizeWSMessageType(messageType)), attribute.String("outcome", normalizeOutcome(outcome))))
	}
}

func RecordSIPTransaction(ctx context.Context, method string, statusCode int, outcome string, duration time.Duration) {
	if instruments := defaultInstruments(); instruments != nil {
		values := attrs(attribute.String("sip.method", normalizeSIPMethod(method)), attribute.String("sip.status_class", normalizeStatusClass(statusCode)), attribute.String("outcome", normalizeOutcome(outcome)))
		instruments.sipTransactions.Add(ctx, 1, values)
		instruments.sipDuration.Record(ctx, maxDuration(duration).Seconds(), values)
	}
}

func RecordPersistenceBatch(ctx context.Context, component, outcome string, duration time.Duration) {
	if instruments := defaultInstruments(); instruments != nil {
		instruments.persistenceBatch.Record(ctx, maxDuration(duration).Seconds(), attrs(attribute.String("component", normalizePersistenceComponent(component)), attribute.String("outcome", normalizeOutcome(outcome))))
	}
}

func RecordPersistenceDrop(ctx context.Context, component string) {
	component = normalizePersistenceComponent(component)
	if instruments := defaultInstruments(); instruments != nil {
		instruments.persistenceDropped.Add(ctx, 1, attrs(attribute.String("component", component), attribute.String("reason", "queue_full")))
	}
	_ = Log(ctx, LogEvent{Severity: SeverityWarn, Component: "persistence", Name: "persistence.record.dropped", Outcome: "dropped", Reason: "queue_full"})
}

func RecordPersistenceQueue(component string, depth, capacity int) {
	instruments := defaultInstruments()
	if instruments == nil {
		return
	}
	utilization := uint64(0)
	if depth > 0 && capacity > 0 {
		value := float64(depth) / float64(capacity)
		if value > 1 {
			value = 1
		}
		utilization = uint64(value * 1_000_000)
	}
	switch normalizePersistenceComponent(component) {
	case "events":
		instruments.persistenceEventsUtil.Store(utilization)
	case "stats":
		instruments.persistenceStatsUtil.Store(utilization)
	}
}

func recordTelemetryOutcome(ctx context.Context, outcome, reason string, count int64) {
	if instruments := defaultInstruments(); instruments != nil && count > 0 {
		instruments.telemetryRecords.Add(ctx, count, attrs(attribute.String("outcome", normalizeOutcome(outcome)), attribute.String("reason", normalizeReason(reason))))
	}
}

func RecordDependency(ctx context.Context, dependency, outcome, reason string, duration time.Duration) {
	if instruments := defaultInstruments(); instruments != nil {
		dependency, outcome, reason = normalizeDependency(dependency), normalizeOutcome(outcome), normalizeReason(reason)
		instruments.dependencyRequests.Add(ctx, 1, attrs(attribute.String("dependency.name", dependency), attribute.String("outcome", outcome), attribute.String("reason", reason)))
		instruments.dependencyDuration.Record(ctx, maxDuration(duration).Seconds(), attrs(attribute.String("dependency.name", dependency), attribute.String("outcome", outcome)))
	}
}

func RecordMediaHealth(ctx context.Context, direction, kind string, packetLoss, jitterSeconds, rttSeconds float64) {
	if instruments := defaultInstruments(); instruments != nil {
		values := attrs(attribute.String("direction", normalizeDirection(direction)), attribute.String("media.kind", normalizeMediaKind(kind)))
		instruments.mediaPacketLoss.Record(ctx, clampNonNegative(packetLoss), values)
		instruments.mediaJitter.Record(ctx, clampNonNegative(jitterSeconds), values)
		instruments.mediaRTT.Record(ctx, clampNonNegative(rttSeconds), values)
	}
}

func RecordMediaRecovery(ctx context.Context, direction, recoveryType, outcome string) {
	RecordMediaRecoveryCount(ctx, direction, recoveryType, outcome, 1)
}

func RecordMediaRecoveryCount(ctx context.Context, direction, recoveryType, outcome string, count int64) {
	if instruments := defaultInstruments(); instruments != nil {
		if count <= 0 {
			return
		}
		instruments.mediaRecovery.Add(ctx, count, attrs(attribute.String("direction", normalizeDirection(direction)), attribute.String("recovery.type", normalizeRecoveryType(recoveryType)), attribute.String("outcome", normalizeOutcome(outcome))))
	}
}

func maxDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}
func clampNonNegative(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}

func normalizeDirection(value string) string {
	switch value {
	case "inbound", "outbound":
		return value
	default:
		return "unknown"
	}
}
func normalizeOutcome(value string) string {
	switch value {
	case "success", "failure", "rejected", "timeout", "cancelled", "dropped", "disabled":
		return value
	default:
		return "unknown"
	}
}
func normalizeReason(value string) string {
	switch value {
	case "none", "invalid_request", "unauthorized", "forbidden", "not_found", "conflict", "peer_closed", "remote_rejected", "deadline_exceeded", "dependency_unavailable", "queue_full", "export_failed", "shutdown_deadline", "internal_error":
		return value
	default:
		return "unknown"
	}
}
func normalizeSIPMethod(value string) string {
	switch value {
	case "INVITE", "ACK", "BYE", "CANCEL", "REGISTER", "MESSAGE", "OPTIONS", "INFO", "PRACK", "UPDATE", "REFER", "NOTIFY", "SUBSCRIBE":
		return value
	default:
		return "OTHER"
	}
}
func normalizeStatusClass(code int) string {
	if code == 0 {
		return "none"
	}
	if code < 100 || code > 699 {
		return "unknown"
	}
	return string(rune('0'+code/100)) + "xx"
}
func normalizePersistenceComponent(value string) string {
	switch value {
	case "events", "stats":
		return value
	default:
		return "unknown"
	}
}
func normalizeDependency(value string) string {
	switch value {
	case "translator", "push_fcm", "push_apns", "push_ttrs", "jwks", "postgres":
		return value
	default:
		return "other"
	}
}
func normalizeMediaKind(value string) string {
	switch value {
	case "audio", "video":
		return value
	default:
		return "unknown"
	}
}
func normalizeRecoveryType(value string) string {
	switch value {
	case "pli", "fir", "nack", "keyframe", "resume":
		return value
	default:
		return "unknown"
	}
}
func normalizeWSMessageType(value string) string {
	switch value {
	case "offer", "ice", "call", "hangup", "dtmf", "accept", "reject", "resume", "ping", "translate", "translate_stop", "request_keyframe", "renegotiate_answer", "client_state", "trunk_resolve", "trunk_push_token", "agent_register":
		return value
	default:
		return "other"
	}
}
func normalizeCallPhase(value string) string {
	switch value {
	case "started", "setup", "resume", "completed":
		return value
	default:
		return "unknown"
	}
}
