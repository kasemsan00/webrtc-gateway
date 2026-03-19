package observability

import (
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (f *fakeClock) Now() time.Time {
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.now = f.now.Add(d)
}

func TestSummaryWindows_AggregateByMinuteAndFiveMinute(t *testing.T) {
	fakeClock := newFakeClock(time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC))
	m := NewRegistry(fakeClock)
	m.Inc(MetricWSSendQueueFullTotal, 2)
	m.ObserveMs(MetricRTPForwardLoopMs, 12.5)
	fakeClock.Advance(61 * time.Second)
	m.ObserveMs(MetricRTPForwardLoopMs, 8.0)

	got := m.Snapshot()
	if got.Counters[MetricWSSendQueueFullTotal] != 2 {
		t.Fatalf("want 2 got %v", got.Counters[MetricWSSendQueueFullTotal])
	}

	if got.Windows.OneMinuteStart.Equal(got.Windows.FiveMinuteStart) {
		t.Fatalf("window boundaries must differ")
	}

	if got.P95Ms[MetricRTPForwardLoopMs] <= 0 {
		t.Fatalf("expected positive p95")
	}
}

type manualTicker struct {
	ch chan time.Time
}

func (m *manualTicker) C() <-chan time.Time {
	return m.ch
}

func (m *manualTicker) Stop() {}

func TestPeriodicSnapshotLogger_EmitsAndStops(t *testing.T) {
	reg := NewRegistry(newFakeClock(time.Now().UTC()))
	calls := 0
	done := make(chan struct{}, 2)

	logger := NewSnapshotLogger(reg, 60*time.Second, func(Summary) {
		calls++
		done <- struct{}{}
	})
	mt := &manualTicker{ch: make(chan time.Time, 2)}
	logger.newTicker = func(time.Duration) ticker { return mt }

	stop := logger.Start()
	mt.ch <- time.Now()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected snapshot hook call")
	}

	stop()
	mt.ch <- time.Now()
	select {
	case <-done:
		t.Fatalf("unexpected emit after stop")
	case <-time.After(200 * time.Millisecond):
	}

	if calls != 1 {
		t.Fatalf("want 1 emit got %d", calls)
	}
}
