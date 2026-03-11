package session

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"strings" // Used for SPS/PPS parsing in OnTrack
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/config"
	pkg_webrtc "k2-gateway/internal/pkg/webrtc"
)

const (
	maxFallbackDuration     = 4 * time.Second
	symmetricRTPTrustWindow = 10 * time.Second
)

// base62 characters for short ID generation
const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// generateShortID generates a 12-character base62 session ID
func generateShortID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("%012d", time.Now().UnixNano()%1000000000000)
	}
	for i := range b {
		b[i] = base62Chars[int(b[i])%62]
	}
	return string(b)
}

// generateSSRC generates a random 32-bit SSRC
func generateSSRC() uint32 {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if crypto/rand fails
		return uint32(time.Now().UnixNano())
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// Session represents a WebRTC-SIP call session with audio and video support
type Session struct {
	ID             string                      `json:"id"`
	PeerConnection *webrtc.PeerConnection      `json:"-"`
	AudioTrack     *webrtc.TrackLocalStaticRTP `json:"-"`
	VideoTrack     *webrtc.TrackLocalStaticRTP `json:"-"`
	RTPConn        *net.UDPConn                `json:"-"`
	VideoRTPConn   *net.UDPConn                `json:"-"`
	AudioRTCPConn  *net.UDPConn                `json:"-"` // Dedicated RTCP port for audio (RTP+1)
	VideoRTCPConn  *net.UDPConn                `json:"-"` // Dedicated RTCP port for video (RTP+1)
	SIPCallID      string                      `json:"sipCallId,omitempty"`
	State          SessionState                `json:"state"`
	Direction      string                      `json:"direction"` // "inbound" or "outbound"
	From           string                      `json:"from,omitempty"`
	To             string                      `json:"to,omitempty"`
	RTPPort        int                         `json:"rtpPort,omitempty"`
	VideoRTPPort   int                         `json:"videoRtpPort,omitempty"`
	AudioRTCPPort  int                         `json:"audioRtcpPort,omitempty"`
	VideoRTCPPort  int                         `json:"videoRtcpPort,omitempty"`
	CreatedAt      time.Time                   `json:"createdAt"`
	UpdatedAt      time.Time                   `json:"updatedAt"`
	// ICE-lite credentials for SIP side
	ICEUfrag string `json:"-"`
	ICEPwd   string `json:"-"`
	// Asterisk RTP endpoints for forwarding WebRTC → Asterisk
	AsteriskAudioAddr            *net.UDPAddr  `json:"-"`
	AsteriskVideoAddr            *net.UDPAddr  `json:"-"`
	AsteriskVideoRTCPAddr        *net.UDPAddr  `json:"-"`
	VideoRTCPLearnedAt           time.Time     `json:"-"`
	VideoRTCPSource              string        `json:"-"`
	VideoRTCPFallbackUntil       time.Time     `json:"-"`
	SymmetricRTPTrustUntil       time.Time     `json:"-"`
	PLIBurstUntil                time.Time     `json:"-"`
	LastSipPLISent               time.Time     `json:"-"`
	LastSipFIRSent               time.Time     `json:"-"`
	VideoRecoveryBurstEnabled    bool          `json:"-"`
	VideoRecoveryBurstWindow     time.Duration `json:"-"`
	VideoRecoveryBurstInterval   time.Duration `json:"-"`
	VideoRecoveryBurstStale      time.Duration `json:"-"`
	VideoRecoveryBurstFIRStale   time.Duration `json:"-"`
	VideoRecoveryBurstUntil      time.Time     `json:"-"`
	VideoRecoveryBurstStartedAt  time.Time     `json:"-"`
	VideoRecoveryBurstLastReason string        `json:"-"`
	// RTP State for re-packetization
	AudioSeq        uint16 `json:"-"`
	AudioSSRC       uint32 `json:"-"`
	VideoSeq        uint16 `json:"-"`
	VideoSSRC       uint32 `json:"-"`
	RemoteAudioSSRC uint32 `json:"-"`
	RemoteVideoSSRC uint32 `json:"-"`
	// Cached SPS/PPS for video injection (WebRTC→SIP direction, from browser's encoder)
	CachedSPS               []byte    `json:"-"`
	CachedPPS               []byte    `json:"-"`
	LastSPSPPSInjectionTime time.Time `json:"-"` // Track last SPS/PPS injection for periodic re-injection
	// SIP-side cached SPS/PPS (SIP→WebRTC direction, from SIP endpoint's encoder)
	// Used to inject parameter sets before keyframes forwarded to browser for decoder recovery.
	SIPCachedSPS []byte `json:"-"`
	SIPCachedPPS []byte `json:"-"`
	// @switch controlled SPS/PPS injection (inject 3 copies before each of first 3 IDRs after @switch)
	SwitchSPSPPSInjectRemaining int       `json:"-"` // Number of IDR frames left to inject SPS/PPS (0 = disabled, 3 = inject next 3 IDRs)
	SwitchReceivedAt            time.Time `json:"-"` // Timestamp when @switch message was received (for debugging)
	// SIP Dialog state for BYE requests
	SIPFromTag       string   `json:"-"` // Our local tag
	SIPToTag         string   `json:"-"` // Remote tag from 200 OK
	SIPRemoteContact string   `json:"-"` // Contact header from 200 OK
	SIPCSeq          int      `json:"-"` // Last CSeq used
	SIPDomain        string   `json:"-"` // SIP domain (dialog state + auth context)
	SIPPort          int      `json:"-"` // SIP port (dialog state + auth context)
	SIPRouteSet      []string `json:"-"` // Route headers from Record-Route (reversed order)
	// SIP Codec Payload Types (for RTP rewriting between SIP <-> WebRTC)
	SIPOpusPT uint8 `json:"-"` // Opus payload type negotiated with SIP peer (e.g., 107, 111)
	// Incoming call state
	IncomingSIPTx   interface{} `json:"-"` // Store SIP ServerTransaction for delayed response
	IncomingSIPReq  interface{} `json:"-"` // Store original SIP INVITE Request for 200 OK
	IncomingINVITE  []byte      `json:"-"` // Store incoming INVITE body (SDP offer)
	IncomingFromURI string      `json:"-"` // Caller URI
	IncomingToURI   string      `json:"-"` // Callee URI
	// SIP Authentication Context (independent of WS connection)
	SIPAuthMode   string `json:"-"` // "public" | "trunk" | ""
	SIPAccountKey string `json:"-"` // For public mode: "username@domain:port"
	SIPTrunkID    int64  `json:"-"` // For trunk mode: trunk ID from DB
	SIPUsername   string `json:"-"` // SIP username (for public mode)
	SIPPassword   string `json:"-"` // SIP password (for public mode, not logged)
	// Incoming call claim (first-accept-wins)
	IncomingClaimed   bool   `json:"-"` // True if this incoming call has been claimed
	IncomingClaimedBy string `json:"-"` // WS client ID that claimed this call
	// PLI (Picture Loss Indication) tracking
	PLISent       int       `json:"pliSent"`     // Number of PLIs sent to SIP
	PLIResponse   int       `json:"pliResponse"` // Number of keyframes received after PLI
	LastPLISent   time.Time `json:"-"`           // Timestamp of last PLI sent
	LastKeyframe  time.Time `json:"-"`           // Timestamp of last keyframe received
	FIRSeq        uint8     `json:"-"`           // FIR sequence number (0-255, wraps around)
	RTPBufferSize int       `json:"-"`           // RTP/RTCP packet buffer size (from Config.RTP.BufferSize, minimum 1500)
	// Video RTP retransmission cache (for WebRTC NACK handling)
	VideoRTPHistoryPackets [][]byte `json:"-"`
	VideoRTPHistorySeq     []uint16 `json:"-"`
	VideoRTPHistorySize    int      `json:"-"`
	// Video optimization flags
	PreserveSTAPA     bool `json:"-"` // If true, preserve STAP-A packets (don't de-aggregate) when they contain SPS+PPS+IDR
	videoRTPHistoryMu sync.Mutex
	mu                sync.RWMutex
}

// Snapshot provides a thread-safe view of session metadata for logging.
type Snapshot struct {
	ID            string
	State         SessionState
	Direction     string
	From          string
	To            string
	SIPCallID     string
	RTPPort       int
	VideoRTPPort  int
	AudioRTCPPort int
	VideoRTCPPort int
	SIPOpusPT     uint8
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Snapshot returns a thread-safe snapshot of session metadata for logging.
func (s *Session) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Snapshot{
		ID:            s.ID,
		State:         s.State,
		Direction:     s.Direction,
		From:          s.From,
		To:            s.To,
		SIPCallID:     s.SIPCallID,
		RTPPort:       s.RTPPort,
		VideoRTPPort:  s.VideoRTPPort,
		AudioRTCPPort: s.AudioRTCPPort,
		VideoRTCPPort: s.VideoRTCPPort,
		SIPOpusPT:     s.SIPOpusPT,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

// NewSession creates a new session with a WebRTC peer connection
func NewSession(id string, cfg *config.Config, turnConfig config.TURNConfig) (*Session, error) {
	// Build ICE servers configuration
	iceServers := pkg_webrtc.BuildICEServers(turnConfig)

	// Create custom MediaEngine with RTCPFeedback
	mediaEngine, err := createCustomMediaEngine()
	if err != nil {
		return nil, fmt.Errorf("failed to create media engine: %w", err)
	}

	// Create API with custom MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	// Create WebRTC peer connection using custom API
	webrtcConfig := webrtc.Configuration{
		ICEServers: iceServers,
	}

	peerConnection, err := api.NewPeerConnection(webrtcConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	// Create audio track (Opus for passthrough)
	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		fmt.Sprintf("pion-audio-%s", id),
	)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to create audio track: %w", err)
	}

	// Add audio track to peer connection
	audioSender, err := peerConnection.AddTrack(audioTrack)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to add audio track: %w", err)
	}

	// Create video track (H.264 for SIP compatibility)
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		fmt.Sprintf("pion-video-%s", id),
	)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to create video track: %w", err)
	}

	// Add video track to peer connection
	videoSender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to add video track: %w", err)
	}

	// Initialize buffer size from config with minimum clamp (1500 bytes for MTU safety)
	rtpBufferSize := cfg.RTP.BufferSize
	if rtpBufferSize < 1500 {
		rtpBufferSize = 1500
	}

	// Create session with both audio and video tracks
	burstWindow := time.Duration(cfg.SIP.VideoRecoveryBurstWindowMS) * time.Millisecond
	if burstWindow <= 0 {
		burstWindow = 12 * time.Second
	}
	burstInterval := time.Duration(cfg.SIP.VideoRecoveryBurstIntervalMS) * time.Millisecond
	if burstInterval <= 0 {
		burstInterval = 800 * time.Millisecond
	}
	burstStale := time.Duration(cfg.SIP.VideoRecoveryBurstStaleMS) * time.Millisecond
	if burstStale <= 0 {
		burstStale = 1200 * time.Millisecond
	}
	burstFIRStale := time.Duration(cfg.SIP.VideoRecoveryBurstFIRStaleMS) * time.Millisecond
	if burstFIRStale <= 0 {
		burstFIRStale = 2500 * time.Millisecond
	}

	session := &Session{
		ID:                          id,
		PeerConnection:              peerConnection,
		AudioTrack:                  audioTrack,
		VideoTrack:                  videoTrack,
		State:                       StateNew,
		CreatedAt:                   time.Now(),
		UpdatedAt:                   time.Now(),
		RTPBufferSize:               rtpBufferSize,
		SwitchSPSPPSInjectRemaining: 0, // 0 = disabled, will be set to 3 when @switch message is received
		VideoRTCPSource:             "unknown",
		SymmetricRTPTrustUntil:      time.Now().Add(symmetricRTPTrustWindow),
		PreserveSTAPA:               cfg.SIP.VideoPreserveSTAPA, // Phase 2: preserve STAP-A if enabled
		VideoRecoveryBurstEnabled:   cfg.SIP.VideoRecoveryBurstEnabled,
		VideoRecoveryBurstWindow:    burstWindow,
		VideoRecoveryBurstInterval:  burstInterval,
		VideoRecoveryBurstStale:     burstStale,
		VideoRecoveryBurstFIRStale:  burstFIRStale,
	}
	session.initVideoRTPHistory()

	// Start reading RTCP packets from the audio sender
	go func() {
		rtcpBuf := make([]byte, session.RTPBufferSize)
		for {
			if _, _, err := audioSender.Read(rtcpBuf); err != nil {
				return
			}
		}
	}()

	// Start reading RTCP packets from the video sender (to receive PLI/FIR)
	go func() {
		rtcpBuf := make([]byte, session.RTPBufferSize)
		for {
			if session.GetState() == StateEnded {
				return
			}

			n, _, rtcpErr := videoSender.Read(rtcpBuf)
			if rtcpErr != nil {
				return
			}

			// Parse RTCP
			packets, err := rtcp.Unmarshal(rtcpBuf[:n])
			if err != nil {
				continue
			}

			for _, p := range packets {
				switch pkt := p.(type) {
				case *rtcp.PictureLossIndication:
					fmt.Printf("[%s] 📸 Received PLI from browser for video (RTPSender)\n", id)
					// Forward to Asterisk to request keyframe
					session.SendBrowserRecoveryToAsterisk("browser-pli")
				case *rtcp.FullIntraRequest:
					fmt.Printf("[%s] 📸 Received FIR from browser for video (RTPSender)\n", id)
					// Forward to Asterisk to request keyframe
					session.SendBrowserRecoveryToAsterisk("browser-fir")
				case *rtcp.TransportLayerNack:
					fmt.Printf("[%s] 🔄 Received NACK from browser for video (RTPSender) - Nacks=%v\n", id, pkt.Nacks)
					if len(pkt.Nacks) > 0 {
						sent, missing := session.RetransmitVideoNACK(pkt.Nacks)
						if missing > 0 {
							// Fallback: ask Asterisk to retransmit missing packets (if supported)
							session.SendNACKToAsterisk(pkt.Nacks)
						}
						if sent > 0 || missing > 0 {
							fmt.Printf("[%s] 🔁 NACK handled (retransmit=%d, missing=%d)\n", id, sent, missing)
						}
					}
				}
			}
		}
	}()

	// Set up TURN/ICE debug logging if enabled
	if cfg.API.DebugTURN {
		// Log ICE gathering state changes
		peerConnection.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
			fmt.Printf("[%s] 🧊 ICE Gathering State: %s\n", id, state.String())
		})

		// Log all ICE candidates as they are discovered
		peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
			if candidate == nil {
				// nil candidate indicates gathering is complete
				fmt.Printf("[%s] 🧊 ICE Candidate gathering complete\n", id)
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
			fmt.Printf("[%s] 🧊 ICE Candidate: type=%s address=%s:%d protocol=%s\n", id, candidateType, candidate.Address, candidate.Port, candidate.Protocol.String())
		})
	}

	// Set ICE connection state change handler
	reconnectGracePeriod := 30 * time.Second
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("[%s] ICE Connection State: %s\n", id, connectionState.String())

		session.mu.Lock()
		session.UpdatedAt = time.Now()
		startRecoveryBurstReason := ""

		switch connectionState {
		case webrtc.ICEConnectionStateConnected:
			// Log selected candidate pair if TURN debug is enabled
			if cfg.API.DebugTURN {
				// Use GetStats to find the selected candidate pair (nominated and succeeded)
				stats := peerConnection.GetStats()
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
						fmt.Printf("[%s] 🧊 Selected Candidate Pair:%s\n", id, turnIndicator)
						fmt.Printf("[%s]   Local:  type=%s address=%s:%d protocol=%s\n", id, localType, localCandidate.IP, localCandidate.Port, localCandidate.Protocol)
						fmt.Printf("[%s]   Remote: type=%s address=%s:%d protocol=%s\n", id, remoteType, remoteCandidate.IP, remoteCandidate.Port, remoteCandidate.Protocol)
					}
				}
			}

			// If reconnecting after network change, transition back to active
			wasReconnecting := session.State == StateReconnecting
			if session.State == StateConnecting || session.State == StateReconnecting {
				session.State = StateActive
				if wasReconnecting {
					fmt.Printf("[%s] ✅ ICE reconnected - resuming call\n", id)
					startRecoveryBurstReason = "ice-reconnected"
				}
			}
			// Send FIR first (to request SPS/PPS + IDR), then PLI burst for fast video start
			go func() {
				fmt.Printf("[%s] 🚀 ICE Connected - Sending FIR + PLI requests for fast video start (with SPS/PPS)\n", id)
				// Send FIR first to request full keyframe with parameter sets
				session.SendFIRToAsterisk()
				time.Sleep(100 * time.Millisecond)
				// Then send PLI multiple times with short delays to ensure keyframe is received
				for i := 0; i < 3; i++ {
					time.Sleep(100 * time.Millisecond)
					session.SendPLIToAsteriskForced("ice-connected")
					session.SendPLItoWebRTC() // PLI to browser
				}
			}()

		case webrtc.ICEConnectionStateDisconnected, webrtc.ICEConnectionStateClosed:
			if session.State == StateEnded {
				break
			}

			// Keep session/media endpoints alive during transient ICE transitions.
			// This allows WS resume + renegotiation to recover without losing SIP RTP sockets.
			if session.State != StateReconnecting {
				session.State = StateReconnecting
				fmt.Printf("[%s] 📡 ICE %s - entering reconnection grace period (%s)\n", id, connectionState.String(), reconnectGracePeriod)
				startRecoveryBurstReason = "ice-reconnecting"

				go func() {
					time.Sleep(reconnectGracePeriod)
					session.mu.Lock()
					defer session.mu.Unlock()

					// If still reconnecting after grace period, treat as terminal.
					if session.State == StateReconnecting {
						fmt.Printf("[%s] ⏰ Grace period expired - ending session\n", id)
						session.State = StateEnded
						if session.RTPConn != nil {
							session.RTPConn.Close()
							session.RTPConn = nil
						}
						if session.VideoRTPConn != nil {
							session.VideoRTPConn.Close()
							session.VideoRTPConn = nil
						}
						if session.AudioRTCPConn != nil {
							session.AudioRTCPConn.Close()
							session.AudioRTCPConn = nil
						}
						if session.VideoRTCPConn != nil {
							session.VideoRTCPConn.Close()
							session.VideoRTCPConn = nil
						}
					}
				}()
			}

		case webrtc.ICEConnectionStateFailed:
			// ICE failed is truly terminal - end immediately
			session.State = StateEnded
			if session.RTPConn != nil {
				session.RTPConn.Close()
				session.RTPConn = nil
			}
			if session.VideoRTPConn != nil {
				session.VideoRTPConn.Close()
				session.VideoRTPConn = nil
			}
			if session.AudioRTCPConn != nil {
				session.AudioRTCPConn.Close()
				session.AudioRTCPConn = nil
			}
			if session.VideoRTCPConn != nil {
				session.VideoRTCPConn.Close()
				session.VideoRTCPConn = nil
			}
		}
		session.mu.Unlock()
		if startRecoveryBurstReason != "" {
			session.StartVideoRecoveryBurst(startRecoveryBurstReason)
		}
	})

	// Set OnTrack handler to forward WebRTC RTP → Asterisk
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		kind := track.Kind().String()
		codec := track.Codec()
		fmt.Printf("[%s] 🎬 OnTrack: kind=%s mime=%s pt=%d ssrc=%d clockRate=%d fmtp=%q\n",
			id, kind, codec.MimeType, track.PayloadType(), track.SSRC(), codec.ClockRate, codec.SDPFmtpLine)

		// Extra logging for video codec
		if kind == "video" {
			fmt.Printf("[%s] 🎬 WebRTC video codec details: %s (fmtp: %s)\n", id, codec.MimeType, codec.SDPFmtpLine)
		}

		// Cache SPS/PPS if available (for Video)
		if kind == "video" {
			params := track.Codec().SDPFmtpLine
			if strings.Contains(params, "sprop-parameter-sets") {
				for _, param := range strings.Split(params, ";") {
					param = strings.TrimSpace(param)
					if strings.HasPrefix(param, "sprop-parameter-sets=") {
						sets := strings.Split(strings.TrimPrefix(param, "sprop-parameter-sets="), ",")
						if len(sets) >= 1 {
							sps, err := base64.StdEncoding.DecodeString(sets[0])
							if err == nil {
								session.mu.Lock()
								session.CachedSPS = sps
								session.mu.Unlock()
								fmt.Printf("[%s] 💾 Cached SPS from SDP (%d bytes)\n", id, len(sps))
							}
						}
						if len(sets) >= 2 {
							pps, err := base64.StdEncoding.DecodeString(sets[1])
							if err == nil {
								session.mu.Lock()
								session.CachedPPS = pps
								session.mu.Unlock()
								fmt.Printf("[%s] 💾 Cached PPS from SDP (%d bytes)\n", id, len(pps))
							}
						}
					}
				}
			}
		}

		// 1. Handle RTCP (PLI/FIR) from WebRTC (Sender Reports & Feedback)
		go func() {
			rtcpBuf := make([]byte, session.RTPBufferSize)
			for {
				if session.GetState() == StateEnded {
					return
				}

				n, _, rtcpErr := receiver.Read(rtcpBuf)
				if rtcpErr != nil {
					return
				}

				// Parse RTCP
				packets, err := rtcp.Unmarshal(rtcpBuf[:n])
				if err != nil {
					continue
				}

				for _, p := range packets {
					switch pkt := p.(type) {
					case *rtcp.PictureLossIndication:
						fmt.Printf("[%s] 📸 Received PLI from browser for %s\n", id, kind)
						// Forward PLI to Asterisk (video only)
						if kind == "video" {
							session.SendBrowserRecoveryToAsterisk("browser-pli")
						}

					case *rtcp.FullIntraRequest:
						fmt.Printf("[%s] 📸 Received FIR from browser for %s\n", id, kind)
						if kind == "video" {
							session.SendBrowserRecoveryToAsterisk("browser-fir")
						}

					case *rtcp.TransportLayerNack:
						fmt.Printf("[%s] 🔄 Received NACK from browser for %s - Nacks=%v\n", id, kind, pkt.Nacks)
						if kind == "video" {
							session.SendNACKToAsterisk(pkt.Nacks)
						}
					}
				}
			}
		}()

		// 2. Start goroutine to forward RTP packets to Asterisk
		go session.forwardRTPToAsterisk(track, kind)
	})

	return session, nil
}

