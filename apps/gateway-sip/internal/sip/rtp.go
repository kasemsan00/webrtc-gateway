package sip

import (
	"fmt"
	"net"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
)

// startRTPListener starts an RTP listener and returns the port
// Uses configurable port range from environment variables
func (s *Server) startRTPListener() (int, error) {
	// Try ports in configured range
	for port := s.rtpConfig.PortMin; port <= s.rtpConfig.PortMax; port++ {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{
			Port: port,
			IP:   net.ParseIP("0.0.0.0"),
		})
		if err != nil {
			continue
		}

		go s.handleRTPPackets(conn)

		udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
		if !ok {
			conn.Close()
			continue
		}

		fmt.Printf("RTP listener started on port: %d\n", udpAddr.Port)
		return udpAddr.Port, nil
	}

	return 0, fmt.Errorf("failed to find available RTP port in range %d-%d", s.rtpConfig.PortMin, s.rtpConfig.PortMax)
}

// handleRTPPackets reads RTP packets and writes them to the audio track
func (s *Server) handleRTPPackets(conn *net.UDPConn) {
	buffer := make([]byte, s.rtpConfig.BufferSize)
	packetCount := 0

	fmt.Printf("🎧 [RTP Audio] Handler started, listening for packets from Asterisk...\n")

	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("❌ [RTP Audio] Error reading packet: %v\n", err)
			return
		}

		packetCount++
		if packetCount <= 5 || packetCount%1000 == 0 {
			fmt.Printf("📦 [RTP Audio] Received packet #%d: %d bytes from %s\n", packetCount, n, addr)
		}

		// Check if audioTrack is nil
		if s.audioTrack == nil {
			if packetCount == 1 {
				fmt.Printf("⚠️ [RTP Audio] AudioTrack is nil, skipping packet\n")
			}
			continue
		}

		if _, err := s.audioTrack.Write(buffer[:n]); err != nil {
			fmt.Printf("❌ [RTP Audio] Error writing to WebRTC track: %v\n", err)
			return
		}

		if packetCount <= 5 {
			fmt.Printf("✅ [RTP Audio] Packet #%d forwarded to WebRTC successfully\n", packetCount)
		}
	}
}

// startRTPListenerForSession starts audio and video RTP listeners for a session
func (s *Server) startRTPListenerForSession(sess *session.Session) (int, error) {
	// Try ports in configured range (need 4 ports: audioRTP, audioRTCP, videoRTP, videoRTCP)
	for port := s.rtpConfig.PortMin; port <= s.rtpConfig.PortMax-3; port += 4 {
		// Create audio RTP listener (port)
		audioConn, err := net.ListenUDP("udp", &net.UDPAddr{
			Port: port,
			IP:   net.ParseIP("0.0.0.0"),
		})
		if err != nil {
			continue
		}

		// Create audio RTCP listener (port + 1)
		audioRTCPPort := port + 1
		audioRTCPConn, err := net.ListenUDP("udp", &net.UDPAddr{
			Port: audioRTCPPort,
			IP:   net.ParseIP("0.0.0.0"),
		})
		if err != nil {
			audioConn.Close()
			continue
		}

		// Create video RTP listener (port + 2)
		videoPort := port + 2
		videoConn, err := net.ListenUDP("udp", &net.UDPAddr{
			Port: videoPort,
			IP:   net.ParseIP("0.0.0.0"),
		})
		if err != nil {
			audioConn.Close()
			audioRTCPConn.Close()
			continue
		}

		// Create video RTCP listener (port + 3)
		videoRTCPPort := port + 3
		videoRTCPConn, err := net.ListenUDP("udp", &net.UDPAddr{
			Port: videoRTCPPort,
			IP:   net.ParseIP("0.0.0.0"),
		})
		if err != nil {
			audioConn.Close()
			audioRTCPConn.Close()
			videoConn.Close()
			continue
		}

		// Store connections in session
		sess.SetRTPConnection(audioConn, port)
		sess.SetAudioRTCPConnection(audioRTCPConn, audioRTCPPort)
		sess.SetVideoRTPConnection(videoConn, videoPort)
		sess.SetVideoRTCPConnection(videoRTCPConn, videoRTCPPort)

		// Start forwarding RTP to session's tracks
		go s.handleAudioRTPPacketsForSession(audioConn, sess)
		go s.handleVideoRTPPacketsForSession(videoConn, sess)

		// Start forwarding RTCP (dedicated ports) to session handlers
		go s.handleAudioRTCPPacketsForSession(audioRTCPConn, sess)
		go s.handleVideoRTCPPacketsForSession(videoRTCPConn, sess)

		// Start periodic PLI sender for fast video start
		go s.startPeriodicPLIForSession(sess)
		// Start keyframe watchdog to recover from stalled decoders (iOS VideoToolbox)
		go s.startKeyframeWatchdogForSession(sess)

		fmt.Printf("[%s] 📞 Audio RTP listener started on port: %d\n", sess.ID, port)
		fmt.Printf("[%s] 📞 Audio RTCP listener started on port: %d\n", sess.ID, audioRTCPPort)
		fmt.Printf("[%s] 📞 Video RTP listener started on port: %d\n", sess.ID, videoPort)
		fmt.Printf("[%s] 📞 Video RTCP listener started on port: %d\n", sess.ID, videoRTCPPort)
		return port, nil
	}

	return 0, fmt.Errorf("failed to find available RTP port in range %d-%d", s.rtpConfig.PortMin, s.rtpConfig.PortMax)
}

