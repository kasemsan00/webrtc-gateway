package session

import (
	"fmt"
	"time"
)

func (s *Session) startWorker(fn func(stop <-chan struct{})) {
	if s == nil || fn == nil {
		return
	}
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.workerStopping {
		return
	}
	if s.workerStop == nil {
		s.workerStop = make(chan struct{})
	}
	s.workerWG.Add(1)
	go func() {
		defer s.workerWG.Done()
		fn(s.workerStop)
	}()
}

func (s *Session) stopWorkers(timeout time.Duration) error {
	if s == nil {
		return nil
	}
	s.workerMu.Lock()
	s.workerStopping = true
	if s.workerStop == nil {
		s.workerMu.Unlock()
		return nil
	}
	s.workerStopOnce.Do(func() {
		close(s.workerStop)
	})
	s.workerMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.workerWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("worker shutdown timeout after %s", timeout)
	}
}
