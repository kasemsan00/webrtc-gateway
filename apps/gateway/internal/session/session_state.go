package session

import (
	"time"
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
	defer s.mu.Unlock()
	s.State = state
	s.UpdatedAt = time.Now()
	if state == StateEnded && s.cancel != nil {
		s.cancel()
	}
}

// GetState returns the current state of the session
func (s *Session) GetState() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}
