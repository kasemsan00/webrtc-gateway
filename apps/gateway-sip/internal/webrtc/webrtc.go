package webrtc

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/config"
	pkg_webrtc "k2-gateway/internal/pkg/webrtc"
)

// Gateway represents a WebRTC gateway
type Gateway struct {
	PeerConnection *webrtc.PeerConnection
	AudioTrack     *webrtc.TrackLocalStaticRTP
	UnicastAddress string
}

// NewGateway creates a new WebRTC gateway
func NewGateway(turnConfig config.TURNConfig, unicastAddress string) (*Gateway, error) {
	webrtcConfig := webrtc.Configuration{
		ICEServers: pkg_webrtc.BuildICEServers(turnConfig),
	}

	peerConnection, err := webrtc.NewPeerConnection(webrtcConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	// Set ICE connection state change handler
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State changed: %s\n", connectionState.String())
	})

	gateway := &Gateway{
		PeerConnection: peerConnection,
		UnicastAddress: unicastAddress,
	}

	return gateway, nil
}

// SetupAudioTrack creates and adds an audio track to the peer connection
func (g *Gateway) SetupAudioTrack() error {
	var err error
	g.AudioTrack, err = webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"pion",
	)
	if err != nil {
		return fmt.Errorf("failed to create audio track: %w", err)
	}

	if _, err = g.PeerConnection.AddTrack(g.AudioTrack); err != nil {
		return fmt.Errorf("failed to add track: %w", err)
	}

	return nil
}

// HandleNegotiation handles the WebRTC offer/answer exchange
func (g *Gateway) HandleNegotiation() error {
	// Wait for offer from stdin
	offer := webrtc.SessionDescription{}
	Decode(readUntilNewline(), &offer)

	// Set remote description
	if err := g.PeerConnection.SetRemoteDescription(offer); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	// Create answer
	answer, err := g.PeerConnection.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("failed to create answer: %w", err)
	}

	// Set local description
	if err := g.PeerConnection.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	<-webrtc.GatheringCompletePromise(g.PeerConnection)

	// Output answer
	fmt.Println(Encode(g.PeerConnection.LocalDescription()))

	return nil
}

// GenerateSDPAnswer generates an SDP answer from an offer
func GenerateSDPAnswer(offer []byte, unicastAddress string, rtpListenerPort int) ([]byte, error) {
	offerParsed := sdp.SessionDescription{}
	if err := offerParsed.Unmarshal(offer); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SDP offer: %w", err)
	}

	answer := sdp.SessionDescription{
		Version: 0,
		Origin: sdp.Origin{
			Username:       "-",
			SessionID:      offerParsed.Origin.SessionID,
			SessionVersion: offerParsed.Origin.SessionID + 2,
			NetworkType:    "IN",
			AddressType:    "IP4",
			UnicastAddress: unicastAddress,
		},
		SessionName: "Pion WebRTC-SIP Gateway",
		ConnectionInformation: &sdp.ConnectionInformation{
			NetworkType: "IN",
			AddressType: "IP4",
			Address:     &sdp.Address{Address: unicastAddress},
		},
		TimeDescriptions: []sdp.TimeDescription{
			{
				Timing: sdp.Timing{
					StartTime: 0,
					StopTime:  0,
				},
			},
		},
		MediaDescriptions: []*sdp.MediaDescription{
			{
				MediaName: sdp.MediaName{
					Media:   "audio",
					Port:    sdp.RangedPort{Value: rtpListenerPort},
					Protos:  []string{"RTP", "AVP"},
					Formats: []string{"0"},
				},
				Attributes: []sdp.Attribute{
					{Key: "rtpmap", Value: "0 PCMU/8000"},
					{Key: "ptime", Value: "20"},
					{Key: "maxptime", Value: "150"},
					{Key: "recvonly"},
				},
			},
		},
	}

	answerBytes, err := answer.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SDP answer: %w", err)
	}

	return answerBytes, nil
}

// readUntilNewline reads from stdin until a newline is encountered
func readUntilNewline() string {
	reader := bufio.NewReader(os.Stdin)

	for {
		input, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			panic(fmt.Errorf("failed to read from stdin: %w", err))
		}

		if input = strings.TrimSpace(input); len(input) > 0 {
			fmt.Println()
			return input
		}
	}
}

// Encode encodes a SessionDescription to base64 JSON
func Encode(obj *webrtc.SessionDescription) string {
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		panic(fmt.Errorf("failed to marshal session description: %w", err))
	}

	return base64.StdEncoding.EncodeToString(jsonBytes)
}

// Decode decodes a base64 JSON string into a SessionDescription
func Decode(input string, obj *webrtc.SessionDescription) {
	jsonBytes, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		panic(fmt.Errorf("failed to decode base64: %w", err))
	}

	if err := json.Unmarshal(jsonBytes, obj); err != nil {
		panic(fmt.Errorf("failed to unmarshal session description: %w", err))
	}
}

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
