package session

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
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

// videoRTCPFeedback is the H.264 feedback set advertised to the phone.
// goog-remb is omitted: this gateway does not generate REMB. transport-cc is
// advertised only when the TWCC sender interceptor is installed with it.
func videoRTCPFeedback(twccEnabled bool) []webrtc.RTCPFeedback {
	feedback := []webrtc.RTCPFeedback{
		{Type: "nack"},
		{Type: "nack", Parameter: "pli"},
		{Type: "ccm", Parameter: "fir"},
	}
	if twccEnabled {
		feedback = append(feedback, webrtc.RTCPFeedback{Type: "transport-cc"})
	}
	return feedback
}

func createCustomMediaEngineWithTWCC(twccEnabled bool) (*webrtc.MediaEngine, error) {
	m := &webrtc.MediaEngine{}

	feedback := videoRTCPFeedback(twccEnabled)

	// Register mode 1 first to preserve the outbound-call default. Incoming SIP
	// calls override the transceiver preference before CreateAnswer based on the
	// mode negotiated with the SIP peer.
	videoCodecs := []webrtc.RTPCodecParameters{
		h264CodecParameters(96, h264ConstrainedBaselineProfile, 1, feedback),
		h264CodecParameters(97, h264BaselineProfile, 1, feedback),
		h264CodecParameters(98, h264ConstrainedBaselineProfile, 0, feedback),
		h264CodecParameters(99, h264BaselineProfile, 0, feedback),
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

// newGatewayWebRTCAPI builds the PeerConnection API used for a new call and for
// a replacement PeerConnection. NACK retransmission stays on the gateway's
// own cache. RTCP SR/RR come from ConfigureRTCPReports. TWCC sender feedback
// is what lets the phone's availableOutgoingBitrate move. The SettingEngine
// carries the ICE gathering limits (interface/IP filters, NAT1To1, network
// types) so every PeerConnection advertises only reachable candidates.
func newGatewayWebRTCAPI(twccEnabled bool, sessionID string, ice config.WebRTCICEConfig) (*webrtc.API, error) {
	mediaEngine, err := createCustomMediaEngineWithTWCC(twccEnabled)
	if err != nil {
		return nil, err
	}
	registry, err := gatewayInterceptorRegistry(twccEnabled, sessionID)
	if err != nil {
		return nil, err
	}
	if twccEnabled {
		if err := registerTransportCC(mediaEngine); err != nil {
			return nil, err
		}
	}
	settingEngine, err := gatewaySettingEngine(ice)
	if err != nil {
		return nil, err
	}
	return webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(registry),
		webrtc.WithSettingEngine(settingEngine),
	), nil
}

// gatewayInterceptorRegistry is the interceptor chain installed on every
// gateway PeerConnection. browserFeedback is first so it sees RTP before the
// report interceptor and so RR writes pass through its accounting wrapper.
func gatewayInterceptorRegistry(twccEnabled bool, sessionID string) (*interceptor.Registry, error) {
	registry := &interceptor.Registry{}
	registry.Add(&browserFeedbackFactory{sessionID: sessionID, twcc: twccEnabled})
	if err := webrtc.ConfigureRTCPReports(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func registerTransportCC(mediaEngine *webrtc.MediaEngine) error {
	mediaEngine.RegisterFeedback(webrtc.RTCPFeedback{Type: webrtc.TypeRTCPFBTransportCC}, webrtc.RTPCodecTypeVideo)
	if err := mediaEngine.RegisterHeaderExtension(
		webrtc.RTPHeaderExtensionCapability{URI: transportCCURI}, webrtc.RTPCodecTypeVideo,
	); err != nil {
		return err
	}
	mediaEngine.RegisterFeedback(webrtc.RTCPFeedback{Type: webrtc.TypeRTCPFBTransportCC}, webrtc.RTPCodecTypeAudio)
	return mediaEngine.RegisterHeaderExtension(
		webrtc.RTPHeaderExtensionCapability{URI: transportCCURI}, webrtc.RTPCodecTypeAudio,
	)
}

type offeredVideoSection struct {
	mid       string
	rejected  bool
	payloads  []webrtc.PayloadType
	rtpMaps   map[webrtc.PayloadType]string
	fmtpLines map[webrtc.PayloadType]string
}

type preferredCodecTrack interface {
	SetPreferredCodec(webrtc.RTPCodecParameters)
}

func parseOfferedVideoSections(sdpText string) []offeredVideoSection {
	var sections []offeredVideoSection
	var current *offeredVideoSection
	for _, rawLine := range strings.Split(sdpText, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if strings.HasPrefix(line, "m=") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[0] == "m=video" {
				sec := offeredVideoSection{
					rejected:  fields[1] == "0",
					rtpMaps:   make(map[webrtc.PayloadType]string),
					fmtpLines: make(map[webrtc.PayloadType]string),
				}
				for _, field := range fields[3:] {
					value, err := strconv.Atoi(field)
					if err == nil && value >= 0 && value <= 127 {
						sec.payloads = append(sec.payloads, webrtc.PayloadType(value))
					}
				}
				sections = append(sections, sec)
				current = &sections[len(sections)-1]
			} else {
				current = nil
			}
			continue
		}
		if current == nil {
			continue
		}
		if current.mid == "" && strings.HasPrefix(line, "a=mid:") {
			current.mid = strings.TrimSpace(strings.TrimPrefix(line, "a=mid:"))
			continue
		}
		if payloadType, value, ok := parseSDPPayloadAttribute(line, "a=rtpmap:"); ok {
			current.rtpMaps[payloadType] = value
			continue
		}
		if payloadType, value, ok := parseSDPPayloadAttribute(line, "a=fmtp:"); ok {
			current.fmtpLines[payloadType] = value
		}
	}
	return sections
}

func (s offeredVideoSection) h264CodecForMode(mode uint8, preferredPT webrtc.PayloadType) (webrtc.RTPCodecParameters, bool) {
	if mode != 0 {
		mode = 1
	}
	feedback := videoRTCPFeedback(true)
	var first webrtc.RTPCodecParameters
	foundFirst := false
	for _, payloadType := range s.payloads {
		rtpMap := strings.Fields(s.rtpMaps[payloadType])
		if len(rtpMap) == 0 || !strings.EqualFold(rtpMap[0], "H264/90000") {
			continue
		}
		fmtpLine := s.fmtpLines[payloadType]
		if h264PacketizationModeFromFMTP(fmtpLine) != mode {
			continue
		}
		codec := webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:     webrtc.MimeTypeH264,
				ClockRate:    90000,
				SDPFmtpLine:  fmtpLine,
				RTCPFeedback: feedback,
			},
			PayloadType: payloadType,
		}
		if preferredPT != 0 && payloadType == preferredPT {
			return codec, true
		}
		if !foundFirst {
			first = codec
			foundFirst = true
		}
	}
	return first, foundFirst
}

