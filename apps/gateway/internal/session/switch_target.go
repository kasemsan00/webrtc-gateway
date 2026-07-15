package session

import (
	"fmt"
	"time"
)

const defaultSwitchDuplicateDebounce = 60 * time.Second

type SwitchTargetDecision struct {
	Ignore          bool
	Reason          string
	Generation      int
	MediaEpoch      uint64
	Queue           string
	Agent           string
	MediaSSRC       uint32
	MediaSource     string
	DuplicateCount  int
	DebounceWindow  time.Duration
	PreviousAt      time.Time
	MediaGeneration string
}

func (s SwitchTargetDecision) LogFields() string {
	return fmt.Sprintf("queue=%s agent=%s generation=%d mediaEpoch=%d reason=%s ssrc=%d source=%s duplicateCount=%d debounceMs=%d",
		s.Queue, s.Agent, s.Generation, s.MediaEpoch, s.Reason, s.MediaSSRC, s.MediaSource, s.DuplicateCount, s.DebounceWindow.Milliseconds())
}

// PrepareAndActivateSwitchVideoTarget atomically decides whether an incoming
// @switch target is accepted and, when normalization is enabled, publishes its
// generation only together with an active complete-IDR gate.
func (s *Session) PrepareAndActivateSwitchVideoTarget(queue, agent string, now time.Time, debounceWindow time.Duration, debounceEnabled bool) (SwitchTargetDecision, SwitchVideoGateActivation) {
	debounceWindow = normalizePositiveDuration(debounceWindow, defaultSwitchDuplicateDebounce)

	s.mu.Lock()
	defer s.mu.Unlock()

	currentSSRC := s.RemoteVideoSSRC
	currentSource := s.SIPVideoRTPSource
	if currentSource == "" && s.AsteriskVideoAddr != nil {
		currentSource = s.AsteriskVideoAddr.String()
	}

	decision := SwitchTargetDecision{
		Queue:          queue,
		Agent:          agent,
		Generation:     s.SwitchGeneration,
		MediaEpoch:     s.MediaEpoch,
		MediaSSRC:      currentSSRC,
		MediaSource:    currentSource,
		DebounceWindow: debounceWindow,
		PreviousAt:     s.SwitchTargetReceivedAt,
	}

	sameTarget := s.SwitchTargetQueue == queue && s.SwitchTargetAgent == agent
	withinDebounce := debounceEnabled &&
		sameTarget &&
		!s.SwitchTargetReceivedAt.IsZero() &&
		now.Sub(s.SwitchTargetReceivedAt) >= 0 &&
		now.Sub(s.SwitchTargetReceivedAt) < debounceWindow
	sameMediaGeneration := s.SwitchMediaSSRC == currentSSRC && s.SwitchMediaSource == currentSource

	if withinDebounce && sameMediaGeneration {
		s.SwitchDuplicateCount++
		decision.Ignore = true
		decision.Reason = "duplicate-target"
		decision.Generation = s.SwitchGeneration
		decision.DuplicateCount = s.SwitchDuplicateCount
		return decision, SwitchVideoGateActivation{
			Active: s.SwitchVideoGateActive, Outcome: SwitchVideoGateActivationUnchanged,
			Generation: s.SwitchVideoGateGeneration, StartedAt: s.SwitchVideoGateStartedAt,
			FeedbackBaseline: s.SwitchVideoGateFeedbackBaseline,
		}
	}

	reason := "new-target"
	if sameTarget && withinDebounce && !sameMediaGeneration {
		reason = "media-generation-changed"
	} else if sameTarget && !s.SwitchTargetReceivedAt.IsZero() && debounceEnabled {
		reason = "debounce-window-expired"
	} else if !debounceEnabled && sameTarget {
		reason = "debounce-disabled"
	}

	s.SwitchGeneration++
	s.SwitchTargetQueue = queue
	s.SwitchTargetAgent = agent
	s.SwitchTargetReceivedAt = now
	s.SwitchMediaSSRC = currentSSRC
	s.SwitchMediaSource = currentSource
	s.SwitchDuplicateCount = 0

	decision.Ignore = false
	decision.Reason = reason
	decision.Generation = s.SwitchGeneration
	decision.DuplicateCount = 0
	decision.MediaGeneration = fmt.Sprintf("ssrc=%d source=%s", currentSSRC, currentSource)
	s.clearSIPVideoParameterSetsLocked()
	activation := s.startSwitchVideoGateLocked(decision.Generation, now, "agent-switch")
	return decision, activation
}

func (s *Session) GetSwitchGeneration() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SwitchGeneration
}

// IsSwitchVideoAuthority reports whether a handler token still belongs to the
// current call media epoch and latest accepted switch generation.
func (s *Session) IsSwitchVideoAuthority(generation int, mediaEpoch uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.MediaEpoch == mediaEpoch && s.SwitchGeneration == generation
}
