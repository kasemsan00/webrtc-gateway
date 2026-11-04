package session

// holdUplinkVideoUntilIDR is true when a WebRTC video packet must not be written
// to SIP yet. Asterisk Echo and other SIP decoders lock onto the first packets
// they see; P-frames before a complete IDR produce a black remote.
// packetStartsIDR is also true for SPS/PPS that belong in front of that IDR,
// and for any packet once an IDR of this call has been accepted. Continuations
// of that IDR are not themselves keyframes and must still be forwarded.
func holdUplinkVideoUntilIDR(uplinkIDRForwarded bool, packetStartsIDR bool) bool {
	return !uplinkIDRForwarded && !packetStartsIDR
}

// uplinkVideoOpenLocked reports whether non-keyframes may be forwarded.
// The caller holds s.mu. An accepted IDR counts even while its packet is
// still sitting in the picture hold.
func (s *Session) uplinkVideoOpenLocked() bool {
	if s == nil {
		return false
	}
	return !s.LastUplinkKeyframe.IsZero() || s.uplinkIDRAccepted
}

// noteUplinkIDRAcceptedLocked opens the uplink for the rest of this IDR.
// The caller holds s.mu.
func (s *Session) noteUplinkIDRAcceptedLocked() {
	if s == nil {
		return
	}
	s.uplinkIDRAccepted = true
}