// handleAudioRTPPacketsForSession reads audio RTP packets and writes them to a session's audio track
func (s *Server) handleAudioRTPPacketsForSession(conn *net.UDPConn, sess *session.Session) {
	buffer := make([]byte, s.rtpConfig.BufferSize)
	packetCount := 0

	fmt.Printf("[%s] Audio RTP handler started, AudioTrack is nil: %v\n", sess.ID, sess.AudioTrack == nil)

	// Create ICE credentials for STUN response
	iceCreds := &ICECredentials{
		LocalUfrag: sess.ICEUfrag,
		LocalPwd:   sess.ICEPwd,
	}

	// Track last DTMF event to avoid duplicate notifications
	var lastDTMFEvent uint8 = 255 // Invalid value
	var lastDTMFEnded bool = true

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if sess.GetState() == session.StateEnded {
				return
			}
			fmt.Printf("[%s] Error reading audio RTP packet: %v\n", sess.ID, err)
			return
		}

		// Handle STUN packets (ICE connectivity check)
		if HandleSTUNPacket(conn, buffer[:n], remoteAddr, iceCreds, sess.ID, "audio") {
			continue
		}

		// Check if this is an RTP packet (minimum 12 bytes header)
		if n < 12 {
			continue
		}

		// Check RTP version (must be 2)
		version := (buffer[0] >> 6) & 0x03
		if version != 2 {
			continue
		}

		// Learn symmetric RTP endpoint from actual RTP source (for NAT/symmetric RTP handling)
		// This ensures RTP forwarding goes to the correct port even if it differs from SDP
		packetCount++
		if packetCount == 1 || packetCount <= 5 {
			sess.UpdateAsteriskAudioEndpointFromRTP(remoteAddr)
		}

		// Check payload type for DTMF (telephone-event, PT 101)
		payloadType := buffer[1] & 0x7F
		if payloadType == DTMFPayloadType {
			// Parse DTMF event
			packet := &rtp.Packet{}
			if err := packet.Unmarshal(buffer[:n]); err == nil {
				dtmfEvent, isEnd, err := ParseDTMFFromRTP(packet)
				if err == nil && dtmfEvent != nil {
					// Only notify on new events or when event ends (to avoid duplicates)
					if dtmfEvent.Event != lastDTMFEvent || (isEnd && !lastDTMFEnded) {
						digitName := GetDigitName(dtmfEvent.Event)
						fmt.Printf("📞 [%s] DTMF received from SIP: '%s' (event=%d, end=%v, duration=%d)\n",
							sess.ID, digitName, dtmfEvent.Event, isEnd, dtmfEvent.Duration)

						if dtmfEvent.Event != lastDTMFEvent {
							s.logEvent(&logstore.Event{
								Timestamp: time.Now(),
								SessionID: sess.ID,
								Category:  "sip",
								Name:      "sip_dtmf_received",
								Data: map[string]interface{}{
									"digits":   digitName,
									"event":    dtmfEvent.Event,
									"duration": dtmfEvent.Duration,
								},
							})
						}

						// Notify WebSocket client only on first packet of new digit
						if dtmfEvent.Event != lastDTMFEvent && s.dtmfNotifier != nil {
							s.dtmfNotifier.NotifyDTMF(sess.ID, digitName)
						}

						lastDTMFEvent = dtmfEvent.Event
						lastDTMFEnded = isEnd
					}
				}
			}
			// Don't forward DTMF packets to WebRTC audio track (they're events, not audio)
			continue
		}

		// Regular audio RTP packet (packetCount already incremented above)
		if packetCount <= 5 || packetCount%1000 == 0 {
			fmt.Printf("[%s] Audio RTP packet #%d received: %d bytes from %s\n", sess.ID, packetCount, n, remoteAddr.String())
		}

		if sess.AudioTrack != nil {
			// CRITICAL: Rewrite RTP payload type from SIP side to WebRTC side
			// SIP may use PT=107 (or other), but WebRTC expects PT=111 (registered in MediaEngine)
			if sess.SIPOpusPT > 0 && sess.SIPOpusPT != 111 {
				// Parse RTP packet to rewrite PT
				packet := &rtp.Packet{}
				if err := packet.Unmarshal(buffer[:n]); err == nil {
					originalPT := packet.Header.PayloadType

					// Rewrite non-DTMF audio packets (DTMF is always PT=101)
					if originalPT != DTMFPayloadType && originalPT == sess.SIPOpusPT {
						packet.Header.PayloadType = 111 // WebRTC side Opus PT

						if packetCount <= 5 {
							fmt.Printf("[%s] 🔄 Rewrite audio PT: %d → 111 (packet #%d)\n", sess.ID, originalPT, packetCount)
						}

						// Re-marshal and write
						if rewrittenBuf, err := packet.Marshal(); err == nil {
							if _, err := sess.AudioTrack.Write(rewrittenBuf); err != nil {
								fmt.Printf("[%s] Error writing rewritten audio to track: %v\n", sess.ID, err)
								return
							}
							continue
						}
					}
				}
			}

			// Passthrough mode (PT already matches or rewrite failed)
			if _, err := sess.AudioTrack.Write(buffer[:n]); err != nil {
				fmt.Printf("[%s] Error writing to audio track: %v\n", sess.ID, err)
				return
			}
		} else if packetCount == 1 {
			fmt.Printf("[%s] WARNING: AudioTrack is nil, cannot forward audio RTP!\n", sess.ID)
		}
	}
}

