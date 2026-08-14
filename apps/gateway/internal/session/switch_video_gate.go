package session

import (
	"fmt"
	"time"
)

const switchVideoGateRejectLogInterval = time.Second

const (
	switchVideoGateStallThreshold = 2 * time.Second
	switchVideoGateStallInterval  = 2 * time.Second
	// Linphone ignores a PLI ~300ms after its post-answer flush IDR. Wait for
	// the encoder to finish that GOP before asking again (CYeTDJRl22lP: 300ms
	// PLI produced no IDR; the real 23-packet GOP arrived ~1.6s later).
	undersizedIDRRetryPLIDelay = 800 * time.Millisecond
)

// MinSwitchVideoGateIDRPackets is the smallest post-switch IDR the gate treats
// as a real camera GOP before the stall timeout. Linphone's 800ms PLI reply is
// sometimes a 22-packet flush (CAoRE65fuUZG) that blacks n1669, while the same
// phone paints when the first IDR is 30–40 packets (kNmvwqMU4rXK, ws2AHWfsi171).
// After the stall timeout the gate accepts a smaller IDR so the picture is not
// held forever.
const MinSwitchVideoGateIDRPackets = 24

const (
	SwitchVideoGateActivationActive    = "active"
	SwitchVideoGateActivationDisabled  = "disabled"
	SwitchVideoGateActivationRejected  = "rejected"
	SwitchVideoGateActivationUnchanged = "unchanged"
)

// SwitchVideoGateActivation is a lock-consistent snapshot of a gate-start
// attempt. Callers may safely log it after the session lock is released.
type SwitchVideoGateActivation struct {
	Active           bool
	Outcome          string
	Generation       int
	StartedAt        time.Time
	FeedbackBaseline int
	RejectReason     string
	NewStart         bool
}

// SwitchVideoGateDecision describes whether a normalized video access unit may
// be emitted and identifies a reserved gated release when present.
type SwitchVideoGateDecision struct {
	Emit        bool
	Reason      string
	Generation  int
	Reservation uint64
}

type SwitchVideoGateStall struct {
	Generation  int
	Elapsed     time.Duration
	RejectedAUs int
	Summary     VideoRecoverySummary
}

// ObserveSwitchVideoGateStall returns a bounded diagnostic snapshot. It does
// not change gate or recovery state and is intended for the existing sampled
// RTP statistics cadence rather than per-packet use.
func (s *Session) ObserveSwitchVideoGateStall(now time.Time, summary VideoRecoverySummary) (SwitchVideoGateStall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.SwitchVideoGateActive {
		return SwitchVideoGateStall{}, false
	}
	elapsed := switchVideoGateElapsed(s.SwitchVideoGateStartedAt, now)
	if elapsed < switchVideoGateStallThreshold {
		return SwitchVideoGateStall{}, false
	}
	if !s.SwitchVideoGateLastStallLogAt.IsZero() &&
		!now.Before(s.SwitchVideoGateLastStallLogAt) &&
		now.Sub(s.SwitchVideoGateLastStallLogAt) < switchVideoGateStallInterval {
		return SwitchVideoGateStall{}, false
	}
	s.SwitchVideoGateLastStallLogAt = now
	return SwitchVideoGateStall{
		Generation:  s.SwitchVideoGateGeneration,
		Elapsed:     elapsed,
		RejectedAUs: s.SwitchVideoGateRejectedCount,
		Summary:     summary,
	}, true
}

