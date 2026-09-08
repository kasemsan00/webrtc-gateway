package telemetry

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"webrtc-sip-gateway/internal/config"
)

func validObservabilityConfig() config.ObservabilityConfig {
	return config.ObservabilityConfig{
		Enable: true, Endpoint: "http://127.0.0.1:4318", Protocol: "http/protobuf",
		ServiceName: "gateway-test", Environment: "test", MetricsIntervalMS: 1000,
		TracesEnable: true, TraceSampleRatio: 1, MaxQueueSize: 2, MaxExportBatchSize: 1,
		ScheduleDelayMS: 10, ExportTimeoutMS: 100, ShutdownTimeoutMS: 100,
	}
}

func TestDisabledRuntimeDoesNotContactCollector(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	cfg := validObservabilityConfig()
	cfg.Enable = false
	cfg.Endpoint = server.URL
	runtime, err := New(context.Background(), cfg, "instance", "version")
	if err != nil {
		t.Fatal(err)
	}
	RecordGatewayStart(context.Background())
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatalf("disabled runtime made %d collector requests", requests.Load())
	}
	if got := runtime.Health().State; got != "disabled" {
		t.Fatalf("health state = %q, want disabled", got)
	}
}

func TestHTTPRuntimeExportsMetricsAndTraces(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		mu.Lock()
		requests[request.URL.Path]++
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/x-protobuf")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := validObservabilityConfig()
	cfg.Endpoint = server.URL
	runtime, err := New(context.Background(), cfg, "instance-http", "v-test")
	if err != nil {
		t.Fatal(err)
	}
	RecordGatewayStart(context.Background())
	_, span := runtime.Tracer().Start(context.Background(), "gateway.test")
	span.End()
	if err := runtime.traceProvider.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.meterProvider.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if requests["/v1/metrics"] == 0 || requests["/v1/traces"] == 0 {
		t.Fatalf("collector paths = %#v, want metrics and traces", requests)
	}
	if snapshot := runtime.Health(); snapshot.Queue.Exported == 0 || snapshot.LastSuccessAt == nil {
		t.Fatalf("health did not observe successful export: %#v", snapshot)
	}
}

func TestGRPCRuntimeConstructionAndRepeatedCleanup(t *testing.T) {
	cfg := validObservabilityConfig()
	cfg.Protocol = "grpc"
	cfg.Endpoint = "http://127.0.0.1:1"
	runtime, err := New(context.Background(), cfg, "instance-grpc", "v-test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	first := runtime.Shutdown(ctx)
	second := runtime.Shutdown(ctx)
	if !errors.Is(second, first) && second != first {
		t.Fatalf("repeated shutdown changed result: first=%v second=%v", first, second)
	}
}

func TestTracesCanBeDisabledIndependently(t *testing.T) {
	var traceRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/traces" {
			traceRequests.Add(1)
		}
		writer.Header().Set("Content-Type", "application/x-protobuf")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := validObservabilityConfig()
	cfg.Endpoint, cfg.TracesEnable = server.URL, false
	runtime, err := New(context.Background(), cfg, "instance-no-traces", "v-test")
	if err != nil {
		t.Fatal(err)
	}
	_, span := runtime.Tracer().Start(context.Background(), "must-not-export")
	span.End()
	if err := runtime.traceProvider.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if got := traceRequests.Load(); got != 0 {
		t.Fatalf("trace requests = %d, want 0", got)
	}
}

func TestInvalidEnabledRuntimeFailsBeforeExporterCreation(t *testing.T) {
	cfg := validObservabilityConfig()
	cfg.Endpoint = "contains-secret.invalid"
	cfg.Headers = "Authorization=Bearer should-never-appear"
	_, err := New(context.Background(), cfg, "instance", "version")
	if err == nil {
		t.Fatal("New succeeded with invalid endpoint")
	}
	if strings.Contains(err.Error(), "should-never-appear") || strings.Contains(err.Error(), cfg.Headers) {
		t.Fatalf("error leaked headers: %v", err)
	}
}

type blockingSpanExporter struct {
	started chan struct{}
	release chan struct{}
	fail    atomic.Bool
	once    sync.Once
}