// handleVideoRTPPacketsForSession reads video RTP packets and writes them to a session's video track
func (s *Server) handleVideoRTPPacketsForSession(conn *net.UDPConn, sess *session.Session) {
	buffer := make([]byte, s.rtpConfig.BufferSize)
	packetCount := 0
	rtcpCount := 0
	var lastSeq uint16
	haveLastSeq := false
	seqGapEvents := 0
	seqGapPackets := 0
	seqOutOfOrder := 0
	seqDuplicates := 0
	lastGapRecovery := time.Time{}

	const (
		burstGapTrigger        = 8
		gapRecoveryMinInterval = 1200 * time.Millisecond
		startupPLIAttempts     = 3
		startupPLIInterval     = 250 * time.Millisecond
		startupKeyframeFresh   = 1200 * time.Millisecond
	)

	fmt.Printf("[%s] Video RTP handler started, VideoTrack is nil: %v\n", sess.ID, sess.VideoTrack == nil)

	// Create ICE credentials for STUN response
	iceCreds := &ICECredentials{
		LocalUfrag: sess.ICEUfrag,
		LocalPwd:   sess.ICEPwd,
	}

	// Create reorder buffer for SIP→WebRTC video.
	// Buffers out-of-order packets (64-slot window, 25ms timeout) and flushes
	// in sequence order to prevent H.264 decoder poisoning from network jitter.
	reorderBuf := session.NewVideoReorderBuffer(sess.ID, func(data []byte, isKeyframe bool) {
		if sess.VideoTrack == nil {
			return
		}
		// SPS/PPS flows naturally from Linphone as regular RTP packets (NAL types 7/8)
		// with correct sequence numbers. No synthetic injection needed here —
		// buildParamSetRTPPacket produced Seq=0 packets that pion passed through unchanged,
		// corrupting the browser's jitter buffer and causing video freeze after ~7 minutes.
		sess.VideoTrack.Write(data)
	})
	defer reorderBuf.Drain()

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if sess.GetState() == session.StateEnded {
				return
			}
			fmt.Printf("[%s] Error reading video RTP packet: %v\n", sess.ID, err)
			return
		}

		// Handle STUN packets (ICE connectivity check)
		if HandleSTUNPacket(conn, buffer[:n], remoteAddr, iceCreds, sess.ID, "video") {
			continue
		}

		// Regular RTP packet
		packetCount++

		// Check if this is an RTCP packet
		if n >= 8 && isRTCPPacketCheck(buffer[:n]) {
			rtcpCount++
			if rtcpCount <= 10 || rtcpCount%100 == 0 {
				fmt.Printf("[%s] 📨 Video RTCP packet #%d received: %d bytes from %s\n", sess.ID, rtcpCount, n, remoteAddr.String())
			}

			// Learn RTCP address from muxed RTCP on RTP port
			sess.UpdateAsteriskVideoRTCPFromRTCP(remoteAddr, "rtp-mux")

			// Handle RTCP and forward PLI/FIR to WebRTC browser
			s.handleRTCPFromSIP(buffer[:n], sess, rtcpCount)
			continue
		}

		// Skip non-RTP packets (Asterisk keep-alives, STUN, etc.)
		// Minimum RTP header is 12 bytes
		if n < 12 {
			// Likely a keep-alive or STUN packet - ignore
			if packetCount <= 10 || packetCount%1000 == 0 {
				fmt.Printf("[%s] ⏭️ Skipping non-RTP packet #%d (%d bytes) - too small for RTP header\n", sess.ID, packetCount, n)
			}
			continue
		}

		// Check RTP version (must be 2)
		version := (buffer[0] >> 6) & 0x03
		if version != 2 {
			if packetCount <= 10 || packetCount%1000 == 0 {
				fmt.Printf("[%s] ⏭️ Skipping non-RTP packet #%d (%d bytes) - version=%d (expected 2)\n", sess.ID, packetCount, n, version)
			}
			continue
		}

		// Learn symmetric RTP endpoint from actual RTP source (for NAT/symmetric RTP handling)
		// This ensures PLI/FIR are sent to the correct port even if it differs from SDP
		if packetCount == 1 || packetCount <= 5 {
			sess.UpdateAsteriskVideoEndpointFromRTP(remoteAddr)
		}

		// Debug: Log small RTP packets
		if n < 50 && packetCount <= 20 {
			pt := buffer[1] & 0x7F
			fmt.Printf("[%s] ⚠️ Small RTP video packet #%d: %d bytes, PT=%d from %s\n",
				sess.ID, packetCount, n, pt, remoteAddr.String())
		}

		// Learn Remote SSRC from valid RTP packets
		if n >= 12 {
			// Extract SSRC directly from packet header without full parsing
			ssrc := uint32(buffer[8])<<24 | uint32(buffer[9])<<16 | uint32(buffer[10])<<8 | uint32(buffer[11])
			previousSSRC := sess.RemoteVideoSSRC

			if previousSSRC == 0 || previousSSRC != ssrc {
				sess.SetRemoteVideoSSRC(ssrc)
				fmt.Printf("[%s] Learned Remote Video SSRC: %d (previous: %d)\n", sess.ID, ssrc, previousSSRC)
				sess.StartVideoRTCPFallbackWindow(4*time.Second, "ssrc-learn")

				// Send FIR first, then a short guarded PLI burst.
				// Keep startup recovery conservative to avoid RTCP storms during @switch answer.
				go func() {
					fmt.Printf("[%s] 🚀 Sending startup FIR + guarded PLI burst\n", sess.ID)
					sess.SendFIRToAsterisk()
					time.Sleep(startupPLIInterval)
					for i := 0; i < startupPLIAttempts; i++ {
						if sess.GetState() == session.StateEnded {
							return
						}

						lastKeyframe, _ := sess.GetKeyframeTimes()
						if !lastKeyframe.IsZero() && time.Since(lastKeyframe) <= startupKeyframeFresh {
							fmt.Printf("[%s] ✅ Startup recovery settled after keyframe; stopping PLI burst\n", sess.ID)
							return
						}

						sess.SendPLIToAsteriskForced("ssrc-learn")
						time.Sleep(startupPLIInterval)
					}
				}()
			}
		}

		// Parse RTP packet for keyframe detection
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buffer[:n]); err == nil {
			seq := packet.Header.SequenceNumber
			isKeyframe := false

			if !haveLastSeq {
				lastSeq = seq
				haveLastSeq = true
			} else {
				delta := uint16(seq - lastSeq)
				switch {
				case delta == 0:
					seqDuplicates++
				case delta < 0x8000:
					if delta > 1 {
						missing := int(delta - 1)
						seqGapEvents++
						seqGapPackets += missing
						if missing >= burstGapTrigger && (lastGapRecovery.IsZero() || time.Since(lastGapRecovery) >= gapRecoveryMinInterval) {
							fmt.Printf("[%s] ⚠️ SIP→WebRTC burst loss detected: missing=%d (seq %d→%d) - requesting keyframe\n",
								sess.ID, missing, lastSeq, seq)
							sess.SendBrowserRecoveryToAsterisk("sip-gap")
							lastGapRecovery = time.Now()
						}
					}
					lastSeq = seq
				default:
					// Old/reordered packet (or wrap edge mis-order): keep baseline for normal progression.
					seqOutOfOrder++
				}
			}

			if packetCount%300 == 0 {
				lastKeyframe, _ := sess.GetKeyframeTimes()
				keyframeAge := "none"
				if !lastKeyframe.IsZero() {
					keyframeAge = time.Since(lastKeyframe).Round(100 * time.Millisecond).String()
				}
				rBuf, rRel, rDrop, rTO := reorderBuf.GetStats()
				rPend := reorderBuf.Pending()
				fmt.Printf("[%s] 📊 SIP→WebRTC video stats: packets=%d gaps=%d missing=%d ooo=%d dup=%d reorder(buf=%d rel=%d drop=%d to=%d pend=%d) keyframeAge=%s\n",
					sess.ID, packetCount, seqGapEvents, seqGapPackets, seqOutOfOrder, seqDuplicates,
					rBuf, rRel, rDrop, rTO, rPend, keyframeAge)
			}

			// Cache RTP packet for possible retransmission to WebRTC (NACK handling)
			sess.CacheVideoRTPPacket(packet.Header.SequenceNumber, buffer[:n])

			if len(packet.Payload) > 0 {
				nalType := packet.Payload[0] & 0x1F
				switch nalType {
				case 5:
					// Keyframe (IDR) detected - Use exported method
					isKeyframe = true
					isPLIResponse, responseTime, pliSent, pliResponse := sess.RecordKeyframe()
					if isPLIResponse {
						fmt.Printf("[%s] ✅ Keyframe received! PLI response time: %v (Sent: %d, Response: %d)\n",
							sess.ID, responseTime, pliSent, pliResponse)
					} else {
						fmt.Printf("[%s] 🔑 Keyframe (IDR) detected in packet #%d\n", sess.ID, packetCount)
					}
				case 7:
					// SPS (Sequence Parameter Set) - cache for SIP→WebRTC injection and log
					sess.CacheSIPSPS(packet.Payload)
					if packetCount <= 20 || packetCount%1000 == 0 {
						fmt.Printf("[%s] 📦 SPS (parameter set) received in packet #%d (size: %d bytes, seq=%d)\n", sess.ID, packetCount, len(packet.Payload), packet.Header.SequenceNumber)
					}
				case 8:
					// PPS (Picture Parameter Set) - cache for SIP→WebRTC injection and log
					sess.CacheSIPPPS(packet.Payload)
					if packetCount <= 20 || packetCount%1000 == 0 {
						fmt.Printf("[%s] 📦 PPS (parameter set) received in packet #%d (size: %d bytes, seq=%d)\n", sess.ID, packetCount, len(packet.Payload), packet.Header.SequenceNumber)
					}
				case 28:
					if len(packet.Payload) > 1 {
						fuHeader := packet.Payload[1]
						startBit := (fuHeader >> 7) & 0x01
						fuNalType := fuHeader & 0x1F
						if fuNalType == 5 && startBit == 1 {
							// Keyframe fragment start detected - Use exported method
							isKeyframe = true
							isPLIResponse, responseTime, pliSent, pliResponse := sess.RecordKeyframe()
							if isPLIResponse {
								fmt.Printf("[%s] ✅ Keyframe fragment start! PLI response time: %v (Sent: %d, Response: %d)\n",
									sess.ID, responseTime, pliSent, pliResponse)
							} else {
								fmt.Printf("[%s] 🔑 Keyframe fragment start in packet #%d\n", sess.ID, packetCount)
							}
						}
					}
				}
			}

			// Push into reorder buffer (handles sequencing + SPS/PPS injection at flush time)
			if sess.VideoTrack != nil {
				reorderBuf.Push(seq, buffer[:n], isKeyframe)
			} else if packetCount == 1 {
				fmt.Printf("[%s] WARNING: VideoTrack is nil, cannot forward video RTP!\n", sess.ID)
			}
		} else {
			// Log RTP parsing errors for debugging
			if packetCount <= 10 || packetCount%1000 == 0 {
				fmt.Printf("[%s] ⚠️ Failed to parse RTP packet #%d (%d bytes): %v\n", sess.ID, packetCount, n, err)

				// Hex dump for debugging Asterisk RTP format
				dumpBytes := 32
				if n < dumpBytes {
					dumpBytes = n
				}
				hexDump := ""
				for i := 0; i < dumpBytes; i++ {
					hexDump += fmt.Sprintf("%02x ", buffer[i])
					if (i+1)%16 == 0 {
						hexDump += "\n"
					}
				}
				fmt.Printf("[%s] 📊 First %d bytes (hex):\n%s\n", sess.ID, dumpBytes, hexDump)

				// Check RTP header bits for common issues
				if n >= 12 {
					version := (buffer[0] >> 6) & 0x03
					padding := (buffer[0] >> 5) & 0x01
					extension := (buffer[0] >> 4) & 0x01
					csrcCount := buffer[0] & 0x0F
					payloadType := buffer[1] & 0x7F
					sequence := uint16(buffer[2])<<8 | uint16(buffer[3])
					timestamp := uint32(buffer[4])<<24 | uint32(buffer[5])<<16 | uint32(buffer[6])<<8 | uint32(buffer[7])
					ssrc := uint32(buffer[8])<<24 | uint32(buffer[9])<<16 | uint32(buffer[10])<<8 | uint32(buffer[11])

					fmt.Printf("[%s] 📊 RTP Header: v=%d, p=%d, x=%d, cc=%d, pt=%d, seq=%d, ts=%d, ssrc=0x%08x\n",
						sess.ID, version, padding, extension, csrcCount, payloadType, sequence, timestamp, ssrc)
				}
			}

			// Fallback: write unparseable packet directly (bypasses reorder buffer)
			if sess.VideoTrack != nil {
				sess.VideoTrack.Write(buffer[:n])
			}
		}
	}
}