// EvaluateSwitchVideoAccessUnit reserves the first decoder-safe access unit for
// the active generation. The caller must commit only after every packet write
// succeeds, or abort the reservation after a failed write. Task 4's single
// media owner must serialize evaluate, packet writes, and commit/abort as one
// operation; the session lock protects gate state but is never held for writes.
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
	elapsed := switchVideoGateElapsed(s.SwitchVideoGateStartedAt, now)
	reason := ""
	switch {
	case switchVideoTransitionBlocksGateReleaseLocked(s, now):
		reason = "blackout-hold"
	case s.SwitchVideoGateReleasing:
		reason = "release-in-progress"
	case au.Generation != generation:
		reason = "stale-generation"
	case !au.IsIDR:
		reason = "non-idr"
	case !au.ParameterSetsReady:
		reason = "parameter-sets-not-ready"
	case len(au.Packets) < MinSwitchVideoGateIDRPackets && elapsed < switchVideoGateStallThreshold:
		reason = "undersized-idr"
	case s.SwitchVideoGateLeaseNonce == ^uint64(0):
		reason = "reservation-exhausted"
	}

	if reason != "" {
		s.SwitchVideoGateRejectedCount++
		rejected := s.SwitchVideoGateRejectedCount
		shouldLog := s.SwitchVideoGateLastRejectLogAt.IsZero() || now.Before(s.SwitchVideoGateLastRejectLogAt) ||
			now.Sub(s.SwitchVideoGateLastRejectLogAt) >= switchVideoGateRejectLogInterval
		s.SwitchVideoGateLastRejectReason = reason
		if shouldLog {
			s.SwitchVideoGateLastRejectLogAt = now
		}
		id := s.ID
		packets := len(au.Packets)
		s.mu.Unlock()
		if shouldLog {
			fmt.Printf("[%s] switch_video_gate_reject generation=%d auGeneration=%d reason=%s elapsedMs=%d rejected=%d packets=%d\n",
				id, generation, au.Generation, reason, elapsed.Milliseconds(), rejected, packets)
		}
		return SwitchVideoGateDecision{Reason: reason, Generation: generation}
	}

	s.SwitchVideoGateReleasing = true
	s.SwitchVideoGateLeaseNonce++
	s.SwitchVideoGateReservation = s.SwitchVideoGateLeaseNonce
	s.SwitchVideoGateReservedPackets = len(au.Packets)
	s.SwitchVideoGateReservedSSRC = 0
	if len(au.Packets) > 0 && au.Packets[0] != nil {
		s.SwitchVideoGateReservedSSRC = au.Packets[0].SSRC
	}
	s.SwitchVideoGateReservedInjection = au.InjectedParameterSets
	reservation := s.SwitchVideoGateReservation
	s.mu.Unlock()

	return SwitchVideoGateDecision{
		Emit: true, Reason: "complete-idr-reserved", Generation: generation,
		Reservation: reservation,
	}
}

