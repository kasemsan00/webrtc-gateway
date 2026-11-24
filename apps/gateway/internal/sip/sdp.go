package sip

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"webrtc-sip-gateway/internal/session"
)

// sipVideoRTCPFeedbackSDP is the AVPF rtcp-fb block advertised on SIP video.
// Wildcard PT matches Linphone. FIR was already offered; NACK/PLI are additive
// so PJSIP/Linphone will honor Picture Loss Indications. chan_sip ignores
// unknown a=rtcp-fb lines and typically answers RTP/AVP, which strips this
// block from our 200 OK — extra lines do not change RTP/RTCP packets we send.
const sipVideoRTCPFeedbackSDP = "a=rtcp-fb:* ccm fir\na=rtcp-fb:* nack\na=rtcp-fb:* nack pli\n"

func isSDPRTCPFeedbackLine(line string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "a=rtcp-fb:")
}

func stripSDPRTCPFeedbackLines(sdp string) string {
	lines := strings.Split(sdp, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if isSDPRTCPFeedbackLine(line) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// primarySDPSectionTracker classifies SDP lines by section while walking a
// body top to bottom. Only the first m=audio and the first m=video are the
// call's primary streams; any later m-line (a second video, an unknown media
// type) is reported as "other" so it cannot overwrite what was read from the
// primary ones.
type primarySDPSectionTracker struct {
	audioSeen bool
	videoSeen bool
	section   string
}

// observe returns the section the line belongs to: "" for session level,
// "audio" or "video" for the primary sections, "other" for everything else.
// isMLine reports whether the line itself opens a section.
func (t *primarySDPSectionTracker) observe(line string) (section string, isMLine bool) {
	if !strings.HasPrefix(line, "m=") {
		return t.section, false
	}
	kind := ""
	if fields := strings.Fields(line); len(fields) > 0 {
		kind = strings.TrimPrefix(strings.ToLower(fields[0]), "m=")
	}
	switch {
	case kind == "audio" && !t.audioSeen:
		t.audioSeen = true
		t.section = "audio"
	case kind == "video" && !t.videoSeen:
		t.videoSeen = true
		t.section = "video"
	default:
		t.section = "other"
	}
	return t.section, true
}

func sdpVideoHasRTCPFeedback(sdp string) bool {
	var tracker primarySDPSectionTracker
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(raw)
		section, isMLine := tracker.observe(line)
		if isMLine {
			continue
		}
		if section == "video" && isSDPRTCPFeedbackLine(line) {
			return true
		}
	}
	return false
}

func replaceSDPMediaProfile(sdp, media string, port int, profile string) string {
	avpf := fmt.Sprintf("m=%s %d RTP/AVPF ", media, port)
	avp := fmt.Sprintf("m=%s %d RTP/AVP ", media, port)
	next := fmt.Sprintf("m=%s %d %s ", media, port, profile)
	if strings.Contains(sdp, avpf) {
		return strings.Replace(sdp, avpf, next, 1)
	}
	return strings.Replace(sdp, avp, next, 1)
}

func rewriteSDPMediaSection(sdp, media string, fn func([]string) []string) string {
	lines := strings.Split(sdp, "\n")
	prefix := "m=" + media + " "
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			start = i
			break
		}
	}
	if start < 0 {
		return sdp
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "m=") {
			end = i
			break
		}
	}
	rewritten := fn(append([]string{}, lines[start:end]...))
	out := make([]string, 0, len(lines)-(end-start)+len(rewritten))
	out = append(out, lines[:start]...)
	out = append(out, rewritten...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

func stripRTCPMuxLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "a=rtcp-mux" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func insertAfterMLine(lines []string, attr string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[0], attr)
	out = append(out, lines[1:]...)
	return out
}

