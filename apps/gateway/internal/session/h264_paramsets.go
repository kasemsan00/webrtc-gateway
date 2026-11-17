package session

// forceNRI modifies the NAL header (first byte) to enforce nal_ref_idc (NRI) = 3 (High Priority)
// This mimics Linphone behavior and ensures Asterisk/chan_sip doesn't drop the packet
func forceNRI(nal []byte) []byte {
	if len(nal) == 0 {
		return nil
	}
	newNAL := make([]byte, len(nal))
	copy(newNAL, nal)
	// NAL Header: F(1) | NRI(2) | Type(5)
	// We want NRI=3 (11 binary) -> 0x60
	// Keep F and Type, overwrite NRI
	// Mask: 1001 1111 (0x9F) -> clears NRI bits
	// Set:  0110 0000 (0x60) -> sets NRI=3
	newNAL[0] = (newNAL[0] & 0x9F) | 0x60
	return newNAL
}
