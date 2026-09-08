package session

import (
	"context"
	"time"

	"webrtc-sip-gateway/internal/telemetry"
)

// SessionState represents the state of a call session
type SessionState string

const (
	StateNew          SessionState = "new"
	StateIncoming     SessionState = "incoming" // Incoming call waiting for answer
	StateConnecting   SessionState = "connecting"
	StateRinging      SessionState = "ringing"
	StateActive       SessionState = "active"
	StateReconnecting SessionState = "reconnecting" // ICE disconnected, waiting for network recovery
	StateEnded        SessionState = "ended"
)

// UpdateState matches the interface expected by some callers, alias to SetState
func (s *Session) UpdateState(state SessionState) {
	s.SetState(state)
}

// SetState sets the session state in a thread-safe manner
func (s *Session) SetState(state SessionState) {
	s.mu.Lock()
	previous := s.State
	s.State = state
	now := time.Now()
	s.UpdatedAt = now
	started, startedAt := s.beginCallTelemetryLocked(now)
	direction := s.Direction
	recordSetup := state == StateActive && previous != StateActive && s.telemetryCallStarted && !s.telemetrySetupRecorded && !s.telemetryTransferred
	setupDuration := time.Duration(0)
	if recordSetup {
		s.telemetrySetupRecorded = true
		s.telemetryCallActiveAt = now
		setupDuration = now.Sub(s.telemetryCallStartedAt)
	}
	recordCompleted := state == StateEnded && s.telemetryCallStarted && !s.telemetryCallCompleted && !s.telemetryTransferred
	duration := time.Duration(0)
	outcome, reason := "success", "none"
	if recordCompleted {
		s.telemetryCallCompleted = true
		duration = now.Sub(s.telemetryCallStartedAt)
		outcome, reason = terminalTelemetryOutcome(s.TerminalAction, s.TerminalReason)
	}
	if state == StateEnded {
		s.clearSIPVideoIDRCacheLocked()
		if s.cancel != nil {
			s.cancel()
		}
	}
	s.mu.Unlock()
	if started {
		s.emitCallStartedTelemetry(direction, startedAt)
	}
	if recordSetup {
		ctx, span := telemetry.StartCallSpan(context.Background(), "setup")
		telemetry.SetSpanCorrelation(span, telemetry.Correlation{SessionID: s.ID})
		telemetry.SetCallDirection(span, direction)
		telemetry.EndSpan(span, "success", "none", 0)
		telemetry.RecordCallSetup(ctx, direction, "success", setupDuration)
		_ = telemetry.Log(ctx, telemetry.LogEvent{Severity: telemetry.SeverityInfo, Component: "call", Name: "call.setup.completed", Outcome: "success", Reason: "none", Correlation: telemetry.Correlation{SessionID: s.ID}, Measurements: []telemetry.Measurement{telemetry.DurationMilliseconds("duration_ms", setupDuration)}})
	}
	if recordCompleted {
		ctx, span := telemetry.StartCallSpan(context.Background(), "completed")
		telemetry.SetSpanCorrelation(span, telemetry.Correlation{SessionID: s.ID})
		telemetry.SetCallDirection(span, direction)
		telemetry.EndSpan(span, outcome, reason, 0)
		telemetry.RecordCallCompleted(ctx, direction, outcome, reason, -1, duration)
		_ = telemetry.Log(ctx, telemetry.LogEvent{Severity: telemetry.SeverityInfo, Component: "call", Name: "call.completed", Outcome: outcome, Reason: reason, Correlation: telemetry.Correlation{SessionID: s.ID}, Measurements: []telemetry.Measurement{telemetry.DurationMilliseconds("duration_ms", duration)}})
	}
}

func terminalTelemetryOutcome(action, terminalReason string) (string, string) {
	switch action {
	case "reject":
		return "rejected", "remote_rejected"
	case "timeout":
		return "timeout", "deadline_exceeded"
	case "cancel":
		return "cancelled", "none"
	}
	if terminalReason == "ice_failed" || terminalReason == "ice_failed_pre_sip" {
		return "failure", "peer_closed"
	}
	return "success", "none"
}

// GetState returns the current state of the session
func (s *Session) GetState() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

// TakeProgressNotify records state as the last notified progress value.
// Returns false when the same state was already notified (dedupe).
func (s *Session) TakeProgressNotify(state SessionState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastNotifiedProgressState == state {
		return false
	}
	s.lastNotifiedProgressState = state
	return true
}

// ApplyICEConnectedCallProgress decides call-progress updates on ICE connected.
// Initial setup (connecting/ringing) keeps SIP progress unchanged; only
// reconnecting is restored to active. recoveryReason drives video recovery burst.
func ApplyICEConnectedCallProgress(current SessionState) (next SessionState, recoveryReason string, stateChanged bool) {
	switch current {
	case StateReconnecting:
		return StateActive, "ice-reconnected", true
	case StateConnecting, StateRinging:
		return current, "initial-call", false
	default:
		return current, "", false
	}
}

// OutboundCallAckState returns the WebSocket progress state to acknowledge a
// client `call` message. Never reports active from ICE readiness alone.
func OutboundCallAckState(current SessionState) SessionState {
	switch current {
	case StateRinging, StateActive, StateEnded:
		return current
	default:
		return StateConnecting
	}
}

// TryMarkRemoteVideoReady marks the first remote-video-ready notify for this
// session. hasParameterSets must be true (SPS/PPS cached or injected on the AU).
// Returns true only on the first successful mark.
func (s *Session) TryMarkRemoteVideoReady(hasParameterSets bool) bool {
	if !hasParameterSets {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.remoteVideoReadyNotified {
		return false
	}
	s.remoteVideoReadyNotified = true
	return true
}

// TryMarkRemoteAudioReady marks the first remote-audio-ready notify.
func (s *Session) TryMarkRemoteAudioReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.remoteAudioReadyNotified {
		return false
	}
	s.remoteAudioReadyNotified = true
	return true
}

// TryClaimUplinkKeyframeKickOnRemoteJoin claims the first-join uplink keyframe
// kick for this session. Returns true only once.
func (s *Session) TryClaimUplinkKeyframeKickOnRemoteJoin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uplinkKeyframeKickOnRemoteJoinDone {
		return false
	}
	s.uplinkKeyframeKickOnRemoteJoinDone = true
	return true
}

// TryClaimUplinkKeyframeKickOnFirstSIPRTCP claims the first SIP video SR/RR
// uplink keyframe kick for this session. Returns true only once.
func (s *Session) TryClaimUplinkKeyframeKickOnFirstSIPRTCP() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uplinkKeyframeKickOnFirstSIPRTCP {
		return false
	}
	s.uplinkKeyframeKickOnFirstSIPRTCP = true
	return true
}

func isTerminalCleanupState(state SessionState, terminalAction string) bool {
	if state == StateEnded {
		return true
	}
	switch terminalAction {
	case "bye", "reject", "cancel", "end":
		return true
	default:
		return false
	}
}