func (e *blockingSpanExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	e.once.Do(func() { close(e.started) })
	select {
	case <-e.release:
		if e.fail.Load() {
			return errors.New("collector refusal containing secret")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *blockingSpanExporter) Shutdown(context.Context) error { return nil }

func TestBoundedSpanProcessorDropsWithoutBlockingProducer(t *testing.T) {
	cfg := validObservabilityConfig()
	cfg.MaxQueueSize = 1
	cfg.MaxExportBatchSize = 1
	cfg.ExportTimeoutMS = 1000
	exporter := &blockingSpanExporter{started: make(chan struct{}), release: make(chan struct{})}
	health := newHealthTracker(true, cfg.MaxQueueSize)
	processor := newBoundedSpanProcessor(exporter, health, cfg)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(processor))
	tracer := provider.Tracer("test")

	_, first := tracer.Start(context.Background(), "first")
	first.End()
	select {
	case <-exporter.started:
	case <-time.After(time.Second):
		t.Fatal("export did not start")
	}
	_, second := tracer.Start(context.Background(), "second")
	second.End()
	started := time.Now()
	for index := 0; index < 100; index++ {
		_, span := tracer.Start(context.Background(), "overflow")
		span.End()
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("producer blocked for %v", elapsed)
	}
	if snapshot := health.snapshot(); snapshot.Queue.Dropped == 0 {
		t.Fatalf("queue did not account for drops: %#v", snapshot)
	}
	close(exporter.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := provider.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

type ignoringContextExporter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *ignoringContextExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	e.once.Do(func() { close(e.started) })
	<-e.release
	return nil
}

func (e *ignoringContextExporter) Shutdown(context.Context) error { return nil }

func TestBoundedSpanProcessorShutdownHonorsDeadline(t *testing.T) {
	cfg := validObservabilityConfig()
	cfg.MaxQueueSize, cfg.MaxExportBatchSize = 1, 1
	exporter := &ignoringContextExporter{started: make(chan struct{}), release: make(chan struct{})}
	processor := newBoundedSpanProcessor(exporter, newHealthTracker(true, 1), cfg)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(processor))
	_, span := provider.Tracer("test").Start(context.Background(), "blocked")
	span.End()
	select {
	case <-exporter.started:
	case <-time.After(time.Second):
		t.Fatal("export did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := provider.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("shutdown exceeded bounded deadline: %v", elapsed)
	}
	close(exporter.release)
}

func TestHealthTransitionsFailureRecoveryAndDrops(t *testing.T) {
	health := newHealthTracker(true, 2)
	if got := health.snapshot().State; got != "unknown" {
		t.Fatalf("initial state = %q", got)
	}
	health.recordFailed()
	if got := health.snapshot().State; got != "unavailable" {
		t.Fatalf("failure state = %q", got)
	}
	health.recordExported(2)
	if got := health.snapshot().State; got != "connected" {
		t.Fatalf("failure history state = %q", got)
	}
	health.recordDropped(2)
	snapshot := health.snapshot()
	if snapshot.State != "degraded" || snapshot.Reason != "queue_dropping" || snapshot.LastSuccessAt == nil || snapshot.LastFailureAt == nil {
		t.Fatalf("unexpected recovery/drop snapshot: %#v", snapshot)
	}
}

func TestSamplerRatios(t *testing.T) {
	parameters := sdktrace.SamplingParameters{TraceID: trace.TraceID{1, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}}
	if got := newSampler(0).ShouldSample(parameters).Decision; got != sdktrace.Drop {
		t.Fatalf("zero ratio decision = %v", got)
	}
	if got := newSampler(1).ShouldSample(parameters).Decision; got != sdktrace.RecordAndSample {
		t.Fatalf("full ratio decision = %v", got)
	}
	lowID := sdktrace.SamplingParameters{TraceID: trace.TraceID{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}}
	highID := sdktrace.SamplingParameters{TraceID: trace.TraceID{1, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}}
	partial := newSampler(0.5)
	if partial.ShouldSample(lowID).Decision == partial.ShouldSample(highID).Decision {
		t.Fatal("partial sampler did not distinguish deterministic trace IDs")
	}
}

type collectingMetricExporter struct {
	fail atomic.Bool
}

func (e *collectingMetricExporter) Temporality(sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.CumulativeTemporality
}
func (e *collectingMetricExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}
func (e *collectingMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	if e.fail.Load() {
		return errors.New("refused")
	}
	return nil
}
func (e *collectingMetricExporter) ForceFlush(context.Context) error { return nil }
func (e *collectingMetricExporter) Shutdown(context.Context) error   { return nil }

func TestTrackingMetricExporterAccountsFailureAndRecovery(t *testing.T) {
	health := newHealthTracker(true, 1)
	delegate := &collectingMetricExporter{}
	exporter := &trackingMetricExporter{Exporter: delegate, health: health}
	delegate.fail.Store(true)
	if err := exporter.Export(context.Background(), &metricdata.ResourceMetrics{}); err == nil {
		t.Fatal("failed export returned nil")
	}
	delegate.fail.Store(false)
	if err := exporter.Export(context.Background(), &metricdata.ResourceMetrics{}); err != nil {
		t.Fatal(err)
	}
	snapshot := health.snapshot()
	if snapshot.Queue.Failed != 1 || snapshot.Queue.Exported != 1 || snapshot.LastFailureAt == nil || snapshot.LastSuccessAt == nil {
		t.Fatalf("unexpected health accounting: %#v", snapshot)
	}
}

func TestThrottledErrorHandlerBoundsLocalDiagnostics(t *testing.T) {
	var output strings.Builder
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	handler := newThrottledErrorHandler(time.Hour)
	for index := 0; index < 20; index++ {
		handler.Handle(errors.New("Authorization: Bearer raw-secret"))
	}
	if count := strings.Count(output.String(), "reason=export_failed"); count != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %q", count, output.String())
	}
	if strings.Contains(output.String(), "raw-secret") {
		t.Fatalf("diagnostic leaked raw exporter error: %q", output.String())
	}
}

func TestMetricAttributeNormalizersUseBoundedVocabulary(t *testing.T) {
	if got := normalizeWSMessageType("session-123@example"); got != "other" {
		t.Fatalf("ws type = %q", got)
	}
	if got := normalizeDependency("https://secret.invalid/?token=x"); got != "other" {
		t.Fatalf("dependency = %q", got)
	}
	if got := normalizeSIPMethod("INVITE sip:alice@example.com"); got != "OTHER" {
		t.Fatalf("method = %q", got)
	}
	if got := normalizeReason("dial tcp 10.0.0.1: secret"); got != "unknown" {
		t.Fatalf("reason = %q", got)
	}
	if got := normalizeStatusClass(486); got != "4xx" {
		t.Fatalf("status class = %q", got)
	}
	_ = metric.WithAttributes // keep compile-time API compatibility visible in this test file
}

func TestMetricCatalogNamesUnitsAndMonotonicity(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	health := newHealthTracker(true, 4)
	created, err := newInstruments(provider.Meter("catalog-test"), health)
	if err != nil {
		t.Fatal(err)
	}
	setDefaultRuntime(&Runtime{enabled: true, meterProvider: provider, meter: provider.Meter("catalog-test"), instruments: created, health: health})
	ctx := context.Background()
	RecordGatewayStart(ctx)
	recordTelemetryOutcome(ctx, "dropped", "queue_full", 1)
	RecordCallStarted(ctx, "outbound")
	RecordCallCompleted(ctx, "outbound", "success", "none", time.Second, 2*time.Second)
	RecordWebSocketConnection(ctx, true)
	RecordWebSocketMessage(ctx, "offer", "success")
	RecordSIPTransaction(ctx, "INVITE", 200, "success", 20*time.Millisecond)
	RecordPersistenceQueue("events", 1, 4)
	RecordPersistenceDrop(ctx, "events")
	RecordPersistenceBatch(ctx, "events", "success", 5*time.Millisecond)
	RecordDependency(ctx, "translator", "success", "none", 10*time.Millisecond)
	RecordMediaHealth(ctx, "inbound", "audio", 0.01, 0.002, 0.02)
	RecordMediaRecovery(ctx, "inbound", "nack", "success")

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]metricdata.Metrics)
	for _, scope := range collected.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			got[measurement.Name] = measurement
		}
	}
	wantUnits := map[string]string{
		"gateway.process.starts": "{start}", "gateway.process.uptime": "s", "gateway.telemetry.records": "{record}", "gateway.telemetry.queue.utilization": "1",
		"gateway.calls.active": "{call}", "gateway.calls.started": "{call}", "gateway.calls.completed": "{call}", "gateway.calls.setup.duration": "s", "gateway.calls.duration": "s",
		"gateway.websocket.connections.active": "{connection}", "gateway.websocket.messages": "{message}",
		"gateway.sip.transactions": "{transaction}", "gateway.sip.transaction.duration": "s",
		"gateway.persistence.queue.utilization": "1", "gateway.persistence.records.dropped": "{record}", "gateway.persistence.batch.duration": "s",
		"gateway.dependency.requests": "{request}", "gateway.dependency.request.duration": "s",
		"gateway.media.packet_loss": "1", "gateway.media.jitter": "s", "gateway.media.rtt": "s", "gateway.media.recovery.requests": "{request}",
	}
	if len(got) != len(wantUnits) {
		t.Fatalf("metric count = %d, want %d; got=%v", len(got), len(wantUnits), got)
	}
	for name, unit := range wantUnits {
		measurement, ok := got[name]
		if !ok {
			t.Errorf("missing metric %q", name)
			continue
		}
		if measurement.Unit != unit {
			t.Errorf("metric %s unit = %q, want %q", name, measurement.Unit, unit)
		}
	}
	for _, name := range []string{"gateway.process.starts", "gateway.telemetry.records", "gateway.calls.started", "gateway.calls.completed", "gateway.websocket.messages", "gateway.sip.transactions", "gateway.persistence.records.dropped", "gateway.dependency.requests", "gateway.media.recovery.requests"} {
		sum, ok := got[name].Data.(metricdata.Sum[int64])
		if !ok || !sum.IsMonotonic {
			t.Errorf("metric %s is not a monotonic integer sum: %#v", name, got[name].Data)
		}
	}
	for _, name := range []string{"gateway.calls.active", "gateway.websocket.connections.active"} {
		sum, ok := got[name].Data.(metricdata.Sum[int64])
		if !ok || sum.IsMonotonic {
			t.Errorf("metric %s is not an up/down sum: %#v", name, got[name].Data)
		}
	}
	if err := provider.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestControlPlaneSpansUseOnlyBoundedNamesAndAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(recorder))
	setDefaultRuntime(&Runtime{enabled: true, tracer: provider.Tracer("bounded-spans"), health: newHealthTracker(true, 1)})
	ctx, httpSpan := StartHTTPSpan(context.Background(), "POST", "/api/items/{itemId}")
	EndSpan(httpSpan, "failure", "forbidden", http.StatusForbidden)
	ctx, wsSpan := StartWebSocketSpan(ctx, "offer")
	SetSpanCorrelation(wsSpan, Correlation{SessionID: "safe-session"})
	EndSpan(wsSpan, "success", "none", 0)
	ctx, sipSpan := StartSIPSpan(ctx, "INVITE")
	SetSpanCorrelation(sipSpan, Correlation{SessionID: "safe-session"})
	EndSIP(ctx, sipSpan, "INVITE", time.Now(), 200, nil)
	ctx, callSpan := StartCallSpan(ctx, "setup")
	SetSpanCorrelation(callSpan, Correlation{SessionID: "safe-session"})
	SetCallDirection(callSpan, "outbound")
	EndSpan(callSpan, "success", "none", 0)
	_, dependencySpan := StartDependencySpan(ctx, "translator")
	EndDependency(ctx, dependencySpan, "translator", time.Now(), errors.New("Authorization Bearer raw-secret at https://private.invalid/?token=x"))

	spans := recorder.Ended()
	if len(spans) != 5 {
		t.Fatalf("span count = %d, want 5", len(spans))
	}
	encoded := strings.Builder{}
	hasSession := false
	for _, span := range spans {
		encoded.WriteString(span.Name())
		for _, value := range span.Attributes() {
			encoded.WriteString(string(value.Key))
			encoded.WriteString(value.Value.Emit())
			if string(value.Key) == "session.id" && value.Value.AsString() == "safe-session" {
				hasSession = true
			}
		}
	}
	text := encoded.String()
	if !strings.Contains(text, "/api/items/{itemId}") || !strings.Contains(text, "websocket.offer") || !strings.Contains(text, "sip.INVITE") || !strings.Contains(text, "call.setup") {
		t.Fatalf("missing bounded control-plane identifiers: %s", text)
	}
	if !hasSession {
		t.Fatal("control-plane spans omitted session.id correlation")
	}
	for _, forbidden := range []string{"raw-secret", "private.invalid", "token=x", "Authorization"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("span leaked %q: %s", forbidden, text)
		}
	}
}