// SetCallInfo sets call information for a session
func (s *Session) SetCallInfo(direction, from, to, sipCallID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Direction = direction
	s.From = from
	s.To = to
	s.SIPCallID = sipCallID
	s.UpdatedAt = time.Now()
}

// SetIncomingInvite stores incoming INVITE metadata (thread-safe)
func (s *Session) SetIncomingInvite(tx interface{}, req interface{}, invite []byte, fromURI, toURI string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.IncomingSIPTx = tx
	s.IncomingSIPReq = req
	s.IncomingINVITE = invite
	s.IncomingFromURI = fromURI
	s.IncomingToURI = toURI
	s.UpdatedAt = time.Now()
}

// UpdateIncomingInviteTransaction updates transaction/request references for retransmissions (thread-safe)
func (s *Session) UpdateIncomingInviteTransaction(tx interface{}, req interface{}, invite []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.IncomingSIPTx = tx
	s.IncomingSIPReq = req
	s.IncomingINVITE = invite
	s.UpdatedAt = time.Now()
}

// GetIncomingInviteRequest returns the stored INVITE request (thread-safe)
func (s *Session) GetIncomingInviteRequest() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.IncomingSIPReq
}

// GetIncomingInvite returns stored INVITE metadata (thread-safe).
// invite is copied to avoid races.
func (s *Session) GetIncomingInvite() (interface{}, interface{}, []byte, string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inviteCopy := make([]byte, len(s.IncomingINVITE))
	copy(inviteCopy, s.IncomingINVITE)
	return s.IncomingSIPTx, s.IncomingSIPReq, inviteCopy, s.IncomingFromURI, s.IncomingToURI
}

