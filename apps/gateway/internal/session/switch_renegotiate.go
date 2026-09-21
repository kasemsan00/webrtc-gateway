package session

// MidCallRenegotiationSourceSwitchMessage marks gateway-initiated WebRTC
// renegotiation when an authoritative @switch MESSAGE is accepted.
const MidCallRenegotiationSourceSwitchMessage = "switch_message"

// MidCallRenegotiationSourceSwitchGateRelease is the legacy source used when
// renegotiation started after @switch video gate release. Apply-answer still
// uses the current (possibly replaced) PeerConnection.
const MidCallRenegotiationSourceSwitchGateRelease = "switch_gate_release"

// IsSwitchVideoRenegotiationSource reports whether a pending mid-call
// renegotiation should apply the client answer onto the session PeerConnection.
func IsSwitchVideoRenegotiationSource(source string) bool {
	return source == MidCallRenegotiationSourceSwitchMessage ||
		source == MidCallRenegotiationSourceSwitchGateRelease
}

// TryClaimSwitchVideoRenegotiation reserves one renegotiation attempt per
// switch generation while no other mid-call negotiation is pending.
// SIP→WebRTC video is held off the replacement PC until the client answers
// (or the attempt fails). The live PC stays parked for make-before-break
// so the last frame remains on the old decoder.
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
	s.SwitchVideoRenegotiateHold = true
	s.switchReplacementPCReady = false
	return true
}

// ReleaseSwitchVideoRenegotiationClaim clears a failed claim so a later retry
// may be attempted for the same generation.
func (s *Session) ReleaseSwitchVideoRenegotiationClaim(generation int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SwitchVideoRenegotiateGeneration == generation {
		s.SwitchVideoRenegotiateGeneration = 0
		s.SwitchVideoRenegotiateHold = false
		s.switchReplacementPCReady = false
	}
}

func switchVideoShouldHoldForRenegotiateLocked(s *Session) bool {
	if s.SwitchVideoRenegotiateHold {
		return true
	}
	if s.PendingMidCallRenegotiation == nil {
		return false
	}
	return IsSwitchVideoRenegotiationSource(s.PendingMidCallRenegotiation.Source)
}

func clearSwitchVideoRenegotiateHoldLocked(s *Session, source string) {
	if IsSwitchVideoRenegotiationSource(source) {
		s.SwitchVideoRenegotiateHold = false
	}
}
