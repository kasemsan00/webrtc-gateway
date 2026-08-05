package session

// SetHeld records local operator hold state. SIP signaling remains owned by
// the gateway SIP layer; this flag is used for idempotency and diagnostics.
func (s *Session) SetHeld(held bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Held = held
}

func (s *Session) IsHeld() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Held
}
