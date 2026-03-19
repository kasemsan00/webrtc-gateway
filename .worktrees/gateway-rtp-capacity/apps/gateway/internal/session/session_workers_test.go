package session

import (
	"testing"
	"time"
)

func TestStopWorkers_StopsTrackedWorker(t *testing.T) {
	s := &Session{
		workerStop: make(chan struct{}),
	}
	s.startWorker(func(stop <-chan struct{}) {
		<-stop
	})

	if err := s.stopWorkers(200 * time.Millisecond); err != nil {
		t.Fatalf("expected stopWorkers to succeed, got %v", err)
	}
}

func TestStopWorkers_TimesOutOnStuckWorker(t *testing.T) {
	s := &Session{
		workerStop: make(chan struct{}),
	}
	s.startWorker(func(stop <-chan struct{}) {
		select {
		case <-stop:
			// ignore stop intentionally to simulate leaked worker
			time.Sleep(300 * time.Millisecond)
		}
	})

	if err := s.stopWorkers(50 * time.Millisecond); err == nil {
		t.Fatalf("expected timeout error for stuck worker")
	}
}
