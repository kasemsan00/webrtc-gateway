package sip

import (
	"fmt"
	"net"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

// normalizedVideoWriteResult reports whether an access unit was emitted and
// whether it committed @switch video gate release.
type normalizedVideoWriteResult struct {
	emitted      bool
	gateReleased bool
	generation   int
}

// writeNormalizedVideoAccessUnit is the single owner of gate evaluation,
// packet writes, and release commit/abort for one normalized access unit.
// Callers must serialize invocations so a later AU cannot overtake a reserved
// IDR while it is being written.
func writeNormalizedVideoAccessUnit(
	sess *session.Session,
	au session.NormalizedH264AccessUnit,
	now time.Time,
	write func([]byte) (int, error),
) normalizedVideoWriteResult {
	if write != nil && sess.HasPendingSIPVideoIDRWrite() {
		_, _ = sess.WritePendingSIPVideoIDR(write)
	}

	decision := sess.EvaluateSwitchVideoAccessUnit(au, now)
	if !decision.Emit {
		if decision.Reason == "undersized-idr" {
			sess.RequestSIPKeyframeAfterUndersizedSwitchIDR(decision.Generation, now)
		}
		return normalizedVideoWriteResult{}
	}

	abort := func(reason string) {
		if decision.Reservation != 0 {
			sess.AbortSwitchVideoGateRelease(au.Generation, decision.Reservation, reason)
		}
	}
	if len(au.Packets) == 0 {
		abort("empty-access-unit")
		return normalizedVideoWriteResult{}
	}

	sess.NumberSIPVideoAccessUnit(&au)

	for _, packet := range au.Packets {
		data, err := packet.Marshal()
		if err != nil {
			fmt.Printf("[%s] h264_au_write_error stage=marshal seq=%d error=%v\n", sess.ID, packet.SequenceNumber, err)
			abort("marshal-failed")
			if au.IsIDR {
				sess.RememberSIPVideoIDR(au, false)
			}
			return normalizedVideoWriteResult{}
		}
		if _, err := write(data); err != nil {
			fmt.Printf("[%s] h264_au_write_error stage=track seq=%d error=%v\n", sess.ID, packet.SequenceNumber, err)
			abort("track-write-failed")
			if au.IsIDR {
				sess.RememberSIPVideoIDR(au, false)
			}
			return normalizedVideoWriteResult{}
		}
		sess.CacheVideoRTPPacket(packet.SequenceNumber, data)
	}
	if au.IsIDR {
		sess.RememberSIPVideoIDR(au, true)
	}

	if decision.Reservation != 0 {
		if !sess.CommitSwitchVideoGateRelease(
			au.Generation,
			decision.Reservation,
			now,
		) {
			fmt.Printf("[%s] h264_au_write_error stage=gate-commit generation=%d reservation=%d\n",
				sess.ID, au.Generation, decision.Reservation)
			return normalizedVideoWriteResult{}
		}
		return normalizedVideoWriteResult{
			emitted:      true,
			gateReleased: true,
			generation:   au.Generation,
		}
	}
	return normalizedVideoWriteResult{emitted: true}
}

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

		// Rescue channel: some SIP peers deliver video RTP to the dedicated RTCP
		// port (RTP+1). Demux those into the same SIP→WebRTC video path.
		videoRTPRescue := make(chan videoUDPIngress, 128)

		// Start forwarding RTP to session's tracks
		go s.handleAudioRTPPacketsForSession(audioConn, sess)
		go s.handleVideoRTPPacketsForSession(videoConn, sess, videoRTPRescue)

		// Start forwarding RTCP (dedicated ports) to session handlers
		go s.handleAudioRTCPPacketsForSession(audioRTCPConn, sess)
		go s.handleVideoRTCPPacketsForSession(videoRTCPConn, sess, videoRTPRescue)

		// Start periodic PLI sender for fast video start
		go s.startPeriodicPLIForSession(sess)
		// Start keyframe watchdog to recover from stalled decoders (iOS VideoToolbox)
		go s.startKeyframeWatchdogForSession(sess)

		fmt.Printf("[%s] 📞 Audio RTP listener started on port: %d\n", sess.ID, port)
		fmt.Printf("[%s] 📞 Audio RTCP listener started on port: %d\n", sess.ID, audioRTCPPort)
		fmt.Printf("[%s] 📞 Video RTP listener started on port: %d\n", sess.ID, videoPort)
		fmt.Printf("[%s] 📞 Video RTCP listener started on port: %d\n", sess.ID, videoRTCPPort)
		sess.MarkMediaForwardReady()
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
			outBuf := buffer[:n]
			packet := &rtp.Packet{}
			if err := packet.Unmarshal(buffer[:n]); err != nil {
				if _, writeErr := sess.AudioTrack.Write(outBuf); writeErr != nil {
					fmt.Printf("[%s] Error writing to audio track: %v\n", sess.ID, writeErr)
					return
				}
				if sess.TryMarkRemoteAudioReady() {
					s.notifyRemoteMediaReady(sess, "audio")
				}
				continue
			}

			outPacket := packet
			if sess.IsTranslatorEnabled() {
				translated, translateErr := sess.ProcessInboundTranslator(packet)
				if translateErr != nil {
					if packetCount <= 5 || packetCount%1000 == 0 {
						fmt.Printf("[%s] ⚠️ Inbound translation error for packet #%d: %v (suppressing original audio)\n",
							sess.ID, packetCount, translateErr)
					}
					continue
				} else if translated != nil {
					outPacket = translated
					if marshaled, marshalErr := translated.Marshal(); marshalErr == nil {
						outBuf = marshaled
					}
					if packetCount <= 5 {
						fmt.Printf("[%s] 🎤 Inbound translated audio packet #%d: %d bytes → %d bytes\n",
							sess.ID, packetCount, len(packet.Payload), len(translated.Payload))
					}
				} else {
					if packetCount <= 5 || packetCount%1000 == 0 {
						fmt.Printf("[%s] 🎤 Inbound translation pending for packet #%d (suppressing original audio)\n",
							sess.ID, packetCount)
					}
					continue
				}
			}

			if sess.IsInboundGainEnabled() {
				gained, gainErr := sess.ProcessInboundGain(outPacket)
				if gainErr != nil {
					if packetCount <= 5 || packetCount%1000 == 0 {
						fmt.Printf("[%s] ⚠️ Inbound gain error for packet #%d: %v (falling back to passthrough)\n",
							sess.ID, packetCount, gainErr)
					}
				} else if gained != nil {
					outPacket = gained
					if marshaled, marshalErr := gained.Marshal(); marshalErr == nil {
						outBuf = marshaled
					}
				}
			}

			// CRITICAL: Rewrite RTP payload type from SIP side to WebRTC side
			// SIP may use PT=107 (or other), but WebRTC expects PT=111 (registered in MediaEngine)
			if sess.SIPOpusPT > 0 && sess.SIPOpusPT != 111 {
				originalPT := outPacket.Header.PayloadType
				if originalPT != DTMFPayloadType && originalPT == sess.SIPOpusPT {
					outPacket.Header.PayloadType = 111
					if packetCount <= 5 {
						fmt.Printf("[%s] 🔄 Rewrite audio PT: %d → 111 (packet #%d)\n", sess.ID, originalPT, packetCount)
					}
					if rewrittenBuf, marshalErr := outPacket.Marshal(); marshalErr == nil {
						outBuf = rewrittenBuf
					}
				}
			}

			if _, err := sess.AudioTrack.Write(outBuf); err != nil {
				fmt.Printf("[%s] Error writing to audio track: %v\n", sess.ID, err)
				return
			}
			if sess.TryMarkRemoteAudioReady() {
				s.notifyRemoteMediaReady(sess, "audio")
			}
		} else if packetCount == 1 {
			fmt.Printf("[%s] WARNING: AudioTrack is nil, cannot forward audio RTP!\n", sess.ID)
		}
	}
}

