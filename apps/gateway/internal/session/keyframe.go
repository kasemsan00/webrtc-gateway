package session

import (
	"fmt"
	"strings"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

const (
	pliKeyframeGrace    = 400 * time.Millisecond
	pliMinInterval      = 150 * time.Millisecond
	pliForceMinInterval = 150 * time.Millisecond
	browserFIRInterval  = 1000 * time.Millisecond
	browserPLIStale     = 600 * time.Millisecond
	browserFIRStale     = 1500 * time.Millisecond
)

// shouldSendPLIToAsterisk gates PLI forwarding to avoid flooding.
// In force mode (browser request), only the minimum interval is enforced.
func (s *Session) shouldSendPLIToAsterisk(now time.Time, force bool) bool {
	s.mu.RLock()
	lastKeyframe := s.LastKeyframe
	lastPLISent := s.LastSipPLISent
	s.mu.RUnlock()

	minInterval := pliMinInterval
	if force {
		minInterval = pliForceMinInterval
	}

	if !lastPLISent.IsZero() && now.Sub(lastPLISent) < minInterval {
		return false
	}
	if force {
		return true
	}
	if lastKeyframe.IsZero() {
		return true
	}
	return now.Sub(lastKeyframe) > pliKeyframeGrace
}

// SendPLIToAsterisk sends a Picture Loss Indication to Asterisk as Compound RTCP (RR + PLI)
// RTCP is sent to RTP port + 1 as per RFC 3550, with fallback to RTP port (for rtcp-mux)
func (s *Session) SendPLIToAsterisk() {
	s.sendPLIToAsterisk(false, "auto")
}

// SendPLIToAsteriskForced sends a PLI to SIP even when keyframe is recent.
// Used for browser-initiated recovery where strict stale checks are too conservative.
func (s *Session) SendPLIToAsteriskForced(trigger string) {
	s.sendPLIToAsterisk(true, trigger)
}

// SendBrowserRecoveryToAsterisk handles browser-originated video recovery feedback.
// It escalates to FIR quickly when keyframes are stale, otherwise sends forced PLI.
// Returns action: "none" | "pli" | "fir" | "both".
func (s *Session) SendBrowserRecoveryToAsterisk(trigger string) string {
	now := time.Now()

	s.mu.Lock()
	lastKeyframe := s.LastKeyframe
	lastFIRReq := s.LastSipFIRSent
	remoteVideoSSRC := s.RemoteVideoSSRC
	destAddr := s.AsteriskVideoAddr
	conn := s.VideoRTCPConn
	if conn == nil {
		conn = s.VideoRTPConn
	}
	_, pliStale, firStale, burstActive := s.getVideoRecoveryPolicy(now, browserFIRInterval, browserPLIStale, browserFIRStale)
	s.mu.Unlock()

	keyframeAge := time.Duration(-1)
	if !lastKeyframe.IsZero() {
		keyframeAge = now.Sub(lastKeyframe)
	}

	if destAddr == nil {
		s.deferBrowserRecoveryToWebRTC(trigger, "missing-sip-video-addr", burstActive, keyframeAge, remoteVideoSSRC)
		return "webrtc"
	}
	if conn == nil {
		s.deferBrowserRecoveryToWebRTC(trigger, "missing-sip-rtcp-conn", burstActive, keyframeAge, remoteVideoSSRC)
		return "webrtc"
	}
	if remoteVideoSSRC == 0 {
		s.deferBrowserRecoveryToWebRTC(trigger, "missing-sip-ssrc", burstActive, keyframeAge, remoteVideoSSRC)
		return "webrtc"
	}

	forceStartupRecovery := burstActive && isBrowserRecoveryTrigger(trigger)
	shouldSendFIR := false
	if !lastKeyframe.IsZero() {
		age := now.Sub(lastKeyframe)
		if age >= firStale {
			shouldSendFIR = true
		}
	} else if burstActive {
		shouldSendFIR = true
	}
	if forceStartupRecovery && (lastFIRReq.IsZero() || now.Sub(lastFIRReq) >= browserFIRInterval) {
		shouldSendFIR = true
	}

	if shouldSendFIR && (lastFIRReq.IsZero() || now.Sub(lastFIRReq) >= browserFIRInterval) {
		if trigger == "ws-request_keyframe" || forceStartupRecovery {
			s.SendFIRToAsterisk()
			s.SendPLIToAsteriskForced(trigger)
			s.logBrowserRecoveryDecision(trigger, "both", burstActive, keyframeAge, remoteVideoSSRC, "forced-startup-or-fir-stale")
			return "both"
		}
		s.SendFIRToAsterisk()
		s.logBrowserRecoveryDecision(trigger, "fir", burstActive, keyframeAge, remoteVideoSSRC, "fir-stale")
		return "fir"
	}

	if !lastKeyframe.IsZero() {
		age := now.Sub(lastKeyframe)
		if age < pliStale && !forceStartupRecovery {
			if trigger == "ws-request_keyframe" || isBrowserRecoveryTrigger(trigger) {
				s.logBrowserRecoveryDecision(trigger, "none", burstActive, age, remoteVideoSSRC, "fresh-keyframe")
			}
			return "none"
		}
	}

	s.SendPLIToAsteriskForced(trigger)
	if trigger == "ws-request_keyframe" || isBrowserRecoveryTrigger(trigger) {
		reason := "pli-stale"
		if forceStartupRecovery {
			reason = "forced-startup"
		}
		s.logBrowserRecoveryDecision(trigger, "pli", burstActive, keyframeAge, remoteVideoSSRC, reason)
	}
	return "pli"
}

func (s *Session) MarkPendingBrowserKeyframeRequest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PendingBrowserKeyframeRequest = true
	s.PendingBrowserKeyframeRequestAt = time.Now()
}

