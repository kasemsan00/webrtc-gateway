package session

import (
	"fmt"
	"strings"
	"time"

	"github.com/pion/rtcp"
)

const (
	pliKeyframeGrace     = 400 * time.Millisecond
	pliMinInterval       = 300 * time.Millisecond
	pliForceMinInterval  = 300 * time.Millisecond
	sipFIRMinInterval    = 1000 * time.Millisecond
	webrtcPLIMinInterval = 400 * time.Millisecond
	webrtcFIRMinInterval = 1000 * time.Millisecond
	browserFIRInterval   = 1000 * time.Millisecond
	browserPLIStale      = 600 * time.Millisecond
	browserFIRStale      = 1500 * time.Millisecond
	// Keep requesting browser IDRs after the queue/auto-200 IDR so a SIP
	// decoder that answers several seconds later (Linphone after ring) is
	// not stuck on P-frames. First-packet PLI still stops on the first IDR.
	lateJoinBrowserPLIWindow = 12 * time.Second
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

	if isBrowserRecoveryTrigger(trigger) && trigger != "switch" {
		if s.RequestSIPVideoIDRReplay(trigger) {
			s.mu.RLock()
			lastKeyframe := s.LastKeyframe
			remoteVideoSSRC := s.RemoteVideoSSRC
			burstActive := s.VideoRecoveryBurstEnabled && !s.VideoRecoveryBurstUntil.IsZero() && now.Before(s.VideoRecoveryBurstUntil)
			s.mu.RUnlock()
			keyframeAge := time.Duration(-1)
			if !lastKeyframe.IsZero() {
				keyframeAge = now.Sub(lastKeyframe)
			}
			s.logBrowserRecoveryDecision(trigger, "replay", burstActive, keyframeAge, remoteVideoSSRC, "cached-idr")
			return "replay"
		}
	}

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
	if forceStartupRecovery && !lastKeyframe.IsZero() && now.Sub(lastKeyframe) < pliStale {
		// A fresh IDR already landed. Forcing another FIR/PLI during the
		// startup window over-drives the SIP encoder (live calls were
		// requesting keyframes 19ms after a complete IDR).
		forceStartupRecovery = false
	}
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
	s.PendingBrowserKeyframeRequestEpoch = s.MediaEpoch
}

func (s *Session) HasPendingBrowserKeyframeRequest() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PendingBrowserKeyframeRequest
}

func (s *Session) clearPendingBrowserKeyframeRequestLocked() {
	s.PendingBrowserKeyframeRequest = false
	s.PendingBrowserKeyframeRequestAt = time.Time{}
	s.PendingBrowserKeyframeRequestEpoch = 0
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
	s.sendSIPFeedback("pli", force, trigger, nil)
}

// SendFIRToAsterisk sends a Full Intra Request to Asterisk as Compound RTCP (RR + FIR)
// RTCP is sent to RTP port + 1 as per RFC 3550, with fallback to RTP port (for rtcp-mux)
func (s *Session) SendFIRToAsterisk() {
	s.sendSIPFeedback("fir", false, "", nil)
}

// SendPLItoWebRTC sends a PLI request to the WebRTC browser to request a keyframe
func (s *Session) SendPLItoWebRTC() {
	s.sendWebRTCFeedback("pli", nil)
}

// SendFIRToWebRTC sends a FIR (Full Intra Request) to the WebRTC browser to request a keyframe
func (s *Session) SendFIRToWebRTC() {
	s.sendWebRTCFeedback("fir", nil)
}

// KickUplinkKeyframeOnRemoteJoinIfNeeded claims the first-join uplink keyframe
// kick and sends FIR + PLI to the WebRTC browser. Returns true when claimed.
// Safe without a PeerConnection (feedback becomes a no-op); claim still sticks.
func (s *Session) KickUplinkKeyframeOnRemoteJoinIfNeeded() bool {
	if !s.TryClaimUplinkKeyframeKickOnRemoteJoin() {
		return false
	}
	fmt.Printf("[%s] 📈 uplink_keyframe_kick reason=remote-ssrc-learn\n", s.ID)
	s.SendFIRToWebRTC()
	s.SendPLItoWebRTC()
	return true
}

// KickUplinkKeyframeOnFirstSIPRTCPIfNeeded claims the first SIP video SR/RR
// and requests a fresh browser IDR. Queue wait-video often claims the
// remote-join kick before the human SIP endpoint answers.
func (s *Session) KickUplinkKeyframeOnFirstSIPRTCPIfNeeded() bool {
	if !s.TryClaimUplinkKeyframeKickOnFirstSIPRTCP() {
		return false
	}
	s.KickUplinkKeyframeForSIPDecoder("sip-rtcp-first")
	return true
}