// ClearIncomingInvite clears stored INVITE state (thread-safe).
func (s *Session) ClearIncomingInvite() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.IncomingSIPTx = nil
	s.IncomingSIPReq = nil
	s.IncomingINVITE = nil
	s.IncomingFromURI = ""
	s.IncomingToURI = ""
	s.UpdatedAt = time.Now()
}

// TryClaimIncoming attempts to claim an incoming call (first-accept-wins).
// Returns true if successfully claimed, false if already claimed by another client.
// Thread-safe atomic operation.
func (s *Session) TryClaimIncoming(clientID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IncomingClaimed {
		// Already claimed by someone else
		return false
	}

	// Claim it
	s.IncomingClaimed = true
	s.IncomingClaimedBy = clientID
	s.UpdatedAt = time.Now()
	return true
}

// SetSIPAuthContext sets the SIP authentication context for this session (thread-safe).
// For public mode: mode="public", accountKey="user@domain" (hostname) or "user@ip:port" (IP literal), domain, username, password, port
// For trunk mode: mode="trunk", trunkID=<id>
func (s *Session) SetSIPAuthContext(mode string, accountKey string, trunkID int64, domain string, username string, password string, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SIPAuthMode = mode
	s.SIPAccountKey = accountKey
	s.SIPTrunkID = trunkID
	s.SIPDomain = domain
	s.SIPUsername = username
	s.SIPPassword = password
	s.SIPPort = port
	s.UpdatedAt = time.Now()
}