// createSDPOffer creates an SDP offer for outbound calls
// Plain RTP version (no SRTP/crypto) since Asterisk doesn't use encryption
// WebRTC side will still use SRTP (handled by pion/webrtc automatically)
func (s *Server) createSDPOffer(rtpPort int, sess *session.Session) []byte {
	sdpSessionID, sdpSessionVersion := sess.NextSDPOriginVersion(time.Now().UnixMilli())
	videoPort := rtpPort + 2 // Video on next even port

	// Determine Opus payload type to use
	// For incoming calls: use the PT negotiated from INVITE SDP (e.g., 107)
	// For outbound calls: default to 111 (will be set when parsing 200 OK answer)
	opusPT := sess.GetSIPOpusPT()
	if opusPT == 0 {
		opusPT = 111 // Default for outbound calls
	}
	videoPT := sess.GetSIPVideoPayloadType()

	// Plain RTP SDP without crypto attributes (like Linphone Desktop)
	// Asterisk will send plain RTP, webrtc-sip-gateway will forward to WebRTC as SRTP
	// Audio: Opus only (passthrough, no transcoding)
	//
	// NOTE: Some Asterisk/chan_sip setups accept RTP/AVPF but do not behave correctly with RTCP feedback/mux.
	// Allow forcing AVP (disabling AVPF) via env to quickly test interoperability.
	forceAVP := strings.ToLower(os.Getenv("SIP_FORCE_AVP"))

	// Determine audio profile (AVP or AVPF)
	audioProfile := "RTP/AVP"
	if s.config.AudioUseAVPF {
		audioProfile = "RTP/AVPF"
	}

	// Determine video profile (AVP or AVPF)
	videoProfile := "RTP/AVP"
	rtcpFbLines := ""
	if s.config.VideoUseAVPF {
		videoProfile = "RTP/AVPF"
		rtcpFbLines = sipVideoRTCPFeedbackSDP
	}

	if forceAVP == "true" || forceAVP == "1" || forceAVP == "yes" {
		audioProfile = "RTP/AVP"
		videoProfile = "RTP/AVP"
		rtcpFbLines = ""
		fmt.Printf("[%s] ⚠️ Forcing RTP/AVP to SIP side (SIP_FORCE_AVP=%s)\n", sess.ID, forceAVP)
	}

	// Build H.264 fmtp line.
	//
	// IMPORTANT (Android/WebRTC):
	// - Android/WebRTC frequently sends IDR as FU-A (NAL=28) fragments.
	// - If we don't advertise packetization-mode=1, some SIP endpoints (e.g. Linphone Desktop)
	//   may refuse to decode FU-A even though bandwidth is present → black screen / very late video.
	//
	// Therefore we explicitly advertise packetization-mode=1.
	//
	// IMPORTANT (profile-level-id):
	// - Some endpoints are strict about profile-level-id matching the actual SPS.
	// - Android can send very small SPS/PPS early (often inside STAP-A) that indicate a different level
	//   than our previous hardcoded profile-level-id. Mismatch can lead to black screen.
	includeVideo := sess.SIPOfferIncludeVideo()
	defaultProfileLevelID := "42E01F"
	videoProfileLevelID := defaultProfileLevelID
	videoFmtp := fmt.Sprintf("a=fmtp:%d profile-level-id=%s;packetization-mode=1", videoPT, videoProfileLevelID)
	if includeVideo {
		if sps, pps, ok := sess.GetCachedSPSPPS(); ok {
			// Derive profile-level-id from SPS if possible.
			// SPS layout: [NAL header][profile_idc][constraints][level_idc]...
			derived := ""
			if len(sps) >= 4 {
				derived = fmt.Sprintf("%02X%02X%02X", sps[1], sps[2], sps[3])
				if strings.HasPrefix(strings.ToUpper(derived), "42E0") {
					videoProfileLevelID = derived
				} else if derived != "" {
					fmt.Printf("[%s] ⚠ Ignoring derived non-baseline profile-level-id=%s, keeping %s\n", sess.ID, derived, defaultProfileLevelID)
				}
			}

			// Base64 encode SPS and PPS for SDP
			b64sps := base64.StdEncoding.EncodeToString(sps)
			b64pps := base64.StdEncoding.EncodeToString(pps)
			videoFmtp = fmt.Sprintf("a=fmtp:%d profile-level-id=%s;packetization-mode=1;sprop-parameter-sets=%s,%s", videoPT, videoProfileLevelID, b64sps, b64pps)
			fmt.Printf("[%s] 🎬 Including sprop-parameter-sets in SDP (SPS: %d bytes, PPS: %d bytes)\n", sess.ID, len(sps), len(pps))
		}
	}

	// Get username for SDP origin field (o=) from session auth context
	// In public mode: uses per-session username (e.g., "00025")
	// In trunk/legacy mode: falls back to server's static username
	_, _, _, _, sdpUsername, _, _ := sess.GetSIPAuthContext()
	if sdpUsername == "" {
		sdpUsername = s.getActiveUsername()
	}
	if sdpUsername == "" {
		sdpUsername = "-" // RFC 4566 fallback when username is unavailable
	}

	sdp := fmt.Sprintf(`v=0
o=%s %d %d IN IP4 %s
s=Talk
c=IN IP4 %s
t=0 0
m=audio %d %s %d 101
a=rtpmap:%d opus/48000/2
a=fmtp:%d minptime=10;useinbandfec=1
a=rtpmap:101 telephone-event/8000
a=fmtp:101 0-16
a=ptime:20
a=rtcp-mux
a=sendrecv
`, sdpUsername, sdpSessionID, sdpSessionVersion, s.publicAddress,
		s.publicAddress,
		rtpPort, audioProfile,
		opusPT, opusPT, opusPT)

	if includeVideo {
		sdp += fmt.Sprintf(`m=video %d %s %d
a=rtpmap:%d H264/90000
%s
a=rtcp-mux
%sa=sendrecv
`, videoPort, videoProfile, videoPT, videoPT, videoFmtp, rtcpFbLines)
	}

	profileNote := "AVP"
	if s.config.AudioUseAVPF || s.config.VideoUseAVPF {
		var profiles []string
		if s.config.AudioUseAVPF {
			profiles = append(profiles, "Audio=AVPF")
		}
		if s.config.VideoUseAVPF {
			profiles = append(profiles, "Video=AVPF")
		}
		profileNote = fmt.Sprintf("AVPF (%s)", strings.Join(profiles, ", "))
	}
	if s.config.DebugSIPInvite {
		fmt.Printf("=== SDP Offer to Asterisk (debug; Plain RTP, no SRTP, Opus PT=%d, Profile=%s) ===\n%s\n=============================\n", opusPT, profileNote, sdp)
	} else {
		fmt.Printf("SDP offer prepared: opusPT=%d profile=%s video=%v bytes=%d\n", opusPT, profileNote, includeVideo, len(sdp))
	}
	if includeVideo && s.config.VideoUseAVPF {
		fmt.Printf("📋 AVPF SDP Details: Profile=%s, RTCP Feedback: ccm fir / nack / nack pli\n", videoProfile)
	}
	return []byte(sdp)
}