// CommitSwitchVideoGateRelease opens the gate only after the reserved access
// unit has been completely written by the single media owner. Callers must use
// the exact reservation returned by EvaluateSwitchVideoAccessUnit.
func (s *Session) CommitSwitchVideoGateRelease(generation int, reservation uint64, now time.Time) bool {
	s.mu.Lock()
	if !s.SwitchVideoGateActive || !s.SwitchVideoGateReleasing ||
		s.SwitchVideoGateGeneration != generation || reservation == 0 ||
		s.SwitchVideoGateReservation != reservation {
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
	s.applyPostGateRecoverySofteningLocked(now)
	s.clearSwitchVideoGateLocked()
	s.mu.Unlock()

	s.stopSwitchVideoBlackoutAfterGateRelease("gate-released")

	fmt.Printf("[%s] switch_video_gate_release generation=%d waitMs=%d ssrc=%d packets=%d injection=%v rejected=%d feedback=%d\n",
		id, generation, wait.Milliseconds(), ssrc, packetCount, injected, rejected, feedback)
	return true
}

// AbortSwitchVideoGateRelease returns a matching reservation to the awaiting
// state so a later complete IDR can be attempted without failing open.
func (s *Session) AbortSwitchVideoGateRelease(generation int, reservation uint64, reason string) bool {
	s.mu.Lock()
	if !s.SwitchVideoGateActive || !s.SwitchVideoGateReleasing ||
		s.SwitchVideoGateGeneration != generation || reservation == 0 ||
		s.SwitchVideoGateReservation != reservation {
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
// generation token. Accepted or active newer generations cannot be superseded;
// a duplicate start for the active generation is idempotent.
func (s *Session) StartSwitchVideoGate(generation int, now time.Time, reason string) bool {
	s.mu.Lock()
	activation := s.startSwitchVideoGateLocked(generation, now, reason)
	id := s.ID
	s.mu.Unlock()

	if activation.NewStart {
		fmt.Printf("[%s] switch_video_gate_start generation=%d reason=%s feedbackBaseline=%d\n",
			id, activation.Generation, reason, activation.FeedbackBaseline)
		s.RequestSIPKeyframeAfterUndersizedSwitchIDR(activation.Generation, now)
	}
	return activation.Outcome == SwitchVideoGateActivationActive
}

func (s *Session) startSwitchVideoGateLocked(generation int, now time.Time, reason string) SwitchVideoGateActivation {
	if !s.VideoAUNormalizeEnabled {
		s.clearSwitchVideoGateLocked()
		return SwitchVideoGateActivation{Outcome: SwitchVideoGateActivationDisabled, Generation: generation}
	}
	if generation != s.SwitchGeneration {
		return SwitchVideoGateActivation{Outcome: SwitchVideoGateActivationRejected, Generation: generation, RejectReason: "non-authoritative-generation"}
	}
	if generation < s.SwitchVideoGateAcceptedGeneration {
		return SwitchVideoGateActivation{Outcome: SwitchVideoGateActivationRejected, Generation: generation, RejectReason: "stale-accepted-generation"}
	}
	if s.SwitchVideoGateActive && generation < s.SwitchVideoGateGeneration {
		return SwitchVideoGateActivation{Outcome: SwitchVideoGateActivationRejected, Generation: generation, RejectReason: "stale-active-generation"}
	}
	if s.SwitchVideoGateActive && generation == s.SwitchVideoGateGeneration {
		return SwitchVideoGateActivation{
			Active: true, Outcome: SwitchVideoGateActivationActive, Generation: generation,
			StartedAt: s.SwitchVideoGateStartedAt, FeedbackBaseline: s.SwitchVideoGateFeedbackBaseline,
		}
	}

	s.clearSwitchVideoGateLocked()
	s.clearSIPVideoIDRCacheLocked()
	s.sipVideoRequireHealthyIDR.Store(true)
	s.sipVideoHealthyIDR.Store(false)
	s.SwitchVideoGateActive = true
	s.SwitchVideoGateGeneration = generation
	s.SwitchVideoGateStartedAt = now
	s.SwitchVideoGateStartReason = reason
	s.SwitchVideoGateFeedbackBaseline = s.PLISent
	return SwitchVideoGateActivation{
		Active: true, Outcome: SwitchVideoGateActivationActive, Generation: generation,
		StartedAt: now, FeedbackBaseline: s.SwitchVideoGateFeedbackBaseline, NewStart: true,
	}
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
	s.resetSwitchVideoGateLocked()
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

func (s *Session) resetSwitchVideoGateLocked() {
	s.clearSwitchVideoGateLocked()
	s.SwitchVideoGateAcceptedGeneration = 0
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
	s.SwitchVideoGateLastStallLogAt = time.Time{}
	s.SwitchVideoUndersizedIDRPLIScheduled = false
	s.SwitchVideoUndersizedIDRPLIGeneration = 0
	s.SwitchVideoGateStillEmitted = false
	s.clearSwitchVideoGateReservationLocked()
}

// RequestSIPKeyframeAfterUndersizedSwitchIDR asks Linphone for a real GOP after
// @switch. Immediate PLI often yields only a flush IDR or P-frames; waiting
// undersizedIDRRetryPLIDelay lets the encoder finish that GOP. Armed on gate
// start so P-frame-only switches (vgm4WIlaRByW) still get the retry.
func (s *Session) RequestSIPKeyframeAfterUndersizedSwitchIDR(generation int, now time.Time) {
	delay, ok := s.scheduleUndersizedIDRPLI(generation, now)
	if !ok {
		return
	}
	id := s.ID
	fmt.Printf("[%s] switch_video_gate_undersized_idr_pli generation=%d delayMs=%d\n",
		id, generation, delay.Milliseconds())
	send := func() { s.sendScheduledUndersizedIDRPLI(generation) }
	if delay <= 0 {
		send()
		return
	}
	time.AfterFunc(delay, send)
}

func (s *Session) scheduleUndersizedIDRPLI(generation int, now time.Time) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.SwitchVideoGateActive || s.SwitchVideoGateReleasing || s.SwitchVideoGateGeneration != generation {
		return 0, false
	}
	if s.SwitchVideoUndersizedIDRPLIScheduled && s.SwitchVideoUndersizedIDRPLIGeneration == generation {
		return 0, false
	}
	s.SwitchVideoUndersizedIDRPLIScheduled = true
	s.SwitchVideoUndersizedIDRPLIGeneration = generation
	wait := undersizedIDRRetryPLIDelay
	if !s.LastSipPLISent.IsZero() {
		elapsed := now.Sub(s.LastSipPLISent)
		if elapsed < pliForceMinInterval {
			if throttle := pliForceMinInterval - elapsed + time.Millisecond; throttle > wait {
				wait = throttle
			}
		}
	}
	return wait, true
}

func (s *Session) ClearSwitchVideoGateStillEmitted() {
	s.mu.Lock()
	s.SwitchVideoGateStillEmitted = false
	s.mu.Unlock()
}

func (s *Session) sendScheduledUndersizedIDRPLI(generation int) {
	s.mu.Lock()
	if !s.SwitchVideoGateActive || s.SwitchVideoGateReleasing || s.SwitchVideoGateGeneration != generation {
		s.mu.Unlock()
		return
	}
	epoch := s.MediaEpoch
	id := s.ID
	s.mu.Unlock()
	if s.GetState() == StateEnded {
		return
	}
	if s.SendSwitchPLIToAsteriskForced(generation, epoch, "undersized-idr") {
		fmt.Printf("[%s] switch_video_gate_undersized_idr_pli_sent generation=%d\n", id, generation)
	}
}

func (s *Session) clearSwitchVideoGateReservationLocked() {
	s.SwitchVideoGateReservation = 0
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