// GetSIPAuthContext returns the SIP authentication context (thread-safe).
func (s *Session) GetSIPAuthContext() (mode string, accountKey string, trunkID int64, domain string, username string, password string, port int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SIPAuthMode, s.SIPAccountKey, s.SIPTrunkID, s.SIPDomain, s.SIPUsername, s.SIPPassword, s.SIPPort
}

// UpdateFromTo updates From/To only when provided (thread-safe)
func (s *Session) UpdateFromTo(fromURI, toURI string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fromURI != "" {
		s.From = fromURI
	}
	if toURI != "" {
		s.To = toURI
	}
	s.UpdatedAt = time.Now()
}

// GetFromTo returns current From/To (thread-safe)
func (s *Session) GetFromTo() (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.From, s.To
}

// GetCallInfo returns direction, from, to, sipCallID (thread-safe)
func (s *Session) GetCallInfo() (string, string, string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Direction, s.From, s.To, s.SIPCallID
}

// CopyIncomingInviteFrom copies incoming INVITE state from another session (thread-safe)
func (s *Session) CopyIncomingInviteFrom(source *Session) {
	if source == nil {
		return
	}
	_tx, _req, invite, fromURI, toURI := source.GetIncomingInvite()
	s.SetIncomingInvite(_tx, _req, invite, fromURI, toURI)
}

