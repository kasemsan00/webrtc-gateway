package session

import (
	"fmt"
	"time"
)

const switchVideoGateRejectLogInterval = time.Second

// SwitchVideoGateDecision describes whether a normalized video access unit may
// be emitted and whether an active switch gate has already been committed.
type SwitchVideoGateDecision struct {
	Emit       bool
	Released   bool
	Reason     string
	Generation int
}

// EvaluateSwitchVideoAccessUnit reserves the first decoder-safe access unit for
// the active generation. The caller must commit only after every packet write
// succeeds, or abort the reservation after a failed write.
func (s *Session) EvaluateSwitchVideoAccessUnit(au NormalizedH264AccessUnit, now time.Time) SwitchVideoGateDecision {
	s.mu.Lock()
	if !s.SwitchVideoGateActive {
		accepted := s.SwitchVideoGateAcceptedGeneration
		s.mu.Unlock()
		if au.Generation < accepted {
			return SwitchVideoGateDecision{Reason: "stale-accepted-generation", Generation: accepted}
		}
		return SwitchVideoGateDecision{Emit: true, Reason: "inactive", Generation: au.Generation}
	}

	generation := s.SwitchVideoGateGeneration
	reason := ""
	switch {
	case s.SwitchVideoGateReleasing:
		reason = "release-in-progress"
	case au.Generation != generation:
		reason = "stale-generation"
	case !au.IsIDR:
		reason = "non-idr"
	case !au.ParameterSetsReady:
		reason = "parameter-sets-not-ready"
	}

	if reason != "" {
		s.SwitchVideoGateRejectedCount++
		rejected := s.SwitchVideoGateRejectedCount
		elapsed := switchVideoGateElapsed(s.SwitchVideoGateStartedAt, now)
		shouldLog := s.SwitchVideoGateLastRejectLogAt.IsZero() ||
			now.Sub(s.SwitchVideoGateLastRejectLogAt) >= switchVideoGateRejectLogInterval
		s.SwitchVideoGateLastRejectReason = reason
		if shouldLog {
			s.SwitchVideoGateLastRejectLogAt = now
		}
		id := s.ID
		s.mu.Unlock()
		if shouldLog {
			fmt.Printf("[%s] switch_video_gate_reject generation=%d auGeneration=%d reason=%s elapsedMs=%d rejected=%d\n",
				id, generation, au.Generation, reason, elapsed.Milliseconds(), rejected)
		}
		return SwitchVideoGateDecision{Reason: reason, Generation: generation}
	}

	s.SwitchVideoGateReleasing = true
	s.SwitchVideoGateReservedPackets = len(au.Packets)
	s.SwitchVideoGateReservedSSRC = 0
	if len(au.Packets) > 0 && au.Packets[0] != nil {
		s.SwitchVideoGateReservedSSRC = au.Packets[0].SSRC
	}
	s.SwitchVideoGateReservedInjection = au.InjectedParameterSets
	s.mu.Unlock()

	return SwitchVideoGateDecision{Emit: true, Reason: "complete-idr-reserved", Generation: generation}
}

// CommitSwitchVideoGateRelease opens the gate only after the reserved access
// unit has been completely written by the caller.
func (s *Session) CommitSwitchVideoGateRelease(generation int, now time.Time) bool {
	s.mu.Lock()
	if !s.SwitchVideoGateActive || !s.SwitchVideoGateReleasing || s.SwitchVideoGateGeneration != generation {
		s.mu.Unlock()
		return false
	}

	wait := switchVideoGateElapsed(s.SwitchVideoGateStartedAt, now)
	rejected := s.SwitchVideoGateRejectedCount
	feedback := s.PLISent - s.SwitchVideoGateFeedbackBaseline
	if feedback < 0 {
		feedback = 0
	}
	id := s.ID
	packetCount := s.SwitchVideoGateReservedPackets
	ssrc := s.SwitchVideoGateReservedSSRC
	injected := s.SwitchVideoGateReservedInjection
	if generation > s.SwitchVideoGateAcceptedGeneration {
		s.SwitchVideoGateAcceptedGeneration = generation
	}
	s.clearSwitchVideoGateLocked()
	s.mu.Unlock()

	fmt.Printf("[%s] switch_video_gate_release generation=%d waitMs=%d ssrc=%d packets=%d injection=%v rejected=%d feedback=%d\n",
		id, generation, wait.Milliseconds(), ssrc, packetCount, injected, rejected, feedback)
	return true
}

