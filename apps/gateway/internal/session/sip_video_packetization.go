package session

const defaultSIPVideoPayloadType uint8 = 96

// SetSIPVideoPayloadType stores the H.264 RTP payload type negotiated with the
// SIP peer. A zero value clears the negotiated value and restores PT 96 as the
// compatibility fallback.
func (s *Session) SetSIPVideoPayloadType(payloadType uint8) {
	s.mu.Lock()
	s.SIPVideoPT = payloadType
	s.mu.Unlock()
}

// GetSIPVideoPayloadType returns the negotiated H.264 RTP payload type. PT 96
// remains the fallback for outbound offers and peers without an explicit
// H.264 rtpmap.
func (s *Session) GetSIPVideoPayloadType() uint8 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sipVideoPayloadTypeLocked()
}

// sipVideoPayloadTypeLocked returns the effective SIP H.264 payload type when
// the caller already holds s.mu. Keeping this separate prevents recursive
// RWMutex acquisition in RTP hot paths while preserving synchronized access
// for callers that use GetSIPVideoPayloadType.
func (s *Session) sipVideoPayloadTypeLocked() uint8 {
	if s.SIPVideoPT == 0 {
		return defaultSIPVideoPayloadType
	}
	return s.SIPVideoPT
}

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
