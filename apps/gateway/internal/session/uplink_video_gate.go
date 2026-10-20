package session

// holdUplinkVideoUntilIDR is true when a WebRTC video packet must not be written
// to SIP yet. Asterisk Echo and other SIP decoders lock onto the first packets
// they see; P-frames before a complete IDR produce a black remote.
func holdUplinkVideoUntilIDR(uplinkIDRForwarded bool, packetStartsIDR bool) bool {
	return !uplinkIDRForwarded && !packetStartsIDR
}
