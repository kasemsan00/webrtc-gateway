package session

import "time"

// SIPNegotiatedMedia captures the primary SIP media transport attributes
// established for the dialog (profile, rtcp-mux, packetization-mode) so later
// gateway-originated mid-call offers keep them consistent.
type SIPNegotiatedMedia struct {
	AudioProfile           string
	VideoProfile           string
	AudioRTCPMux           bool
	VideoRTCPMux           bool
	VideoPacketizationMode uint8
}

// SetSIPNegotiatedMedia records the dialog's negotiated primary media settings.
func (s *Session) SetSIPNegotiatedMedia(m SIPNegotiatedMedia) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sipMediaNegotiated = true
	s.sipNegotiatedMedia = m
	s.mu.Unlock()
}

// GetSIPNegotiatedMedia returns the negotiated primary media settings if set.
func (s *Session) GetSIPNegotiatedMedia() (SIPNegotiatedMedia, bool) {
	if s == nil {
		return SIPNegotiatedMedia{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sipNegotiatedMedia, s.sipMediaNegotiated
}

// NextSDPOriginVersion returns the dialog's constant SDP origin sess-id and a
// monotonically non-decreasing sess-version for RFC 3264 o= generation.
func (s *Session) NextSDPOriginVersion(nowMillis int64) (sessID, sessVersion int64) {
	if nowMillis <= 0 {
		nowMillis = time.Now().UnixMilli()
		if nowMillis <= 0 {
			nowMillis = 1
		}
	}
	if s == nil {
		return nowMillis, nowMillis
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sdpSessionID == 0 {
		s.sdpSessionID = nowMillis
		s.sdpSessionVersion = nowMillis
	} else {
		s.sdpSessionVersion++
	}
	return s.sdpSessionID, s.sdpSessionVersion
}

// RememberLastWireSIPAnswer saves the last SDP body sent on a SIP 200 OK for
// an inbound INVITE/re-INVITE/UPDATE so unchanged refreshes keep their o= version.
func (s *Session) RememberLastWireSIPAnswer(sdp []byte) {
	if s == nil || len(sdp) == 0 {
		return
	}
	copySDP := make([]byte, len(sdp))
	copy(copySDP, sdp)
	s.mu.Lock()
	s.lastWireSIPAnswer = copySDP
	s.mu.Unlock()
}

// LastWireSIPAnswer returns a copy of the last wire SIP SDP answer sent, or nil.
func (s *Session) LastWireSIPAnswer() []byte {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.lastWireSIPAnswer) == 0 {
		return nil
	}
	copySDP := make([]byte, len(s.lastWireSIPAnswer))
	copy(copySDP, s.lastWireSIPAnswer)
	return copySDP
}

// SIPMediaSlotKind names what an m-line position of the SIP dialog carries.
type SIPMediaSlotKind string

const (
	// SIPMediaSlotAudio is the call's audio: the first m=audio of the offer.
	SIPMediaSlotAudio SIPMediaSlotKind = "audio"
	// SIPMediaSlotVideo is the call's primary video: the first m=video.
	SIPMediaSlotVideo SIPMediaSlotKind = "video"
	// SIPMediaSlotDeclined is an m-line the gateway does not use. It stays in
	// the SDP with port 0 so m-line count and order match the peer's offer.
	SIPMediaSlotDeclined SIPMediaSlotKind = "declined"
)

// SIPMediaSlot is one m-line position of the SIP dialog's SDP.
type SIPMediaSlot struct {
	Kind SIPMediaSlotKind
	// DeclinedLine is this position's m= line with port 0. It is what the
	// gateway writes for a declined slot, and for an audio or video slot when
	// it has no media section of its own to put there.
	DeclinedLine string
}

// SetSIPMediaLayout records the m-line order of the latest SIP offer the peer
// sent on this dialog. A nil layout means the plain audio[+video] order.
func (s *Session) SetSIPMediaLayout(layout []SIPMediaSlot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sipMediaLayout = append([]SIPMediaSlot(nil), layout...)
	s.mu.Unlock()
}

// SIPMediaLayout returns a copy of the m-line order recorded by
// SetSIPMediaLayout, or nil.
func (s *Session) SIPMediaLayout() []SIPMediaSlot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.sipMediaLayout) == 0 {
		return nil
	}
	return append([]SIPMediaSlot(nil), s.sipMediaLayout...)
}