// isRTCPPacketCheck checks if a packet is RTCP based on payload type
func isRTCPPacketCheck(data []byte) bool {
	if len(data) < 8 {
		return false
	}

	// Check RTP version (should be 2)
	version := (data[0] >> 6) & 0x03
	if version != 2 {
		return false
	}

	// RTCP payload types: 200-207
	payloadType := data[1]
	return payloadType >= 200 && payloadType <= 207
}

// seqAhead returns true if a is newer than b for 16-bit RTP sequence numbers.
func seqAhead(a, b uint16) bool {
	d := uint16(a - b)
	return d != 0 && d < 0x8000
}

// handleRTCPFromSIP parses RTCP packets from SIP/Linphone and forwards PLI/FIR to WebRTC browser
func (s *Server) handleRTCPFromSIP(data []byte, sess *session.Session, rtcpCount int) {
	packets, err := rtcp.Unmarshal(data)
	if err != nil {
		if rtcpCount <= 5 {
			fmt.Printf("[%s] Error parsing RTCP from SIP: %v\n", sess.ID, err)
		}
		return
	}

	for _, pkt := range packets {
		switch p := pkt.(type) {
		case *rtcp.PictureLossIndication:
			fmt.Printf("[%s] 📸 Received PLI from Linphone/SIP - Forwarding to WebRTC browser (Media SSRC=%d)\n", sess.ID, p.MediaSSRC)
			sess.SendPLItoWebRTC()

		case *rtcp.FullIntraRequest:
			fmt.Printf("[%s] 📸 Received FIR from Linphone/SIP - Forwarding to WebRTC browser\n", sess.ID)
			sess.SendPLItoWebRTC()

		case *rtcp.ReceiverReport:
			if rtcpCount <= 3 {
				fmt.Printf("[%s] Received RR from Linphone (SSRC=%d)\n", sess.ID, p.SSRC)
			}

		case *rtcp.SenderReport:
			if rtcpCount <= 3 {
				fmt.Printf("[%s] Received SR from Linphone (SSRC=%d)\n", sess.ID, p.SSRC)
			}

		case *rtcp.TransportLayerNack:
			// Forward NACK from SIP/Linphone to WebRTC browser for packet retransmission
			fmt.Printf("[%s] 🔄 Received NACK from Linphone/SIP - Forwarding to WebRTC browser (Media SSRC=%d, Nacks=%v)\n",
				sess.ID, p.MediaSSRC, p.Nacks)
			sess.SendNACKToWebRTC(p.MediaSSRC, p.Nacks)

		default:
			if rtcpCount <= 5 {
				fmt.Printf("[%s] Received RTCP type %T from Linphone\n", sess.ID, pkt)
			}
		}
	}
}