func (s *Session) HasPendingBrowserKeyframeRequest() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PendingBrowserKeyframeRequest
}

func (s *Session) clearPendingBrowserKeyframeRequestLocked() {
	s.PendingBrowserKeyframeRequest = false
	s.PendingBrowserKeyframeRequestAt = time.Time{}
}

func (s *Session) ClearPendingBrowserKeyframeRequest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearPendingBrowserKeyframeRequestLocked()
}

// FlushPendingBrowserKeyframeRequest sends SIP-directed recovery if a client
// keyframe request was deferred. Returns action from SendBrowserRecoveryToAsterisk,
// or "none" if nothing was pending.
func (s *Session) FlushPendingBrowserKeyframeRequest(reason string) string {
	s.mu.Lock()
	if !s.PendingBrowserKeyframeRequest {
		s.mu.Unlock()
		return "none"
	}
	s.clearPendingBrowserKeyframeRequestLocked()
	s.mu.Unlock()

	action := s.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	fmt.Printf("[%s] 📈 request_keyframe_flushed reason=%s action=%s\n", s.ID, reason, action)
	return action
}

func (s *Session) deferBrowserRecoveryToWebRTC(trigger, reason string, burstActive bool, keyframeAge time.Duration, remoteVideoSSRC uint32) {
	if trigger == "ws-request_keyframe" {
		s.MarkPendingBrowserKeyframeRequest()
	}

	if trigger == "browser-fir" {
		s.SendFIRToWebRTC()
	} else {
		s.SendPLItoWebRTC()
	}

	if trigger == "ws-request_keyframe" {
		fmt.Printf("[%s] 📈 request_keyframe_deferred target=webrtc reason=%s burst=%v keyframeAge=%s remoteVideoSSRC=%d\n",
			s.ID, reason, burstActive, keyframeAge, remoteVideoSSRC)
		return
	}

	fmt.Printf("[%s] 📈 browser_recovery_deferred trigger=%s target=webrtc reason=%s burst=%v keyframeAge=%s remoteVideoSSRC=%d\n",
		s.ID, trigger, reason, burstActive, keyframeAge, remoteVideoSSRC)
}

func isBrowserRecoveryTrigger(trigger string) bool {
	switch trigger {
	case "browser-pli", "browser-fir", "ws-request_keyframe", "switch":
		return true
	default:
		return false
	}
}

