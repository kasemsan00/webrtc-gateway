package session

import "strings"

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

// SetRemoteHeld records whether the SIP far end has put the call on hold and
// reports whether that changed.
func (s *Session) SetRemoteHeld(held bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.RemoteHeld == held {
		return false
	}
	s.RemoteHeld = held
	return true
}

// SetEndReason records why the call failed. The first reason wins.
func (s *Session) SetEndReason(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.EndReason == "" {
		s.EndReason = reason
	}
}

// GetEndReason returns why the call failed, or "" when it simply ended.
func (s *Session) GetEndReason() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.EndReason
}

// IsRemoteHoldOffer reports whether a mid-call SDP offer holds the call: its
// audio direction is sendonly or inactive, or it uses the older c=0.0.0.0 form.
// An audio-level attribute overrides the session-level one.
func IsRemoteHoldOffer(sdp string) bool {
	var sessionDirection, audioDirection string
	var sessionZero, audioZero, audioHasConnection bool
	section := "session"
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "m=") {
			section = "other"
			if strings.HasPrefix(line, "m=audio") {
				section = "audio"
			}
			continue
		}
		if section == "other" {
			continue
		}
		switch line {
		case "a=sendrecv", "a=sendonly", "a=recvonly", "a=inactive":
			direction := strings.TrimPrefix(line, "a=")
			if section == "audio" {
				audioDirection = direction
			} else {
				sessionDirection = direction
			}
			continue
		}
		if strings.HasPrefix(line, "c=") {
			fields := strings.Fields(line)
			zero := len(fields) == 3 && fields[2] == "0.0.0.0"
			if section == "audio" {
				audioHasConnection = true
				audioZero = zero
			} else {
				sessionZero = zero
			}
		}
	}
	direction := audioDirection
	if direction == "" {
		direction = sessionDirection
	}
	if direction == "sendonly" || direction == "inactive" {
		return true
	}
	if audioHasConnection {
		return audioZero
	}
	return sessionZero
}