// startPeriodicPLIForSession sends PLI requests to the browser at regular intervals
func (s *Server) startPeriodicPLIForSession(sess *session.Session) {
	fmt.Printf("[%s] 🔄 Starting periodic PLI sender for fast video start\n", sess.ID)

	// Wait a bit for the connection to establish
	time.Sleep(500 * time.Millisecond)

	// Limit periodic PLI to startup window only.
	pliDeadline := time.Now().Add(20 * time.Second)

	// Send at most every 2 seconds.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	pliCount := 0

	// Small initial burst to kickstart keyframe delivery.
	for i := 0; i < 5; i++ {
		if sess.GetState() == session.StateEnded {
			return
		}
		sess.SendPLItoWebRTC()
		pliCount++
		time.Sleep(200 * time.Millisecond)
	}

	for range ticker.C {
		if sess.GetState() == session.StateEnded {
			fmt.Printf("[%s] Stopping periodic PLI sender - session ended\n", sess.ID)
			return
		}

		if time.Now().After(pliDeadline) {
			fmt.Printf("[%s] Stopping periodic PLI sender - startup window ended\n", sess.ID)
			return
		}
		pliCount++
		// Log less frequently after initial period
		if pliCount <= 20 || pliCount%10 == 0 {
			fmt.Printf("[%s] 🔄 Periodic PLI #%d to browser\n", sess.ID, pliCount)
		}
		sess.SendPLItoWebRTC()
	}
}