func (s *Session) logBrowserRecoveryDecision(trigger, action string, burstActive bool, keyframeAge time.Duration, remoteVideoSSRC uint32, reason string) {
	learnedAddr, learnedSource := s.GetLearnedVideoRTCPAddr()
	target := "none"
	if remoteVideoSSRC == 0 {
		target = "missing-ssrc"
	} else if learnedAddr != nil {
		target = fmt.Sprintf("%s/%s", learnedSource, learnedAddr.String())
	} else {
		s.mu.RLock()
		if s.AsteriskVideoAddr != nil {
			target = fmt.Sprintf("negotiated/%s", s.AsteriskVideoAddr.String())
		}
		s.mu.RUnlock()
	}

	if trigger == "ws-request_keyframe" {
		fmt.Printf("[%s] 📈 request_keyframe_handled action=%s burst=%v keyframeAge=%s remoteVideoSSRC=%d target=%s reason=%s\n",
			s.ID, action, burstActive, keyframeAge, remoteVideoSSRC, target, reason)
		return
	}

	fmt.Printf("[%s] 📈 browser_recovery_handled trigger=%s action=%s burst=%v keyframeAge=%s remoteVideoSSRC=%d target=%s reason=%s\n",
		s.ID, trigger, action, burstActive, keyframeAge, remoteVideoSSRC, target, reason)
}

func (s *Session) sendPLIToAsterisk(force bool, trigger string) {
	now := time.Now()
	s.mu.RLock()
	destAddr := s.AsteriskVideoAddr
	conn := s.VideoRTCPConn
	if conn == nil {
		conn = s.VideoRTPConn
	}
	senderSSRC := s.VideoSSRC
	if senderSSRC == 0 {
		senderSSRC = 0x87654321 // match SSRC used for video RTP forwarding
	}
	mediaSSRC := s.RemoteVideoSSRC
	s.mu.RUnlock()

	// Check prerequisites
	if destAddr == nil {
		fmt.Printf("[%s] ⚠️ Cannot send PLI: AsteriskVideoAddr is nil\n", s.ID)
		return
	}
	if conn == nil {
		fmt.Printf("[%s] ⚠️ Cannot send PLI: VideoRTPConn is nil\n", s.ID)
		return
	}
	if mediaSSRC == 0 {
		fmt.Printf("[%s] ⚠️ Cannot send PLI: RemoteVideoSSRC is 0 (not learned yet)\n", s.ID)
		return
	}

	// watchdog-fir path should immediately follow FIR with a PLI burst hint.
	// switch recovery gets exactly one PLI bypass after SIP video is routable.
	allowBypass := force && (trigger == "watchdog-fir" || (trigger == "switch" && s.ConsumeSwitchPLIBypass()))
	if !allowBypass && !s.shouldSendPLIToAsterisk(now, force) {
		if force {
			if trigger == "" {
				trigger = "manual"
			}
			s.logBrowserRecoveryDecision(trigger, "skip", s.IsVideoRecoveryBurstActive(), -1, mediaSSRC, "rate-limited")
			fmt.Printf("[%s] ⏱️ Skipping forced PLI to Asterisk - too frequent (trigger=%s)\n", s.ID, trigger)
		} else {
			fmt.Printf("[%s] ⏱️ Skipping PLI to Asterisk - keyframe is recent or PLI too frequent\n", s.ID)
		}
		return
	}

	// Create Compound RTCP packet: RR + PLI (RFC 3550 requires compound packets)
	rr := &rtcp.ReceiverReport{SSRC: senderSSRC}
	pli := &rtcp.PictureLossIndication{SenderSSRC: senderSSRC, MediaSSRC: mediaSSRC}

	// Marshal compound packet
	out, err := rtcp.Marshal([]rtcp.Packet{rr, pli})
	if err != nil {
		fmt.Printf("[%s] ❌ Error marshalling PLI: %v\n", s.ID, err)
		return
	}

	learnedAddr, learnedSource := s.GetLearnedVideoRTCPAddr()
	useFallback := s.ShouldUseVideoRTCPFallback()

	targets := s.getVideoFeedbackTargets(destAddr, learnedAddr, useFallback)
	if len(targets) == 0 {
		return
	}

	pliCount := 0
	counted := false
	for _, target := range targets {
		if _, err := conn.WriteToUDP(out, target.Addr); err != nil {
			fmt.Printf("[%s] ⚠️ Error sending PLI to %s %s: %v\n", s.ID, target.Label, target.Addr, err)
			continue
		}

		if !counted {
			s.mu.Lock()
			s.PLISent++
			s.LastPLISent = now
			s.LastSipPLISent = now
			pliCount = s.PLISent
			s.mu.Unlock()
			counted = true
		}

		if target.IsPrimary {
			if force {
				if trigger == "" {
					trigger = "manual"
				}
				fmt.Printf("[%s] 🚀 Sent forced PLI #%d to %s %s (source=%s, Sender=%d, Media=%d, trigger=%s)\n",
					s.ID, pliCount, target.Label, target.Addr, learnedSource, senderSSRC, mediaSSRC, trigger)
			} else {
				fmt.Printf("[%s] 🚀 Sent PLI #%d to %s %s (source=%s, Sender=%d, Media=%d)\n",
					s.ID, pliCount, target.Label, target.Addr, learnedSource, senderSSRC, mediaSSRC)
			}
			continue
		}

		if target.Kind == "rtp" {
			fmt.Printf("[%s] 🔄 Sent PLI fallback to RTP port %s:%d (rtcp-mux compatibility)\n",
				s.ID, target.Addr.IP, target.Addr.Port)
		} else {
			fmt.Printf("[%s] 🔄 Sent PLI fallback to RTCP port %s:%d\n",
				s.ID, target.Addr.IP, target.Addr.Port)
		}
	}

}