// CopyCallInfoFrom copies direction/from/to/sipCallID from another session (thread-safe)
func (s *Session) CopyCallInfoFrom(source *Session) {
	if source == nil {
		return
	}
	direction, from, to, sipCallID := source.GetCallInfo()
	s.SetCallInfo(direction, from, to, sipCallID)
}

// HasDialogState returns true if SIP dialog tags are set (thread-safe)
func (s *Session) HasDialogState() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SIPFromTag != "" && s.SIPToTag != ""
}

// GetSIPDialogState returns dialog metadata (thread-safe).
func (s *Session) GetSIPDialogState() (fromTag, toTag, remoteContact string, routeSet []string, cseq int, domain string, port int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	routeCopy := make([]string, len(s.SIPRouteSet))
	copy(routeCopy, s.SIPRouteSet)
	return s.SIPFromTag, s.SIPToTag, s.SIPRemoteContact, routeCopy, s.SIPCSeq, s.SIPDomain, s.SIPPort
}

// NextSIPCSeq increments and returns the next CSeq (thread-safe).
func (s *Session) NextSIPCSeq() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SIPCSeq++
	return s.SIPCSeq
}

// SetSIPDialogState sets the SIP dialog state for BYE requests
func (s *Session) SetSIPDialogState(fromTag, toTag, remoteContact, domain string, port, cseq int, routeSet []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SIPFromTag = fromTag
	s.SIPToTag = toTag
	s.SIPRemoteContact = remoteContact
	if domain != "" {
		s.SIPDomain = domain
	}
	if port > 0 {
		s.SIPPort = port
	}
	s.SIPCSeq = cseq
	// Only update RouteSet if provided (don't overwrite with empty list from ACK)
	if len(routeSet) > 0 {
		s.SIPRouteSet = routeSet
	}
	s.UpdatedAt = time.Now()
	fmt.Printf("[%s] SIP Dialog state set - FromTag: %s, ToTag: %s, Domain: %s, RouteSet: %v\n", s.ID, fromTag, toTag, domain, s.SIPRouteSet)
}

