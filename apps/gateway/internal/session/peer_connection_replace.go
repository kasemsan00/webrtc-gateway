package session

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
	pkg_webrtc "webrtc-sip-gateway/internal/pkg/webrtc"
)

// createReplacementPeerConnection builds a new PeerConnection with fresh
// ICE/DTLS, keeping SIP RTP sockets. Resume closes the live PC immediately.
// @switch uses keepExisting so the old PC stays up until the new one
// ICE-connects (make-before-break).
func (s *Session) createReplacementPeerConnection(
	turnConfig config.TURNConfig,
	debugTURN bool,
	videoDiag renegotiateVideoOfferDiagnostics,
	keepExisting bool,
) (*webrtc.PeerConnection, error) {
	var videoOnTrackObserved atomic.Bool

	// The auxiliary video track lives on the PeerConnection being replaced.
	// Stop writing to it before that PeerConnection can close.
	s.detachAuxVideoTrack()

	// Resume tears the live PeerConnection down. Detach its tracks while
	// holding videoEgressMu so no RTP write is in flight on them when it
	// closes: writing a TrackLocalStaticRTP concurrently with Close can
	// deadlock pion (see video_egress.go). Lock order: videoEgressMu, s.mu.
	//
	// The PeerConnection parked by an unfinished @switch is dropped here too:
	// resume clears it, and a second @switch parks oldPC in its place. It is
	// detached together with its tracks in this same section, so nothing
	// writes to it or can restore it (AbortMakeBeforeBreak) once it closes.
	s.videoEgressMu.Lock()
	s.audioEgressMu.Lock()
	s.mu.Lock()
	oldPC := s.PeerConnection
	oldAudio := s.AudioTrack
	oldVideo := s.VideoTrack
	parkedPC := s.legacyPeerConnection
	dropParked := parkedPC != nil && parkedPC != oldPC && (!keepExisting || oldPC != nil)
	s.pendingRemoteICE = nil
	if !keepExisting {
		s.PeerConnection = nil
		s.AudioTrack = nil
		s.VideoTrack = nil
	}
	if !keepExisting || dropParked {
		s.legacyPeerConnection = nil
		s.legacyAudioTrack = nil
		s.legacyVideoTrack = nil
	}
	s.mu.Unlock()
	s.audioEgressMu.Unlock()
	s.videoEgressMu.Unlock()

	if !keepExisting && oldPC != nil {
		fmt.Printf("[%s] 🔄 Closing old PeerConnection\n", s.ID)
		ClosePeerConnectionAsync(oldPC, s.ID)
	}
	if dropParked {
		fmt.Printf("[%s] 🔄 Closing parked make-before-break PeerConnection\n", s.ID)
		ClosePeerConnectionAsync(parkedPC, s.ID)
	}

	iceServers := pkg_webrtc.BuildICEServers(turnConfig)
	api, err := newGatewayWebRTCAPI(s.WebRTCTWCCEnabled, s.ID, s.WebRTCICE)
	if err != nil {
		return nil, fmt.Errorf("failed to create WebRTC API: %w", err)
	}
	fmt.Printf("[%s] webrtc_api twcc=%v replaced=true\n", s.ID, s.WebRTCTWCCEnabled)
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
	if keepExisting && oldPC != nil {
		s.armMakeBeforeBreakLocked(oldPC, oldAudio, oldVideo)
		fmt.Printf("[%s] switch_renegotiate_make_before_break_armed\n", s.ID)
	}
	s.AudioTrack = audioTrack
	s.VideoTrack = videoTrack
	s.mu.Unlock()

	// Same browser RTCP handling as the first PeerConnection: PLI/FIR use the
	// browser-pli/browser-fir triggers and NACKs are retransmitted from cache.
	go s.drainSenderRTCP(audioSender)
	go s.readVideoSenderRTCP(videoSender)

	if debugTURN {
		newPC.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
			fmt.Printf("[%s] 🧊 ICE Gathering State (renegotiated): %s\n", s.ID, state.String())
		})
	}
	candidateLog := newLocalCandidateLog(s.ID, "replacement")
	newPC.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		candidateLog.Observe(candidate)
		if !debugTURN {
			return
		}
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
			s.commitMakeBeforeBreak(newPC)
			s.StartVideoRecoveryBurst("renegotiated-ice-connected")
			s.RequestSIPVideoIDRReplay("renegotiated-ice-connected")
			s.signalProactivePLIWake()
			go func() {
				fmt.Printf("[%s] 🚀 Renegotiated - Sending FIR + PLI requests for fast video start (with SPS/PPS)\n", id)
				if s.GetState() == StateEnded {
					return
				}
				s.SendFIRToAsterisk()
				s.SendPLIToAsteriskForced("renegotiated-ice")
				s.SendPLItoWebRTC()
			}()
		} else if connectionState == webrtc.ICEConnectionStateDisconnected {
			// Same grace as the first PeerConnection: a resumed call whose
			// network drops again stays reconnecting and can resume again.
			s.mu.Lock()
			entered := false
			if !isTerminalCleanupState(s.State, s.TerminalAction) {
				entered = s.enterICEReconnectGraceLocked(iceReconnectGracePeriod, "disconnected (renegotiated)")
			}
			s.mu.Unlock()
			if entered {
				s.StartVideoRecoveryBurst("ice-reconnecting")
			}
		} else if connectionState == webrtc.ICEConnectionStateFailed {
			if s.AbortMakeBeforeBreak() {
				fmt.Printf("[%s] ❌ Renegotiated connection failed; restored previous PeerConnection\n", id)
				return
			}
			fmt.Printf("[%s] ❌ Renegotiated connection failed\n", id)
			s.mu.Lock()
			var after func()
			if s.State != StateEnded {
				after = s.endForICELocked("ice_failed_renegotiated")
			}
			s.mu.Unlock()
			runICEEndedHook(after)
		}
	})

	newPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if s.routeAuxUplinkTrack(newPC, track, receiver) {
			return
		}
		kind := track.Kind().String()
		if kind == "video" {
			videoOnTrackObserved.Store(true)
		}
		codec := track.Codec()
		fmt.Printf("[%s] 🎬 OnTrack (renegotiated): kind=%s mime=%s pt=%d ssrc=%d clockRate=%d fmtp=%q\n",
			id, kind, codec.MimeType, track.PayloadType(), track.SSRC(), codec.ClockRate, codec.SDPFmtpLine)
		if kind == "video" {
			fmt.Printf("[%s] 🎬 WebRTC video codec details (renegotiated): %s (fmtp: %s)\n", id, codec.MimeType, codec.SDPFmtpLine)
			s.signalProactivePLIWake()
		}
		go s.readReceiverRTCP(receiver, kind)
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