// createSDPAnswerForInvite creates SDP answer for inbound INVITE by mirroring offer constraints.
// This prevents strict SIP peers from hanging up when 200 OK advertises unsupported profile/mux settings.
func (s *Server) createSDPAnswerForInvite(rtpPort int, sess *session.Session, inviteSDP []byte) []byte {
	audioProfile := "RTP/AVP"
	videoProfile := "RTP/AVP"
	audioRtcpMux := false
	videoRtcpMux := false
	videoPacketizationMode := sipVideoPacketizationMode(inviteSDP) == 1
	sess.SetSIPOfferIncludeVideo(session.AnalyzeOfferVideo(string(inviteSDP)).HasVideoMLine)
	if videoPT, ok := parseH264PayloadType(inviteSDP); ok {
		sess.SetSIPVideoPayloadType(videoPT)
	} else {
		sess.SetSIPVideoPayloadType(0)
	}

	if len(inviteSDP) > 0 {
		// Profile and rtcp-mux come from the primary audio and video m-lines
		// only. A second m=video is a separate stream with its own settings.
		var tracker primarySDPSectionTracker
		lines := strings.Split(string(inviteSDP), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			currentMedia, isMLine := tracker.observe(line)
			if isMLine {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					switch currentMedia {
					case "audio":
						audioProfile = parts[2]
					case "video":
						videoProfile = parts[2]
					}
				}
				continue
			}

			if line == "a=rtcp-mux" {
				if currentMedia == "audio" {
					audioRtcpMux = true
				}
				if currentMedia == "video" {
					videoRtcpMux = true
				}
				continue
			}

		}
	}

	// RFC 6184 defaults an omitted packetization-mode to 0. An answer must not
	// introduce mode 1 when the offer did not advertise it. The WebRTC→SIP
	// forwarding path converts FU-A to Single NAL Unit packets for mode-0 peers.
	answerPacketizationMode := uint8(1)
	if !videoPacketizationMode {
		answerPacketizationMode = 0
	}
	sess.SetSIPVideoPacketizationMode(answerPacketizationMode)

	negotiated := session.SIPNegotiatedMedia{
		AudioProfile:           audioProfile,
		VideoProfile:           videoProfile,
		AudioRTCPMux:           audioRtcpMux,
		VideoRTCPMux:           videoRtcpMux,
		VideoPacketizationMode: answerPacketizationMode,
	}
	sess.SetSIPNegotiatedMedia(negotiated)

	base := s.createSDPOffer(rtpPort, sess)
	sdp := constrainSDPToNegotiatedDialog(string(base), rtpPort, negotiated)

	fmt.Printf("[%s] 📋 Inbound SDP answer constrained to offer (audio=%s video=%s audio_mux=%v video_mux=%v offer_pmode1=%v answer_pmode1=%v)\n",
		sess.ID,
		audioProfile,
		videoProfile,
		audioRtcpMux,
		videoRtcpMux,
		videoPacketizationMode,
		answerPacketizationMode == 1,
	)

	return []byte(sdp)
}

