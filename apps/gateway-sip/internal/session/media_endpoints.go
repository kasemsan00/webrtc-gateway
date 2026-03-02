package session

import (
	"fmt"
	"net"
	"time"
)

// CloseMediaTransports closes all session RTP/RTCP sockets and clears transport pointers/ports.
// Socket closes are performed outside the session lock to keep lock hold time short.
func (s *Session) CloseMediaTransports() {
	s.mu.Lock()
	audioRTP := s.RTPConn
	videoRTP := s.VideoRTPConn
	audioRTCP := s.AudioRTCPConn
	videoRTCP := s.VideoRTCPConn

	s.RTPConn = nil
	s.VideoRTPConn = nil
	s.AudioRTCPConn = nil
	s.VideoRTCPConn = nil

	s.RTPPort = 0
	s.VideoRTPPort = 0
	s.AudioRTCPPort = 0
	s.VideoRTCPPort = 0
	s.UpdatedAt = time.Now()
	s.mu.Unlock()

	if audioRTP != nil {
		if err := audioRTP.Close(); err != nil {
			fmt.Printf("[%s] ⚠️ Close audio RTP conn error: %v\n", s.ID, err)
		}
	}
	if videoRTP != nil {
		if err := videoRTP.Close(); err != nil {
			fmt.Printf("[%s] ⚠️ Close video RTP conn error: %v\n", s.ID, err)
		}
	}
	if audioRTCP != nil {
		if err := audioRTCP.Close(); err != nil {
			fmt.Printf("[%s] ⚠️ Close audio RTCP conn error: %v\n", s.ID, err)
		}
	}
	if videoRTCP != nil {
		if err := videoRTCP.Close(); err != nil {
			fmt.Printf("[%s] ⚠️ Close video RTCP conn error: %v\n", s.ID, err)
		}
	}
}

// SetRTPConnection sets the audio RTP connection for a session
func (s *Session) SetRTPConnection(conn *net.UDPConn, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RTPConn = conn
	s.RTPPort = port
	s.UpdatedAt = time.Now()
}

// SetVideoRTPConnection sets the video RTP connection for a session
func (s *Session) SetVideoRTPConnection(conn *net.UDPConn, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VideoRTPConn = conn
	s.VideoRTPPort = port
	s.UpdatedAt = time.Now()
}

// SetAudioRTCPConnection sets the audio RTCP connection for a session
func (s *Session) SetAudioRTCPConnection(conn *net.UDPConn, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AudioRTCPConn = conn
	s.AudioRTCPPort = port
	s.UpdatedAt = time.Now()
}

// SetVideoRTCPConnection sets the video RTCP connection for a session
func (s *Session) SetVideoRTCPConnection(conn *net.UDPConn, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VideoRTCPConn = conn
	s.VideoRTCPPort = port
	s.UpdatedAt = time.Now()
}

// SetAsteriskEndpoints sets the Asterisk RTP endpoints for forwarding WebRTC → Asterisk
func (s *Session) SetAsteriskEndpoints(audioAddr, videoAddr *net.UDPAddr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AsteriskAudioAddr = audioAddr
	s.AsteriskVideoAddr = videoAddr
	s.UpdatedAt = time.Now()
	fmt.Printf("[%s] Asterisk endpoints set - Audio: %s, Video: %s\n", s.ID, audioAddr, videoAddr)
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	ip := make(net.IP, len(addr.IP))
	copy(ip, addr.IP)
	return &net.UDPAddr{IP: ip, Port: addr.Port, Zone: addr.Zone}
}

func (s *Session) inSymmetricRTPTrustWindow(now time.Time) bool {
	return !s.SymmetricRTPTrustUntil.IsZero() && now.Before(s.SymmetricRTPTrustUntil)
}