// HasCachedSPSPPS checks if both SPS and PPS are cached (thread-safe)
func (s *Session) HasCachedSPSPPS() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.CachedSPS) > 0 && len(s.CachedPPS) > 0
}

// GetCachedSPSPPS returns copies of cached SPS/PPS (thread-safe)
// Returns (sps, pps, ok) where ok indicates if both are available
func (s *Session) GetCachedSPSPPS() ([]byte, []byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.CachedSPS) == 0 || len(s.CachedPPS) == 0 {
		return nil, nil, false
	}
	// Return copies to avoid race conditions
	sps := make([]byte, len(s.CachedSPS))
	pps := make([]byte, len(s.CachedPPS))
	copy(sps, s.CachedSPS)
	copy(pps, s.CachedPPS)
	return sps, pps, true
}

// WaitForSPSPPS waits up to the specified timeout for SPS/PPS to be cached
// Returns true if SPS/PPS are available, false if timeout
func (s *Session) WaitForSPSPPS(timeout time.Duration) bool {
	// Check immediately first
	if s.HasCachedSPSPPS() {
		fmt.Printf("[%s] ✅ SPS/PPS already cached (immediate check)\n", s.ID)
		return true
	}

	fmt.Printf("[%s] ⏳ Waiting up to %v for SPS/PPS to be cached...\n", s.ID, timeout)

	// Poll every 50ms for SPS/PPS availability
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	timeoutChan := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			if s.HasCachedSPSPPS() {
				s.mu.RLock()
				spsLen := len(s.CachedSPS)
				ppsLen := len(s.CachedPPS)
				s.mu.RUnlock()
				fmt.Printf("[%s] ✅ SPS/PPS cached (SPS: %d bytes, PPS: %d bytes)\n", s.ID, spsLen, ppsLen)
				return true
			}
		case <-timeoutChan:
			fmt.Printf("[%s] ⚠️ SPS/PPS not ready after %v; proceeding without sprop-parameter-sets\n", s.ID, timeout)
			return false
		}
	}
}