func constrainSDPToNegotiatedDialog(sdp string, rtpPort int, m session.SIPNegotiatedMedia) string {
	if m.AudioProfile != "" {
		sdp = replaceSDPMediaProfile(sdp, "audio", rtpPort, m.AudioProfile)
	}
	videoPort := rtpPort + 2
	if m.VideoProfile != "" {
		sdp = replaceSDPMediaProfile(sdp, "video", videoPort, m.VideoProfile)
	}
	if !m.AudioRTCPMux {
		sdp = rewriteSDPMediaSection(sdp, "audio", stripRTCPMuxLines)
	}
	if !m.VideoRTCPMux {
		rtcpPort := videoPort + 1
		sdp = rewriteSDPMediaSection(sdp, "video", func(lines []string) []string {
			return insertAfterMLine(stripRTCPMuxLines(lines), fmt.Sprintf("a=rtcp:%d", rtcpPort))
		})
	}
	if m.VideoPacketizationMode == 0 {
		sdp = strings.ReplaceAll(sdp, ";packetization-mode=1", ";packetization-mode=0")
	}
	if m.VideoProfile == "RTP/AVPF" {
		if !sdpVideoHasRTCPFeedback(sdp) {
			sdp = rewriteSDPMediaSection(sdp, "video", appendVideoRTCPFeedbackLines)
		}
	} else if m.VideoProfile != "" {
		sdp = stripSDPRTCPFeedbackLines(sdp)
	}
	return sdp
}

func appendVideoRTCPFeedbackLines(lines []string) []string {
	fbLines := strings.Split(strings.TrimSuffix(sipVideoRTCPFeedbackSDP, "\n"), "\n")
	out := make([]string, 0, len(lines)+len(fbLines))
	inserted := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inserted && (trimmed == "a=sendrecv" || trimmed == "a=sendonly" || trimmed == "a=recvonly" || trimmed == "a=inactive") {
			out = append(out, fbLines...)
			inserted = true
		}
		out = append(out, line)
	}
	if !inserted {
		out = append(out, fbLines...)
	}
	return out
}

func parseSDPOrigin(sdp []byte) (sessID, ver int64, ok bool) {
	for _, raw := range strings.Split(string(sdp), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if !strings.HasPrefix(line, "o=") {
			continue
		}
		fields := strings.Fields(line[2:])
		if len(fields) < 6 {
			return 0, 0, false
		}
		id, err1 := strconv.ParseInt(fields[1], 10, 64)
		v, err2 := strconv.ParseInt(fields[2], 10, 64)
		if err1 != nil || err2 != nil {
			return 0, 0, false
		}
		return id, v, true
	}
	return 0, 0, false
}

