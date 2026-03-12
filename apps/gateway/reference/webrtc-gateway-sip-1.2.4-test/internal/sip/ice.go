package sip

import (
	"crypto/rand"
	"fmt"
	"net"

	"github.com/pion/stun"
)

// ICE-lite implementation for SIP-to-RTP gateway
// Uses pion/stun library for proper RFC 5389 compliant STUN handling

// ICECredentials holds ICE-lite credentials for a session
type ICECredentials struct {
	LocalUfrag  string
	LocalPwd    string
	RemoteUfrag string
	RemotePwd   string
}

// GenerateICECredentials generates random ICE credentials
func GenerateICECredentials() *ICECredentials {
	return &ICECredentials{
		LocalUfrag: generateRandomString(8),  // 8 chars
		LocalPwd:   generateRandomString(22), // 22+ chars as per RFC
	}
}

// generateRandomString generates a random alphanumeric string
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	bytes := make([]byte, length)
	rand.Read(bytes)
	for i := range bytes {
		bytes[i] = charset[int(bytes[i])%len(charset)]
	}
	return string(bytes)
}

// IsSTUNPacket checks if a packet is a STUN message
func IsSTUNPacket(data []byte) bool {
	return stun.IsMessage(data)
}

// HandleSTUNBindingRequest processes a STUN binding request and returns a response
func HandleSTUNBindingRequest(data []byte, remoteAddr *net.UDPAddr, creds *ICECredentials) ([]byte, error) {
	// Parse incoming STUN message
	msg := new(stun.Message)
	msg.Raw = data
	if err := msg.Decode(); err != nil {
		return nil, fmt.Errorf("failed to decode STUN message: %w", err)
	}

	// Check if it's a binding request
	if msg.Type != stun.BindingRequest {
		return nil, fmt.Errorf("not a binding request: %s", msg.Type)
	}

	fmt.Printf("[ICE] Received STUN Binding Request from %s (tx=%x)\n", remoteAddr.String(), msg.TransactionID[:4])

	// Build response with the SAME transaction ID as the request
	response := new(stun.Message)
	response.SetType(stun.BindingSuccess)
	response.TransactionID = msg.TransactionID // Use request's transaction ID

	// Add XOR-MAPPED-ADDRESS
	xorAddr := &stun.XORMappedAddress{
		IP:   remoteAddr.IP,
		Port: remoteAddr.Port,
	}
	if err := xorAddr.AddTo(response); err != nil {
		return nil, fmt.Errorf("failed to add XOR-MAPPED-ADDRESS: %w", err)
	}

	// Add MESSAGE-INTEGRITY using local password
	if creds != nil && creds.LocalPwd != "" {
		integrity := stun.NewShortTermIntegrity(creds.LocalPwd)
		if err := integrity.AddTo(response); err != nil {
			return nil, fmt.Errorf("failed to add MESSAGE-INTEGRITY: %w", err)
		}
	}

	// Add FINGERPRINT
	if err := stun.Fingerprint.AddTo(response); err != nil {
		return nil, fmt.Errorf("failed to add FINGERPRINT: %w", err)
	}

	fmt.Printf("[ICE] Built STUN response: %d bytes for %s (tx=%x)\n", len(response.Raw), remoteAddr.String(), response.TransactionID[:4])

	return response.Raw, nil
}

// HandleSTUNPacket handles STUN packet (detection + response) for RTP handlers
// Consolidates the duplicate STUN handling logic in handleAudioRTPPacketsForSession and handleVideoRTPPacketsForSession
func HandleSTUNPacket(conn *net.UDPConn, data []byte, remoteAddr *net.UDPAddr,
	iceCreds *ICECredentials, sessionID, mediaType string) bool {

	if !IsSTUNPacket(data) {
		return false
	}

	response, err := HandleSTUNBindingRequest(data, remoteAddr, iceCreds)
	if err != nil {
		fmt.Printf("[Session %s] Error handling %s STUN: %v\n", sessionID, mediaType, err)
		return true
	}

	bytesWritten, err := conn.WriteToUDP(response, remoteAddr)
	if err != nil {
		fmt.Printf("[Session %s] Error sending %s STUN response: %v\n", sessionID, mediaType, err)
	} else {
		fmt.Printf("[Session %s] Sent %s STUN response: %d bytes to %s\n",
			sessionID, mediaType, bytesWritten, remoteAddr.String())
	}

	return true
}
