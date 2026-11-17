package webrtc

import (
	"fmt"
	"net"
)

// GetUnicastAddress determines and returns the unicast IP address
func GetUnicastAddress(preferredAddress string) (string, error) {
	if preferredAddress != "" {
		return preferredAddress, nil
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("failed to get interface addresses: %w", err)
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no suitable unicast address found")
}