// startKeyframeWatchdogForSession requests keyframes when video stalls (SIP → WebRTC)
func (s *Server) startKeyframeWatchdogForSession(sess *session.Session) {
	if !s.config.VideoKeyframeWatchdogEnabled {
		return
	}

	interval := time.Duration(s.config.VideoKeyframeWatchdogIntervalMS) * time.Millisecond
	if interval < 200*time.Millisecond {
		interval = 200 * time.Millisecond
	}

	stale := time.Duration(s.config.VideoKeyframeStaleMS) * time.Millisecond
	if stale < 1000*time.Millisecond {
		stale = 1000 * time.Millisecond
	}

	firStale := time.Duration(s.config.VideoKeyframeFIRStaleMS) * time.Millisecond
	if firStale < stale {
		firStale = stale
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if sess.GetState() == session.StateEnded {
			return
		}

		// Only start after we have a remote video SSRC (video is flowing)
		if sess.GetRemoteVideoSSRC() == 0 {
			continue
		}

		lastKeyframe, _ := sess.GetKeyframeTimes()
		lastSipPLI, lastSipFIR := sess.GetSIPRecoveryTimes()
		now := time.Now()

		// If we never saw a keyframe, treat it as stale
		keyframeAge := now.Sub(lastKeyframe)
		if lastKeyframe.IsZero() {
			keyframeAge = stale + time.Second
		}

		if keyframeAge < stale {
			continue
		}

		if keyframeAge >= firStale {
			if !lastSipFIR.IsZero() && now.Sub(lastSipFIR) < interval {
				continue
			}
			fmt.Printf("[%s] ⚠️ Keyframe stale for %v (>= %v) - sending FIR to SIP\n",
				sess.ID, keyframeAge, firStale)
			sess.SendFIRToAsterisk()
			continue
		}

		// Throttle PLI requests using last SIP-side request time to avoid flooding.
		if !lastSipPLI.IsZero() && now.Sub(lastSipPLI) < interval {
			continue
		}

		fmt.Printf("[%s] ⚠️ Keyframe stale for %v (>= %v) - sending PLI to SIP\n",
			sess.ID, keyframeAge, stale)
		sess.SendPLIToAsterisk()
	}
}

