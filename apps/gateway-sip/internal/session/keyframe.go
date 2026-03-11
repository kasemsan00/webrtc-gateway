package session

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

const (
	pliKeyframeGrace    = 500 * time.Millisecond
	pliMinInterval      = 200 * time.Millisecond
	pliForceMinInterval = 200 * time.Millisecond
	browserFIRInterval  = 2000 * time.Millisecond
	browserPLIStale     = 1500 * time.Millisecond
	browserFIRStale     = 3000 * time.Millisecond
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
func (s *Session) SendBrowserRecoveryToAsterisk(trigger string) {
	now := time.Now()

	s.mu.RLock()
	lastKeyframe := s.LastKeyframe
	lastFIRReq := s.LastSipFIRSent
	s.mu.RUnlock()

	if !lastKeyframe.IsZero() {
		age := now.Sub(lastKeyframe)
		if age < browserPLIStale {
			return
		}
		if age >= browserFIRStale {
			if lastFIRReq.IsZero() || now.Sub(lastFIRReq) >= browserFIRInterval {
				s.SendFIRToAsterisk()
				return
			}
		}
	} else {
		if lastFIRReq.IsZero() || now.Sub(lastFIRReq) >= browserFIRInterval {
			s.SendFIRToAsterisk()
			return
		}
	}

	s.SendPLIToAsteriskForced(trigger)
}

func (s *Session) sendPLIToAsterisk(force bool, trigger string) {
	now := time.Now()
	// watchdog-fir path should immediately follow FIR with a PLI burst hint,
	// so allow one bypass of interval throttling.
	allowBypass := force && trigger == "watchdog-fir"
	if !allowBypass && !s.shouldSendPLIToAsterisk(now, force) {
		if force {
			if trigger == "" {
				trigger = "manual"
			}
			fmt.Printf("[%s] ⏱️ Skipping forced PLI to Asterisk - too frequent (trigger=%s)\n", s.ID, trigger)
		} else {
			fmt.Printf("[%s] ⏱️ Skipping PLI to Asterisk - keyframe is recent or PLI too frequent\n", s.ID)
		}
		return
	}

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

	// Primary: Send to learned RTCP address if available; otherwise default RTP+1
	primaryAddr := learnedAddr
	primaryLabel := "learned"
	if primaryAddr == nil {
		primaryAddr = &net.UDPAddr{
			IP:   destAddr.IP,
			Port: destAddr.Port + 1,
		}
		primaryLabel = "rtp+1"
	}

	if _, err := conn.WriteToUDP(out, primaryAddr); err != nil {
		fmt.Printf("[%s] ⚠️ Error sending PLI to %s RTCP addr %s: %v\n", s.ID, primaryLabel, primaryAddr, err)
	} else {
		s.mu.Lock()
		s.PLISent++
		s.LastPLISent = now
		s.LastSipPLISent = now
		pliCount := s.PLISent
		s.mu.Unlock()

		if force {
			if trigger == "" {
				trigger = "manual"
			}
			fmt.Printf("[%s] 🚀 Sent forced PLI #%d to %s RTCP addr %s (source=%s, Sender=%d, Media=%d, trigger=%s)\n",
				s.ID, pliCount, primaryLabel, primaryAddr, learnedSource, senderSSRC, mediaSSRC, trigger)
		} else {
			fmt.Printf("[%s] 🚀 Sent PLI #%d to %s RTCP addr %s (source=%s, Sender=%d, Media=%d)\n",
				s.ID, pliCount, primaryLabel, primaryAddr, learnedSource, senderSSRC, mediaSSRC)
		}
	}

	if !useFallback {
		return
	}

	// Fallback: also send to RTP+1 if learned != RTP+1
	fallbackRtcpAddr := &net.UDPAddr{
		IP:   destAddr.IP,
		Port: destAddr.Port + 1,
	}
	if learnedAddr == nil || learnedAddr.Port != fallbackRtcpAddr.Port || !learnedAddr.IP.Equal(fallbackRtcpAddr.IP) {
		if _, err := conn.WriteToUDP(out, fallbackRtcpAddr); err == nil {
			fmt.Printf("[%s] 🔄 Sent PLI fallback to RTCP port %s:%d\n",
				s.ID, fallbackRtcpAddr.IP, fallbackRtcpAddr.Port)
		}
	}

	// Fallback: also send to RTP port for mux compatibility
	rtpAddr := &net.UDPAddr{
		IP:   destAddr.IP,
		Port: destAddr.Port,
	}
	if learnedAddr == nil || learnedAddr.Port != rtpAddr.Port || !learnedAddr.IP.Equal(rtpAddr.IP) {
		if _, err := conn.WriteToUDP(out, rtpAddr); err == nil {
			fmt.Printf("[%s] 🔄 Sent PLI fallback to RTP port %s:%d (rtcp-mux compatibility)\n",
				s.ID, destAddr.IP, rtpAddr.Port)
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

	// Primary: Send to learned RTCP address if available; otherwise default RTP+1
	primaryAddr := learnedAddr
	primaryLabel := "learned"
	if primaryAddr == nil {
		primaryAddr = &net.UDPAddr{
			IP:   destAddr.IP,
			Port: destAddr.Port + 1,
		}
		primaryLabel = "rtp+1"
	}

	if _, err := conn.WriteToUDP(out, primaryAddr); err != nil {
		fmt.Printf("[%s] ⚠️ Error sending FIR to %s RTCP addr %s: %v\n", s.ID, primaryLabel, primaryAddr, err)
	} else {
		s.mu.Lock()
		s.PLISent++
		now := time.Now()
		s.LastPLISent = now
		s.LastSipPLISent = now
		s.LastSipFIRSent = now
		firCount := s.PLISent
		s.mu.Unlock()

		fmt.Printf("[%s] 🚀 Sent FIR #%d to %s RTCP addr %s (source=%s, Sender=%d, Media=%d, Seq=%d)\n",
			s.ID, firCount, primaryLabel, primaryAddr, learnedSource, senderSSRC, mediaSSRC, currentSeq)
	}

	if !useFallback {
		return
	}

	// Fallback: also send to RTP+1 if learned != RTP+1
	fallbackRtcpAddr := &net.UDPAddr{
		IP:   destAddr.IP,
		Port: destAddr.Port + 1,
	}
	if learnedAddr == nil || learnedAddr.Port != fallbackRtcpAddr.Port || !learnedAddr.IP.Equal(fallbackRtcpAddr.IP) {
		if _, err := conn.WriteToUDP(out, fallbackRtcpAddr); err == nil {
			fmt.Printf("[%s] 🔄 Sent FIR fallback to RTCP port %s:%d\n",
				s.ID, fallbackRtcpAddr.IP, fallbackRtcpAddr.Port)
		}
	}

	// Fallback: also send to RTP port for mux compatibility
	rtpAddr := &net.UDPAddr{
		IP:   destAddr.IP,
		Port: destAddr.Port,
	}
	if learnedAddr == nil || learnedAddr.Port != rtpAddr.Port || !learnedAddr.IP.Equal(rtpAddr.IP) {
		if _, err := conn.WriteToUDP(out, rtpAddr); err == nil {
			fmt.Printf("[%s] 🔄 Sent FIR fallback to RTP port %s:%d (rtcp-mux compatibility)\n",
				s.ID, destAddr.IP, rtpAddr.Port)
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

		primaryAddr := learnedAddr
		primaryLabel := "learned"
		if primaryAddr == nil {
			primaryAddr = &net.UDPAddr{
				IP:   destAddr.IP,
				Port: destAddr.Port + 1,
			}
			primaryLabel = "rtp+1"
		}

		if _, err := conn.WriteToUDP(out, primaryAddr); err == nil {
			fmt.Printf("[%s] 🔄 Sent NACK to %s RTCP addr %s (source=%s, Sender=%d, Media=%d, Nacks=%v)\n",
				s.ID, primaryLabel, primaryAddr, learnedSource, senderSSRC, mediaSSRC, nacks)
		}

		if !useFallback {
			return
		}

		fallbackRtcpAddr := &net.UDPAddr{
			IP:   destAddr.IP,
			Port: destAddr.Port + 1,
		}
		if learnedAddr == nil || learnedAddr.Port != fallbackRtcpAddr.Port || !learnedAddr.IP.Equal(fallbackRtcpAddr.IP) {
			if _, err := conn.WriteToUDP(out, fallbackRtcpAddr); err == nil {
				fmt.Printf("[%s] 🔄 Sent NACK fallback to RTCP port %s:%d\n",
					s.ID, fallbackRtcpAddr.IP, fallbackRtcpAddr.Port)
			}
		}

		rtpAddr := &net.UDPAddr{
			IP:   destAddr.IP,
			Port: destAddr.Port,
		}
		if learnedAddr == nil || learnedAddr.Port != rtpAddr.Port || !learnedAddr.IP.Equal(rtpAddr.IP) {
			if _, err := conn.WriteToUDP(out, rtpAddr); err == nil {
				fmt.Printf("[%s] 🔄 Sent NACK fallback to RTP port %s:%d (rtcp-mux compatibility)\n",
					s.ID, destAddr.IP, rtpAddr.Port)
			}
		}
	}
}
