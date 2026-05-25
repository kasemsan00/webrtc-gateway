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
	return fmt.Sprintf("queue=%s agent=%s generation=%d reason=%s ssrc=%d source=%s duplicateCount=%d debounceMs=%d",
		s.Queue, s.Agent, s.Generation, s.Reason, s.MediaSSRC, s.MediaSource, s.DuplicateCount, s.DebounceWindow.Milliseconds())
}

// PrepareSwitchVideoTarget decides whether an incoming @switch target should
// start a fresh recovery generation or be treated as a duplicate retry.
func (s *Session) PrepareSwitchVideoTarget(queue, agent string, now time.Time, debounceWindow time.Duration, debounceEnabled bool) SwitchTargetDecision {
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
		return decision
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
	return decision
}

func (s *Session) GetSwitchGeneration() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SwitchGeneration
}