func TestUnsampledWebSocketControlCreatesNoExportedSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()), sdktrace.WithSpanProcessor(recorder))
	setDefaultRuntime(&Runtime{enabled: true, tracer: provider.Tracer("unsampled"), health: newHealthTracker(true, 1)})
	ctx, span := StartWebSocketSpan(context.Background(), "offer")
	EndSpan(span, "success", "none", 0)
	if spans := recorder.Ended(); len(spans) != 0 {
		t.Fatalf("unsampled spans = %d, want 0", len(spans))
	}
	_ = ctx
}

func TestSIPTransactionTelemetryUsesCataloguedAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(recorder))
	setDefaultRuntime(&Runtime{enabled: true, tracer: provider.Tracer("sip-catalog"), health: newHealthTracker(true, 1)})
	ctx, invite := StartSIPSpan(context.Background(), "INVITE")
	EndSIP(ctx, invite, "INVITE", time.Now(), 200, nil)
	ctx, register := StartSIPSpan(ctx, "REGISTER")
	EndSIP(ctx, register, "REGISTER", time.Now(), 500, errors.New("sip registrar secret"))
	deadline, cancel := context.WithTimeout(ctx, time.Nanosecond)
	defer cancel()
	<-deadline.Done()
	ctx, message := StartSIPSpan(deadline, "MESSAGE")
	EndSIP(deadline, message, "MESSAGE", time.Now(), 0, deadline.Err())
	ctx, bye := StartSIPSpan(ctx, "BYE")
	EndSIP(ctx, bye, "BYE", time.Now(), 200, nil)
	encoded := strings.Builder{}
	for _, span := range recorder.Ended() {
		encoded.WriteString(span.Name())
		for _, value := range span.Attributes() {
			encoded.WriteString(string(value.Key) + "=" + value.Value.Emit() + ";")
		}
	}
	text := encoded.String()
	for _, want := range []string{"sip.INVITE", "sip.REGISTER", "sip.MESSAGE", "sip.BYE", "outcome=timeout", "reason=deadline_exceeded"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	for _, forbidden := range []string{"sip registrar secret", "alice", "sip:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("SIP telemetry leaked %q: %s", forbidden, text)
		}
	}
}

