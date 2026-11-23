package session

import (
	"net"
	"testing"

	pionice "github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// loopbackSettingEngine restricts Pion ICE gathering in tests to IPv4 loopback
// with mDNS disabled so Windows Defender Firewall never prompts for LAN or
// multicast (224.0.0.251:5353) sockets during `go test`.
func loopbackSettingEngine() webrtc.SettingEngine {
	var se webrtc.SettingEngine
	se.SetIncludeLoopbackCandidate(true)
	se.SetIPFilter(func(ip net.IP) bool {
		return ip.IsLoopback()
	})
	se.SetICEMulticastDNSMode(pionice.MulticastDNSModeDisabled)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	return se
}

func newLoopbackPeerConnection(t *testing.T) *webrtc.PeerConnection {
	t.Helper()
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(loopbackSettingEngine())).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new loopback peer connection: %v", err)
	}
	return pc
}

func newLoopbackPeerConnectionWithMediaEngine(t *testing.T, mediaEngine *webrtc.MediaEngine) *webrtc.PeerConnection {
	t.Helper()
	pc, err := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithSettingEngine(loopbackSettingEngine()),
	).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new loopback peer connection with media engine: %v", err)
	}
	return pc
}
