package session

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pion/webrtc/v4"
)

const (
	h264ConstrainedBaselineProfile = "42e01f"
	h264BaselineProfile            = "42001f"
)

func h264CodecParameters(payloadType webrtc.PayloadType, profile string, packetizationMode uint8, feedback []webrtc.RTCPFeedback) webrtc.RTPCodecParameters {
	return webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			SDPFmtpLine:  fmt.Sprintf("level-asymmetry-allowed=1;packetization-mode=%d;profile-level-id=%s", packetizationMode, profile),
			RTCPFeedback: feedback,
		},
		PayloadType: payloadType,
	}
}

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

	// Register mode 1 first to preserve the outbound-call default. Incoming SIP
	// calls override the transceiver preference before CreateAnswer based on the
	// mode negotiated with the SIP peer.
	videoCodecs := []webrtc.RTPCodecParameters{
		h264CodecParameters(96, h264ConstrainedBaselineProfile, 1, videoRTCPFeedback),
		h264CodecParameters(97, h264BaselineProfile, 1, videoRTCPFeedback),
		h264CodecParameters(98, h264ConstrainedBaselineProfile, 0, videoRTCPFeedback),
		h264CodecParameters(99, h264BaselineProfile, 0, videoRTCPFeedback),
	}
	for _, codec := range videoCodecs {
		if err := m.RegisterCodec(codec, webrtc.RTPCodecTypeVideo); err != nil {
			return nil, err
		}
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

func offeredH264CodecForPacketizationMode(offerSDP string, mode uint8) (webrtc.RTPCodecParameters, error) {
	if mode != 0 {
		mode = 1
	}

	var videoPayloads []webrtc.PayloadType
	rtpMaps := make(map[webrtc.PayloadType]string)
	fmtpLines := make(map[webrtc.PayloadType]string)
	currentMedia := ""
	for _, rawLine := range strings.Split(offerSDP, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if strings.HasPrefix(line, "m=") {
			fields := strings.Fields(line)
			currentMedia = "other"
			if len(fields) >= 4 && fields[0] == "m=video" {
				currentMedia = "video"
				for _, field := range fields[3:] {
					value, err := strconv.Atoi(field)
					if err == nil && value >= 0 && value <= 127 {
						videoPayloads = append(videoPayloads, webrtc.PayloadType(value))
					}
				}
			}
			continue
		}
		if currentMedia != "video" {
			continue
		}
		if payloadType, value, ok := parseSDPPayloadAttribute(line, "a=rtpmap:"); ok {
			rtpMaps[payloadType] = value
			continue
		}
		if payloadType, value, ok := parseSDPPayloadAttribute(line, "a=fmtp:"); ok {
			fmtpLines[payloadType] = value
		}
	}

	feedback := []webrtc.RTCPFeedback{
		{Type: "nack"},
		{Type: "nack", Parameter: "pli"},
		{Type: "ccm", Parameter: "fir"},
		{Type: "goog-remb"},
		{Type: "transport-cc"},
	}
	for _, payloadType := range videoPayloads {
		rtpMap := strings.Fields(rtpMaps[payloadType])
		if len(rtpMap) == 0 || !strings.EqualFold(rtpMap[0], "H264/90000") {
			continue
		}
		fmtpLine := fmtpLines[payloadType]
		if h264PacketizationModeFromFMTP(fmtpLine) != mode {
			continue
		}
		return webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:     webrtc.MimeTypeH264,
				ClockRate:    90000,
				SDPFmtpLine:  fmtpLine,
				RTCPFeedback: feedback,
			},
			PayloadType: payloadType,
		}, nil
	}
	return webrtc.RTPCodecParameters{}, fmt.Errorf("WebRTC offer has no H264 packetization-mode=%d codec", mode)
}

func parseSDPPayloadAttribute(line, prefix string) (webrtc.PayloadType, string, bool) {
	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return 0, "", false
	}
	parts := strings.SplitN(line[len(prefix):], " ", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	value, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || value < 0 || value > 127 {
		return 0, "", false
	}
	return webrtc.PayloadType(value), strings.TrimSpace(parts[1]), true
}

