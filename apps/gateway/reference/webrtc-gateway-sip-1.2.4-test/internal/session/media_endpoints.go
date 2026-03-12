package session

import (
	"fmt"
	"net"
	"time"
)

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
	fmt.Printf("[Session %s] Asterisk endpoints set - Audio: %s, Video: %s\n", s.ID, audioAddr, videoAddr)
}

// UpdateAsteriskVideoEndpointFromRTP updates the video endpoint based on actual RTP source (symmetric RTP)
// This handles cases where the actual RTP source port differs from SDP (NAT, symmetric RTP, etc.)
func (s *Session) UpdateAsteriskVideoEndpointFromRTP(remoteAddr *net.UDPAddr) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only update if session is still active
	if s.State == StateEnded {
		return
	}

	// If no endpoint set yet, set it directly
	if s.AsteriskVideoAddr == nil {
		s.AsteriskVideoAddr = remoteAddr
		s.UpdatedAt = time.Now()
		fmt.Printf("[Session %s] 🔄 Symmetric RTP: Set video endpoint from RTP source: %s\n", s.ID, remoteAddr)
		return
	}

	// Check if IP matches (or original IP is 0.0.0.0/nil)
	ipMatches := s.AsteriskVideoAddr.IP.Equal(remoteAddr.IP) || s.AsteriskVideoAddr.IP.IsUnspecified()

	// Update port if IP matches and port differs
	if ipMatches && s.AsteriskVideoAddr.Port != remoteAddr.Port {
		oldAddr := s.AsteriskVideoAddr.String()
		s.AsteriskVideoAddr = remoteAddr
		s.UpdatedAt = time.Now()
		fmt.Printf("[Session %s] 🔄 Symmetric RTP: Updated video endpoint port %s → %s (IP: %s)\n",
			s.ID, oldAddr, remoteAddr, remoteAddr.IP)
	} else if !ipMatches && s.AsteriskVideoAddr.IP.IsUnspecified() {
		// If original IP was unspecified, update to actual IP
		oldAddr := s.AsteriskVideoAddr.String()
		s.AsteriskVideoAddr = remoteAddr
		s.UpdatedAt = time.Now()
		fmt.Printf("[Session %s] 🔄 Symmetric RTP: Updated video endpoint IP %s → %s\n",
			s.ID, oldAddr, remoteAddr)
	}
}

// UpdateAsteriskVideoRTCPFromRTCP learns the RTCP address based on real RTCP traffic.
func (s *Session) UpdateAsteriskVideoRTCPFromRTCP(remoteAddr *net.UDPAddr, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == StateEnded {
		return
	}

	if s.AsteriskVideoRTCPAddr == nil {
		s.AsteriskVideoRTCPAddr = remoteAddr
		s.VideoRTCPLearnedAt = time.Now()
		s.VideoRTCPSource = source
		s.UpdatedAt = time.Now()
		fmt.Printf("[Session %s] 📍 Learned SIP Video RTCP addr: %s (source=%s)\n", s.ID, remoteAddr, source)
		return
	}

	ipMatches := s.AsteriskVideoRTCPAddr.IP.Equal(remoteAddr.IP) || s.AsteriskVideoRTCPAddr.IP.IsUnspecified()
	if ipMatches && s.AsteriskVideoRTCPAddr.Port != remoteAddr.Port {
		oldAddr := s.AsteriskVideoRTCPAddr.String()
		s.AsteriskVideoRTCPAddr = remoteAddr
		s.VideoRTCPLearnedAt = time.Now()
		s.VideoRTCPSource = source
		s.UpdatedAt = time.Now()
		fmt.Printf("[Session %s] 📍 Updated SIP Video RTCP addr %s → %s (source=%s)\n", s.ID, oldAddr, remoteAddr, source)
	} else if !ipMatches && s.AsteriskVideoRTCPAddr.IP.IsUnspecified() {
		oldAddr := s.AsteriskVideoRTCPAddr.String()
		s.AsteriskVideoRTCPAddr = remoteAddr
		s.VideoRTCPLearnedAt = time.Now()
		s.VideoRTCPSource = source
		s.UpdatedAt = time.Now()
		fmt.Printf("[Session %s] 📍 Updated SIP Video RTCP IP %s → %s (source=%s)\n", s.ID, oldAddr, remoteAddr, source)
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
	fmt.Printf("[Session %s] 🔄 RTCP fallback window enabled (%s) until %s\n", s.ID, reason, s.VideoRTCPFallbackUntil.Format(time.RFC3339Nano))
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

// UpdateAsteriskAudioEndpointFromRTP updates the audio endpoint based on actual RTP source (symmetric RTP)
func (s *Session) UpdateAsteriskAudioEndpointFromRTP(remoteAddr *net.UDPAddr) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only update if session is still active
	if s.State == StateEnded {
		return
	}

	// If no endpoint set yet, set it directly
	if s.AsteriskAudioAddr == nil {
		s.AsteriskAudioAddr = remoteAddr
		s.UpdatedAt = time.Now()
		fmt.Printf("[Session %s] 🔄 Symmetric RTP: Set audio endpoint from RTP source: %s\n", s.ID, remoteAddr)
		return
	}

	// Check if IP matches (or original IP is 0.0.0.0/nil)
	ipMatches := s.AsteriskAudioAddr.IP.Equal(remoteAddr.IP) || s.AsteriskAudioAddr.IP.IsUnspecified()

	// Update port if IP matches and port differs
	if ipMatches && s.AsteriskAudioAddr.Port != remoteAddr.Port {
		oldAddr := s.AsteriskAudioAddr.String()
		s.AsteriskAudioAddr = remoteAddr
		s.UpdatedAt = time.Now()
		fmt.Printf("[Session %s] 🔄 Symmetric RTP: Updated audio endpoint port %s → %s (IP: %s)\n",
			s.ID, oldAddr, remoteAddr, remoteAddr.IP)
	} else if !ipMatches && s.AsteriskAudioAddr.IP.IsUnspecified() {
		// If original IP was unspecified, update to actual IP
		oldAddr := s.AsteriskAudioAddr.String()
		s.AsteriskAudioAddr = remoteAddr
		s.UpdatedAt = time.Now()
		fmt.Printf("[Session %s] 🔄 Symmetric RTP: Updated audio endpoint IP %s → %s\n",
			s.ID, oldAddr, remoteAddr)
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
	s.PLIBurstUntil = time.Time{}

	// NOTE: Do NOT clear CachedSPS/CachedPPS here.
	// We keep Offer/SDP-derived SPS/PPS as a fallback so SIP SDP can include sprop-parameter-sets
	// even before RTP starts flowing. Early RTP parsing will still overwrite within the first packets.
	s.LastSPSPPSInjectionTime = time.Time{}

	s.UpdatedAt = time.Now()
	fmt.Printf("[Session %s] 🔄 Media state reset (Audio + Video) for new call\n", s.ID)
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
