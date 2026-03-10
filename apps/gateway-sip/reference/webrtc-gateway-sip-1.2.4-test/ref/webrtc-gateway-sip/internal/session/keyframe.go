package session

import (
	"fmt"
	"net"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// SendPLIToAsterisk sends a Picture Loss Indication to Asterisk as Compound RTCP (RR + PLI)
// RTCP is sent to RTP port + 1 as per RFC 3550, with fallback to RTP port (for rtcp-mux)
func (s *Session) SendPLIToAsterisk() {
	s.mu.RLock()
	destAddr := s.AsteriskVideoAddr
	conn := s.VideoRTPConn
	senderSSRC := s.VideoSSRC
	if senderSSRC == 0 {
		senderSSRC = 0x87654321 // match SSRC used for video RTP forwarding
	}
	mediaSSRC := s.RemoteVideoSSRC
	s.mu.RUnlock()

	// Check prerequisites
	if destAddr == nil {
		fmt.Printf("[Session %s] ⚠️ Cannot send PLI: AsteriskVideoAddr is nil\n", s.ID)
		return
	}
	if conn == nil {
		fmt.Printf("[Session %s] ⚠️ Cannot send PLI: VideoRTPConn is nil\n", s.ID)
		return
	}
	if mediaSSRC == 0 {
		fmt.Printf("[Session %s] ⚠️ Cannot send PLI: RemoteVideoSSRC is 0 (not learned yet)\n", s.ID)
		return
	}

	// Create Compound RTCP packet: RR + PLI (RFC 3550 requires compound packets)
	rr := &rtcp.ReceiverReport{
		SSRC: senderSSRC,
	}
	pli := &rtcp.PictureLossIndication{
		SenderSSRC: senderSSRC,
		MediaSSRC:  mediaSSRC,
	}

	// Marshal compound packet
	out, err := rtcp.Marshal([]rtcp.Packet{rr, pli})
	if err != nil {
		fmt.Printf("[Session %s] ❌ Error marshalling PLI: %v\n", s.ID, err)
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
		fmt.Printf("[Session %s] ⚠️ Error sending PLI to %s RTCP addr %s: %v\n", s.ID, primaryLabel, primaryAddr, err)
	} else {
		s.mu.Lock()
		s.PLISent++
		s.LastPLISent = time.Now()
		pliCount := s.PLISent
		s.mu.Unlock()

		fmt.Printf("[Session %s] 🚀 Sent PLI #%d to %s RTCP addr %s (source=%s, Sender=%d, Media=%d)\n",
			s.ID, pliCount, primaryLabel, primaryAddr, learnedSource, senderSSRC, mediaSSRC)
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
			fmt.Printf("[Session %s] 🔄 Sent PLI fallback to RTCP port %s:%d\n",
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
			fmt.Printf("[Session %s] 🔄 Sent PLI fallback to RTP port %s:%d (rtcp-mux compatibility)\n",
				s.ID, destAddr.IP, rtpAddr.Port)
		}
	}

}

// SendFIRToAsterisk sends a Full Intra Request to Asterisk as Compound RTCP (RR + FIR)
// RTCP is sent to RTP port + 1 as per RFC 3550, with fallback to RTP port (for rtcp-mux)
func (s *Session) SendFIRToAsterisk() {
	s.mu.Lock()
	destAddr := s.AsteriskVideoAddr
	conn := s.VideoRTPConn
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
		fmt.Printf("[Session %s] ⚠️ Cannot send FIR: AsteriskVideoAddr is nil\n", s.ID)
		return
	}
	if conn == nil {
		fmt.Printf("[Session %s] ⚠️ Cannot send FIR: VideoRTPConn is nil\n", s.ID)
		return
	}
	if mediaSSRC == 0 {
		fmt.Printf("[Session %s] ⚠️ Cannot send FIR: RemoteVideoSSRC is 0 (not learned yet)\n", s.ID)
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
		fmt.Printf("[Session %s] ❌ Error marshalling FIR: %v\n", s.ID, err)
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
		fmt.Printf("[Session %s] ⚠️ Error sending FIR to %s RTCP addr %s: %v\n", s.ID, primaryLabel, primaryAddr, err)
	} else {
		s.mu.Lock()
		s.PLISent++
		s.LastPLISent = time.Now()
		firCount := s.PLISent
		s.mu.Unlock()

		fmt.Printf("[Session %s] 🚀 Sent FIR #%d to %s RTCP addr %s (source=%s, Sender=%d, Media=%d, Seq=%d)\n",
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
			fmt.Printf("[Session %s] 🔄 Sent FIR fallback to RTCP port %s:%d\n",
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
			fmt.Printf("[Session %s] 🔄 Sent FIR fallback to RTP port %s:%d (rtcp-mux compatibility)\n",
				s.ID, destAddr.IP, rtpAddr.Port)
		}
	}
}

// SendPLItoWebRTC sends a PLI request to the WebRTC browser to request a keyframe
func (s *Session) SendPLItoWebRTC() {
	if s.PeerConnection == nil {
		fmt.Printf("[Session %s] Cannot forward PLI: PeerConnection is nil\n", s.ID)
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
				fmt.Printf("[Session %s] Error sending PLI to browser: %v\n", s.ID, err)
			} else {
				fmt.Printf("[Session %s] 🚀 Sent PLI from Asterisk to WebRTC browser/mobile (SSRC=%d)\n", s.ID, ssrc)
			}
			return
		}
	}

	fmt.Printf("[Session %s] Cannot forward PLI: No video receiver found\n", s.ID)
}