// SendFIRToAsterisk sends a Full Intra Request to Asterisk as Compound RTCP (RR + FIR)
// RTCP is sent to RTP port + 1 as per RFC 3550, with fallback to RTP port (for rtcp-mux)
func (s *Session) SendFIRToAsterisk() {
	s.mu.Lock()
	destAddr := s.AsteriskVideoAddr
	conn := s.VideoRTCPConn
	if conn == nil {
		conn = s.VideoRTPConn
	}
	senderSSRC := s.VideoSSRC
	if senderSSRC == 0 {
		senderSSRC = 0x87654321 // match SSRC used for video RTP forwarding
	}
	mediaSSRC := s.RemoteVideoSSRC
	currentSeq := s.FIRSeq
	s.FIRSeq++ // uint8 naturally wraps at 256
	s.mu.Unlock()

	// Check prerequisites
	if destAddr == nil {
		fmt.Printf("[%s] ⚠️ Cannot send FIR: AsteriskVideoAddr is nil\n", s.ID)
		return
	}
	if conn == nil {
		fmt.Printf("[%s] ⚠️ Cannot send FIR: VideoRTPConn is nil\n", s.ID)
		return
	}
	if mediaSSRC == 0 {
		fmt.Printf("[%s] ⚠️ Cannot send FIR: RemoteVideoSSRC is 0 (not learned yet)\n", s.ID)
		return
	}

	// Create Compound RTCP packet: RR + FIR (RFC 3550 requires compound packets)
	rr := &rtcp.ReceiverReport{
		SSRC: senderSSRC,
	}
	fir := &rtcp.FullIntraRequest{
		SenderSSRC: senderSSRC,
		MediaSSRC:  mediaSSRC,
		FIR: []rtcp.FIREntry{
			{
				SSRC:           mediaSSRC,
				SequenceNumber: currentSeq,
			},
		},
	}

	// Marshal compound packet
	out, err := rtcp.Marshal([]rtcp.Packet{rr, fir})
	if err != nil {
		fmt.Printf("[%s] ❌ Error marshalling FIR: %v\n", s.ID, err)
		return
	}

	learnedAddr, learnedSource := s.GetLearnedVideoRTCPAddr()
	useFallback := s.ShouldUseVideoRTCPFallback()

	targets := s.getVideoFeedbackTargets(destAddr, learnedAddr, useFallback)
	if len(targets) == 0 {
		return
	}

	firCount := 0
	counted := false
	for _, target := range targets {
		if _, err := conn.WriteToUDP(out, target.Addr); err != nil {
			fmt.Printf("[%s] ⚠️ Error sending FIR to %s %s: %v\n", s.ID, target.Label, target.Addr, err)
			continue
		}

		if !counted {
			s.mu.Lock()
			s.PLISent++
			now := time.Now()
			s.LastPLISent = now
			s.LastSipPLISent = now
			s.LastSipFIRSent = now
			firCount = s.PLISent
			s.mu.Unlock()
			counted = true
		}

		if target.IsPrimary {
			fmt.Printf("[%s] 🚀 Sent FIR #%d to %s %s (source=%s, Sender=%d, Media=%d, Seq=%d)\n",
				s.ID, firCount, target.Label, target.Addr, learnedSource, senderSSRC, mediaSSRC, currentSeq)
			continue
		}

		if target.Kind == "rtp" {
			fmt.Printf("[%s] 🔄 Sent FIR fallback to RTP port %s:%d (rtcp-mux compatibility)\n",
				s.ID, target.Addr.IP, target.Addr.Port)
		} else {
			fmt.Printf("[%s] 🔄 Sent FIR fallback to RTCP port %s:%d\n",
				s.ID, target.Addr.IP, target.Addr.Port)
		}
	}
}