func h264PacketizationModeFromFMTP(fmtpLine string) uint8 {
	for _, parameter := range strings.Split(fmtpLine, ";") {
		parts := strings.SplitN(parameter, "=", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "packetization-mode") {
			if strings.TrimSpace(parts[1]) == "1" {
				return 1
			}
			return 0
		}
	}
	return 0
}

// PreferWebRTCH264PacketizationMode restricts every video transceiver to the
// first compatible codec/PT from the WebRTC offer for the RFC 6184 mode
// negotiated on the SIP leg. This must run after SetRemoteDescription and
// before CreateAnswer so the answer preserves the offerer's payload mapping.
func PreferWebRTCH264PacketizationMode(pc *webrtc.PeerConnection, offerSDP string, mode uint8) error {
	codec, err := offeredH264CodecForPacketizationMode(offerSDP, mode)
	if err != nil {
		return err
	}
	for _, transceiver := range pc.GetTransceivers() {
		if transceiver.Kind() != webrtc.RTPCodecTypeVideo {
			continue
		}
		if err := transceiver.SetCodecPreferences([]webrtc.RTPCodecParameters{codec}); err != nil {
			return fmt.Errorf("set H264 packetization-mode=%d preference: %w", mode, err)
		}
	}
	return nil
}

func localH264CodecsForPacketizationMode(mode uint8) []webrtc.RTPCodecParameters {
	if mode != 0 {
		mode = 1
	}
	feedback := []webrtc.RTCPFeedback{
		{Type: "nack"},
		{Type: "nack", Parameter: "pli"},
		{Type: "ccm", Parameter: "fir"},
		{Type: "goog-remb"},
		{Type: "transport-cc"},
	}
	if mode == 0 {
		return []webrtc.RTPCodecParameters{
			h264CodecParameters(98, h264ConstrainedBaselineProfile, 0, feedback),
			h264CodecParameters(99, h264BaselineProfile, 0, feedback),
		}
	}
	return []webrtc.RTPCodecParameters{
		h264CodecParameters(96, h264ConstrainedBaselineProfile, 1, feedback),
		h264CodecParameters(97, h264BaselineProfile, 1, feedback),
	}
}

// restoreSwitchOfferH264Preferences replaces the single remote-PT lock from
// PreferWebRTCH264PacketizationMode with the negotiated codec plus the local
// MediaEngine codecs for the SIP mode. In-place CreateOffer on a one-codec
// lock is what broke React Native after 1.3.7; 1.3.6 advertised a wider list
// and kept the same WS offer/answer contract.
func restoreSwitchOfferH264Preferences(pc *webrtc.PeerConnection, mode uint8) error {
	if pc == nil {
		return fmt.Errorf("peer connection not available")
	}
	if mode != 0 {
		mode = 1
	}

	codecs := make([]webrtc.RTPCodecParameters, 0, 4)
	seen := make(map[webrtc.PayloadType]bool)
	if ld := pc.LocalDescription(); ld != nil && ld.SDP != "" {
		if negotiated, err := offeredH264CodecForPacketizationMode(ld.SDP, mode); err == nil {
			codecs = append(codecs, negotiated)
			seen[negotiated.PayloadType] = true
		}
	}
	for _, codec := range localH264CodecsForPacketizationMode(mode) {
		if seen[codec.PayloadType] {
			continue
		}
		codecs = append(codecs, codec)
		seen[codec.PayloadType] = true
	}
	if len(codecs) == 0 {
		return fmt.Errorf("no H264 packetization-mode=%d codecs for switch offer", mode)
	}

	for _, transceiver := range pc.GetTransceivers() {
		if transceiver.Kind() != webrtc.RTPCodecTypeVideo {
			continue
		}
		if err := transceiver.SetCodecPreferences(codecs); err != nil {
			return fmt.Errorf("restore H264 packetization-mode=%d switch offer preference: %w", mode, err)
		}
	}
	return nil
}
