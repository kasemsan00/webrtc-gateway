package session

import (
	"fmt"
	"time"

	"k2-gateway/internal/config"
)

// StartSwitchVideoBlackout enables the legacy temporary hold that intentionally
// drops SIP->WebRTC video packets until a fresh keyframe is received or timeout
// is reached.
func (s *Session) StartSwitchVideoBlackout(blackout, maxWait time.Duration, reason string) {
	s.StartSwitchVideoTransitionHold(config.SIPSwitchVideoTransitionBlackout, blackout, maxWait, reason)
}

// StartSwitchVideoTransitionHold enables the @switch video transition hold.
// Preserve mode gates non-keyframes until a fresh keyframe while keeping the
// previous rendered frame eligible to remain visible. Blackout mode keeps the
// legacy minimum all-packet hold before keyframe release.
func (s *Session) StartSwitchVideoTransitionHold(mode string, blackout, maxWait time.Duration, reason string) {
	s.startSwitchVideoTransitionHold(0, 0, false, mode, blackout, maxWait, reason)
}

// StartSwitchVideoTransitionHoldIfAuthoritative starts the transition hold
// only while the supplied switch token remains current.
func (s *Session) StartSwitchVideoTransitionHoldIfAuthoritative(generation int, mediaEpoch uint64, mode string, blackout, maxWait time.Duration, reason string) bool {
	return s.startSwitchVideoTransitionHold(generation, mediaEpoch, true, mode, blackout, maxWait, reason)
}

func (s *Session) startSwitchVideoTransitionHold(generation int, mediaEpoch uint64, requireAuthority bool, mode string, blackout, maxWait time.Duration, reason string) bool {
	if !s.SwitchVideoBlackoutEnabled {
		if requireAuthority {
			return s.IsSwitchVideoAuthority(generation, mediaEpoch)
		}
		return true
	}
	mode = normalizeSwitchVideoTransitionMode(mode)
	if blackout <= 0 {
		blackout = 300 * time.Millisecond
	}
	if maxWait < blackout {
		maxWait = blackout
	}

	now := time.Now()
	holdUntil := now
	if mode == config.SIPSwitchVideoTransitionBlackout {
		holdUntil = now.Add(blackout)
	}

	s.mu.Lock()
	if requireAuthority && (s.MediaEpoch != mediaEpoch || s.SwitchGeneration != generation) {
		s.mu.Unlock()
		return false
	}
	s.SwitchVideoTransitionMode = mode
	s.SwitchVideoBlackoutStarted = now
	s.SwitchVideoBlackoutUntil = holdUntil
	s.SwitchVideoBlackoutMaxWait = now.Add(maxWait)
	s.SwitchVideoFirstKeyframeAt = time.Time{}
	until := s.SwitchVideoBlackoutUntil
	maxUntil := s.SwitchVideoBlackoutMaxWait
	s.mu.Unlock()

	fmt.Printf("[%s] switch_transition_hold_start mode=%s reason=%s blackoutUntil=%s maxWaitUntil=%s\n",
		s.ID,
		mode,
		reason,
		until.Format(time.RFC3339Nano),
		maxUntil.Format(time.RFC3339Nano),
	)
	return true
}

// StopSwitchVideoBlackout ends the temporary @switch blackout hold.
func (s *Session) StopSwitchVideoBlackout(reason string) {
	now := time.Now()

	s.mu.Lock()
	wasActive := isSwitchVideoTransitionActiveLocked(s)
	if !wasActive {
		s.mu.Unlock()
		return
	}
	startedAt := s.SwitchVideoBlackoutStarted
	if startedAt.IsZero() {
		startedAt = now
	}
	mode := s.SwitchVideoTransitionMode
	firstKeyframeMs := elapsedMs(startedAt, s.SwitchVideoFirstKeyframeAt)
	s.SwitchVideoBlackoutUntil = time.Time{}
	s.SwitchVideoBlackoutMaxWait = time.Time{}
	s.SwitchVideoBlackoutStarted = time.Time{}
	s.SwitchVideoFirstKeyframeAt = time.Time{}
	s.mu.Unlock()

	heldMs := now.Sub(startedAt).Milliseconds()
	if heldMs < 0 {
		heldMs = 0
	}
	fmt.Printf("[%s] switch_transition_hold_end mode=%s reason=%s heldMs=%d firstKeyframeMs=%d\n",
		s.ID, normalizeSwitchVideoTransitionMode(mode), reason, heldMs, firstKeyframeMs)
}

