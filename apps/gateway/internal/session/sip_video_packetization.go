package session

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
