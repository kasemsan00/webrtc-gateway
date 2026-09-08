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
// It also holds SIP→WebRTC video until the client answers (or the attempt fails)
// so Android is not decoding a live GOP during setRemoteDescription.
// The offer is built on a replacement PeerConnection so the Android
// client can answer on its own new PC (1.1.0 resume-style).
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