// UpdateAsteriskVideoEndpointFromRTP updates the video endpoint based on actual RTP source (symmetric RTP)
// This handles cases where the actual RTP source port differs from SDP (NAT, symmetric RTP, etc.)
func (s *Session) UpdateAsteriskVideoEndpointFromRTP(remoteAddr *net.UDPAddr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()

	// Only update if session is still active
	if s.State == StateEnded {
		return
	}
	if remoteAddr == nil || remoteAddr.IP == nil {
		return
	}

	trustWindow := s.inSymmetricRTPTrustWindow(now)
	updatedVideoEndpoint := false

	// If no endpoint set yet, set it directly
	if s.AsteriskVideoAddr == nil {
		s.AsteriskVideoAddr = cloneUDPAddr(remoteAddr)
		s.UpdatedAt = now
		updatedVideoEndpoint = true
		fmt.Printf("[%s] 🔄 Symmetric RTP: Set video endpoint from RTP source: %s\n", s.ID, remoteAddr)
	} else {
		// Check if IP matches (or original IP is 0.0.0.0/nil)
		ipMatches := s.AsteriskVideoAddr.IP.Equal(remoteAddr.IP) || s.AsteriskVideoAddr.IP.IsUnspecified()

		// Update port if IP matches and port differs
		if ipMatches && s.AsteriskVideoAddr.Port != remoteAddr.Port {
			oldAddr := s.AsteriskVideoAddr.String()
			s.AsteriskVideoAddr = cloneUDPAddr(remoteAddr)
			s.UpdatedAt = now
			updatedVideoEndpoint = true
			fmt.Printf("[%s] 🔄 Symmetric RTP: Updated video endpoint port %s → %s (IP: %s)\n",
				s.ID, oldAddr, remoteAddr, remoteAddr.IP)
		} else if !ipMatches && s.AsteriskVideoAddr.IP.IsUnspecified() {
			oldAddr := s.AsteriskVideoAddr.String()
			s.AsteriskVideoAddr = cloneUDPAddr(remoteAddr)
			s.UpdatedAt = now
			updatedVideoEndpoint = true
			fmt.Printf("[%s] 🔄 Symmetric RTP: Updated video endpoint IP %s → %s\n",
				s.ID, oldAddr, remoteAddr)
		} else if !ipMatches && trustWindow {
			fmt.Printf("[%s] 🔍 Symmetric RTP: observed video source %s but keeping negotiated endpoint %s\n",
				s.ID, remoteAddr, s.AsteriskVideoAddr)
		}
	}

	seedRTCP := false
	if s.AsteriskVideoRTCPAddr == nil || s.AsteriskVideoRTCPAddr.IP.IsUnspecified() {
		seedRTCP = true
	} else if trustWindow && (!s.AsteriskVideoRTCPAddr.IP.Equal(remoteAddr.IP) || s.AsteriskVideoRTCPAddr.Port != remoteAddr.Port) {
		seedRTCP = true
	}

	if seedRTCP {
		s.AsteriskVideoRTCPAddr = cloneUDPAddr(remoteAddr)
		s.VideoRTCPLearnedAt = now
		s.VideoRTCPSource = "rtp-learn"
		fmt.Printf("[%s] 📍 Seeded SIP Video RTCP addr from RTP source: %s\n", s.ID, s.AsteriskVideoRTCPAddr)
	}

	if updatedVideoEndpoint || seedRTCP {
		fallbackUntil := now.Add(maxFallbackDuration)
		if fallbackUntil.After(s.VideoRTCPFallbackUntil) {
			s.VideoRTCPFallbackUntil = fallbackUntil
			fmt.Printf("[%s] 🔄 RTCP fallback window refreshed (rtp-source-update) until %s\n", s.ID, s.VideoRTCPFallbackUntil.Format(time.RFC3339Nano))
		}
	}
}

