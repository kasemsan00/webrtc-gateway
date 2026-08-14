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

func (s *Session) endVideoRecoveryBurst(now time.Time, reason string) bool {
	startedAt := s.VideoRecoveryBurstStartedAt
	lastReason := s.VideoRecoveryBurstLastReason
	burstActive := !s.VideoRecoveryBurstUntil.IsZero() || !startedAt.IsZero() || lastReason != ""
	switchActive := !s.SwitchVideoRecoveryStartedAt.IsZero() || !s.SwitchVideoRecoveryUntil.IsZero()
	if !burstActive && !switchActive {
		return false
	}
	if lastReason == "switch" && reason == "timeout" && s.SwitchVideoRecoveryUnstableCount > 0 {
		reason = "timeout-rtp-unstable"
	}
	s.VideoRecoveryBurstUntil = time.Time{}
	s.VideoRecoveryBurstStartedAt = time.Time{}
	s.VideoRecoveryBurstLastReason = ""
	s.VideoRTCPFallbackUntil = time.Time{}

	if burstActive {
		recoveryMS := int64(-1)
		if !startedAt.IsZero() {
			recoveryMS = now.Sub(startedAt).Milliseconds()
		}
		fmt.Printf("[%s] 📈 video_recovery_window_end reason=%s keyframe_recovery_ms=%d\n", s.ID, reason, recoveryMS)
	}
	// Switch recovery is a separate state machine. A resume/reconnect burst can
	// overwrite VideoRecoveryBurstLastReason while the switch is still active,
	// so finish it based on its own state rather than the last burst reason.
	if switchActive {
		s.finishSwitchVideoRecoveryLocked(now, reason)
	}
	return true
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

const (
	KeyframeWatchdogNone = "none"
	KeyframeWatchdogPLI  = "pli"
	KeyframeWatchdogFIR  = "fir"
)

// KeyframeWatchdogDecision is the SIP→WebRTC keyframe watchdog action for one tick.
type KeyframeWatchdogDecision struct {
	Action string
	Reason string
}

// KeyframeWatchdogInput is one watchdog tick. BurstActive and GateActive must
// be sampled after the sleep, not before.
type KeyframeWatchdogInput struct {
	Now          time.Time
	LastKeyframe time.Time
	LastRTP      time.Time
	LastSipPLI   time.Time
	LastSipFIR   time.Time
	Stale        time.Duration
	FIRStale     time.Duration
	Interval     time.Duration
	BurstActive  bool
	GateActive   bool
	HealthyIDR   bool
}

// DecideKeyframeWatchdog chooses PLI/FIR only when video has actually stalled.
// After the burst window, a long GOP with packets still arriving is not a stall
// (2wEu56CA9cGy: Linphone GOP ~6s, stale=4s forced an IDR every ~6s for the
// whole call and n1669 stuttered). Do not treat RTP as healthy during an
// active switch gate or before a full post-switch GOP (Al8uLPjnbirH).
func DecideKeyframeWatchdog(in KeyframeWatchdogInput) KeyframeWatchdogDecision {
	keyframeAge := in.Now.Sub(in.LastKeyframe)
	if in.LastKeyframe.IsZero() {
		keyframeAge = in.Stale + time.Second
	}
	if keyframeAge < in.Stale {
		return KeyframeWatchdogDecision{Action: KeyframeWatchdogNone, Reason: "fresh-keyframe"}
	}

	rtpFlowing := !in.LastRTP.IsZero() && in.Now.Sub(in.LastRTP) < in.Stale
	if !in.BurstActive && !in.GateActive && in.HealthyIDR && rtpFlowing {
		return KeyframeWatchdogDecision{Action: KeyframeWatchdogNone, Reason: "rtp-flowing"}
	}

	if keyframeAge >= in.FIRStale {
		if !in.LastSipFIR.IsZero() && in.Now.Sub(in.LastSipFIR) < in.Interval {
			return KeyframeWatchdogDecision{Action: KeyframeWatchdogNone, Reason: "fir-throttled"}
		}
		return KeyframeWatchdogDecision{Action: KeyframeWatchdogFIR, Reason: "keyframe-stale"}
	}
	if !in.LastSipPLI.IsZero() && in.Now.Sub(in.LastSipPLI) < in.Interval {
		return KeyframeWatchdogDecision{Action: KeyframeWatchdogNone, Reason: "pli-throttled"}
	}
	if in.BurstActive {
		return KeyframeWatchdogDecision{Action: KeyframeWatchdogPLI, Reason: "burst-stale"}
	}
	return KeyframeWatchdogDecision{Action: KeyframeWatchdogPLI, Reason: "keyframe-stale"}
}

// IsVideoRecoveryBurstActive reports whether the temporary startup/recovery window is active.
func (s *Session) IsVideoRecoveryBurstActive() bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, _, active := s.getVideoRecoveryPolicy(now, s.VideoRecoveryBurstInterval, s.VideoRecoveryBurstStale, s.VideoRecoveryBurstFIRStale)
	return active
}
