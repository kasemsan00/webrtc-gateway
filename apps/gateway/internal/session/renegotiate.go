package session

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/config"
	pkg_webrtc "k2-gateway/internal/pkg/webrtc"
)

const RENEGOTIATE_ICE_GATHER_TIMEOUT = 3 * time.Second
const renegotiateVideoOnTrackWatchdog = 3 * time.Second

type renegotiateVideoOfferDiagnostics struct {
	HasVideoMLine     bool
	VideoPort         int
	VideoDirection    string
	ExpectVideoUplink bool
}

func analyzeRenegotiateVideoOffer(sdp string) renegotiateVideoOfferDiagnostics {
	diag := renegotiateVideoOfferDiagnostics{
		HasVideoMLine:     false,
		VideoPort:         -1,
		VideoDirection:    "unspecified",
		ExpectVideoUplink: false,
	}
	if sdp == "" {
		return diag
	}

	inVideoSection := false
	lines := strings.Split(sdp, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "m=") {
			inVideoSection = strings.HasPrefix(line, "m=video ")
			if inVideoSection {
				diag.HasVideoMLine = true
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if port, err := strconv.Atoi(fields[1]); err == nil {
						diag.VideoPort = port
					}
				}
			}
			continue
		}

		if !inVideoSection {
			continue
		}

		switch line {
		case "a=sendrecv":
			diag.VideoDirection = "sendrecv"
		case "a=sendonly":
			diag.VideoDirection = "sendonly"
		case "a=recvonly":
			diag.VideoDirection = "recvonly"
		case "a=inactive":
			diag.VideoDirection = "inactive"
		}
	}

	diag.ExpectVideoUplink = diag.HasVideoMLine && diag.VideoPort != 0 &&
		(diag.VideoDirection == "sendrecv" || diag.VideoDirection == "sendonly")

	return diag
}

func waitForGatheringComplete(gatherComplete <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-gatherComplete:
		return true
	case <-time.After(timeout):
		return false
	}
}

