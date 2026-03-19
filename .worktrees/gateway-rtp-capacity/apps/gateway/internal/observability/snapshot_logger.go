package observability

import (
	"sync"
	"time"
)

type ticker interface {
	C() <-chan time.Time
	Stop()
}

type standardTicker struct {
	t *time.Ticker
}

func (s standardTicker) C() <-chan time.Time {
	return s.t.C
}

func (s standardTicker) Stop() {
	s.t.Stop()
}

type SnapshotLogger struct {
	registry  *Registry
	interval  time.Duration
	onSummary func(Summary)
	newTicker func(time.Duration) ticker
}

func NewSnapshotLogger(registry *Registry, interval time.Duration, onSummary func(Summary)) *SnapshotLogger {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	if onSummary == nil {
		onSummary = func(Summary) {}
	}
	return &SnapshotLogger{
		registry:  registry,
		interval:  interval,
		onSummary: onSummary,
		newTicker: func(d time.Duration) ticker {
			return standardTicker{t: time.NewTicker(d)}
		},
	}
}

func (s *SnapshotLogger) Start() func() {
	if s.registry == nil {
		return func() {}
	}
	t := s.newTicker(s.interval)
	done := make(chan struct{})
	var once sync.Once

	go func() {
		for {
			select {
			case <-done:
				return
			case <-t.C():
				s.onSummary(s.registry.Snapshot())
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(done)
			t.Stop()
		})
	}
}