// UpdateAsteriskVideoRTCPFromRTCP learns the RTCP address based on real RTCP traffic.
func (s *Session) UpdateAsteriskVideoRTCPFromRTCP(remoteAddr *net.UDPAddr, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()

	if s.State == StateEnded {
		return
	}
	if remoteAddr == nil || remoteAddr.IP == nil {
		return
	}
	trustWindow := s.inSymmetricRTPTrustWindow(now)

	if s.AsteriskVideoRTCPAddr == nil {
		s.AsteriskVideoRTCPAddr = cloneUDPAddr(remoteAddr)
		s.VideoRTCPLearnedAt = now
		s.VideoRTCPSource = source
		s.UpdatedAt = now
		fmt.Printf("[%s] 📍 Learned SIP Video RTCP addr: %s (source=%s)\n", s.ID, remoteAddr, source)
		return
	}

	ipMatches := s.AsteriskVideoRTCPAddr.IP.Equal(remoteAddr.IP) || s.AsteriskVideoRTCPAddr.IP.IsUnspecified()
	if ipMatches && s.AsteriskVideoRTCPAddr.Port != remoteAddr.Port {
		oldAddr := s.AsteriskVideoRTCPAddr.String()
		s.AsteriskVideoRTCPAddr = cloneUDPAddr(remoteAddr)
		s.VideoRTCPLearnedAt = now
		s.VideoRTCPSource = source
		s.UpdatedAt = now
		fmt.Printf("[%s] 📍 Updated SIP Video RTCP addr %s → %s (source=%s)\n", s.ID, oldAddr, remoteAddr, source)
	} else if !ipMatches && (s.AsteriskVideoRTCPAddr.IP.IsUnspecified() || trustWindow) {
		oldAddr := s.AsteriskVideoRTCPAddr.String()
		s.AsteriskVideoRTCPAddr = cloneUDPAddr(remoteAddr)
		s.VideoRTCPLearnedAt = now
		s.VideoRTCPSource = source
		s.UpdatedAt = now
		fmt.Printf("[%s] 📍 Updated SIP Video RTCP IP %s → %s (source=%s, trustWindow=%v)\n", s.ID, oldAddr, remoteAddr, source, trustWindow)
	}
}

// StartVideoRTCPFallbackWindow enables dual-port RTCP sending for a short window.
func (s *Session) StartVideoRTCPFallbackWindow(duration time.Duration, reason string) {
	if duration <= 0 || duration > maxFallbackDuration {
		duration = maxFallbackDuration
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	until := time.Now().Add(duration)
	if until.After(s.VideoRTCPFallbackUntil) {
		s.VideoRTCPFallbackUntil = until
	}
	fmt.Printf("[%s] 🔄 RTCP fallback window enabled (%s) until %s\n", s.ID, reason, s.VideoRTCPFallbackUntil.Format(time.RFC3339Nano))
}

// ShouldUseVideoRTCPFallback returns true if dual-port sending should be used.
func (s *Session) ShouldUseVideoRTCPFallback() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Now().Before(s.VideoRTCPFallbackUntil)
}

// GetLearnedVideoRTCPAddr returns the learned RTCP address (if any).
func (s *Session) GetLearnedVideoRTCPAddr() (*net.UDPAddr, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AsteriskVideoRTCPAddr, s.VideoRTCPSource
}

// MediaEndpointStatus captures current SIP/media endpoint readiness for resume diagnostics.
type MediaEndpointStatus struct {
	AudioRTPReady    bool
	VideoRTPReady    bool
	AudioRTCPReady   bool
	VideoRTCPReady   bool
	HasAsteriskAudio bool
	HasAsteriskVideo bool
	AudioRTPPort     int
	VideoRTPPort     int
	AudioRTCPPort    int
	VideoRTCPPort    int
}

// GetMediaEndpointStatus returns a thread-safe snapshot of endpoint readiness.
func (s *Session) GetMediaEndpointStatus() MediaEndpointStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return MediaEndpointStatus{
		AudioRTPReady:    s.RTPConn != nil,
		VideoRTPReady:    s.VideoRTPConn != nil,
		AudioRTCPReady:   s.AudioRTCPConn != nil,
		VideoRTCPReady:   s.VideoRTCPConn != nil,
		HasAsteriskAudio: s.AsteriskAudioAddr != nil,
		HasAsteriskVideo: s.AsteriskVideoAddr != nil,
		AudioRTPPort:     s.RTPPort,
		VideoRTPPort:     s.VideoRTPPort,
		AudioRTCPPort:    s.AudioRTCPPort,
		VideoRTCPPort:    s.VideoRTCPPort,
	}
}