func rewriteSDPOrigin(sdp []byte, sessID, ver int64) []byte {
	lines := strings.Split(string(sdp), "\n")
	for i, raw := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if !strings.HasPrefix(trimmed, "o=") {
			continue
		}
		fields := strings.Fields(trimmed[2:])
		if len(fields) < 6 {
			break
		}
		fields[1] = strconv.FormatInt(sessID, 10)
		fields[2] = strconv.FormatInt(ver, 10)
		updated := "o=" + strings.Join(fields, " ")
		if strings.HasSuffix(raw, "\r") {
			updated += "\r"
		}
		lines[i] = updated
		return []byte(strings.Join(lines, "\n"))
	}
	return sdp
}

func stripSDPOriginNumbers(sdp []byte) string {
	return string(rewriteSDPOrigin(sdp, 1, 1))
}

func finalizeInboundAnswerSDP(sess *session.Session, candidate []byte) (wire []byte, changed bool) {
	if sess == nil {
		return candidate, false
	}
	prev := sess.LastWireSIPAnswer()
	if len(prev) == 0 {
		sess.RememberLastWireSIPAnswer(candidate)
		return candidate, false
	}
	if stripSDPOriginNumbers(prev) == stripSDPOriginNumbers(candidate) {
		return prev, false
	}
	prevID, prevVer, okPrev := parseSDPOrigin(prev)
	_, candVer, okCand := parseSDPOrigin(candidate)
	if !okPrev || !okCand || candVer <= prevVer {
		id, ver := sess.NextSDPOriginVersion(time.Now().UnixMilli())
		if okPrev && id != prevID {
			id = prevID
		}
		candidate = rewriteSDPOrigin(candidate, id, ver)
	}
	sess.RememberLastWireSIPAnswer(candidate)
	return candidate, true
}

func sipVideoPacketizationMode(sdp []byte) uint8 {
	h264PT, hasH264PT := parseH264PayloadType(sdp)
	var tracker primarySDPSectionTracker
	for _, rawLine := range strings.Split(string(sdp), "\n") {
		line := strings.TrimSpace(rawLine)
		currentMedia, isMLine := tracker.observe(line)
		if isMLine {
			continue
		}
		if currentMedia != "video" || !sdpFmtpHasParameter(line, "packetization-mode", "1") {
			continue
		}
		if hasH264PT {
			if pt, ok := parseFmtpPayloadType(line); !ok || pt != h264PT {
				continue
			}
		}
		return 1
	}
	return 0
}

func parseFmtpPayloadType(line string) (uint8, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 || !strings.HasPrefix(strings.ToLower(fields[0]), "a=fmtp:") {
		return 0, false
	}
	pt, err := strconv.ParseUint(fields[0][len("a=fmtp:"):], 10, 8)
	if err != nil {
		return 0, false
	}
	return uint8(pt), true
}

func sdpFmtpHasParameter(line, key, value string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 || !strings.HasPrefix(strings.ToLower(fields[0]), "a=fmtp:") {
		return false
	}
	for _, parameter := range strings.Split(strings.Join(fields[1:], " "), ";") {
		parts := strings.SplitN(strings.TrimSpace(parameter), "=", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), key) && strings.TrimSpace(parts[1]) == value {
			return true
		}
	}
	return false
}

// parseAsteriskSDPAndSetEndpoints parses Asterisk's SDP answer to extract RTP ports
// and configures the session for forwarding WebRTC → Asterisk
// parseSDPMediaEndpoints returns the IPv4 c= address, the audio and video
// ports, and the video profile of the call's primary streams: the first
// m=audio and the first m=video. The address is the last c= seen at session
// level or inside those two sections. Later m-lines (a second video stream,
// other media types) and their c= lines are ignored, so they cannot move the
// primary RTP destinations.
func parseSDPMediaEndpoints(lines []string) (ip string, audioPort, videoPort int, videoProfile string) {
	var tracker primarySDPSectionTracker
	for _, line := range lines {
		line = strings.TrimSpace(line)

		section, isMLine := tracker.observe(line)
		if section == "other" {
			continue
		}
		if isMLine {
			parts := strings.Fields(line)
			switch section {
			case "audio":
				if len(parts) >= 2 {
					fmt.Sscanf(parts[1], "%d", &audioPort)
				}
			case "video":
				if len(parts) >= 2 {
					fmt.Sscanf(parts[1], "%d", &videoPort)
				}
				if len(parts) >= 3 {
					videoProfile = parts[2]
				}
			}
			continue
		}
		if strings.HasPrefix(line, "c=IN IP4 ") {
			ip = strings.TrimPrefix(line, "c=IN IP4 ")
		}
	}
	return ip, audioPort, videoPort, videoProfile
}

