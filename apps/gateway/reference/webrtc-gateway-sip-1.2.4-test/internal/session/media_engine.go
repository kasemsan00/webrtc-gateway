package session

import (
	"github.com/pion/webrtc/v4"
)

// createCustomMediaEngine creates a MediaEngine with custom RTCPFeedback support
func createCustomMediaEngine() (*webrtc.MediaEngine, error) {
	m := &webrtc.MediaEngine{}

	// Video codecs with custom RTCPFeedback
	videoRTCPFeedback := []webrtc.RTCPFeedback{
		{Type: "nack"},                   // Negative ACK for lost packets
		{Type: "nack", Parameter: "pli"}, // Picture Loss Indication
		{Type: "ccm", Parameter: "fir"},  // Full Intra Request
		{Type: "goog-remb"},              // Receiver Estimated Max Bitrate
		{Type: "transport-cc"},           // Transport-Wide Congestion Control
	}

	// Register H.264 with custom feedback
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			RTCPFeedback: videoRTCPFeedback,
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}

	// Register VP8 with custom feedback (fallback)
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeVP8,
			ClockRate:    90000,
			RTCPFeedback: videoRTCPFeedback,
		},
		PayloadType: 97,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}

	// Audio codecs
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypePCMU,
			ClockRate: 8000,
		},
		PayloadType: 0,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}

	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48000,
			Channels:  2,
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}

	return m, nil
}