// AbortSwitchVideoGateRelease returns a matching reservation to the awaiting
// state so a later complete IDR can be attempted without failing open.
func (s *Session) AbortSwitchVideoGateRelease(generation int, reason string) bool {
	s.mu.Lock()
	if !s.SwitchVideoGateActive || !s.SwitchVideoGateReleasing || s.SwitchVideoGateGeneration != generation {
		s.mu.Unlock()
		return false
	}
	s.SwitchVideoGateReleasing = false
	s.clearSwitchVideoGateReservationLocked()
	id := s.ID
	s.mu.Unlock()

	fmt.Printf("[%s] switch_video_gate_release_abort generation=%d reason=%s\n", id, generation, reason)
	return true
}

// StartSwitchVideoGate replaces an older active generation with an explicit
// generation token. Accepted or active newer generations cannot be superseded.
func (s *Session) StartSwitchVideoGate(generation int, now time.Time, reason string) bool {
	s.mu.Lock()
	if !s.VideoAUNormalizeEnabled ||
		generation != s.SwitchGeneration ||
		generation < s.SwitchVideoGateAcceptedGeneration ||
		(s.SwitchVideoGateActive && generation < s.SwitchVideoGateGeneration) {
		s.mu.Unlock()
		return false
	}
	s.clearSwitchVideoGateLocked()
	s.SwitchVideoGateActive = true
	s.SwitchVideoGateGeneration = generation
	s.SwitchVideoGateStartedAt = now
	s.SwitchVideoGateStartReason = reason
	s.SwitchVideoGateFeedbackBaseline = s.PLISent
	id := s.ID
	feedbackBaseline := s.SwitchVideoGateFeedbackBaseline
	s.mu.Unlock()

	fmt.Printf("[%s] switch_video_gate_start generation=%d reason=%s feedbackBaseline=%d\n",
		id, generation, reason, feedbackBaseline)
	return true
}

// StopSwitchVideoGate clears only the matching active generation.
func (s *Session) StopSwitchVideoGate(generation int, now time.Time, reason string) bool {
	s.mu.Lock()
	if !s.SwitchVideoGateActive || s.SwitchVideoGateGeneration != generation {
		s.mu.Unlock()
		return false
	}
	wait := switchVideoGateElapsed(s.SwitchVideoGateStartedAt, now)
	id := s.ID
	s.clearSwitchVideoGateLocked()
	s.mu.Unlock()

	fmt.Printf("[%s] switch_video_gate_stop generation=%d reason=%s waitMs=%d\n",
		id, generation, reason, wait.Milliseconds())
	return true
}

// ForceStopSwitchVideoGate clears all gate lifecycle state, including the
// accepted-generation floor. It is intended only for teardown or full reset.
func (s *Session) ForceStopSwitchVideoGate(now time.Time, reason string) {
	s.mu.Lock()
	generation := s.SwitchVideoGateGeneration
	wait := switchVideoGateElapsed(s.SwitchVideoGateStartedAt, now)
	id := s.ID
	s.clearSwitchVideoGateLocked()
	s.SwitchVideoGateAcceptedGeneration = 0
	s.mu.Unlock()

	fmt.Printf("[%s] switch_video_gate_force_stop generation=%d reason=%s waitMs=%d\n",
		id, generation, reason, wait.Milliseconds())
}

// IsSwitchVideoGateActive reports whether video is awaiting or writing a
// decoder-safe access unit for a switch generation.
func (s *Session) IsSwitchVideoGateActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SwitchVideoGateActive
}

func (s *Session) clearSwitchVideoGateLocked() {
	s.SwitchVideoGateActive = false
	s.SwitchVideoGateReleasing = false
	s.SwitchVideoGateGeneration = 0
	s.SwitchVideoGateStartedAt = time.Time{}
	s.SwitchVideoGateStartReason = ""
	s.SwitchVideoGateFeedbackBaseline = 0
	s.SwitchVideoGateRejectedCount = 0
	s.SwitchVideoGateLastRejectReason = ""
	s.SwitchVideoGateLastRejectLogAt = time.Time{}
	s.clearSwitchVideoGateReservationLocked()
}

func (s *Session) clearSwitchVideoGateReservationLocked() {
	s.SwitchVideoGateReservedPackets = 0
	s.SwitchVideoGateReservedSSRC = 0
	s.SwitchVideoGateReservedInjection = false
}

func switchVideoGateElapsed(start, now time.Time) time.Duration {
	if start.IsZero() || now.Before(start) {
		return 0
	}
	return now.Sub(start)
}
