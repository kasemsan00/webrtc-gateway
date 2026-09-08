package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"webrtc-sip-gateway/internal/config"
)

const instrumentationName = "webrtc-sip-gateway"

// Runtime owns the optional process-wide OpenTelemetry providers. Application
// producers never receive an exporter and therefore cannot perform network I/O.
type Runtime struct {
	enabled       bool
	meter         metric.Meter
	tracer        trace.Tracer
	instruments   *instruments
	meterProvider *sdkmetric.MeterProvider
	traceProvider *sdktrace.TracerProvider
	health        *healthTracker
	shutdownOnce  sync.Once
	shutdownErr   error
}

// New installs no-op providers when disabled and OTLP providers when enabled.
func New(ctx context.Context, cfg config.ObservabilityConfig, instanceID, version string) (*Runtime, error) {
	health := newHealthTracker(cfg.Enable, cfg.MaxQueueSize)
	if !cfg.Enable {
		meterProvider := metricnoop.NewMeterProvider()
		traceProvider := tracenoop.NewTracerProvider()
		otel.SetMeterProvider(meterProvider)
		otel.SetTracerProvider(traceProvider)
		runtime := &Runtime{health: health, meter: meterProvider.Meter(instrumentationName), tracer: traceProvider.Tracer(instrumentationName)}
		setDefaultRuntime(runtime)
		return runtime, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	resource, err := buildResource(ctx, cfg, instanceID, version)
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}
	headers := parseHeaders(cfg.Headers)
	timeout := time.Duration(cfg.ExportTimeoutMS) * time.Millisecond

	traceExporter, metricExporter, err := newOTLPExporters(ctx, cfg, headers, timeout)
	if err != nil {
		return nil, err
	}
	trackedMetricExporter := &trackingMetricExporter{Exporter: metricExporter, health: health}
	reader := sdkmetric.NewPeriodicReader(trackedMetricExporter,
		sdkmetric.WithInterval(time.Duration(cfg.MetricsIntervalMS)*time.Millisecond),
		sdkmetric.WithTimeout(timeout),
	)
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithResource(resource), sdkmetric.WithReader(reader))

	var traceProvider *sdktrace.TracerProvider
	if cfg.TracesEnable {
		processor := newBoundedSpanProcessor(traceExporter, health, cfg)
		traceProvider = sdktrace.NewTracerProvider(
			sdktrace.WithResource(resource),
			sdktrace.WithSampler(newSampler(cfg.TraceSampleRatio)),
			sdktrace.WithSpanProcessor(processor),
		)
	} else {
		_ = traceExporter.Shutdown(ctx)
		traceProvider = sdktrace.NewTracerProvider(sdktrace.WithResource(resource), sdktrace.WithSampler(sdktrace.NeverSample()))
	}

	otel.SetMeterProvider(meterProvider)
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetErrorHandler(newThrottledErrorHandler(time.Minute))

	runtime := &Runtime{
		enabled: true, health: health, meterProvider: meterProvider, traceProvider: traceProvider,
		meter: meterProvider.Meter(instrumentationName), tracer: traceProvider.Tracer(instrumentationName),
	}
	runtime.instruments, err = newInstruments(runtime.meter, health)
	if err != nil {
		_ = runtime.Shutdown(ctx)
		return nil, fmt.Errorf("create telemetry instruments: %w", err)
	}
	setDefaultRuntime(runtime)
	return runtime, nil
}

func newSampler(ratio float64) sdktrace.Sampler {
	return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
}

func (r *Runtime) Enabled() bool { return r != nil && r.enabled }
func (r *Runtime) Meter() metric.Meter {
	if r == nil {
		return otel.Meter(instrumentationName)
	}
	return r.meter
}
func (r *Runtime) Tracer() trace.Tracer {
	if r == nil {
		return otel.Tracer(instrumentationName)
	}
	return r.tracer
}
func (r *Runtime) Health() HealthSnapshot {
	if r == nil {
		return HealthSnapshot{State: "disabled", Reason: "telemetry_disabled"}
	}
	return r.health.snapshot()
}

