package sip

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"k2-gateway/internal/session"
)

// createSDPOffer creates an SDP offer for outbound calls
// Plain RTP version (no SRTP/crypto) since Asterisk doesn't use encryption
// WebRTC side will still use SRTP (handled by pion/webrtc automatically)
func (s *Server) createSDPOffer(rtpPort int, sess *session.Session) []byte {
	sessionID := time.Now().UnixNano() / 1000000 // Use milliseconds like Linphone
	videoPort := rtpPort + 2                     // Video on next even port

	// Determine Opus payload type to use
	// For incoming calls: use the PT negotiated from INVITE SDP (e.g., 107)
	// For outbound calls: default to 111 (will be set when parsing 200 OK answer)
	opusPT := sess.SIPOpusPT
	if opusPT == 0 {
		opusPT = 111 // Default for outbound calls
	}

	// Plain RTP SDP without crypto attributes (like Linphone Desktop)
	// Asterisk will send plain RTP, k2-gateway will forward to WebRTC as SRTP
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
		// Phase 4: Match Linphone Mobile RTCP feedback format exactly
		// Linphone Mobile uses wildcard: a=rtcp-fb:* ccm fir
		// This is simpler and more compatible than per-codec feedback
		rtcpFbLines = "a=rtcp-fb:* ccm fir\n"
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
	defaultProfileLevelID := "42801F"
	videoProfileLevelID := defaultProfileLevelID
	videoFmtp := fmt.Sprintf("a=fmtp:96 profile-level-id=%s;packetization-mode=1", videoProfileLevelID)
	if sps, pps, ok := sess.GetCachedSPSPPS(); ok {
		// Derive profile-level-id from SPS if possible.
		// SPS layout: [NAL header][profile_idc][constraints][level_idc]...
		derived := ""
		if len(sps) >= 4 {
			derived = fmt.Sprintf("%02X%02X%02X", sps[1], sps[2], sps[3])
			if derived != "" {
				videoProfileLevelID = derived
			}
		}

		// Base64 encode SPS and PPS for SDP
		b64sps := base64.StdEncoding.EncodeToString(sps)
		b64pps := base64.StdEncoding.EncodeToString(pps)
		videoFmtp = fmt.Sprintf("a=fmtp:96 profile-level-id=%s;packetization-mode=1;sprop-parameter-sets=%s,%s", videoProfileLevelID, b64sps, b64pps)
		fmt.Printf("[%s] 🎬 Including sprop-parameter-sets in SDP (SPS: %d bytes, PPS: %d bytes)\n", sess.ID, len(sps), len(pps))
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
m=video %d %s 96
a=rtpmap:96 H264/90000
%s
a=rtcp-mux
%sa=sendrecv
`, sdpUsername, sessionID, sessionID, s.publicAddress,
		s.publicAddress,
		rtpPort, audioProfile,
		opusPT, opusPT, opusPT,
		videoPort, videoProfile, videoFmtp, rtcpFbLines)

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
	fmt.Printf("=== SDP Offer to Asterisk (Plain RTP, no SRTP, Opus PT=%d, Profile=%s) ===\n%s\n=============================\n", opusPT, profileNote, sdp)
	if s.config.VideoUseAVPF {
		fmt.Printf("📋 AVPF SDP Details: Profile=%s, RTCP Feedback: rtcp-fb:* ccm fir (matching Linphone Mobile)\n", videoProfile)
	}
	return []byte(sdp)
}

// parseAsteriskSDPAndSetEndpoints parses Asterisk's SDP answer to extract RTP ports
// and configures the session for forwarding WebRTC → Asterisk
func (s *Server) parseAsteriskSDPAndSetEndpoints(sdpBody []byte, sess *session.Session) {
	sdpStr := string(sdpBody)
	lines := strings.Split(sdpStr, "\n")

	// Store full SDP for debugging if video is rejected
	fullSDP := sdpStr

	var asteriskIP string
	var audioPort, videoPort int
	var videoProfile string
	var hasRtcpFb bool

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Get connection IP from c= line
		if strings.HasPrefix(line, "c=IN IP4 ") {
			asteriskIP = strings.TrimPrefix(line, "c=IN IP4 ")
		}

		// Get audio port from m=audio line
		if strings.HasPrefix(line, "m=audio ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				fmt.Sscanf(parts[1], "%d", &audioPort)
			}
		}

		// Get video port and profile from m=video line
		if strings.HasPrefix(line, "m=video ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				fmt.Sscanf(parts[1], "%d", &videoPort)
				// Extract profile (RTP/AVP or RTP/AVPF)
				if len(parts) >= 3 {
					videoProfile = parts[2]
				}
			}
		}

		// Check for RTCP feedback attributes for payload 96
		if strings.HasPrefix(line, "a=rtcp-fb:96 ") {
			hasRtcpFb = true
		}
	}

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
						fmt.Printf("[%s] ⚠️ AVPF profile accepted but no RTCP feedback attributes found in answer (payload 96)\n",
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
