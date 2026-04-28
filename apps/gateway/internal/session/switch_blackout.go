package session

import (
	"fmt"
	"time"
)

// StartSwitchVideoBlackout enables a temporary hold that intentionally drops
// SIP->WebRTC video packets. This keeps the rendered screen black during @switch
// until a fresh keyframe is received or timeout is reached.
func (s *Session) StartSwitchVideoBlackout(blackout, maxWait time.Duration, reason string) {
	if !s.SwitchVideoBlackoutEnabled {
		return
	}
	if blackout <= 0 {
		blackout = 700 * time.Millisecond
	}
	if maxWait < blackout {
		maxWait = blackout
	}

	now := time.Now()

	s.mu.Lock()
	s.SwitchVideoBlackoutStarted = now
	s.SwitchVideoBlackoutUntil = now.Add(blackout)
	s.SwitchVideoBlackoutMaxWait = now.Add(maxWait)
	until := s.SwitchVideoBlackoutUntil
	maxUntil := s.SwitchVideoBlackoutMaxWait
	s.mu.Unlock()

	fmt.Printf("[%s] ⬛ switch_blackout_start reason=%s blackoutUntil=%s maxWaitUntil=%s\n",
		s.ID,
		reason,
		until.Format(time.RFC3339Nano),
		maxUntil.Format(time.RFC3339Nano),
	)
}

// StopSwitchVideoBlackout ends the temporary @switch blackout hold.
func (s *Session) StopSwitchVideoBlackout(reason string) {
	now := time.Now()

	s.mu.Lock()
	wasActive := !s.SwitchVideoBlackoutUntil.IsZero()
	if !wasActive {
		s.mu.Unlock()
		return
	}
	startedAt := s.SwitchVideoBlackoutStarted
	if startedAt.IsZero() {
		startedAt = now
	}
	s.SwitchVideoBlackoutUntil = time.Time{}
	s.SwitchVideoBlackoutMaxWait = time.Time{}
	s.SwitchVideoBlackoutStarted = time.Time{}
	s.mu.Unlock()

	heldMs := now.Sub(startedAt).Milliseconds()
	if heldMs < 0 {
		heldMs = 0
	}
	fmt.Printf("[%s] ⬛ switch_blackout_end reason=%s heldMs=%d\n", s.ID, reason, heldMs)
}

// ShouldHoldSwitchVideoPacket returns true when SIP->WebRTC forwarding should
// hold (drop) the current packet because @switch blackout is active.
//
// Behavior:
//  1. Before blackoutUntil: drop all packets.
//  2. After blackoutUntil and before maxWait: pass only first keyframe packet,
//     keep dropping non-keyframes.
//  3. At/after maxWait: release hold automatically.
func (s *Session) ShouldHoldSwitchVideoPacket(now time.Time, isKeyframe bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.SwitchVideoBlackoutUntil.IsZero() {
		return false
	}

	if now.Before(s.SwitchVideoBlackoutUntil) {
		return true
	}

	if isKeyframe {
		s.SwitchVideoBlackoutUntil = time.Time{}
		s.SwitchVideoBlackoutMaxWait = time.Time{}
		s.SwitchVideoBlackoutStarted = time.Time{}
		fmt.Printf("[%s] ⬛ switch_blackout_end reason=keyframe_recovered\n", s.ID)
		return false
	}

	// After minimum blackout has elapsed, continue holding until keyframe arrives,
	// but do not exceed max-wait boundary.
	if !s.SwitchVideoBlackoutMaxWait.IsZero() && now.Before(s.SwitchVideoBlackoutMaxWait) {
		return true
	}

	// Timeout safety: release automatically.
	s.SwitchVideoBlackoutUntil = time.Time{}
	s.SwitchVideoBlackoutMaxWait = time.Time{}
	s.SwitchVideoBlackoutStarted = time.Time{}
	fmt.Printf("[%s] ⬛ switch_blackout_end reason=timeout\n", s.ID)
	return false
}
