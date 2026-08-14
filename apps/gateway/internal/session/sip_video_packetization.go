package session

// SetSIPOfferIncludeVideo controls whether outbound SIP SDP includes an m=video
// section. Audio-only and recvonly WebRTC offers must set this to false.
func (s *Session) SetSIPOfferIncludeVideo(include bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := include
	s.sipOfferIncludeVideo = &value
}

// SIPOfferIncludeVideo reports whether SIP SDP should advertise video.
// Unset sessions default to true for backward compatibility.
func (s *Session) SIPOfferIncludeVideo() bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sipOfferIncludeVideo == nil {
		return true
	}
	return *s.sipOfferIncludeVideo
}

// SetSIPVideoPacketizationMode stores the RFC 6184 packetization mode negotiated
// with the SIP peer. Unsupported values fall back to mode 1, which is the
// gateway default for outbound SIP offers.
func (s *Session) SetSIPVideoPacketizationMode(mode uint8) {
	if mode != 0 && mode != 1 {
		mode = 1
	}
	s.mu.Lock()
	s.SIPVideoPacketizationMode = mode
	s.mu.Unlock()
}

// GetSIPVideoPacketizationMode returns the negotiated RFC 6184 mode.
func (s *Session) GetSIPVideoPacketizationMode() uint8 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SIPVideoPacketizationMode
}
