package session

import (
	"net"
	"testing"
	"time"

	"github.com/pion/rtcp"

	"webrtc-sip-gateway/internal/config"
)

// With the H.264 AU normalizer on, SIP->WebRTC video is renumbered, so a
// browser NACK names sequence numbers Asterisk never used. It must not be
// forwarded; without renumbering the fallback still works.
func TestForwardBrowserNACKToAsterisk_OnlyWithoutRenumbering(t *testing.T) {
	asterisk, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer asterisk.Close()
	local, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()

	s := &Session{
		ID:                      "nack-space",
		VideoRTPConn:            local,
		AsteriskVideoAddr:       asterisk.LocalAddr().(*net.UDPAddr),
		RemoteVideoSSRC:         1234,
		VideoFeedbackTransport:  config.SIPVideoFeedbackTransportRTP,
		VideoAUNormalizeEnabled: true,
	}
	nacks := []rtcp.NackPair{{PacketID: 10}}
	buf := make([]byte, 1500)

	s.forwardBrowserNACKToAsterisk(nacks)
	_ = asterisk.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := asterisk.ReadFromUDP(buf); err == nil {
		t.Fatal("renumbered NACK must not reach Asterisk")
	}

	s.VideoAUNormalizeEnabled = false
	s.forwardBrowserNACKToAsterisk(nacks)
	_ = asterisk.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := asterisk.ReadFromUDP(buf); err != nil {
		t.Fatalf("NACK in Asterisk's own sequence space should be forwarded: %v", err)
	}
}

// Media moved mid-call: the RTCP address learned from the old endpoint must
// not keep receiving SIP-leg keyframe requests.
func TestUpdateAsteriskEndpointsFromOffer_ForgetsOldVideoRTCPAddr(t *testing.T) {
	s := &Session{ID: "rtcp-move"}
	s.SetAsteriskEndpoints(&net.UDPAddr{IP: net.IPv4(10, 0, 0, 5), Port: 10000}, &net.UDPAddr{IP: net.IPv4(10, 0, 0, 5), Port: 10002})
	s.AsteriskVideoRTCPAddr = &net.UDPAddr{IP: net.IPv4(10, 0, 0, 5), Port: 10003}
	s.VideoRTCPSource = "rtcp"

	if !s.UpdateAsteriskEndpointsFromOffer(net.IPv4(10, 0, 0, 9), 20000, 20002) {
		t.Fatal("expected endpoints to move")
	}
	if addr, source := s.GetLearnedVideoRTCPAddr(); addr != nil || source != "unknown" {
		t.Fatalf("expected learned RTCP addr cleared, got %v (%s)", addr, source)
	}
	if !s.inSymmetricRTPTrustWindow(time.Now()) {
		t.Fatal("expected the trust window reopened for the new endpoint")
	}
}