// Shutdown is idempotent and honors the caller's deadline.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || !r.enabled {
		return nil
	}
	r.shutdownOnce.Do(func() {
		var errs []error
		if r.traceProvider != nil {
			errs = append(errs, r.traceProvider.Shutdown(ctx))
		}
		if r.meterProvider != nil {
			errs = append(errs, r.meterProvider.Shutdown(ctx))
		}
		r.shutdownErr = errors.Join(errs...)
	})
	return r.shutdownErr
}

func buildResource(ctx context.Context, cfg config.ObservabilityConfig, instanceID, version string) (*sdkresource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(version),
		attribute.String("deployment.environment", cfg.Environment),
		attribute.String("service.instance.id", instanceID),
	}
	sanitizer := NewSanitizer(256)
	for _, item := range strings.Split(cfg.ResourceAttributes, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok || key == "" || isDeniedKey(key) {
			continue
		}
		clean, _ := sanitizer.Sanitize(key, value).(string)
		if clean != "" && clean != RedactedValue && clean != OmittedValue {
			attrs = append(attrs, attribute.String(key, clean))
		}
	}
	custom := sdkresource.NewWithAttributes(semconv.SchemaURL, attrs...)
	return sdkresource.Merge(sdkresource.Default(), custom)
}

func parseHeaders(value string) map[string]string {
	headers := make(map[string]string)
	for _, item := range strings.Split(value, ",") {
		key, raw, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		decoded, err := url.QueryUnescape(strings.TrimSpace(raw))
		if err != nil {
			decoded = strings.TrimSpace(raw)
		}
		headers[strings.TrimSpace(key)] = decoded
	}
	return headers
}

func newOTLPExporters(ctx context.Context, cfg config.ObservabilityConfig, headers map[string]string, timeout time.Duration) (sdktrace.SpanExporter, sdkmetric.Exporter, error) {
	if cfg.Protocol == "grpc" {
		traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(cfg.Endpoint), otlptracegrpc.WithHeaders(headers), otlptracegrpc.WithTimeout(timeout))
		if err != nil {
			return nil, nil, fmt.Errorf("create OTLP gRPC trace exporter: %w", err)
		}
		metricExporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(cfg.Endpoint), otlpmetricgrpc.WithHeaders(headers), otlpmetricgrpc.WithTimeout(timeout))
		if err != nil {
			_ = traceExporter.Shutdown(ctx)
			return nil, nil, fmt.Errorf("create OTLP gRPC metric exporter: %w", err)
		}
		return traceExporter, metricExporter, nil
	}
	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.Endpoint), otlptracehttp.WithHeaders(headers), otlptracehttp.WithTimeout(timeout))
	if err != nil {
		return nil, nil, fmt.Errorf("create OTLP HTTP trace exporter: %w", err)
	}
	metricExporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(cfg.Endpoint), otlpmetrichttp.WithHeaders(headers), otlpmetrichttp.WithTimeout(timeout))
	if err != nil {
		_ = traceExporter.Shutdown(ctx)
		return nil, nil, fmt.Errorf("create OTLP HTTP metric exporter: %w", err)
	}
	return traceExporter, metricExporter, nil
}

type trackingMetricExporter struct {
	sdkmetric.Exporter
	health *healthTracker
}

func (e *trackingMetricExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	err := e.Exporter.Export(ctx, data)
	if err != nil {
		e.health.recordFailed()
		recordTelemetryOutcome(context.Background(), "failure", "export_failed", 1)
	} else {
		e.health.recordExported(1)
		recordTelemetryOutcome(context.Background(), "success", "none", 1)
	}
	return err
}

type throttledErrorHandler struct {
	mu       sync.Mutex
	last     time.Time
	interval time.Duration
}

func newThrottledErrorHandler(interval time.Duration) *throttledErrorHandler {
	return &throttledErrorHandler{interval: interval}
}
func (h *throttledErrorHandler) Handle(err error) {
	if err == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.last.IsZero() && time.Since(h.last) < h.interval {
		return
	}
	h.last = time.Now()
	log.Printf("OpenTelemetry exporter degraded: reason=export_failed")
}