// UpdateAsteriskAudioEndpointFromRTP updates the audio endpoint based on actual RTP source (symmetric RTP)
func (s *Session) UpdateAsteriskAudioEndpointFromRTP(remoteAddr *net.UDPAddr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()

	// Only update if session is still active
	if s.State == StateEnded {
		return
	}
	if remoteAddr == nil || remoteAddr.IP == nil {
		return
	}

	// If no endpoint set yet, set it directly
	if s.AsteriskAudioAddr == nil {
		s.AsteriskAudioAddr = cloneUDPAddr(remoteAddr)
		s.UpdatedAt = now
		fmt.Printf("[%s] 🔄 Symmetric RTP: Set audio endpoint from RTP source: %s\n", s.ID, remoteAddr)
		return
	}

	// Check if IP matches (or original IP is 0.0.0.0/nil)
	ipMatches := s.AsteriskAudioAddr.IP.Equal(remoteAddr.IP) || s.AsteriskAudioAddr.IP.IsUnspecified()

	// Update port if IP matches and port differs
	if ipMatches && s.AsteriskAudioAddr.Port != remoteAddr.Port {
		oldAddr := s.AsteriskAudioAddr.String()
		s.AsteriskAudioAddr = cloneUDPAddr(remoteAddr)
		s.UpdatedAt = now
		fmt.Printf("[%s] 🔄 Symmetric RTP: Updated audio endpoint port %s → %s (IP: %s)\n",
			s.ID, oldAddr, remoteAddr, remoteAddr.IP)
	} else if !ipMatches && s.AsteriskAudioAddr.IP.IsUnspecified() {
		// If original IP was unspecified, update to actual IP
		oldAddr := s.AsteriskAudioAddr.String()
		s.AsteriskAudioAddr = cloneUDPAddr(remoteAddr)
		s.UpdatedAt = now
		fmt.Printf("[%s] 🔄 Symmetric RTP: Updated audio endpoint IP %s → %s\n",
			s.ID, oldAddr, remoteAddr)
	} else if !ipMatches {
		fmt.Printf("[%s] 🔍 Symmetric RTP: observed audio source %s but keeping negotiated endpoint %s\n",
			s.ID, remoteAddr, s.AsteriskAudioAddr)
	}
}

// SetRemoteAudioSSRC sets the remote audio SSRC
func (s *Session) SetRemoteAudioSSRC(ssrc uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RemoteAudioSSRC = ssrc
}

// SetRemoteVideoSSRC sets the remote video SSRC
func (s *Session) SetRemoteVideoSSRC(ssrc uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RemoteVideoSSRC = ssrc
}

// ResetMediaState resets both audio and video RTP state for a new call
// This ensures PLI requests are sent again and SSRC/Seq are reset
// IMPORTANT: Call this when starting a new call on an existing session
func (s *Session) ResetMediaState() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset audio RTP state
	s.AudioSeq = 0
	s.AudioSSRC = 0
	s.RemoteAudioSSRC = 0

	// Reset video RTP state
	s.VideoSeq = 0
	s.VideoSSRC = 0
	s.RemoteVideoSSRC = 0

	// Reset learned RTCP routing info
	s.AsteriskVideoRTCPAddr = nil
	s.VideoRTCPLearnedAt = time.Time{}
	s.VideoRTCPSource = "unknown"
	s.VideoRTCPFallbackUntil = time.Time{}
	s.SymmetricRTPTrustUntil = time.Now().Add(symmetricRTPTrustWindow)
	s.PLIBurstUntil = time.Time{}

	// Reset negotiated SIP media endpoints so a new call cannot reuse stale addresses.
	s.AsteriskAudioAddr = nil
	s.AsteriskVideoAddr = nil

	// NOTE: Do NOT clear CachedSPS/CachedPPS here.
	// We keep Offer/SDP-derived SPS/PPS as a fallback so SIP SDP can include sprop-parameter-sets
	// even before RTP starts flowing. Early RTP parsing will still overwrite within the first packets.
	s.LastSPSPPSInjectionTime = time.Time{}

	s.UpdatedAt = time.Now()
	fmt.Printf("[%s] 🔄 Media state reset (Audio + Video) for new call\n", s.ID)
}