// handleAudioRTCPPacketsForSession reads RTCP packets from the dedicated audio RTCP port (RTP+1)
// This supports classic non-muxed RTCP. Muxed RTCP on the RTP port is handled in handleAudioRTPPacketsForSession.
func (s *Server) handleAudioRTCPPacketsForSession(conn *net.UDPConn, sess *session.Session) {
	buffer := make([]byte, s.rtpConfig.BufferSize)
	rtcpCount := 0

	fmt.Printf("[%s] 📨 Audio RTCP handler started (dedicated port for non-muxed RTCP)\n", sess.ID)

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if sess.GetState() == session.StateEnded {
				return
			}
			fmt.Printf("[%s] Error reading audio RTCP packet: %v\n", sess.ID, err)
			return
		}

		rtcpCount++
		if rtcpCount <= 10 || rtcpCount%100 == 0 {
			fmt.Printf("[%s] 📨 Audio RTCP packet #%d received: %d bytes from %s (dedicated port)\n", sess.ID, rtcpCount, n, remoteAddr.String())
		}

		// Parse and handle RTCP packets (reuse existing handler)
		packets, err := rtcp.Unmarshal(buffer[:n])
		if err != nil {
			if rtcpCount <= 5 {
				fmt.Printf("[%s] Error parsing audio RTCP: %v\n", sess.ID, err)
			}
			continue
		}

		// Process RTCP packets (receiver reports, sender reports, etc.)
		for _, pkt := range packets {
			switch p := pkt.(type) {
			case *rtcp.ReceiverReport:
				if rtcpCount <= 3 {
					fmt.Printf("[%s] 📊 Received audio RR from SIP (SSRC=%d, dedicated port)\n", sess.ID, p.SSRC)
				}

			case *rtcp.SenderReport:
				if rtcpCount <= 3 {
					fmt.Printf("[%s] 📊 Received audio SR from SIP (SSRC=%d, dedicated port)\n", sess.ID, p.SSRC)
				}

			default:
				if rtcpCount <= 5 {
					fmt.Printf("[%s] Received audio RTCP type %T (dedicated port)\n", sess.ID, pkt)
				}
			}
		}
	}
}