// SendPLItoWebRTC sends a PLI request to the WebRTC browser to request a keyframe
func (s *Session) SendPLItoWebRTC() {
	if s.PeerConnection == nil {
		fmt.Printf("[%s] Cannot forward PLI: PeerConnection is nil\n", s.ID)
		return
	}

	// Get the video track's SSRC from the remote track
	for _, receiver := range s.PeerConnection.GetReceivers() {
		if receiver.Track() != nil && receiver.Track().Kind() == webrtc.RTPCodecTypeVideo {
			ssrc := uint32(receiver.Track().SSRC())

			// Create PLI packet to send to browser
			pli := &rtcp.PictureLossIndication{
				MediaSSRC: ssrc,
			}

			// Write RTCP PLI to the WebRTC peer connection
			if err := s.PeerConnection.WriteRTCP([]rtcp.Packet{pli}); err != nil {
				// Common during early call setup: RTCP is attempted before DTLS transport starts.
				// This is expected and extremely noisy, so suppress this specific case.
				if strings.Contains(err.Error(), "DTLS transport has not started yet") ||
					strings.Contains(err.Error(), "read/write on closed pipe") {
					return
				}
				fmt.Printf("[%s] Error sending PLI to browser: %v\n", s.ID, err)
			} else {
				fmt.Printf("[%s] 🚀 Sent PLI from Asterisk to WebRTC browser/mobile (SSRC=%d)\n", s.ID, ssrc)
			}
			return
		}
	}

	fmt.Printf("[%s] Cannot forward PLI: No video receiver found\n", s.ID)
}

// SendFIRToWebRTC sends a FIR (Full Intra Request) to the WebRTC browser to request a keyframe
func (s *Session) SendFIRToWebRTC() {
	if s.PeerConnection == nil {
		fmt.Printf("[%s] Cannot send FIR: PeerConnection is nil\n", s.ID)
		return
	}

	for _, receiver := range s.PeerConnection.GetReceivers() {
		if receiver.Track() != nil && receiver.Track().Kind() == webrtc.RTPCodecTypeVideo {
			ssrc := uint32(receiver.Track().SSRC())

			// Get and increment FIR sequence number (must be done before creating packet)
			s.mu.Lock()
			currentSeq := s.FIRSeq
			s.FIRSeq++ // uint8 naturally wraps at 256 (0-255)
			s.PLISent++
			s.LastPLISent = time.Now()
			firCount := s.PLISent
			s.mu.Unlock()

			// FIR requires FIREntry with SSRC and SequenceNumber
			// Without FIREntry, Chrome won't count it as a valid FIR
			fir := &rtcp.FullIntraRequest{
				SenderSSRC: ssrc,
				MediaSSRC:  ssrc,
				FIR: []rtcp.FIREntry{
					{
						SSRC:           ssrc,
						SequenceNumber: currentSeq,
					},
				},
			}

			if err := s.PeerConnection.WriteRTCP([]rtcp.Packet{fir}); err != nil {
				if strings.Contains(err.Error(), "read/write on closed pipe") {
					return
				}
				fmt.Printf("[%s] ❌ Error sending FIR to browser: %v\n", s.ID, err)
			} else {
				fmt.Printf("[%s] 📸 Sent FIR #%d to WebRTC browser (SSRC=%d, Seq=%d)\n",
					s.ID, firCount, ssrc, currentSeq)
			}
			return
		}
	}

	fmt.Printf("[%s] Cannot send FIR: No video receiver found\n", s.ID)
}