// GetAudioRTPInfo returns the audio RTP connection details for DTMF transmission
// Returns: conn, destAddr, ssrc
func (s *Session) GetAudioRTPInfo() (*net.UDPConn, *net.UDPAddr, uint32) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.RTPConn, s.AsteriskAudioAddr, s.AudioSSRC
}

// SetAudioSSRC sets the audio SSRC (used when initializing DTMF)
func (s *Session) SetAudioSSRC(ssrc uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AudioSSRC = ssrc
}

// GetAndIncrementAudioSeq returns the current audio sequence number and increments it
func (s *Session) GetAndIncrementAudioSeq(count uint16) (baseSeq uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	baseSeq = s.AudioSeq
	s.AudioSeq += count
	return baseSeq
}

// GetAudioTimestamp returns an approximate audio timestamp based on current time
func (s *Session) GetAudioTimestamp() uint32 {
	// Use current time to generate timestamp (8000 Hz clock rate)
	// This creates a reasonable approximation for DTMF events
	return uint32(time.Now().UnixNano() / 125000) // 125000 ns = 1 sample at 8kHz
}

// RecordKeyframe records a keyframe reception and returns (isPLIResponse, responseTime, pliSent, pliResponse)
func (s *Session) RecordKeyframe() (bool, time.Duration, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastKeyframe = time.Now()
	isPLIResponse := time.Since(s.LastPLISent) < 2*time.Second && s.PLISent > 0

	if isPLIResponse {
		s.PLIResponse++
	}

	return isPLIResponse, time.Since(s.LastPLISent), s.PLISent, s.PLIResponse
}

// GetKeyframeTimes returns last keyframe and last PLI/FIR send time (thread-safe)
func (s *Session) GetKeyframeTimes() (time.Time, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastKeyframe, s.LastPLISent
}

// GetSIPRecoveryTimes returns last SIP keyframe request times (thread-safe).
func (s *Session) GetSIPRecoveryTimes() (time.Time, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastSipPLISent, s.LastSipFIRSent
}

// GetRemoteVideoSSRC returns the learned remote video SSRC (thread-safe)
func (s *Session) GetRemoteVideoSSRC() uint32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.RemoteVideoSSRC
}

// CacheSIPSPS caches a SPS NAL unit received from the SIP-side RTP stream.
// Used for SIP→WebRTC keyframe recovery injection (thread-safe).
func (s *Session) CacheSIPSPS(sps []byte) {
	if len(sps) <= 1 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SIPCachedSPS = make([]byte, len(sps))
	copy(s.SIPCachedSPS, sps)
}

// CacheSIPPPS caches a PPS NAL unit received from the SIP-side RTP stream.
// Used for SIP→WebRTC keyframe recovery injection (thread-safe).
func (s *Session) CacheSIPPPS(pps []byte) {
	if len(pps) <= 1 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SIPCachedPPS = make([]byte, len(pps))
	copy(s.SIPCachedPPS, pps)
}

// GetSIPCachedSPSPPS returns copies of SIP-side cached SPS/PPS (thread-safe).
// Returns (sps, pps, ok) where ok indicates if both are available.
func (s *Session) GetSIPCachedSPSPPS() ([]byte, []byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.SIPCachedSPS) == 0 || len(s.SIPCachedPPS) == 0 {
		return nil, nil, false
	}
	sps := make([]byte, len(s.SIPCachedSPS))
	pps := make([]byte, len(s.SIPCachedPPS))
	copy(sps, s.SIPCachedSPS)
	copy(pps, s.SIPCachedPPS)
	return sps, pps, true
}
