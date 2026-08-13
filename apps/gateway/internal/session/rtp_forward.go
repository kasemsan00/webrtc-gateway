package session

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// forwardRTPToAsterisk forwards RTP packets from WebRTC track to Asterisk
// This is extracted to be reusable for both initial connection and renegotiation
func (s *Session) forwardRTPToAsterisk(track *webrtc.TrackRemote, kind string) {
	buffer := make([]byte, s.RTPBufferSize)
	packetCount := 0
	mode0Reassembler := h264Mode0Reassembler{}
	mode0Reassembled := 0
	mode0Dropped := 0

	for {
		n, _, err := track.Read(buffer)
		if err != nil {
			if s.GetState() == StateEnded {
				return
			}
			fmt.Printf("[%s] Error reading %s from WebRTC: %v\n", s.ID, kind, err)
			return
		}

		packetCount++

		// Parse RTP packet
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buffer[:n]); err != nil {
			continue
		}

		if kind == "video" && s.GetSIPVideoPacketizationMode() == 0 {
			var result h264Mode0Result
			packet, result = mode0Reassembler.push(packet)
			switch result {
			case h264Mode0Buffered:
				continue
			case h264Mode0Dropped:
				mode0Dropped++
				if mode0Dropped <= 5 || mode0Dropped%100 == 0 {
					fmt.Printf("[%s] h264_mode0_drop count=%d reason=invalid-discontinuous-or-oversized-fua\n", s.ID, mode0Dropped)
				}
				continue
			case h264Mode0Reassembled:
				mode0Reassembled++
				if mode0Reassembled <= 5 || mode0Reassembled%100 == 0 {
					fmt.Printf("[%s] h264_mode0_reassembled count=%d nal_bytes=%d\n", s.ID, mode0Reassembled, len(packet.Payload))
				}
			}
		} else if kind == "video" {
			mode0Reassembler.reset()
		}

		// Check for SPS/PPS (must cache) or Keyframe (IDR) to inject SPS/PPS
		isKeyframe := false
		isSPS := false
		isPPS := false
		isSTAPA := false
		var nalType uint8
		var fuStartBit uint8
		var fuInnerNalType uint8
		if kind == "video" && len(packet.Payload) > 0 {
			nalType = packet.Payload[0] & 0x1F
			// NAL types: 5 = IDR (keyframe), 7 = SPS, 8 = PPS, 24 = STAP-A, 28 = FU-A
			if nalType == 7 {
				// SPS - Cache it from the stream (browser may not include in SDP)
				isSPS = true
				s.mu.Lock()
				curLen := len(s.CachedSPS)
				shouldUpdate := len(packet.Payload) > 1 && (curLen == 0 || !bytes.Equal(s.CachedSPS, packet.Payload))
				if shouldUpdate {
					s.CachedSPS = make([]byte, len(packet.Payload))
					copy(s.CachedSPS, packet.Payload)
					fmt.Printf("[%s] 💾 Cached SPS from RTP stream (packet #%d, %d bytes)\\n", s.ID, packetCount, len(packet.Payload))
				}
				s.mu.Unlock()
			} else if nalType == 8 {
				// PPS - Cache it from the stream (browser may not include in SDP)
				isPPS = true
				s.mu.Lock()
				curLen := len(s.CachedPPS)
				shouldUpdate := len(packet.Payload) > 1 && (curLen == 0 || !bytes.Equal(s.CachedPPS, packet.Payload))
				if shouldUpdate {
					s.CachedPPS = make([]byte, len(packet.Payload))
					copy(s.CachedPPS, packet.Payload)
					fmt.Printf("[%s] 💾 Cached PPS from RTP stream (packet #%d, %d bytes)\\n", s.ID, packetCount, len(packet.Payload))
				}
				s.mu.Unlock()
			} else if nalType == 5 {
				isKeyframe = true
			} else if nalType == 24 {
				// STAP-A (Single-Time Aggregation Packet Type A) - multiple NALs in one RTP packet
				// Common in some WebRTC implementations (especially mobile browsers)
				isSTAPA = true
				if packetCount <= 20 {
					fmt.Printf("[%s] 📦 STAP-A detected in packet #%d (%d bytes) - will de-aggregate\n", s.ID, packetCount, len(packet.Payload))
				}
			} else if nalType == 28 && len(packet.Payload) > 1 {
				// FU-A fragmentation - check if it's straight of IDR
				fuHeader := packet.Payload[1]
				fuStartBit = (fuHeader >> 7) & 0x01
				fuInnerNalType = fuHeader & 0x1F
				if fuInnerNalType == 5 && fuStartBit == 1 {
					isKeyframe = true
				}
			}
		}
		// Log SPS/PPS forwarding
		if isSPS || isPPS {
			nalName := "SPS"
			if isPPS {
				nalName = "PPS"
			}
			if packetCount <= 50 {
				fmt.Printf("[%s] 📦 Forwarding %s from browser to Asterisk (packet #%d)\\n", s.ID, nalName, packetCount)
			}
		}

		// Forward to Asterisk
		s.mu.Lock() // Lock for updating Seq/SSRC and Injection
		var pendingWrites [][]byte
		var destAddr *net.UDPAddr
		var conn *net.UDPConn
		dropPacket := false

		if kind == "audio" {
			destAddr = s.AsteriskAudioAddr
			conn = s.RTPConn

			if !s.mediaForwardReady {
				s.mu.Unlock()
				if packetCount == 1 || packetCount == 10 || packetCount%200 == 0 {
					fmt.Printf("[%s] deferring WebRTC→SIP audio until media forward ready (packet #%d)\n", s.ID, packetCount)
				}
				continue
			}

			// Initialize SSRC if needed
			if s.AudioSSRC == 0 {
				s.AudioSSRC = generateSSRC()        // Random SSRC
				s.AudioSeq = uint16(generateSSRC()) // Random Start Seq
			}

			// Debug logging for first few packets
			if packetCount <= 5 {
				fmt.Printf("[%s] 🎵 Audio packet #%d: PT=%d, payload=%d bytes\n",
					s.ID, packetCount, packet.Header.PayloadType, len(packet.Payload))
			}

			// S2S Translation: if translator is active, process audio through the pipeline
			if s.TranslatorEnabled && s.Translator != nil {
				s.mu.Unlock() // Release lock before potentially blocking translator call
				translated, err := s.Translator.Process(packet)
				s.mu.Lock()

				if err != nil {
					fmt.Printf("[%s] ⚠️ Translation error for packet #%d: %v (suppressing original audio)\n",
						s.ID, packetCount, err)
					dropPacket = true
				} else if translated != nil {
					// Use translated packet instead of original
					packet = translated
					if packetCount <= 5 {
						fmt.Printf("[%s] 🎤 Translated audio packet #%d: %d bytes → %d bytes\n",
							s.ID, packetCount, len(packet.Payload), len(translated.Payload))
					}
				} else {
					if packetCount <= 5 || packetCount%1000 == 0 {
						fmt.Printf("[%s] 🎤 Translation pending for packet #%d (suppressing original audio)\n",
							s.ID, packetCount)
					}
					dropPacket = true
				}
			}

			if dropPacket {
				s.mu.Unlock()
				continue
			}

			// Rewrite Header (passthrough mode - no transcoding)
			s.AudioSeq++
			packet.Header.SSRC = s.AudioSSRC
			packet.Header.SequenceNumber = s.AudioSeq

			// CRITICAL: Rewrite PayloadType to match SIP side negotiation
			// WebRTC sends Opus as PT=111, but SIP side may expect PT=107 (or other)
			if s.SIPOpusPT > 0 && s.SIPOpusPT != 111 && packet.Header.PayloadType == 111 {
				packet.Header.PayloadType = s.SIPOpusPT
				if packetCount <= 5 {
					fmt.Printf("[%s] 🔄 Rewrite audio PT: 111 → %d (packet #%d)\n", s.ID, s.SIPOpusPT, packetCount)
				}
			}

			packet.Header.Extension = false
			packet.Header.Extensions = nil
			packet.Header.CSRC = nil
			packet.Header.Padding = false
		} else {
			destAddr = s.AsteriskVideoAddr
			conn = s.VideoRTPConn

			if !s.mediaForwardReady {
				if isSTAPA && len(packet.Payload) > 1 {
					s.cacheSTAPAParameterSetsLocked(packet.Payload[1:], packetCount)
				}
				s.mu.Unlock()
				if packetCount == 1 || packetCount == 10 || packetCount%200 == 0 {
					fmt.Printf("[%s] deferring WebRTC→SIP video until media forward ready (packet #%d)\n", s.ID, packetCount)
				}
				continue
			}

			// PHASE 4 DEBUG: Log NAL types from first 100 WebRTC packets to diagnose SPS/PPS timing
			if packetCount <= 100 && len(packet.Payload) > 0 {
				nalType := packet.Payload[0] & 0x1F
				nri := (packet.Payload[0] >> 5) & 0x03
				nalTypeName := "Unknown"
				switch nalType {
				case 1:
					nalTypeName = "P-slice"
				case 5:
					nalTypeName = "IDR (Keyframe)"
				case 7:
					nalTypeName = "SPS"
				case 8:
					nalTypeName = "PPS"
				case 24:
					nalTypeName = "STAP-A"
				case 28:
					nalTypeName = "FU-A"
				}
				fmt.Printf("[%s] 🔍 WebRTC Video Packet #%d: NAL Type=%d (%s), NRI=%d, Size=%d bytes\n",
					s.ID, packetCount, nalType, nalTypeName, nri, len(packet.Payload))
			}

			// Initialize SSRC if needed
			if s.VideoSSRC == 0 {
				s.VideoSSRC = generateSSRC()        // Random SSRC
				s.VideoSeq = uint16(generateSSRC()) // Random Start Seq

				// 🚀 First video packet to Asterisk - request keyframes from browser
				go func() {
					fmt.Printf("[%s] 🚀 Starting video forwarding to Asterisk (SSRC=%d) - requesting keyframes from browser\n", s.ID, s.VideoSSRC)
					for i := 0; i < 4; i++ {
						if s.GetState() == StateEnded {
							return
						}
						if s.ShouldStopStartupBrowserPLI() {
							fmt.Printf("[%s] Stopping first-packet PLI burst - uplink keyframe ready\n", s.ID)
							return
						}
						s.SendPLItoWebRTC()
						select {
						case <-time.After(400 * time.Millisecond):
						case <-s.ctx.Done():
							return
						}
					}
				}()
			}

			// Handle STAP-A: de-aggregate into multiple single-NAL RTP packets
			// This ensures compatibility with decoders (like OpenH264/Linphone) that expect single NAL per packet
			if isSTAPA && len(packet.Payload) > 1 {
				payload := packet.Payload[1:] // Skip NAL header (first byte is STAP-A type 24)

				// First pass: scan all NALs to cache SPS/PPS and detect IDR
				offset := 0
				containsIDR := false
				containsSPS := false
				containsPPS := false
				for offset+2 <= len(payload) {
					nalSize := int(payload[offset])<<8 | int(payload[offset+1])
					offset += 2
					if offset+nalSize > len(payload) {
						break
					}
					nalUnit := payload[offset : offset+nalSize]
					offset += nalSize

					if len(nalUnit) > 0 {
						nalType := nalUnit[0] & 0x1F
						switch nalType {
						case 7:
							containsSPS = true
							curLen := len(s.CachedSPS)
							shouldUpdate := len(nalUnit) > 0 && (curLen == 0 || !bytes.Equal(s.CachedSPS, nalUnit))
							if shouldUpdate {
								s.CachedSPS = make([]byte, len(nalUnit))
								copy(s.CachedSPS, nalUnit)
								if packetCount <= 50 {
									fmt.Printf("[%s] 💾 Cached SPS from STAP-A (%d bytes)\n", s.ID, len(nalUnit))
								}
							}
						case 8:
							containsPPS = true
							curLen := len(s.CachedPPS)
							shouldUpdate := len(nalUnit) > 0 && (curLen == 0 || !bytes.Equal(s.CachedPPS, nalUnit))
							if shouldUpdate {
								s.CachedPPS = make([]byte, len(nalUnit))
								copy(s.CachedPPS, nalUnit)
								if packetCount <= 50 {
									fmt.Printf("[%s] 💾 Cached PPS from STAP-A (%d bytes)\n", s.ID, len(nalUnit))
								}
							}
						case 5:
							containsIDR = true
							isKeyframe = true
						}
					}
				}

				// Inject SPS/PPS before STAP-A IDR only when STAP-A does not already carry both.
				// This avoids duplicate SPS/PPS bursts at call startup that can destabilize decoders.
				shouldInjectBeforeSTAPA := containsIDR && len(s.CachedSPS) > 0 && len(s.CachedPPS) > 0 && !(containsSPS && containsPPS)
				if shouldInjectBeforeSTAPA {
					injectReason := "keyframe"
					redundancyCount := 1
					if s.SwitchSPSPPSInjectRemaining > 0 {
						injectReason = "@switch"
						redundancyCount = 2
					}

					s.LastSPSPPSInjectionTime = time.Now()
					fmt.Printf("[%s] 💉 STAP-A contains IDR - injecting Separate SPS/PPS (NRI=3) (%s, %dx) before de-aggregation (stapaHasSPS=%v, stapaHasPPS=%v)\n",
						s.ID, injectReason, redundancyCount, containsSPS, containsPPS)

					for i := 0; i < redundancyCount; i++ {
						// 1. Send SPS (with forced NRI=3)
						s.VideoSeq++
						spsPacket := &rtp.Packet{
							Header: rtp.Header{
								Version:        2,
								PayloadType:    96,
								SequenceNumber: s.VideoSeq,
								Timestamp:      packet.Header.Timestamp,
								SSRC:           s.VideoSSRC,
								Marker:         false,
							},
							Payload: forceNRI(s.CachedSPS),
						}
						if out, err := spsPacket.Marshal(); err == nil {
							if destAddr != nil && conn != nil {
								pendingWrites = append(pendingWrites, out)
								if injectReason == "@switch" {
									fmt.Printf("[%s] 💉 @switch STAP-A: Injected SPS (NRI=3) copy %d/%d (Seq=%d, Size=%d)\n",
										s.ID, i+1, redundancyCount, spsPacket.Header.SequenceNumber, len(out))
								}
							}
						}

						// 2. Send PPS (with forced NRI=3)
						s.VideoSeq++
						ppsPacket := &rtp.Packet{
							Header: rtp.Header{
								Version:        2,
								PayloadType:    96,
								SequenceNumber: s.VideoSeq,
								Timestamp:      packet.Header.Timestamp,
								SSRC:           s.VideoSSRC,
								Marker:         false,
							},
							Payload: forceNRI(s.CachedPPS),
						}
						if out, err := ppsPacket.Marshal(); err == nil {
							if destAddr != nil && conn != nil {
								pendingWrites = append(pendingWrites, out)
								if injectReason == "@switch" {
									fmt.Printf("[%s] 💉 @switch STAP-A: Injected PPS (NRI=3) copy %d/%d (Seq=%d, Size=%d)\n",
										s.ID, i+1, redundancyCount, ppsPacket.Header.SequenceNumber, len(out))
								}
							}
						}
					}

					if injectReason == "@switch" {
						// Decrement remaining count (one IDR handled)
						s.SwitchSPSPPSInjectRemaining--
						fmt.Printf("[%s] 🔀 @switch STAP-A: Injected for IDR (remaining: %d)\n", s.ID, s.SwitchSPSPPSInjectRemaining)
					}
				}

				// Second pass: send all NALs as separate RTP packets
				offset = 0
				nalCount := 0
				for offset+2 <= len(payload) {
					nalSize := int(payload[offset])<<8 | int(payload[offset+1])
					offset += 2
					if offset+nalSize > len(payload) {
						break
					}
					nalUnit := payload[offset : offset+nalSize]
					offset += nalSize
					nalCount++

					if len(nalUnit) > 0 {
						nalType := nalUnit[0] & 0x1F

						// Send each NAL as separate RTP packet
						s.VideoSeq++
						nalPacket := &rtp.Packet{
							Header: rtp.Header{
								Version:        2,
								PayloadType:    96,
								SequenceNumber: s.VideoSeq,
								Timestamp:      packet.Header.Timestamp,
								SSRC:           s.VideoSSRC,
								Marker:         false,
							},
							Payload: nalUnit,
						}

						// Set marker bit on last NAL unit (if original packet had marker)
						isLastNAL := (offset >= len(payload))
						if isLastNAL && packet.Header.Marker {
							nalPacket.Header.Marker = true
						}

						if out, err := nalPacket.Marshal(); err == nil {
							if destAddr != nil && conn != nil {
								pendingWrites = append(pendingWrites, out)
								if packetCount <= 50 {
									fmt.Printf("[%s] 📤 De-aggregated STAP-A NAL #%d type=%d (Seq=%d, Size=%d, Marker=%v)\n",
										s.ID, nalCount, nalType, nalPacket.Header.SequenceNumber, len(out), nalPacket.Header.Marker)
								}
							}
						}
					}
				}

				stapaForwarded := destAddr != nil && conn != nil && len(pendingWrites) > 0
				s.mu.Unlock()
				// Flush buffered writes outside lock. Never record an uplink
				// IDR while holding s.mu — RecordUplinkKeyframe takes the same lock.
				for _, w := range pendingWrites {
					_, _ = conn.WriteToUDP(w, destAddr)
				}
				if containsIDR {
					if stapaForwarded {
						s.RecordUplinkKeyframe()
						fmt.Printf("[%s] 🔑 KEYFRAME (STAP-A IDR) forwarded in video packet #%d → Asterisk\n", s.ID, packetCount)
					} else {
						fmt.Printf("[%s] 🔑 KEYFRAME (STAP-A IDR) held in video packet #%d - Asterisk endpoint not set yet\n", s.ID, packetCount)
					}
				}
				continue // STAP-A handled, skip normal forwarding
			}

			// Inject SPS/PPS ONLY before IDR keyframe start (NAL=5 or FU-A start with type=5)
			// This prevents SPS/PPS from breaking FU-A fragment chains
			shouldInjectSPSPPS := false
			if len(s.CachedSPS) > 0 && len(s.CachedPPS) > 0 {
				// ONLY inject before IDR start (isKeyframe already detects this correctly)
				if isKeyframe {
					shouldInjectSPSPPS = true
				}
				// Periodic injection REMOVED to prevent mid-FU-A corruption
			}

			if shouldInjectSPSPPS {
				s.LastSPSPPSInjectionTime = time.Now()

				// Send Separate SPS/PPS packets with forced NRI=3.
				// Use a small warmup redundancy window at call startup for decoder reliability.
				redundancyCount := 1
				if isKeyframe && packetCount <= 120 {
					redundancyCount = 2
				}

				for i := 0; i < redundancyCount; i++ {
					// 1. Send SPS (with forced NRI=3)
					// Marker=false (same as Linphone Mobile)
					s.VideoSeq++
					spsPacket := &rtp.Packet{
						Header: rtp.Header{
							Version:        2,
							PayloadType:    96,
							SequenceNumber: s.VideoSeq,
							Timestamp:      packet.Header.Timestamp,
							SSRC:           s.VideoSSRC,
							Marker:         false,
						},
						Payload: forceNRI(s.CachedSPS),
					}

					if out, err := spsPacket.Marshal(); err == nil {
						if destAddr != nil && conn != nil {
							pendingWrites = append(pendingWrites, out)
							if (isKeyframe && i == 0) || packetCount <= 100 {
								fmt.Printf("[%s] 💉 Injected SPS (NRI=3) copy %d/%d (Seq=%d, Size=%d, Keyframe=%v)\n",
									s.ID, i+1, redundancyCount, spsPacket.Header.SequenceNumber, len(out), isKeyframe)
							}
						}
					}

					// 2. Send PPS (with forced NRI=3)
					// Marker=false (same as Linphone Mobile)
					s.VideoSeq++
					ppsPacket := &rtp.Packet{
						Header: rtp.Header{
							Version:        2,
							PayloadType:    96,
							SequenceNumber: s.VideoSeq,
							Timestamp:      packet.Header.Timestamp,
							SSRC:           s.VideoSSRC,
							Marker:         false,
						},
						Payload: forceNRI(s.CachedPPS),
					}

					if out, err := ppsPacket.Marshal(); err == nil {
						if destAddr != nil && conn != nil {
							pendingWrites = append(pendingWrites, out)
							if (isKeyframe && i == 0) || packetCount <= 100 {
								fmt.Printf("[%s] 💉 Injected PPS (NRI=3) copy %d/%d (Seq=%d, Size=%d, Keyframe=%v)\n",
									s.ID, i+1, redundancyCount, ppsPacket.Header.SequenceNumber, len(out), isKeyframe)
							}
						}
					}
				}
			}

			// Rewrite Header of the Keyframe (or regular packet)
			s.VideoSeq++
			packet.Header.SSRC = s.VideoSSRC
			packet.Header.SequenceNumber = s.VideoSeq
			packet.Header.PayloadType = 96 // H264
			packet.Header.Extension = false
			packet.Header.Extensions = nil
			packet.Header.CSRC = nil
			packet.Header.Padding = false
		}
		s.mu.Unlock()

		// Flush any buffered SPS/PPS injection writes outside lock
		for _, w := range pendingWrites {
			conn.WriteToUDP(w, destAddr)
		}

		if destAddr != nil && conn != nil {
			// Serialize packet
			outBuf, err := packet.Marshal()
			if err != nil {
				fmt.Printf("[%s] Error marshalling RTP: %v\n", s.ID, err)
				continue
			}

			// Log first few forwarded packets
			if packetCount <= 5 || packetCount%500 == 0 || isKeyframe {
				fmt.Printf("[%s] 📤 Re-packetized %s #%d (Seq=%d, SSRC=%d, Size=%d) → %s\n",
					s.ID, kind, packetCount, packet.Header.SequenceNumber, packet.Header.SSRC, len(outBuf), destAddr)
			}

			_, err = conn.WriteToUDP(outBuf, destAddr)
			if err != nil {
				if packetCount <= 5 {
					fmt.Printf("[%s] Error forwarding %s to Asterisk: %v\n", s.ID, kind, err)
				}
			} else if kind == "video" && isKeyframe {
				s.RecordUplinkKeyframe()
				fmt.Printf("[%s] 🔑 KEYFRAME (Forwarded) detected in video packet #%d → Asterisk\n", s.ID, packetCount)
			}
		} else if packetCount == 1 {
			fmt.Printf("[%s] ⚠️ Cannot forward %s: Asterisk endpoint not set yet\n", s.ID, kind)
		}
	}
}

func (s *Session) cacheSTAPAParameterSetsLocked(payload []byte, packetCount int) {
	offset := 0
	for offset+2 <= len(payload) {
		nalSize := int(payload[offset])<<8 | int(payload[offset+1])
		offset += 2
		if offset+nalSize > len(payload) {
			break
		}
		nalUnit := payload[offset : offset+nalSize]
		offset += nalSize
		if len(nalUnit) == 0 {
			continue
		}
		switch nalUnit[0] & 0x1F {
		case 7:
			if len(s.CachedSPS) == 0 || !bytes.Equal(s.CachedSPS, nalUnit) {
				s.CachedSPS = append([]byte(nil), nalUnit...)
				if packetCount <= 50 {
					fmt.Printf("[%s] 💾 Cached SPS from STAP-A (%d bytes)\n", s.ID, len(nalUnit))
				}
			}
		case 8:
			if len(s.CachedPPS) == 0 || !bytes.Equal(s.CachedPPS, nalUnit) {
				s.CachedPPS = append([]byte(nil), nalUnit...)
				if packetCount <= 50 {
					fmt.Printf("[%s] 💾 Cached PPS from STAP-A (%d bytes)\n", s.ID, len(nalUnit))
				}
			}
		}
	}
}