// SendNACKToWebRTC forwards a NACK (Negative Acknowledgement) to the WebRTC browser
// requesting retransmission of lost packets
func (s *Session) SendNACKToWebRTC(mediaSSRC uint32, nacks []rtcp.NackPair) {
	if s.PeerConnection == nil {
		fmt.Printf("[%s] Cannot forward NACK: PeerConnection is nil\n", s.ID)
		return
	}

	// Create NACK packet to send to browser
	nack := &rtcp.TransportLayerNack{
		MediaSSRC: mediaSSRC,
		Nacks:     nacks,
	}

	// Write RTCP NACK to the WebRTC peer connection
	if err := s.PeerConnection.WriteRTCP([]rtcp.Packet{nack}); err != nil {
		if strings.Contains(err.Error(), "read/write on closed pipe") {
			return
		}
		fmt.Printf("[%s] Error sending NACK to browser: %v\n", s.ID, err)
	} else {
		fmt.Printf("[%s] 🔄 Forwarded NACK to WebRTC browser (MediaSSRC=%d, Nacks=%v)\n", s.ID, mediaSSRC, nacks)
	}
}

// SendNACKToAsterisk sends a NACK to Asterisk requesting retransmission of lost packets
// RTCP is sent to RTP port + 1 as per RFC 3550
func (s *Session) SendNACKToAsterisk(nacks []rtcp.NackPair) {
	s.mu.RLock()
	destAddr := s.AsteriskVideoAddr
	conn := s.VideoRTCPConn
	if conn == nil {
		conn = s.VideoRTPConn
	}
	senderSSRC := s.VideoSSRC
	if senderSSRC == 0 {
		senderSSRC = 0x87654321 // match SSRC used for video RTP forwarding
	}
	mediaSSRC := s.RemoteVideoSSRC
	s.mu.RUnlock()

	if destAddr != nil && conn != nil && mediaSSRC != 0 {
		learnedAddr, learnedSource := s.GetLearnedVideoRTCPAddr()
		useFallback := s.ShouldUseVideoRTCPFallback()

		// Create Compound RTCP packet: RR + NACK (RFC 3550 requires compound packets)
		rr := &rtcp.ReceiverReport{
			SSRC: senderSSRC,
		}
		nack := &rtcp.TransportLayerNack{
			SenderSSRC: senderSSRC,
			MediaSSRC:  mediaSSRC,
			Nacks:      nacks,
		}

		// Marshal compound packet
		out, err := rtcp.Marshal([]rtcp.Packet{rr, nack})
		if err != nil {
			fmt.Printf("[%s] Error marshalling NACK: %v\n", s.ID, err)
			return
		}

		targets := s.getVideoFeedbackTargets(destAddr, learnedAddr, useFallback)
		for _, target := range targets {
			if _, err := conn.WriteToUDP(out, target.Addr); err != nil {
				continue
			}

			if target.IsPrimary {
				fmt.Printf("[%s] 🔄 Sent NACK to %s %s (source=%s, Sender=%d, Media=%d, Nacks=%v)\n",
					s.ID, target.Label, target.Addr, learnedSource, senderSSRC, mediaSSRC, nacks)
				continue
			}

			if target.Kind == "rtp" {
				fmt.Printf("[%s] 🔄 Sent NACK fallback to RTP port %s:%d (rtcp-mux compatibility)\n",
					s.ID, target.Addr.IP, target.Addr.Port)
			} else {
				fmt.Printf("[%s] 🔄 Sent NACK fallback to RTCP port %s:%d\n",
					s.ID, target.Addr.IP, target.Addr.Port)
			}
		}
	}
}