// updateAsteriskEndpointsFromMidCallOffer follows Asterisk when a re-INVITE or
// UPDATE moves its media (new c= address or m= port). Hold offers using
// c=0.0.0.0 or port 0 leave the current endpoints alone.
func (s *Server) updateAsteriskEndpointsFromMidCallOffer(sdpBody []byte, sess *session.Session) {
	if sess == nil || len(sdpBody) == 0 {
		return
	}
	ipStr, audioPort, videoPort, _ := parseSDPMediaEndpoints(strings.Split(string(sdpBody), "\n"))
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if sess.UpdateAsteriskEndpointsFromOffer(ip, audioPort, videoPort) {
		fmt.Printf("[%s] sip_midcall_media_moved ip=%s audio=%d video=%d\n", sess.ID, ip, audioPort, videoPort)
	}
}

func seedSIPVideoSPSPPSFromSDP(sdpBody []byte, sess *session.Session, source string) bool {
	if sess == nil || len(sdpBody) == 0 {
		return false
	}
	sps, pps, ok := session.ExtractH264SpropParameterSets(string(sdpBody))
	if !ok {
		return false
	}
	return sess.SeedSIPCachedSPSPPSFromSDP(sps, pps, source)
}

func (s *Server) parseAsteriskSDPAndSetEndpoints(sdpBody []byte, sess *session.Session) {
	sdpStr := string(sdpBody)
	lines := strings.Split(sdpStr, "\n")

	seedSIPVideoSPSPPSFromSDP(sdpBody, sess, "sip-sdp")

	// Store full SDP for debugging if video is rejected
	fullSDP := sdpStr

	hasRtcpFb := sdpVideoHasRTCPFeedback(sdpStr)
	asteriskIP, audioPort, videoPort, videoProfile := parseSDPMediaEndpoints(lines)

	if asteriskIP != "" && audioPort > 0 {
		audioAddr := &net.UDPAddr{
			IP:   net.ParseIP(asteriskIP),
			Port: audioPort,
		}

		var videoAddr *net.UDPAddr
		if videoPort > 0 {
			videoAddr = &net.UDPAddr{
				IP:   net.ParseIP(asteriskIP),
				Port: videoPort,
			}
		}

		sess.SetAsteriskEndpoints(audioAddr, videoAddr)
		fmt.Printf("[%s] ✅ Asterisk RTP endpoints configured - Audio: %s:%d, Video: %s:%d\n",
			sess.ID, asteriskIP, audioPort, asteriskIP, videoPort)

		// Check if video was rejected (port=0)
		if videoPort == 0 {
			fmt.Printf("[%s] ❌ CRITICAL: Asterisk rejected video (port=0) - Video stream will not work!\n", sess.ID)
			if s.config.VideoUseAVPF {
				fmt.Printf("[%s] ⚠️ AVPF was offered but Asterisk rejected video\n", sess.ID)
				fmt.Printf("[%s] 📋 Full SDP Answer from Asterisk (for debugging):\n%s\n", sess.ID, fullSDP)
				fmt.Printf("[%s] 💡 TROUBLESHOOTING: Check Asterisk configuration:\n", sess.ID)
				fmt.Printf("[%s]    - For res_pjsip: Set 'use_avpf=yes' in endpoint config\n", sess.ID)
				fmt.Printf("[%s]    - For chan_sip: Set 'videosupport=yes' in peer config\n", sess.ID)
				fmt.Printf("[%s]    - Ensure H.264 codec is enabled: 'allow=h264' or 'allow=!all,allow=h264'\n", sess.ID)
				fmt.Printf("[%s]    - Verify Asterisk version supports AVPF (Asterisk 13+ recommended)\n", sess.ID)
				fmt.Printf("[%s]    - Check Asterisk logs: 'asterisk -rvvv' or 'core show settings'\n", sess.ID)
			} else {
				fmt.Printf("[%s] 📋 Full SDP Answer from Asterisk (for debugging):\n%s\n", sess.ID, fullSDP)
				fmt.Printf("[%s] 💡 TROUBLESHOOTING: Check Asterisk configuration:\n", sess.ID)
				fmt.Printf("[%s]    - For res_pjsip: Set 'videosupport=yes' in endpoint config\n", sess.ID)
				fmt.Printf("[%s]    - For chan_sip: Set 'videosupport=yes' in peer config\n", sess.ID)
				fmt.Printf("[%s]    - Ensure H.264 codec is enabled: 'allow=h264' or 'allow=!all,allow=h264'\n", sess.ID)
			}
		}

		// Validate AVPF negotiation if we offered it
		if s.config.VideoUseAVPF {
			if videoPort > 0 {
				// Video was accepted, check AVPF negotiation
				if videoProfile == "RTP/AVPF" {
					if hasRtcpFb {
						fmt.Printf("[%s] ✅ AVPF negotiation successful - Asterisk accepted RTP/AVPF with RTCP feedback\n",
							sess.ID)
					} else {
						fmt.Printf("[%s] ⚠️ AVPF profile accepted but no RTCP feedback attributes found in answer\n",
							sess.ID)
						fmt.Printf("[%s] 💡 Asterisk may not support RTCP feedback - video will work but PLI/FIR/NACK may not function\n",
							sess.ID)
					}
				} else if videoProfile != "" {
					fmt.Printf("[%s] ⚠️ AVPF was offered but Asterisk answered with %s (AVPF not negotiated, using %s)\n",
						sess.ID, videoProfile, videoProfile)
					fmt.Printf("[%s] 💡 Video will work but RTCP feedback (PLI/FIR/NACK) will not be available\n",
						sess.ID)
				}
			}
		}
	} else {
		fmt.Printf("[%s] ⚠️ Failed to parse Asterisk SDP endpoints (IP: %s, Audio: %d, Video: %d)\n",
			sess.ID, asteriskIP, audioPort, videoPort)
	}
}