func TestWebSocketReconnectMetricsStayBounded(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	health := newHealthTracker(true, 4)
	created, err := newInstruments(provider.Meter("ws-reconnect"), health)
	if err != nil {
		t.Fatal(err)
	}
	setDefaultRuntime(&Runtime{enabled: true, meterProvider: provider, meter: provider.Meter("ws-reconnect"), instruments: created, health: health})
	ctx := context.Background()
	RecordWebSocketConnection(ctx, true)
	RecordWebSocketConnection(ctx, false)
	RecordWebSocketConnection(ctx, true)
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatal(err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name != "gateway.websocket.connections.active" {
				continue
			}
			sum, ok := measurement.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) == 0 {
				t.Fatalf("unexpected active-connection metric: %#v", measurement.Data)
			}
			if sum.DataPoints[0].Value != 1 {
				t.Fatalf("reconnect active connections = %d, want 1", sum.DataPoints[0].Value)
			}
			for _, point := range sum.DataPoints {
				for _, key := range point.Attributes.ToSlice() {
					name := string(key.Key)
					if strings.Contains(name, "session") || strings.Contains(name, "client") {
						t.Fatalf("websocket metric used unbounded attribute %s", name)
					}
				}
			}
			return
		}
	}
	t.Fatal("missing gateway.websocket.connections.active")
}
