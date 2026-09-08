package session

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
	pkg_webrtc "webrtc-sip-gateway/internal/pkg/webrtc"
)

// createReplacementPeerConnection closes the live WebRTC PeerConnection and
// builds a new one with fresh ICE/DTLS, keeping SIP RTP sockets. Used by
// resume (gateway answers a client offer) and @switch (gateway offers so
// Android can answer on a new PC as well).
func (s *Session) createReplacementPeerConnection(
	turnConfig config.TURNConfig,
	debugTURN bool,
	videoDiag renegotiateVideoOfferDiagnostics,
) (*webrtc.PeerConnection, error) {
	var videoOnTrackObserved atomic.Bool

	s.mu.Lock()
	oldPC := s.PeerConnection
	s.PeerConnection = nil
	s.pendingRemoteICE = nil
	s.mu.Unlock()

	if oldPC != nil {
		fmt.Printf("[%s] 🔄 Closing old PeerConnection\n", s.ID)
		oldPC.Close()
	}

	iceServers := pkg_webrtc.BuildICEServers(turnConfig)
	mediaEngine, err := createCustomMediaEngine()
	if err != nil {
		return nil, fmt.Errorf("failed to create media engine: %w", err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
	newPC, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: iceServers})
	if err != nil {
		return nil, fmt.Errorf("failed to create new PeerConnection: %w", err)
	}

	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		fmt.Sprintf("pion-audio-%s-renegotiate", s.ID),
	)
	if err != nil {
		newPC.Close()
		return nil, fmt.Errorf("failed to create audio track: %w", err)
	}
	audioSender, err := newPC.AddTrack(audioTrack)
	if err != nil {
		newPC.Close()
		return nil, fmt.Errorf("failed to add audio track: %w", err)
	}

	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
		"video",
		fmt.Sprintf("pion-video-%s-renegotiate", s.ID),
	)
	if err != nil {
		newPC.Close()
		return nil, fmt.Errorf("failed to create video track: %w", err)
	}
	videoSender, err := newPC.AddTrack(videoTrack)
	if err != nil {
		newPC.Close()
		return nil, fmt.Errorf("failed to add video track: %w", err)
	}

	s.mu.Lock()
	s.AudioTrack = audioTrack
	s.VideoTrack = videoTrack
	s.mu.Unlock()

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

	if debugTURN {
		newPC.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
			fmt.Printf("[%s] 🧊 ICE Gathering State (renegotiated): %s\n", s.ID, state.String())
		})
		newPC.OnICECandidate(func(candidate *webrtc.ICECandidate) {
			if candidate == nil {
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

	id := s.ID
	newPC.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		s.mu.RLock()
		current := s.PeerConnection
		s.mu.RUnlock()
		if !iceCallbackIsForCurrentPeerConnection(current, newPC) {
			fmt.Printf("[%s] 🧊 ICE %s ignored on replaced PeerConnection (renegotiated)\n", id, connectionState.String())
			return
		}

		fmt.Printf("[%s] 🧊 ICE Connection State (renegotiated): %s\n", id, connectionState.String())

		if connectionState == webrtc.ICEConnectionStateConnected {
			if debugTURN {
				stats := newPC.GetStats()
				var selectedPair *webrtc.ICECandidatePairStats
				var localCandidate *webrtc.ICECandidateStats
				var remoteCandidate *webrtc.ICECandidateStats
				for _, stat := range stats {
					if pairStats, ok := stat.(webrtc.ICECandidatePairStats); ok {
						if pairStats.Nominated {
							selectedPair = &pairStats
							break
						}
					}
				}
				if selectedPair != nil {
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
			s.RequestSIPVideoIDRReplay("renegotiated-ice-connected")
			go func() {
				fmt.Printf("[%s] 🚀 Renegotiated - Sending FIR + PLI requests for fast video start (with SPS/PPS)\n", id)
				if s.GetState() == StateEnded {
					return
				}
				s.SendFIRToAsterisk()
				s.SendPLIToAsteriskForced("renegotiated-ice")
				s.SendPLItoWebRTC()
			}()
		} else if connectionState == webrtc.ICEConnectionStateFailed {
			fmt.Printf("[%s] ❌ Renegotiated connection failed\n", id)
			s.SetState(StateEnded)
		}
	})

	newPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		kind := track.Kind().String()
		if kind == "video" {
			videoOnTrackObserved.Store(true)
		}
		codec := track.Codec()
		fmt.Printf("[%s] 🎬 OnTrack (renegotiated): kind=%s mime=%s pt=%d ssrc=%d clockRate=%d fmtp=%q\n",
			id, kind, codec.MimeType, track.PayloadType(), track.SSRC(), codec.ClockRate, codec.SDPFmtpLine)
		if kind == "video" {
			fmt.Printf("[%s] 🎬 WebRTC video codec details (renegotiated): %s (fmtp: %s)\n", id, codec.MimeType, codec.SDPFmtpLine)
		}
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

	return newPC, nil
}
