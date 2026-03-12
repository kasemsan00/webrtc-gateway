// Package webrtc provides shared utilities for WebRTC operations
package webrtc

import (
	"fmt"

	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/config"
)

// BuildICEServers constructs ICE servers configuration from TURN config
// This is a shared utility used across multiple files
func BuildICEServers(turnConfig config.TURNConfig) []webrtc.ICEServer {
	var iceServers []webrtc.ICEServer

	if turnConfig.Server == "" {
		return iceServers
	}

	server := webrtc.ICEServer{
		URLs: []string{turnConfig.Server},
	}

	if turnConfig.Username != "" && turnConfig.Password != "" {
		server.Username = turnConfig.Username
		server.Credential = turnConfig.Password
		fmt.Printf("TURN server configured: %s (with credentials)\n", turnConfig.Server)
	} else {
		fmt.Printf("TURN server configured: %s (no credentials)\n", turnConfig.Server)
	}

	iceServers = append(iceServers, server)
	return iceServers
}
