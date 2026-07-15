package session

// MidCallRenegotiationSourceSwitchGateRelease marks gateway-initiated
// WebRTC renegotiation after @switch video gate release.
const MidCallRenegotiationSourceSwitchGateRelease = "switch_gate_release"

// TryClaimSwitchVideoRenegotiation reserves one renegotiation attempt per
// switch generation while no other mid-call negotiation is pending.
func (s *Session) TryClaimSwitchVideoRenegotiation(generation int) bool {
	if generation <= 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.TerminalAction != "" || s.PendingMidCallRenegotiation != nil {
		return false
	}
	if s.SwitchVideoRenegotiateGeneration == generation {
		return false
	}

	s.SwitchVideoRenegotiateGeneration = generation
	return true
}

// ReleaseSwitchVideoRenegotiationClaim clears a failed claim so a later retry
// may be attempted for the same generation.
func (s *Session) ReleaseSwitchVideoRenegotiationClaim(generation int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SwitchVideoRenegotiateGeneration == generation {
		s.SwitchVideoRenegotiateGeneration = 0
	}
}
