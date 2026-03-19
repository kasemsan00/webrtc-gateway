package observability

import (
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type MetricName string

const (
	MetricWSSendQueueFullTotal    MetricName = "ws_send_queue_full_total"
	MetricSIPSetupTimeoutTotal    MetricName = "sip_setup_timeout_total"
	MetricRTPLossSpikeTotal       MetricName = "rtp_loss_spike_total"
	MetricSessionCleanupErrorTotal MetricName = "session_cleanup_error_total"
	MetricRTPForwardLoopMs        MetricName = "rtp_forward_loop_ms"
	MetricSIPOutgoingSetupMs      MetricName = "sip_outgoing_setup_ms"
	MetricSIPIncomingSetupMs      MetricName = "sip_incoming_setup_ms"
)

type observation struct {
	at    time.Time
	value float64
}

type SummaryWindows struct {
	OneMinuteStart  time.Time `json:"oneMinuteStart"`
	FiveMinuteStart time.Time `json:"fiveMinuteStart"`
}

type RuntimeSummary struct {
	Goroutines    int     `json:"goroutines"`
	HeapAllocBytes uint64 `json:"heapAllocBytes"`
}

type Summary struct {
	Windows  SummaryWindows           `json:"windows"`
	Counters map[MetricName]float64   `json:"counters"`
	P95Ms    map[MetricName]float64   `json:"timersMsP95"`
	Runtime  RuntimeSummary           `json:"runtime"`
}

type Registry struct {
	mu       sync.RWMutex
	clock    Clock
	counters map[MetricName]float64
	timersMs map[MetricName][]observation
}

func NewRegistry(clock Clock) *Registry {
	if clock == nil {
		clock = realClock{}
	}
	return &Registry{
		clock:    clock,
		counters: make(map[MetricName]float64),
		timersMs: make(map[MetricName][]observation),
	}
}

func (r *Registry) Inc(name MetricName, delta float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[name] += delta
}

func (r *Registry) ObserveMs(name MetricName, value float64) {
	now := r.clock.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timersMs[name] = append(r.timersMs[name], observation{at: now, value: value})
}

func (r *Registry) Snapshot() Summary {
	now := r.clock.Now()
	oneMinuteStart := now.Add(-1 * time.Minute)
	fiveMinuteStart := now.Add(-5 * time.Minute)

	r.mu.RLock()
	counters := map[MetricName]float64{
		MetricWSSendQueueFullTotal:    0,
		MetricSIPSetupTimeoutTotal:    0,
		MetricRTPLossSpikeTotal:       0,
		MetricSessionCleanupErrorTotal: 0,
	}
	for k, v := range r.counters {
		counters[k] = v
	}
	p95 := map[MetricName]float64{
		MetricRTPForwardLoopMs:   0,
		MetricSIPOutgoingSetupMs: 0,
		MetricSIPIncomingSetupMs: 0,
	}
	for name, samples := range r.timersMs {
		values := make([]float64, 0, len(samples))
		for _, s := range samples {
			if s.at.Before(fiveMinuteStart) {
				continue
			}
			values = append(values, s.value)
		}
		p95[name] = percentile(values, 95)
	}
	r.mu.RUnlock()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	return Summary{
		Windows: SummaryWindows{
			OneMinuteStart:  oneMinuteStart,
			FiveMinuteStart: fiveMinuteStart,
		},
		Counters: counters,
		P95Ms:    p95,
		Runtime: RuntimeSummary{
			Goroutines:    runtime.NumGoroutine(),
			HeapAllocBytes: ms.HeapAlloc,
		},
	}
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	weight := rank - float64(lo)
	return sorted[lo]*(1-weight) + sorted[hi]*weight
}