// ShouldHoldSwitchVideoPacket returns true when SIP->WebRTC forwarding should
// hold (drop) the current packet because @switch transition hold is active.
//
// Behavior:
//  1. Preserve mode: hold non-keyframes until the first keyframe or max-wait.
//  2. Blackout mode: before blackoutUntil, drop all packets.
//  3. After blackoutUntil and before maxWait, pass only first keyframe packet.
//  4. At/after maxWait, release hold automatically.
func (s *Session) ShouldHoldSwitchVideoPacket(now time.Time, isKeyframe bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !isSwitchVideoTransitionActiveLocked(s) {
		return false
	}

	mode := normalizeSwitchVideoTransitionMode(s.SwitchVideoTransitionMode)
	startedAt := s.SwitchVideoBlackoutStarted
	if startedAt.IsZero() {
		startedAt = now
	}

	if mode == config.SIPSwitchVideoTransitionBlackout && now.Before(s.SwitchVideoBlackoutUntil) {
		return true
	}

	if isKeyframe {
		s.SwitchVideoFirstKeyframeAt = now
		heldMs := safeDurationMs(now.Sub(startedAt))
		firstKeyframeMs := safeDurationMs(now.Sub(startedAt))
		s.SwitchVideoBlackoutUntil = time.Time{}
		s.SwitchVideoBlackoutMaxWait = time.Time{}
		s.SwitchVideoBlackoutStarted = time.Time{}
		s.SwitchVideoFirstKeyframeAt = time.Time{}
		fmt.Printf("[%s] switch_transition_hold_end mode=%s reason=keyframe_recovered heldMs=%d firstKeyframeMs=%d\n",
			s.ID, mode, heldMs, firstKeyframeMs)
		return false
	}

	// After minimum blackout has elapsed, continue holding until keyframe arrives,
	// but do not exceed max-wait boundary.
	if !s.SwitchVideoBlackoutMaxWait.IsZero() && now.Before(s.SwitchVideoBlackoutMaxWait) {
		return true
	}

	// Timeout safety: release automatically.
	heldMs := safeDurationMs(now.Sub(startedAt))
	s.SwitchVideoBlackoutUntil = time.Time{}
	s.SwitchVideoBlackoutMaxWait = time.Time{}
	s.SwitchVideoBlackoutStarted = time.Time{}
	s.SwitchVideoFirstKeyframeAt = time.Time{}
	fmt.Printf("[%s] switch_transition_hold_end mode=%s reason=timeout heldMs=%d firstKeyframeMs=-1 fallback=timeout\n",
		s.ID, mode, heldMs)
	return false
}

func normalizeSwitchVideoTransitionMode(mode string) string {
	switch mode {
	case config.SIPSwitchVideoTransitionBlackout:
		return config.SIPSwitchVideoTransitionBlackout
	default:
		return config.SIPSwitchVideoTransitionPreserve
	}
}

func isSwitchVideoTransitionActiveLocked(s *Session) bool {
	return !s.SwitchVideoBlackoutUntil.IsZero() || !s.SwitchVideoBlackoutMaxWait.IsZero()
}

func elapsedMs(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() {
		return -1
	}
	return safeDurationMs(end.Sub(start))
}

func safeDurationMs(duration time.Duration) int64 {
	ms := duration.Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}