// parseOpusPayloadType parses SDP to extract Opus payload type from a=rtpmap lines
// Returns the payload type (e.g., 107, 111) or 0 if not found
func parseOpusPayloadType(sdpBody []byte) uint8 {
	sdpStr := string(sdpBody)
	lines := strings.Split(sdpStr, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for: a=rtpmap:<pt> opus/48000/2
		if strings.HasPrefix(line, "a=rtpmap:") && strings.Contains(line, "opus/48000") {
			// Extract payload type from "a=rtpmap:107 opus/48000/2"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ptStr := strings.TrimPrefix(parts[0], "a=rtpmap:")
				if pt, err := strconv.ParseUint(ptStr, 10, 8); err == nil {
					return uint8(pt)
				}
			}
		}
	}
	return 0 // Not found
}

// parseH264PayloadType returns the RTP payload type mapped to H.264 in the
// video media section. SIP answers must preserve this mapping for strict
// chan_sip peers, and WebRTC->SIP RTP must use the same negotiated value.
func parseH264PayloadType(sdpBody []byte) (uint8, bool) {
	var tracker primarySDPSectionTracker
	for _, rawLine := range strings.Split(string(sdpBody), "\n") {
		line := strings.TrimSpace(rawLine)
		currentMedia, isMLine := tracker.observe(line)
		if isMLine {
			continue
		}
		if currentMedia != "video" || !strings.HasPrefix(strings.ToLower(line), "a=rtpmap:") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[1], "H264/90000") {
			continue
		}
		ptText := strings.TrimPrefix(strings.ToLower(fields[0]), "a=rtpmap:")
		pt, err := strconv.ParseUint(ptText, 10, 8)
		if err == nil {
			return uint8(pt), true
		}
	}
	return 0, false
}