// SendFIRToWebRTC sends a FIR (Full Intra Request) to the WebRTC browser to request a keyframe
func (s *Session) SendFIRToWebRTC() {
	if s.PeerConnection == nil {
		fmt.Printf("[Session %s] Cannot send FIR: PeerConnection is nil\n", s.ID)
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
				fmt.Printf("[Session %s] ❌ Error sending FIR to browser: %v\n", s.ID, err)
			} else {
				fmt.Printf("[Session %s] 📸 Sent FIR #%d to WebRTC browser (SSRC=%d, Seq=%d)\n",
					s.ID, firCount, ssrc, currentSeq)
			}
			return
		}
	}

	fmt.Printf("[Session %s] Cannot send FIR: No video receiver found\n", s.ID)
}

// SendNACKToWebRTC forwards a NACK (Negative Acknowledgement) to the WebRTC browser
// requesting retransmission of lost packets
func (s *Session) SendNACKToWebRTC(mediaSSRC uint32, nacks []rtcp.NackPair) {
	if s.PeerConnection == nil {
		fmt.Printf("[Session %s] Cannot forward NACK: PeerConnection is nil\n", s.ID)
		return
	}

	// Create NACK packet to send to browser
	nack := &rtcp.TransportLayerNack{
		MediaSSRC: mediaSSRC,
		Nacks:     nacks,
	}

	// Write RTCP NACK to the WebRTC peer connection
	if err := s.PeerConnection.WriteRTCP([]rtcp.Packet{nack}); err != nil {
		fmt.Printf("[Session %s] Error sending NACK to browser: %v\n", s.ID, err)
	} else {
		fmt.Printf("[Session %s] 🔄 Forwarded NACK to WebRTC browser (MediaSSRC=%d, Nacks=%v)\n", s.ID, mediaSSRC, nacks)
	}
}

// SendNACKToAsterisk sends a NACK to Asterisk requesting retransmission of lost packets
// RTCP is sent to RTP port + 1 as per RFC 3550
func (s *Session) SendNACKToAsterisk(nacks []rtcp.NackPair) {
	s.mu.RLock()
	destAddr := s.AsteriskVideoAddr
	conn := s.VideoRTPConn
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
			fmt.Printf("[Session %s] Error marshalling NACK: %v\n", s.ID, err)
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
			fmt.Printf("[Session %s] 🔄 Sent NACK to %s RTCP addr %s (source=%s, Sender=%d, Media=%d, Nacks=%v)\n",
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
				fmt.Printf("[Session %s] 🔄 Sent NACK fallback to RTCP port %s:%d\n",
					s.ID, fallbackRtcpAddr.IP, fallbackRtcpAddr.Port)
			}
		}

		rtpAddr := &net.UDPAddr{
			IP:   destAddr.IP,
			Port: destAddr.Port,
		}
		if learnedAddr == nil || learnedAddr.Port != rtpAddr.Port || !learnedAddr.IP.Equal(rtpAddr.IP) {
			if _, err := conn.WriteToUDP(out, rtpAddr); err == nil {
				fmt.Printf("[Session %s] 🔄 Sent NACK fallback to RTP port %s:%d (rtcp-mux compatibility)\n",
					s.ID, destAddr.IP, rtpAddr.Port)
			}
		}
	}
}