// handleVideoRTCPPacketsForSession reads RTCP packets from the dedicated video RTCP port (RTP+1)
// This supports classic non-muxed RTCP. Muxed RTCP on the RTP port is handled in handleVideoRTPPacketsForSession.
func (s *Server) handleVideoRTCPPacketsForSession(conn *net.UDPConn, sess *session.Session) {
	buffer := make([]byte, s.rtpConfig.BufferSize)
	rtcpCount := 0

	fmt.Printf("[%s] 📨 Video RTCP handler started (dedicated port for non-muxed RTCP)\n", sess.ID)

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if sess.GetState() == session.StateEnded {
				return
			}
			fmt.Printf("[%s] Error reading video RTCP packet: %v\n", sess.ID, err)
			return
		}

		rtcpCount++
		if rtcpCount <= 10 || rtcpCount%100 == 0 {
			fmt.Printf("[%s] 📨 Video RTCP packet #%d received: %d bytes from %s (dedicated port)\n", sess.ID, rtcpCount, n, remoteAddr.String())
		}

		// Learn RTCP address from dedicated RTCP port
		sess.UpdateAsteriskVideoRTCPFromRTCP(remoteAddr, "rtcp-dedicated")

		// Reuse existing RTCP handler (supports PLI/FIR/NACK forwarding to WebRTC)
		s.handleRTCPFromSIP(buffer[:n], sess, rtcpCount)
	}
}
