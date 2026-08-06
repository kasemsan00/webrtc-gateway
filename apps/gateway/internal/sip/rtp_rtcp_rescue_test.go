package sip

import "testing"

func TestIsLikelyRTPPacketRejectsRTCP(t *testing.T) {
	// V=2, PT=200 (SR)
	rtcp := []byte{0x80, 200, 0x00, 0x06, 0, 0, 0, 1, 0, 0, 0, 0}
	if isLikelyRTPPacket(rtcp) {
		t.Fatalf("expected RTCP SR not to look like RTP")
	}
}

func TestIsLikelyRTPPacketAcceptsH264(t *testing.T) {
	// V=2, PT=96, 12-byte header + payload
	rtp := []byte{
		0x80, 96, 0x12, 0x34,
		0x00, 0x00, 0x00, 0x01,
		0x10, 0x20, 0x30, 0x40,
		0x67, 0x42, 0xe0, 0x1f,
	}
	if !isLikelyRTPPacket(rtp) {
		t.Fatalf("expected H264 RTP to be rescued from RTCP port")
	}
}
