package session

import (
	"fmt"
	"time"
)

// StartVideoRecoveryBurst enables temporary aggressive keyframe recovery tuning.
func (s *Session) StartVideoRecoveryBurst(reason string) {
	if !s.VideoRecoveryBurstEnabled {
		return
	}

	now := time.Now()
	window := s.VideoRecoveryBurstWindow
	if window <= 0 {
		window = 12 * time.Second
	}

	s.mu.Lock()
	s.VideoRecoveryBurstStartedAt = now
	s.VideoRecoveryBurstUntil = now.Add(window)
	s.VideoRecoveryBurstLastReason = reason
	if s.VideoRecoveryBurstUntil.After(s.VideoRTCPFallbackUntil) {
		s.VideoRTCPFallbackUntil = s.VideoRecoveryBurstUntil
	}

	interval := s.VideoRecoveryBurstInterval
	stale := s.VideoRecoveryBurstStale
	firStale := s.VideoRecoveryBurstFIRStale
	if firStale < stale {
		firStale = stale
	}
	until := s.VideoRecoveryBurstUntil
	s.mu.Unlock()

	fmt.Printf("[%s] 📈 video_recovery_window_start reason=%s until=%s\n", s.ID, reason, until.Format(time.RFC3339Nano))
	fmt.Printf("[%s] 📈 recovery_policy interval=%s stale=%s firStale=%s\n", s.ID, interval, stale, firStale)
}

func (s *Session) endVideoRecoveryBurst(now time.Time, reason string) {
	startedAt := s.VideoRecoveryBurstStartedAt
	s.VideoRecoveryBurstUntil = time.Time{}
	s.VideoRecoveryBurstStartedAt = time.Time{}
	s.VideoRecoveryBurstLastReason = ""
	s.VideoRTCPFallbackUntil = time.Time{}

	recoveryMS := int64(-1)
	if !startedAt.IsZero() {
		recoveryMS = now.Sub(startedAt).Milliseconds()
	}
	fmt.Printf("[%s] 📈 video_recovery_window_end reason=%s keyframe_recovery_ms=%d\n", s.ID, reason, recoveryMS)
}

// StopVideoRecoveryBurstIfActive ends the burst window when media recovery is complete.
func (s *Session) StopVideoRecoveryBurstIfActive(reason string) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.VideoRecoveryBurstUntil.IsZero() {
		return
	}
	s.endVideoRecoveryBurst(now, reason)
}

func (s *Session) getVideoRecoveryPolicy(now time.Time, interval, stale, firStale time.Duration) (time.Duration, time.Duration, time.Duration, bool) {
	if !s.VideoRecoveryBurstEnabled || s.VideoRecoveryBurstUntil.IsZero() {
		return interval, stale, firStale, false
	}

	if !now.Before(s.VideoRecoveryBurstUntil) {
		s.endVideoRecoveryBurst(now, "timeout")
		return interval, stale, firStale, false
	}

	if s.VideoRecoveryBurstInterval > 0 && s.VideoRecoveryBurstInterval < interval {
		interval = s.VideoRecoveryBurstInterval
	}
	if s.VideoRecoveryBurstStale > 0 && s.VideoRecoveryBurstStale < stale {
		stale = s.VideoRecoveryBurstStale
	}
	if s.VideoRecoveryBurstFIRStale > 0 && s.VideoRecoveryBurstFIRStale < firStale {
		firStale = s.VideoRecoveryBurstFIRStale
	}
	if firStale < stale {
		firStale = stale
	}

	return interval, stale, firStale, true
}

// GetVideoRecoveryPolicy returns effective watchdog thresholds for current session state.
func (s *Session) GetVideoRecoveryPolicy(interval, stale, firStale time.Duration) (time.Duration, time.Duration, time.Duration, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getVideoRecoveryPolicy(now, interval, stale, firStale)
}