func offeredH264CodecForPacketizationMode(offerSDP string, mode uint8) (webrtc.RTPCodecParameters, error) {
	if mode != 0 {
		mode = 1
	}
	sections := parseOfferedVideoSections(offerSDP)
	var first webrtc.RTPCodecParameters
	foundAny := false
	for _, sec := range sections {
		if sec.rejected {
			continue
		}
		codec, ok := sec.h264CodecForMode(mode, 0)
		if !ok {
			return webrtc.RTPCodecParameters{}, fmt.Errorf("WebRTC offer has no H264 packetization-mode=%d codec", mode)
		}
		if !foundAny {
			first = codec
			foundAny = true
		}
	}
	if !foundAny {
		return webrtc.RTPCodecParameters{}, fmt.Errorf("WebRTC offer has no H264 packetization-mode=%d codec", mode)
	}
	return first, nil
}

func localVideoPayloadByMid(pc *webrtc.PeerConnection) map[string]webrtc.PayloadType {
	if pc == nil {
		return nil
	}
	local := pc.LocalDescription()
	if local == nil || local.SDP == "" {
		return nil
	}
	byMid := make(map[string]webrtc.PayloadType)
	for _, sec := range parseOfferedVideoSections(local.SDP) {
		if sec.rejected || sec.mid == "" || len(sec.payloads) == 0 {
			continue
		}
		for _, pt := range sec.payloads {
			rtpMap := strings.Fields(sec.rtpMaps[pt])
			if len(rtpMap) > 0 && strings.EqualFold(rtpMap[0], "H264/90000") {
				byMid[sec.mid] = pt
				break
			}
		}
	}
	return byMid
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

// PreferWebRTCH264PacketizationMode restricts every active video transceiver to
// a compatible codec/PT from its own m=video section in the WebRTC offer for
// the RFC 6184 mode negotiated on the SIP leg, preserving any previously
// answered payload type on that mid when still offered. This must run after
// SetRemoteDescription and before CreateAnswer so both the answer SDP and
// bound local tracks use the offerer's exact per-section payload mapping.
// Audio-only offers have no video m-line and must not fail H264 selection.
func PreferWebRTCH264PacketizationMode(pc *webrtc.PeerConnection, offerSDP string, mode uint8) error {
	if !AnalyzeOfferVideo(offerSDP).HasVideoMLine {
		return nil
	}
	if pc == nil {
		return fmt.Errorf("peer connection not available")
	}
	fallbackCodec, err := offeredH264CodecForPacketizationMode(offerSDP, mode)
	if err != nil {
		return err
	}
	sections := parseOfferedVideoSections(offerSDP)
	sectionByMid := make(map[string]offeredVideoSection, len(sections))
	var activeSections []offeredVideoSection
	for _, sec := range sections {
		if sec.mid != "" {
			sectionByMid[sec.mid] = sec
		}
		if !sec.rejected {
			activeSections = append(activeSections, sec)
		}
	}
	existingPTByMid := localVideoPayloadByMid(pc)

	videoIndex := 0
	for _, transceiver := range pc.GetTransceivers() {
		if transceiver.Kind() != webrtc.RTPCodecTypeVideo {
			continue
		}
		mid := transceiver.Mid()
		codec := fallbackCodec
		matchedSection := false
		if mid != "" {
			if sec, ok := sectionByMid[mid]; ok {
				if sec.rejected {
					videoIndex++
					continue
				}
				if c, ok := sec.h264CodecForMode(mode, existingPTByMid[mid]); ok {
					codec = c
					matchedSection = true
				}
			}
		}
		if !matchedSection && videoIndex < len(activeSections) {
			sec := activeSections[videoIndex]
			if c, ok := sec.h264CodecForMode(mode, existingPTByMid[sec.mid]); ok {
				codec = c
			}
		}
		videoIndex++

		if err := transceiver.SetCodecPreferences([]webrtc.RTPCodecParameters{codec}); err != nil {
			return fmt.Errorf("set H264 packetization-mode=%d preference: %w", mode, err)
		}
		if sender := transceiver.Sender(); sender != nil {
			if pinned, ok := sender.Track().(preferredCodecTrack); ok {
				pinned.SetPreferredCodec(codec)
			}
		}
	}
	return nil
}