// RecordUplinkKeyframe marks that a WebRTC→SIP IDR was actually written to SIP.
// Must not be called while holding s.mu.
func (s *Session) RecordUplinkKeyframe() {
	s.mu.Lock()
	s.LastUplinkKeyframe = time.Now()
	s.mu.Unlock()
}

// HasUplinkKeyframe reports whether any WebRTC→SIP IDR has been forwarded.
func (s *Session) HasUplinkKeyframe() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.LastUplinkKeyframe.IsZero()
}

// HasRecentUplinkKeyframe reports whether an uplink IDR was forwarded within maxAge.
func (s *Session) HasRecentUplinkKeyframe(maxAge time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.LastUplinkKeyframe.IsZero() && time.Since(s.LastUplinkKeyframe) <= maxAge
}

// HasUplinkKeyframeSince reports whether an uplink IDR was forwarded at or after t.
func (s *Session) HasUplinkKeyframeSince(t time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.LastUplinkKeyframe.IsZero() && !s.LastUplinkKeyframe.Before(t)
}

// KickUplinkKeyframeForSIPDecoder requests a fresh browser IDR so a SIP decoder
// that just became reachable (200 OK / first RTP dest) is not stuck on P-frames.
func (s *Session) KickUplinkKeyframeForSIPDecoder(reason string) {
	kickAt := time.Now()
	fmt.Printf("[%s] 📈 uplink_keyframe_kick reason=%s\n", s.ID, reason)
	s.SendFIRToWebRTC()
	s.SendPLItoWebRTC()
	go func() {
		for i := 0; i < 3; i++ {
			if s.GetState() == StateEnded {
				return
			}
			if s.HasUplinkKeyframeSince(kickAt) {
				fmt.Printf("[%s] Stopping %s PLI burst - uplink IDR delivered\n", s.ID, reason)
				return
			}
			time.Sleep(300 * time.Millisecond)
			s.SendPLItoWebRTC()
		}
	}()
}

// ShouldStopStartupBrowserPLI is true once the browser encoder is producing
// parameter sets and at least one IDR. Used by the short first-packet burst.
func (s *Session) ShouldStopStartupBrowserPLI() bool {
	return s.HasCachedSPSPPS() && s.HasUplinkKeyframe()
}

// ShouldStopPeriodicBrowserPLI is true only after an uplink IDR has been
// forwarded and the late-join window since SIP video dest-ready has elapsed.
// Queue auto-answer IDRs must not stop periodic PLI before Linphone answers.
func (s *Session) ShouldStopPeriodicBrowserPLI() bool {
	if !s.ShouldStopStartupBrowserPLI() {
		return false
	}
	s.mu.RLock()
	readyAt := s.sipVideoDestReadyAt
	s.mu.RUnlock()
	if readyAt.IsZero() {
		return false
	}
	return time.Since(readyAt) >= lateJoinBrowserPLIWindow
}

// ShouldContinueSwitchFeedbackBurst reports whether delayed @switch FIR/PLI
// retries are still useful. Immediate kick is sent separately.
func (s *Session) ShouldContinueSwitchFeedbackBurst(generation int, mediaEpoch uint64, requireAuthority bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.State == StateEnded || s.State == StateReconnecting {
		return false
	}
	if requireAuthority && (s.MediaEpoch != mediaEpoch || s.SwitchGeneration != generation) {
		return false
	}
	return !s.SwitchFeedbackBurstSatisfied
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
	rtpConn := s.VideoRTPConn
	rtcpConn := s.VideoRTCPConn
	senderSSRC := s.VideoSSRC
	if senderSSRC == 0 {
		senderSSRC = 0x87654321 // match SSRC used for video RTP forwarding
	}
	mediaSSRC := s.RemoteVideoSSRC
	s.mu.RUnlock()
	if rtcpConn == nil {
		rtcpConn = rtpConn
	}
	if rtpConn == nil {
		rtpConn = rtcpConn
	}
	conn := rtcpConn
	if conn == nil {
		conn = rtpConn
	}

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
			writeConn := feedbackConnForTarget(target, rtpConn, rtcpConn)
			if writeConn == nil {
				writeConn = conn
			}
			if _, err := writeConn.WriteToUDP(out, target.Addr); err != nil {
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
