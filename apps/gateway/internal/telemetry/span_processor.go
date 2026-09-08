package telemetry

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"webrtc-sip-gateway/internal/config"
)

// boundedSpanProcessor never waits in OnEnd. Collector outages consume only
// the configured queue and exporter timeout, never SIP/WS/media goroutines.
type boundedSpanProcessor struct {
	exporter      sdktrace.SpanExporter
	health        *healthTracker
	queue         chan sdktrace.ReadOnlySpan
	batchSize     int
	delay         time.Duration
	exportTimeout time.Duration
	stop          chan struct{}
	done          chan struct{}
	stopOnce      sync.Once
	stopped       atomic.Bool
	exportActive  atomic.Bool
	pending       atomic.Int64
}

func newBoundedSpanProcessor(exporter sdktrace.SpanExporter, health *healthTracker, cfg config.ObservabilityConfig) *boundedSpanProcessor {
	p := &boundedSpanProcessor{
		exporter: exporter, health: health, queue: make(chan sdktrace.ReadOnlySpan, cfg.MaxQueueSize),
		batchSize: cfg.MaxExportBatchSize, delay: time.Duration(cfg.ScheduleDelayMS) * time.Millisecond,
		exportTimeout: time.Duration(cfg.ExportTimeoutMS) * time.Millisecond,
		stop:          make(chan struct{}), done: make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *boundedSpanProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (p *boundedSpanProcessor) OnEnd(span sdktrace.ReadOnlySpan) {
	if p == nil || p.stopped.Load() || !span.SpanContext().IsSampled() {
		return
	}
	p.pending.Add(1)
	select {
	case p.queue <- span:
		p.health.recordAccepted(len(p.queue))
		recordTelemetryOutcome(context.Background(), "success", "none", 1)
	default:
		p.pending.Add(-1)
		p.health.recordDropped(len(p.queue))
		recordTelemetryOutcome(context.Background(), "dropped", "queue_full", 1)
	}
}

func (p *boundedSpanProcessor) run() {
	defer close(p.done)
	ticker := time.NewTicker(p.delay)
	defer ticker.Stop()
	batch := make([]sdktrace.ReadOnlySpan, 0, p.batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		p.exportActive.Store(true)
		defer p.exportActive.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), p.exportTimeout)
		err := p.exporter.ExportSpans(ctx, batch)
		cancel()
		if err != nil {
			p.health.recordFailed()
			recordTelemetryOutcome(context.Background(), "failure", "export_failed", 1)
		} else {
			p.health.recordExported(len(batch))
		}
		p.pending.Add(-int64(len(batch)))
		batch = batch[:0]
	}
	for {
		select {
		case span := <-p.queue:
			batch = append(batch, span)
			p.health.setDepth(len(p.queue))
			if len(batch) >= p.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-p.stop:
			for {
				select {
				case span := <-p.queue:
					batch = append(batch, span)
				default:
					p.health.setDepth(0)
					flush()
					return
				}
			}
		}
	}
}

func (p *boundedSpanProcessor) ForceFlush(ctx context.Context) error {
	// The periodic worker owns batches. Waiting for an empty queue is bounded by
	// the caller and avoids concurrent access to exporter/batch state.
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if p.pending.Load() == 0 && !p.exportActive.Load() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *boundedSpanProcessor) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.stopOnce.Do(func() { p.stopped.Store(true); close(p.stop) })
	select {
	case <-p.done:
		return p.exporter.Shutdown(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}