func waitForRenegotiateIceGatheringComplete(pc *webrtc.PeerConnection, timeout time.Duration) bool {
	if pc == nil {
		return false
	}
	gatherDone := webrtc.GatheringCompletePromise(pc)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-gatherDone:
			return true
		case <-ticker.C:
			// Early-exit only when candidate set is reasonably usable for cross-network handover.
			// A single host candidate can be a false-ready signal on Wi-Fi -> Cellular transitions.
			if ld := pc.LocalDescription(); ld != nil && hasUsableResumeCandidatesInSDP(ld.SDP) {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

func hasUsableResumeCandidatesInSDP(sdp string) bool {
	if sdp == "" {
		return false
	}
	candidateCount := strings.Count(sdp, "\na=candidate:")
	if strings.HasPrefix(sdp, "a=candidate:") {
		candidateCount++
	}
	if candidateCount >= 2 {
		return true
	}
	return strings.Contains(sdp, " typ srflx ") || strings.Contains(sdp, " typ relay ")
}

// RenegotiatePeerConnection recreates the PeerConnection with a new SDP offer
// Used for call resumption after network changes (Part 5)
// This preserves the SIP call and RTP connections while creating a fresh WebRTC connection
func (s *Session) RenegotiatePeerConnection(newOfferSDP string, turnConfig config.TURNConfig, debugTURN bool) error {
	// Best-effort: cache H.264 SPS/PPS from the new offer SDP (if present).
	// Do this outside the session lock to avoid holding locks during parsing/base64 decode.
	offerSPS, offerPPS, offerOK := ExtractH264SpropParameterSets(newOfferSDP)

	fmt.Printf("[%s] 🔄 Renegotiating PeerConnection...\n", s.ID)
	videoDiag := analyzeRenegotiateVideoOffer(newOfferSDP)
	fmt.Printf("[%s] 🔍 Renegotiate offer video diagnostics: hasVideoMLine=%v videoPort=%d videoDirection=%s expectVideoUplink=%v\n",
		s.ID,
		videoDiag.HasVideoMLine,
		videoDiag.VideoPort,
		videoDiag.VideoDirection,
		videoDiag.ExpectVideoUplink,
	)
	var videoOnTrackObserved atomic.Bool

	if offerOK {
		s.mu.Lock()
		s.CachedSPS = make([]byte, len(offerSPS))
		s.CachedPPS = make([]byte, len(offerPPS))
		copy(s.CachedSPS, offerSPS)
		copy(s.CachedPPS, offerPPS)
		s.mu.Unlock()
		fmt.Printf("[%s] 💾 Cached SPS/PPS from SDP (renegotiate-offer) (SPS=%d bytes, PPS=%d bytes)\n", s.ID, len(offerSPS), len(offerPPS))
	}

	// 1. Close old PeerConnection (RTP connections are preserved)
	// Capture old PC under lock, close outside lock
	s.mu.Lock()
	oldPC := s.PeerConnection
	s.PeerConnection = nil
	s.mu.Unlock()

	if oldPC != nil {
		fmt.Printf("[%s] 🔄 Closing old PeerConnection\n", s.ID)
		oldPC.Close()
	}

	// 2. Build ICE servers configuration
	iceServers := pkg_webrtc.BuildICEServers(turnConfig)

	// 3. Create custom MediaEngine with RTCPFeedback
	mediaEngine, err := createCustomMediaEngine()
	if err != nil {
		return fmt.Errorf("failed to create media engine: %w", err)
	}

	// Create API with custom MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	// Create new PeerConnection using custom API
	webrtcConfig := webrtc.Configuration{
		ICEServers: iceServers,
	}

	newPC, err := api.NewPeerConnection(webrtcConfig)
	if err != nil {
		return fmt.Errorf("failed to create new PeerConnection: %w", err)
	}

	// 4. Create audio track (Opus for passthrough)
	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		fmt.Sprintf("pion-audio-%s-renegotiate", s.ID),
	)
	if err != nil {
		newPC.Close()
		return fmt.Errorf("failed to create audio track: %w", err)
	}

	// Add audio track to peer connection
	audioSender, err := newPC.AddTrack(audioTrack)
	if err != nil {
		newPC.Close()
		return fmt.Errorf("failed to add audio track: %w", err)
	}

	// 5. Create video track (H.264 for SIP compatibility)
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		fmt.Sprintf("pion-video-%s-renegotiate", s.ID),
	)
	if err != nil {
		newPC.Close()
		return fmt.Errorf("failed to create video track: %w", err)
	}

	// Add video track to peer connection
	videoSender, err := newPC.AddTrack(videoTrack)
	if err != nil {
		newPC.Close()
		return fmt.Errorf("failed to add video track: %w", err)
	}

	// Update session tracks under lock
	s.mu.Lock()
	s.AudioTrack = audioTrack
	s.VideoTrack = videoTrack
	s.mu.Unlock()

	// 6. Set up RTCP readers (similar to CreateSession)
	go func() {
		rtcpBuf := make([]byte, s.RTPBufferSize)
		for {
			if _, _, err := audioSender.Read(rtcpBuf); err != nil {
				return
			}
		}
	}()

	go func() {
		rtcpBuf := make([]byte, s.RTPBufferSize)
		for {
			if s.GetState() == StateEnded {
				return
			}
			n, _, rtcpErr := videoSender.Read(rtcpBuf)
			if rtcpErr != nil {
				return
			}
			packets, err := rtcp.Unmarshal(rtcpBuf[:n])
			if err != nil {
				continue
			}
			for _, p := range packets {
				switch pkt := p.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					s.SendBrowserRecoveryToAsterisk("browser-rtcp")
				case *rtcp.TransportLayerNack:
					_ = pkt
				}
			}
		}
	}()

	// 7. Set up TURN/ICE debug logging if enabled
	if debugTURN {
		// Log ICE gathering state changes
		newPC.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
			fmt.Printf("[%s] 🧊 ICE Gathering State (renegotiated): %s\n", s.ID, state.String())
		})

		// Log all ICE candidates as they are discovered
		newPC.OnICECandidate(func(candidate *webrtc.ICECandidate) {
			if candidate == nil {
				// nil candidate indicates gathering is complete
				fmt.Printf("[%s] 🧊 ICE Candidate gathering complete (renegotiated)\n", s.ID)
				return
			}
			candidateType := "unknown"
			switch candidate.Typ {
			case webrtc.ICECandidateTypeHost:
				candidateType = "host"
			case webrtc.ICECandidateTypeSrflx:
				candidateType = "srflx"
			case webrtc.ICECandidateTypePrflx:
				candidateType = "prflx"
			case webrtc.ICECandidateTypeRelay:
				candidateType = "relay"
			}
			fmt.Printf("[%s] 🧊 ICE Candidate (renegotiated): type=%s address=%s:%d protocol=%s\n", s.ID, candidateType, candidate.Address, candidate.Port, candidate.Protocol.String())
		})
	}

	// 7. Set up ICE connection state handler
	id := s.ID
	newPC.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("[%s] 🧊 ICE Connection State (renegotiated): %s\n", id, connectionState.String())

		if connectionState == webrtc.ICEConnectionStateConnected {
			// Log selected candidate pair if TURN debug is enabled
			if debugTURN {
				// Use GetStats to find the selected candidate pair (nominated and succeeded)
				stats := newPC.GetStats()
				var selectedPair *webrtc.ICECandidatePairStats
				var localCandidate *webrtc.ICECandidateStats
				var remoteCandidate *webrtc.ICECandidateStats

				// Find the nominated candidate pair (selected pair)
				for _, stat := range stats {
					if pairStats, ok := stat.(webrtc.ICECandidatePairStats); ok {
						if pairStats.Nominated {
							selectedPair = &pairStats
							break
						}
					}
				}

				if selectedPair != nil {
					// Look up local and remote candidate stats
					for _, stat := range stats {
						if candidateStats, ok := stat.(webrtc.ICECandidateStats); ok {
							if candidateStats.ID == selectedPair.LocalCandidateID {
								localCandidate = &candidateStats
							}
							if candidateStats.ID == selectedPair.RemoteCandidateID {
								remoteCandidate = &candidateStats
							}
							if localCandidate != nil && remoteCandidate != nil {
								break
							}
						}
					}

					if localCandidate != nil && remoteCandidate != nil {
						localType := localCandidate.CandidateType.String()
						remoteType := remoteCandidate.CandidateType.String()
						isUsingTURN := localCandidate.CandidateType == webrtc.ICECandidateTypeRelay || remoteCandidate.CandidateType == webrtc.ICECandidateTypeRelay
						turnIndicator := ""
						if isUsingTURN {
							turnIndicator = " ✅ TURN RELAY ACTIVE"
						}
						fmt.Printf("[%s] 🧊 Selected Candidate Pair (renegotiated):%s\n", id, turnIndicator)
						fmt.Printf("[%s]   Local:  type=%s address=%s:%d protocol=%s\n", id, localType, localCandidate.IP, localCandidate.Port, localCandidate.Protocol)
						fmt.Printf("[%s]   Remote: type=%s address=%s:%d protocol=%s\n", id, remoteType, remoteCandidate.IP, remoteCandidate.Port, remoteCandidate.Protocol)
					}
				}
			}

			fmt.Printf("[%s] ✅ Renegotiated connection established\n", id)
			s.SetState(StateActive)
			s.StartVideoRecoveryBurst("renegotiated-ice-connected")
			// Send FIR first (to request SPS/PPS + IDR), then PLI burst for fast video start
			go func() {
				fmt.Printf("[%s] 🚀 Renegotiated - Sending FIR + PLI requests for fast video start (with SPS/PPS)\n", id)
				// Send FIR first to request full keyframe with parameter sets
				s.SendFIRToAsterisk()
				// Then send PLI burst immediately
				for i := 0; i < 3; i++ {
					s.SendPLIToAsteriskForced("renegotiated-ice")
					s.SendPLItoWebRTC() // PLI to browser
				}
			}()
		} else if connectionState == webrtc.ICEConnectionStateFailed {
			fmt.Printf("[%s] ❌ Renegotiated connection failed\n", id)
			s.SetState(StateEnded)
		}
	})

	// 8. Set up OnTrack handler to forward WebRTC RTP → Asterisk
	newPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		kind := track.Kind().String()
		if kind == "video" {
			videoOnTrackObserved.Store(true)
		}
		codec := track.Codec()
		fmt.Printf("[%s] 🎬 OnTrack (renegotiated): kind=%s mime=%s pt=%d ssrc=%d clockRate=%d fmtp=%q\n",
			id, kind, codec.MimeType, track.PayloadType(), track.SSRC(), codec.ClockRate, codec.SDPFmtpLine)

		// Extra logging for video codec (profile-level-id critical for H264 decode compatibility)
		if kind == "video" {
			fmt.Printf("[%s] 🎬 WebRTC video codec details (renegotiated): %s (fmtp: %s)\n", id, codec.MimeType, codec.SDPFmtpLine)
		}

		// Forward RTP packets to Asterisk (reuse existing forwarding logic)
		go s.forwardRTPToAsterisk(track, kind)
	})

	if videoDiag.ExpectVideoUplink {
		go func(sessionID string, diag renegotiateVideoOfferDiagnostics) {
			time.Sleep(renegotiateVideoOnTrackWatchdog)
			if videoOnTrackObserved.Load() || s.GetState() == StateEnded {
				return
			}
			fmt.Printf("[%s] ⚠️ Expected client video uplink but no video OnTrack within %s (hasVideoMLine=%v, videoPort=%d, videoDirection=%s)\n",
				sessionID,
				renegotiateVideoOnTrackWatchdog,
				diag.HasVideoMLine,
				diag.VideoPort,
				diag.VideoDirection,
			)
		}(s.ID, videoDiag)
	}

	// 9. Set remote description (the new offer from client)
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  newOfferSDP,
	}
	if err := newPC.SetRemoteDescription(offer); err != nil {
		newPC.Close()
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	// 10. Create answer
	answer, err := newPC.CreateAnswer(nil)
	if err != nil {
		newPC.Close()
		return fmt.Errorf("failed to create answer: %w", err)
	}

	// 11. Set local description
	if err := newPC.SetLocalDescription(answer); err != nil {
		newPC.Close()
		return fmt.Errorf("failed to set local description: %w", err)
	}

	// 11.5. Wait for ICE gathering to complete (CRITICAL for renegotiation!)
	// If gathering takes too long, continue with currently available candidates.
	// This avoids blocking resume for slow network transitions.
	if waitForRenegotiateIceGatheringComplete(newPC, RENEGOTIATE_ICE_GATHER_TIMEOUT) {
		fmt.Printf("[%s] 🧊 ICE gathering complete for renegotiated connection\n", s.ID)
	} else {
		fmt.Printf("[%s] ⚠️ ICE gathering timeout after %s during renegotiation - proceeding with partial candidates\n", s.ID, RENEGOTIATE_ICE_GATHER_TIMEOUT)
	}

	// 12. Store new PeerConnection under lock
	s.mu.Lock()
	s.PeerConnection = newPC
	s.UpdatedAt = time.Now()
	s.mu.Unlock()

	fmt.Printf("[%s] ✅ PeerConnection renegotiated successfully\n", s.ID)
	return nil
}