// videoUDPIngress carries a datagram into the SIP→WebRTC video path.
type videoUDPIngress struct {
	data []byte
	addr *net.UDPAddr
	via  string // "rtp" | "rtcp-rescue"
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	ip := make(net.IP, len(addr.IP))
	copy(ip, addr.IP)
	return &net.UDPAddr{IP: ip, Port: addr.Port, Zone: addr.Zone}
}

func isLikelyRTPPacket(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	if (data[0]>>6)&0x03 != 2 {
		return false
	}
	return !isRTCPPacketCheck(data)
}

// handleVideoRTPPacketsForSession reads video RTP packets and writes them to a session's video track
func (s *Server) handleVideoRTPPacketsForSession(conn *net.UDPConn, sess *session.Session, rescue <-chan videoUDPIngress) {
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
		startupPLIAttempts     = 4
		startupPLIInterval     = 300 * time.Millisecond
		startupKeyframeFresh   = 800 * time.Millisecond
		uplinkExtraPLIAttempts = 3
	)

	fmt.Printf("[%s] Video RTP handler started, VideoTrack is nil: %v\n", sess.ID, sess.VideoTrack == nil)

	udpPackets := make(chan videoUDPIngress, 128)
	udpErr := make(chan error, 1)
	go func() {
		readBuf := make([]byte, s.rtpConfig.BufferSize)
		for {
			n, remoteAddr, err := conn.ReadFromUDP(readBuf)
			if err != nil {
				udpErr <- err
				return
			}
			pkt := videoUDPIngress{
				data: append([]byte(nil), readBuf[:n]...),
				addr: cloneUDPAddr(remoteAddr),
				via:  "rtp",
			}
			select {
			case udpPackets <- pkt:
			default:
				// Prefer newest media over stalling the UDP reader.
				select {
				case <-udpPackets:
				default:
				}
				udpPackets <- pkt
			}
		}
	}()

	// Create ICE credentials for STUN response
	iceCreds := &ICECredentials{
		LocalUfrag: sess.ICEUfrag,
		LocalPwd:   sess.ICEPwd,
	}

	// Reorder packets first, then (by default) emit only complete H.264 access
	// units. Strict mobile decoders can remain black after receiving one broken
	// FU-A chain even though RTP bytes continue to arrive.
	var auNormalizer *session.H264AccessUnitNormalizer
	if sess.VideoAUNormalizeEnabled {
		auNormalizer = session.NewH264AccessUnitNormalizer(session.H264AccessUnitNormalizerConfig{}, func(au session.NormalizedH264AccessUnit) {
			if sess.VideoTrack == nil {
				if au.IsIDR {
					sess.RememberSIPVideoIDR(au, false)
				}
				return
			}
			now := time.Now()
			result := writeNormalizedVideoAccessUnit(sess, au, now, sess.WriteVideoToWebRTC)
			if !result.emitted {
				return
			}
			if result.gateReleased && s.switchRenegotiationStarter != nil {
				s.switchRenegotiationStarter.StartSwitchVideoRenegotiation(sess.ID, result.generation)
			}
			if au.IsIDR {
				sess.MarkSIPVideoIDRSize(len(au.Packets))
				isPLIResponse, responseTime, pliSent, pliResponse := sess.RecordKeyframe()
				sess.MarkSwitchVideoKeyframe(now)
				sess.MarkSwitchVideoProgress(now, false)
				fmt.Printf("[%s] h264_au_normalized status=complete-idr packets=%d injected_parameter_sets=%v source_timestamp=%d pli_response=%v response_time=%v pli_sent=%d pli_responses=%d\n",
					sess.ID, len(au.Packets), au.InjectedParameterSets, au.SourceTimestamp,
					isPLIResponse, responseTime, pliSent, pliResponse)
				_, _, hasCachedSets := sess.GetSIPCachedSPSPPS()
				hasParameterSets := hasCachedSets || au.InjectedParameterSets
				if sess.TryMarkRemoteVideoReady(hasParameterSets) {
					s.notifyRemoteMediaReady(sess, "video")
				}
			}
		})
		if sps, pps, ok := sess.GetSIPCachedSPSPPS(); ok {
			auNormalizer.SetParameterSets(sps, pps)
		}
		sess.BindH264AUParameterSetSeeder(auNormalizer.SetParameterSets)
		sess.BindH264AUReplayRewriter(auNormalizer.RewriteForReplay)
		sess.BindH264AUNumberer(auNormalizer.NumberAccessUnit)
		defer sess.BindH264AUParameterSetSeeder(nil)
		defer sess.BindH264AUReplayRewriter(nil)
		defer sess.BindH264AUNumberer(nil)
	}
	reorderBuf := session.NewVideoReorderBuffer(sess.ID, func(data []byte, isKeyframe bool) {
		if sess.VideoTrack == nil {
			return
		}
		if auNormalizer != nil {
			packet := &rtp.Packet{}
			if err := packet.Unmarshal(data); err != nil {
				fmt.Printf("[%s] h264_au_drop reason=rtp-unmarshal error=%v\n", sess.ID, err)
				return
			}
			auNormalizer.Push(packet)
			return
		}
		// Explicit rollback path: preserve the legacy raw reordered stream.
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(data); err == nil {
			sess.CacheVideoRTPPacket(packet.SequenceNumber, data)
			_, _ = sess.WriteVideoToWebRTC(data)
		}
	})
	lastSwitchGeneration := sess.GetSwitchGeneration()
	idrReplayNotify := sess.SIPVideoIDRReplayNotify()
	idrReplayTicker := time.NewTicker(250 * time.Millisecond)
	defer idrReplayTicker.Stop()
	tryWritePendingSIPVideoIDR := func() {
		if sess.GetState() == session.StateEnded || sess.VideoTrack == nil || !sess.HasPendingSIPVideoIDRWrite() {
			return
		}
		if wrote, reason := sess.WritePendingSIPVideoIDR(sess.WriteVideoToWebRTC); wrote {
			fmt.Printf("[%s] sip_video_idr_flushed reason=%s\n", sess.ID, reason)
		}
	}
	defer func() {
		reorderBuf.Drain()
		if auNormalizer != nil {
			auNormalizer.Drain()
		}
	}()
	buildVideoSummary := func(keyframeAge time.Duration) session.VideoRecoverySummary {
		rBuf, rRel, rDrop, rTO := reorderBuf.GetStats()
		return session.VideoRecoverySummary{
			Packets:         packetCount,
			Gaps:            seqGapEvents,
			Missing:         seqGapPackets,
			OutOfOrder:      seqOutOfOrder,
			Duplicates:      seqDuplicates,
			ReorderBuffered: rBuf,
			ReorderReleased: rRel,
			ReorderDropped:  rDrop,
			ReorderTimedOut: rTO,
			ReorderPending:  reorderBuf.Pending(),
			LastKeyframeAge: keyframeAge,
		}
	}
	updateSwitchSummary := func(keyframeAge time.Duration) session.VideoRecoverySummary {
		summary := buildVideoSummary(keyframeAge)
		sess.UpdateSwitchVideoRecoverySummary(summary)
		return summary
	}

	for {
		tryWritePendingSIPVideoIDR()
		var ingress videoUDPIngress
		select {
		case <-sess.Done():
			return
		case err := <-udpErr:
			if sess.GetState() == session.StateEnded {
				return
			}
			fmt.Printf("[%s] Error reading video RTP packet: %v\n", sess.ID, err)
			return
		case ingress = <-udpPackets:
		case ingress = <-rescue:
		case <-idrReplayNotify:
			continue
		case <-idrReplayTicker.C:
			continue
		}

		n := copy(buffer, ingress.data)
		remoteAddr := ingress.addr

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
		sess.NoteSIPVideoRTP(time.Now())

		// A switch generation is authoritative even when RTPengine preserves the
		// SIP-side SSRC. Reset queued/source-specific state before classifying or
		// caching the first packet of the new generation.
		currentGeneration := sess.GetSwitchGeneration()
		if currentGeneration != lastSwitchGeneration {
			reorderBuf.Reset()
			haveLastSeq = false
			if auNormalizer != nil {
				auNormalizer.ResetForSwitch(currentGeneration)
			}
			fmt.Printf("[%s] h264_au_source_reset reason=switch-generation previous_generation=%d generation=%d\n",
				sess.ID, lastSwitchGeneration, currentGeneration)
			lastSwitchGeneration = currentGeneration
		}

		// Learn symmetric RTP endpoint from actual RTP source (for NAT/symmetric RTP handling)
		// This ensures PLI/FIR are sent to the correct port even if it differs from SDP
		if packetCount == 1 || packetCount <= 5 {
			sess.UpdateAsteriskVideoEndpointFromRTP(remoteAddr)
		}
		sess.UpdateSIPVideoRTPSource(remoteAddr)

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
				if previousSSRC != 0 {
					reorderBuf.Reset()
					if auNormalizer != nil {
						auNormalizer.ResetSource()
					}
					fmt.Printf("[%s] h264_au_source_reset reason=ssrc-change previous_ssrc=%d ssrc=%d\n", sess.ID, previousSSRC, ssrc)
				}
				sess.SetRemoteVideoSSRC(ssrc)
				fmt.Printf("[%s] Learned Remote Video SSRC: %d (previous: %d)\n", sess.ID, ssrc, previousSSRC)
				fmt.Printf("[%s] 📈 sip_video_ssrc_learned ssrc=%d previous=%d\n", sess.ID, ssrc, previousSSRC)
				sess.StartVideoRTCPFallbackWindow(4*time.Second, "ssrc-learn")
				_ = sess.FlushPendingBrowserKeyframeRequest("ssrc-learn")

				// Send FIR first, then a short guarded PLI burst.
				// Keep startup recovery conservative to avoid RTCP storms during @switch answer.
				go func() {
					// First remote SSRC learn: also kick WebRTC uplink keyframe so
					// late-joining SIP decoders (e.g. Linphone after long ring) get an IDR.
					uplinkKick := sess.KickUplinkKeyframeOnRemoteJoinIfNeeded()
					if uplinkKick {
						kickAt := time.Now()
						go func() {
							for i := 1; i < uplinkExtraPLIAttempts; i++ {
								if sess.GetState() == session.StateEnded {
									return
								}
								if sess.HasUplinkKeyframeSince(kickAt) {
									return
								}
								time.Sleep(startupPLIInterval)
								sess.SendPLItoWebRTC()
							}
						}()
					}

					fmt.Printf("[%s] 🚀 Sending startup FIR + guarded PLI burst\n", sess.ID)
					sess.SendFIRToAsterisk()
					if sess.IsSwitchVideoRecoveryActive() {
						fmt.Printf("[%s] switch_recovery_ssrc_ready sending immediate SIP keyframe request\n", sess.ID)
						sess.SendPLIToAsteriskForced("switch")
					}
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
				keyframeAgeDuration := time.Duration(-1)
				if !lastKeyframe.IsZero() {
					keyframeAgeDuration = time.Since(lastKeyframe)
					keyframeAge = keyframeAgeDuration.Round(100 * time.Millisecond).String()
				}
				rBuf, rRel, rDrop, rTO := reorderBuf.GetStats()
				rPend := reorderBuf.Pending()
				fmt.Printf("[%s] 📊 SIP→WebRTC video stats: packets=%d gaps=%d missing=%d ooo=%d dup=%d reorder(buf=%d rel=%d drop=%d to=%d pend=%d) keyframeAge=%s\n",
					sess.ID, packetCount, seqGapEvents, seqGapPackets, seqOutOfOrder, seqDuplicates,
					rBuf, rRel, rDrop, rTO, rPend, keyframeAge)
				summary := updateSwitchSummary(keyframeAgeDuration)
				sess.ObserveSIPVideoRTPDisorder(summary, time.Now())
				if stall, ok := sess.ObserveSwitchVideoGateStall(time.Now(), summary); ok {
					fmt.Printf("[%s] switch_video_gate_stalled generation=%d elapsed_ms=%d rejected_aus=%d packets=%d gaps=%d missing=%d ooo=%d reorder_timeout=%d pending=%d feedback=%s\n",
						sess.ID, stall.Generation, stall.Elapsed.Milliseconds(), stall.RejectedAUs,
						stall.Summary.Packets, stall.Summary.Gaps, stall.Summary.Missing,
						stall.Summary.OutOfOrder, stall.Summary.ReorderTimedOut,
						stall.Summary.ReorderPending, sess.GetVideoFeedbackTransport())
				}
				if auNormalizer != nil {
					auStats := auNormalizer.Stats()
					fmt.Printf("[%s] h264_au_stats emitted=%d dropped_incomplete=%d dropped_overflow=%d pending_packets=%d\n",
						sess.ID, auStats.Emitted, auStats.DroppedIncomplete, auStats.DroppedOverflow, auStats.PendingPackets)
				}
			}

			// With normalization enabled, cache the rewritten outbound packet in the
			// normalizer callback so browser NACK sequence numbers remain aligned.
			egressData := buffer[:n]
			if auNormalizer == nil {
				sess.CacheVideoRTPPacket(packet.SequenceNumber, egressData)
			}

			if len(packet.Payload) > 0 {
				nalType := packet.Payload[0] & 0x1F
				switch nalType {
				case 5:
					// Preliminary IDR detection is only for transition hold. Successful
					// keyframe accounting happens after a complete normalized AU flush.
					isKeyframe = true
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
							// Preliminary FU-A start detection is only for transition hold.
							isKeyframe = true
						}
					}
				}
			}
			if isKeyframe && auNormalizer == nil {
				isPLIResponse, responseTime, pliSent, pliResponse := sess.RecordKeyframe()
				sess.MarkSwitchVideoKeyframe(time.Now())
				fmt.Printf("[%s] h264_au_normalized status=legacy-keyframe-start packet=%d pli_response=%v response_time=%v pli_sent=%d pli_responses=%d\n",
					sess.ID, packetCount, isPLIResponse, responseTime, pliSent, pliResponse)
				_, _, hasCachedSets := sess.GetSIPCachedSPSPPS()
				if sess.TryMarkRemoteVideoReady(hasCachedSets) {
					s.notifyRemoteMediaReady(sess, "video")
				}
			}

			if auNormalizer == nil && sess.ShouldHoldSwitchVideoPacket(time.Now(), isKeyframe) {
				continue
			}
			lastKeyframe, _ := sess.GetKeyframeTimes()
			keyframeAgeDuration := time.Duration(-1)
			if !lastKeyframe.IsZero() {
				keyframeAgeDuration = time.Since(lastKeyframe)
			}
			updateSwitchSummary(keyframeAgeDuration)
			// Do not let a bare IDR/FU-A start satisfy switch recovery. The
			// normalizer callback marks the keyframe only after the AU is complete.
			sess.MarkSwitchVideoProgress(time.Now(), auNormalizer == nil && isKeyframe)

			// Push into sequence reorder; complete-AU validation follows at flush.
			if sess.VideoTrack != nil {
				reorderBuf.Push(seq, egressData, isKeyframe)
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

			if sess.VideoTrack != nil && auNormalizer == nil {
				sess.CacheVideoRTPPacket(packet.SequenceNumber, buffer[:n])
				_, _ = sess.WriteVideoToWebRTC(buffer[:n])
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

	sawSIPVideoReport := false
	for _, pkt := range packets {
		switch p := pkt.(type) {
		case *rtcp.PictureLossIndication:
			fmt.Printf("[%s] 📸 Received PLI from Linphone/SIP - Forwarding to WebRTC browser (Media SSRC=%d)\n", sess.ID, p.MediaSSRC)
			sess.SendPLItoWebRTC()

		case *rtcp.FullIntraRequest:
			fmt.Printf("[%s] 📸 Received FIR from Linphone/SIP - Forwarding to WebRTC browser\n", sess.ID)
			sess.SendPLItoWebRTC()

		case *rtcp.ReceiverReport:
			sawSIPVideoReport = true
			if rtcpCount <= 3 {
				fmt.Printf("[%s] Received RR from Linphone (SSRC=%d)\n", sess.ID, p.SSRC)
			}

		case *rtcp.SenderReport:
			sawSIPVideoReport = true
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
	if sawSIPVideoReport {
		sess.KickUplinkKeyframeOnFirstSIPRTCPIfNeeded()
	}
}

// startPeriodicPLIForSession sends PLI requests to the browser at regular intervals
func (s *Server) startPeriodicPLIForSession(sess *session.Session) {
	fmt.Printf("[%s] 🔄 Starting periodic PLI sender for fast video start\n", sess.ID)

	// Wait a bit for the connection to establish
	time.Sleep(500 * time.Millisecond)

	// Keep requesting browser IDRs through the late-join window so a SIP
	// decoder that answers after queue auto-200 (Linphone after 5–10s ring)
	// is not stuck on P-frames. Stop once dest-ready + 12s has elapsed and
	// an uplink IDR has already been forwarded.
	pliDeadline := time.Now().Add(15 * time.Second)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	pliCount := 0
	stopIfReady := func(reason string) bool {
		if !sess.ShouldStopPeriodicBrowserPLI() {
			return false
		}
		fmt.Printf("[%s] Stopping periodic PLI sender - %s\n", sess.ID, reason)
		return true
	}

	for i := 0; i < 3; i++ {
		state := sess.GetState()
		if state == session.StateEnded || state == session.StateReconnecting {
			return
		}
		if stopIfReady("late-join window elapsed") {
			return
		}
		sess.SendPLItoWebRTC()
		pliCount++
		time.Sleep(400 * time.Millisecond)
	}

	for range ticker.C {
		state := sess.GetState()
		if state == session.StateEnded {
			fmt.Printf("[%s] Stopping periodic PLI sender - session ended\n", sess.ID)
			return
		}
		if state == session.StateReconnecting {
			fmt.Printf("[%s] Stopping periodic PLI sender - session reconnecting\n", sess.ID)
			return
		}
		if stopIfReady("late-join window elapsed") {
			return
		}
		if time.Now().After(pliDeadline) && !sess.NeedsPostSwitchUplinkKeyframe() {
			fmt.Printf("[%s] Stopping periodic PLI sender - startup window ended\n", sess.ID)
			return
		}
		pliCount++
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

	baseInterval := time.Duration(s.config.VideoKeyframeWatchdogIntervalMS) * time.Millisecond
	if baseInterval < 200*time.Millisecond {
		baseInterval = 200 * time.Millisecond
	}

	baseStale := time.Duration(s.config.VideoKeyframeStaleMS) * time.Millisecond
	if baseStale < 1000*time.Millisecond {
		baseStale = 1000 * time.Millisecond
	}

	baseFirStale := time.Duration(s.config.VideoKeyframeFIRStaleMS) * time.Millisecond
	if baseFirStale < baseStale {
		baseFirStale = baseStale
	}

	for {
		interval, stale, firStale, burstActive := sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFirStale)
		time.Sleep(interval)
		if sess.GetState() == session.StateEnded {
			return
		}
		// Re-read after sleep. Sampling before sleep used a stale burst flag
		// (Al8uLPjnbirH: skip rtp-flowing 370ms after @switch because the
		// previous tick still thought burst was over).
		interval, stale, firStale, burstActive = sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFirStale)

		// Only start after we have a remote video SSRC (video is flowing)
		if sess.GetRemoteVideoSSRC() == 0 {
			continue
		}

		lastKeyframe, _ := sess.GetKeyframeTimes()
		lastSipPLI, lastSipFIR := sess.GetSIPRecoveryTimes()
		now := time.Now()
		decision := session.DecideKeyframeWatchdog(session.KeyframeWatchdogInput{
			Now:          now,
			LastKeyframe: lastKeyframe,
			LastRTP:      sess.LastSIPVideoRTPAt(),
			LastSipPLI:   lastSipPLI,
			LastSipFIR:   lastSipFIR,
			Stale:        stale,
			FIRStale:     firStale,
			Interval:     interval,
			BurstActive:  burstActive,
			GateActive:   sess.IsSwitchVideoGateActive(),
			HealthyIDR:   sess.HasHealthySIPVideoIDR(),
		})

		keyframeAge := now.Sub(lastKeyframe)
		if lastKeyframe.IsZero() {
			keyframeAge = stale + time.Second
		}

		switch decision.Action {
		case session.KeyframeWatchdogNone:
			if decision.Reason == "rtp-flowing" {
				fmt.Printf("[%s] keyframe_watchdog_skip reason=rtp-flowing keyframeAge=%v stale=%v\n",
					sess.ID, keyframeAge, stale)
			}
			continue
		case session.KeyframeWatchdogFIR:
			fmt.Printf("[%s] ⚠️ Keyframe stale for %v (>= %v) - sending FIR to SIP\n",
				sess.ID, keyframeAge, firStale)
			sess.SendFIRToAsterisk()
		default:
			if burstActive {
				fmt.Printf("[%s] ⚠️ (burst) Keyframe stale for %v (>= %v) - sending PLI to SIP\n",
					sess.ID, keyframeAge, stale)
			} else {
				fmt.Printf("[%s] ⚠️ Keyframe stale for %v (>= %v) - sending PLI to SIP\n",
					sess.ID, keyframeAge, stale)
			}
			sess.SendPLIToAsterisk()
		}
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
// Some SIP peers also deliver video RTP to this port; those datagrams are rescued into the video RTP path.
func (s *Server) handleVideoRTCPPacketsForSession(conn *net.UDPConn, sess *session.Session, rescue chan<- videoUDPIngress) {
	buffer := make([]byte, s.rtpConfig.BufferSize)
	rtcpCount := 0
	rescueCount := 0

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

		// RTP misdelivered to the RTCP port must not be treated as RTCP (drops keyframes).
		if isLikelyRTPPacket(buffer[:n]) {
			rescueCount++
			pkt := videoUDPIngress{
				data: append([]byte(nil), buffer[:n]...),
				addr: cloneUDPAddr(remoteAddr),
				via:  "rtcp-rescue",
			}
			select {
			case rescue <- pkt:
				if rescueCount <= 10 || rescueCount%100 == 0 {
					fmt.Printf("[%s] 🛟 Video RTP on RTCP port #%d: %d bytes from %s → video path\n",
						sess.ID, rescueCount, n, remoteAddr.String())
				}
			default:
				fmt.Printf("[%s] ⚠️ Dropped rescued video RTP (ingress full): %d bytes from %s\n",
					sess.ID, n, remoteAddr.String())
			}
			continue
		}

		if n < 8 || !isRTCPPacketCheck(buffer[:n]) {
			continue
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
