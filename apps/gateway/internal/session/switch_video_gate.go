package session

import (
	"fmt"
	"time"
)

const switchVideoGateRejectLogInterval = time.Second

// SwitchVideoGateDecision describes whether a normalized video access unit may
// be emitted and whether it atomically released an active switch gate.
type SwitchVideoGateDecision struct {
	Emit       bool
	Released   bool
	Reason     string
	Generation int
}

// EvaluateSwitchVideoAccessUnit decides whether a complete normalized access
// unit is safe to emit for the active switch generation.
func (s *Session) EvaluateSwitchVideoAccessUnit(au NormalizedH264AccessUnit, now time.Time) SwitchVideoGateDecision {
	s.mu.Lock()
	if !s.SwitchVideoGateActive {
		s.mu.Unlock()
		return SwitchVideoGateDecision{Emit: true, Reason: "inactive", Generation: au.Generation}
	}

	generation := s.SwitchVideoGateGeneration
	reason := ""
	switch {
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
		elapsed := now.Sub(s.SwitchVideoGateStartedAt)
		shouldLog := reason != s.SwitchVideoGateLastRejectReason ||
			s.SwitchVideoGateLastRejectLogAt.IsZero() ||
			now.Sub(s.SwitchVideoGateLastRejectLogAt) >= switchVideoGateRejectLogInterval
		if shouldLog {
			s.SwitchVideoGateLastRejectReason = reason
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

	wait := now.Sub(s.SwitchVideoGateStartedAt)
	rejected := s.SwitchVideoGateRejectedCount
	feedback := s.PLISent - s.SwitchVideoGatePLIBaseline
	if feedback < 0 {
		feedback = 0
	}
	id := s.ID
	packetCount := len(au.Packets)
	ssrc := uint32(0)
	if packetCount > 0 && au.Packets[0] != nil {
		ssrc = au.Packets[0].SSRC
	}
	injected := au.InjectedParameterSets
	s.clearSwitchVideoGateLocked()
	s.mu.Unlock()

	fmt.Printf("[%s] switch_video_gate_release generation=%d waitMs=%d ssrc=%d packets=%d injection=%v rejected=%d feedback=%d\n",
		id, generation, wait.Milliseconds(), ssrc, packetCount, injected, rejected, feedback)
	return SwitchVideoGateDecision{Emit: true, Released: true, Reason: "complete-idr", Generation: generation}
}

// StartSwitchVideoGate replaces any active gate with a snapshot of the current
// switch generation. Normalization-disabled sessions keep passing video through.
func (s *Session) StartSwitchVideoGate(now time.Time, reason string) {
	s.mu.Lock()
	if !s.VideoAUNormalizeEnabled {
		s.mu.Unlock()
		return
	}
	s.SwitchVideoGateActive = true
	s.SwitchVideoGateGeneration = s.SwitchGeneration
	s.SwitchVideoGateStartedAt = now
	s.SwitchVideoGateStartReason = reason
	s.SwitchVideoGatePLIBaseline = s.PLISent
	s.SwitchVideoGateRejectedCount = 0
	s.SwitchVideoGateLastRejectReason = ""
	s.SwitchVideoGateLastRejectLogAt = time.Time{}
	id := s.ID
	generation := s.SwitchVideoGateGeneration
	feedbackBaseline := s.SwitchVideoGatePLIBaseline
	s.mu.Unlock()

	fmt.Printf("[%s] switch_video_gate_start generation=%d reason=%s feedbackBaseline=%d\n",
		id, generation, reason, feedbackBaseline)
}

// StopSwitchVideoGate explicitly clears the current gate.
func (s *Session) StopSwitchVideoGate(reason string) {
	s.mu.Lock()
	active := s.SwitchVideoGateActive
	generation := s.SwitchVideoGateGeneration
	wait := time.Duration(0)
	if active && !s.SwitchVideoGateStartedAt.IsZero() {
		wait = time.Since(s.SwitchVideoGateStartedAt)
	}
	id := s.ID
	s.clearSwitchVideoGateLocked()
	s.mu.Unlock()

	fmt.Printf("[%s] switch_video_gate_stop generation=%d reason=%s active=%v waitMs=%d\n",
		id, generation, reason, active, wait.Milliseconds())
}

// IsSwitchVideoGateActive reports whether video is waiting for a decoder-safe
// access unit from the current switch generation.
func (s *Session) IsSwitchVideoGateActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SwitchVideoGateActive
}

func (s *Session) clearSwitchVideoGateLocked() {
	s.SwitchVideoGateActive = false
	s.SwitchVideoGateGeneration = 0
	s.SwitchVideoGateStartedAt = time.Time{}
	s.SwitchVideoGateStartReason = ""
	s.SwitchVideoGatePLIBaseline = 0
	s.SwitchVideoGateRejectedCount = 0
	s.SwitchVideoGateLastRejectReason = ""
	s.SwitchVideoGateLastRejectLogAt = time.Time{}
}
