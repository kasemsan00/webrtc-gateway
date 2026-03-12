package session

// buildSTAPAPayload creates a STAP-A payload from SPS and PPS NAL units
// STAP-A format: [STAP-A indicator (1 byte)] + [NAL size (2 bytes)][NAL unit] + ...
// This allows sending multiple NAL units (SPS+PPS) in a single RTP packet
func buildSTAPAPayload(sps, pps []byte) []byte {
	if len(sps) == 0 || len(pps) == 0 {
		return nil
	}

	// Calculate STAP-A indicator byte
	// F (1 bit) = 0, NRI (2 bits) = use max priority (3), Type (5 bits) = 24 (STAP-A)
	// NRI=3 ensures this parameter set packet is treated as high priority
	stapaIndicator := byte(0x60 | 24) // 0x78 = (NRI=3, Type=24)

	// Calculate total payload size:
	// 1 (indicator) + 2 (sps len) + len(sps) + 2 (pps len) + len(pps)
	payloadSize := 1 + 2 + len(sps) + 2 + len(pps)
	payload := make([]byte, payloadSize)

	offset := 0

	// Write STAP-A indicator
	payload[offset] = stapaIndicator
	offset++

	// Write SPS: [2-byte length][NAL unit]
	payload[offset] = byte(len(sps) >> 8)
	payload[offset+1] = byte(len(sps) & 0xFF)
	offset += 2
	copy(payload[offset:], sps)
	offset += len(sps)

	// Write PPS: [2-byte length][NAL unit]
	payload[offset] = byte(len(pps) >> 8)
	payload[offset+1] = byte(len(pps) & 0xFF)
	offset += 2
	copy(payload[offset:], pps)

	return payload
}

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
