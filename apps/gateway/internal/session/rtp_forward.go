package session

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// rtpByteReader is the WebRTC track read used by the uplink forwarder.
// TrackRemote.Read returns interceptor attributes this loop does not use.
type rtpByteReader interface {
	Read(b []byte) (int, error)
}

type trackRTPReader struct {
	track *webrtc.TrackRemote
}

func (r trackRTPReader) Read(b []byte) (int, error) {
	n, _, err := r.track.Read(b)
	return n, err
}

// forwardRTPToAsterisk forwards RTP packets from WebRTC track to Asterisk
// This is extracted to be reusable for both initial connection and renegotiation
func (s *Session) forwardRTPToAsterisk(track *webrtc.TrackRemote, kind string) {
	s.forwardRTPToAsteriskFrom(trackRTPReader{track: track}, kind)
}

func (s *Session) forwardRTPToAsteriskFrom(track rtpByteReader, kind string) {
	if kind == "video" {
		defer s.flushUplinkPicture()
	}
	buffer := make([]byte, s.RTPBufferSize)
	packetCount := 0
	heldUntilIDR := 0
	mode0Reassembler := h264Mode0Reassembler{}
	mode0Reassembled := 0
	mode0Dropped := 0
	browserRTP := 0
	browserRTPWindow := time.Time{}
	browserRTPPerSec := 0

	for {
		n, err := track.Read(buffer)
		if err != nil {
			if videoReadShouldStop(err, s.GetState() == StateEnded) {
				if s.GetState() != StateEnded {
					fmt.Printf("[%s] stopped reading %s from WebRTC: %v\n", s.ID, kind, err)
				}
				return
			}
			fmt.Printf("[%s] Error reading %s from WebRTC (reader stays up): %v\n", s.ID, kind, err)
			time.Sleep(20 * time.Millisecond)
			continue
		}

		packetCount++
		if kind == "video" {
			now := time.Now()
			browserRTP++
			if browserRTPWindow.IsZero() {
				browserRTPWindow = now
			}
			if elapsed := now.Sub(browserRTPWindow); elapsed >= time.Second {
				browserRTPPerSec = int(float64(browserRTP) / elapsed.Seconds())
				browserRTP = 0
				browserRTPWindow = now
				s.MaybeLogUplinkVideoStall(now, browserRTPPerSec)
			}
		}

		// Parse RTP packet
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buffer[:n]); err != nil {
			continue
		}

		if kind == "video" && s.GetSIPVideoPacketizationMode() == 0 {
			decision := mode0Reassembler.push(packet)
			packet = decision.packet
			switch decision.result {
			case h264Mode0Buffered:
				continue
			case h264Mode0Dropped:
				mode0Dropped++
				if mode0Dropped <= 5 || mode0Dropped%100 == 0 || decision.reason == "oversized-fua" {
					fmt.Printf("[%s] h264_mode0_drop count=%d reason=%s nal_bytes=%d\n", s.ID, mode0Dropped, decision.reason, decision.nalBytes)
				}
				if s.NoteMode0Drop(time.Now(), decision.reason, decision.nalBytes) {
					go s.SendBrowserPLIImmediate("mode0_drop")
				}
				s.MaybeLogUplinkVideoStall(time.Now(), browserRTPPerSec)
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
				if !s.rejectUplinkPictureSizeChangeLocked(packet.Payload) {
					curLen := len(s.CachedSPS)
					shouldUpdate := len(packet.Payload) > 1 && (curLen == 0 || !bytes.Equal(s.CachedSPS, packet.Payload))
					if shouldUpdate {
						s.CachedSPS = make([]byte, len(packet.Payload))
						copy(s.CachedSPS, packet.Payload)
						fmt.Printf("[%s] 💾 Cached SPS from RTP stream (packet #%d, %d bytes)\\n", s.ID, packetCount, len(packet.Payload))
					}
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
			if s.rejectUplinkPictureSizeChangeLocked(packet.Payload) {
				s.mu.Unlock()
				s.MaybeLogUplinkVideoStall(time.Now(), browserRTPPerSec)
				continue
			}
			destAddr = s.AsteriskVideoAddr
			conn = s.VideoRTPConn
			// s.mu is already held here. Do not call the public getter because it
			// acquires s.mu.RLock and would self-deadlock on the first video packet.
			videoPT := s.sipVideoPayloadTypeLocked()

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
						if skip, _ := s.SkipUplinkKeyframeRequest("startup", time.Now()); skip {
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

			uplinkOpen := s.uplinkVideoOpenLocked()
			// STAP-A learns whether it contains an IDR only after the inner scan.
			// Probing the hold here would drop that IDR or clear mode-0 recovery
			// before the scan runs. SPS/PPS ride with the IDR they precede so a
			// cold start does not drop them and then inject a second pair.
			held := false
			pictureSizePLI := false
			if !isSTAPA {
				var sizeHeld bool
				sizeHeld, pictureSizePLI = s.holdUplinkForPictureSizeLocked(isKeyframe, isSPS || isPPS, nalType, time.Now())
				held = s.holdUplinkForMode0Locked(isKeyframe) || holdUplinkVideoUntilIDR(uplinkOpen, isKeyframe || isSPS || isPPS)
				held = held || sizeHeld
			}
			if isSPS || isPPS {
				s.recordUplinkParameterSetIfForwarded(held, packet.Header.Timestamp, isSPS, isPPS)
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

				sizeHeld, sizePLI := s.holdUplinkForPictureSizeLocked(containsIDR, containsSPS || containsPPS, nalType, time.Now())
				if s.holdUplinkForMode0Locked(containsIDR) || holdUplinkVideoUntilIDR(uplinkOpen, containsIDR || containsSPS || containsPPS) || sizeHeld {
					s.mu.Unlock()
					if sizePLI {
						go s.SendBrowserPLIImmediate("picture_size_change")
					}
					heldUntilIDR++
					if heldUntilIDR <= 5 || heldUntilIDR%100 == 0 {
						fmt.Printf("[%s] holding WebRTC→SIP video until uplink IDR (held=%d kind=stap-a)\n", s.ID, heldUntilIDR)
					}
					continue
				}
				if containsIDR {
					s.noteUplinkIDRAcceptedLocked()
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
								PayloadType:    videoPT,
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
								PayloadType:    videoPT,
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
								PayloadType:    videoPT,
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
				// The picture hold assigns the marker from the browser bit or
				// the next timestamp, so one IDR stays one access unit.
				if stapaForwarded {
					for _, w := range pendingWrites {
						s.enqueueUplinkVideo(conn, destAddr, w, false, packet.Header.Timestamp, packetCount)
					}
				} else if containsIDR {
					fmt.Printf("[%s] 🔑 KEYFRAME (STAP-A IDR) held in video packet #%d - Asterisk endpoint not set yet\n", s.ID, packetCount)
				}
				continue // STAP-A handled, skip normal forwarding
			}

			if held {
				s.mu.Unlock()
				if pictureSizePLI {
					go s.SendBrowserPLIImmediate("picture_size_change")
				}
				heldUntilIDR++
				if heldUntilIDR <= 5 || heldUntilIDR%100 == 0 {
					fmt.Printf("[%s] holding WebRTC→SIP video until uplink IDR (held=%d nal=%d)\n", s.ID, heldUntilIDR, nalType)
				}
				continue
			}
			if isKeyframe {
				// Accept before the picture hold releases this packet. The next
				// FU-A fragment or P-slice is already in the read loop, and
				// RecordUplinkKeyframe does not run until the write.
				s.noteUplinkIDRAcceptedLocked()
			}

			// Inject SPS/PPS once per IDR access unit. Packetization-mode 0 puts
			// every slice in its own NAL type 5 packet, so a packet-count window
			// repeats SPS/PPS in front of each slice and resets the far decoder.
			redundancyCount := 0
			if len(s.CachedSPS) > 0 && len(s.CachedPPS) > 0 && isKeyframe {
				redundancyCount = s.uplinkKeyframeParameterSetCopiesLocked(packet.Header.Timestamp)
			}

			if redundancyCount > 0 {
				s.LastSPSPPSInjectionTime = time.Now()

				for i := 0; i < redundancyCount; i++ {
					// 1. Send SPS (with forced NRI=3)
					// Marker=false (same as Linphone Mobile)
					s.VideoSeq++
					spsPacket := &rtp.Packet{
						Header: rtp.Header{
							Version:        2,
							PayloadType:    videoPT,
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
							PayloadType:    videoPT,
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
			packet.Header.PayloadType = videoPT // H264 negotiated with the SIP peer
			packet.Header.Extension = false
			packet.Header.Extensions = nil
			packet.Header.CSRC = nil
			packet.Header.Padding = false
		}
		s.mu.Unlock()

		videoForward := kind == "video" && destAddr != nil && conn != nil
		// Flush any buffered SPS/PPS injection writes outside lock.
		// Video datagrams share one picture hold so slices keep their timestamp
		// and only the browser's last slice (or the slice before a new
		// timestamp) gets the marker. Audio stays on this loop.
		if videoForward {
			for _, w := range pendingWrites {
				s.enqueueUplinkVideo(conn, destAddr, w, false, packet.Header.Timestamp, packetCount)
			}
		} else {
			for _, w := range pendingWrites {
				conn.WriteToUDP(w, destAddr)
			}
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

			if videoForward {
				s.enqueueUplinkVideo(conn, destAddr, outBuf, isKeyframe, packet.Header.Timestamp, packetCount)
			} else if _, err = conn.WriteToUDP(outBuf, destAddr); err != nil && packetCount <= 5 {
				fmt.Printf("[%s] Error forwarding %s to Asterisk: %v\n", s.ID, kind, err)
			}
		} else if packetCount == 1 {
			fmt.Printf("[%s] ⚠️ Cannot forward %s: Asterisk endpoint not set yet\n", s.ID, kind)
		}
	}
}

// videoReadShouldStop is true when the track is gone. A short buffer or a
// transient read error must not end the loop: pion records TWCC only while
// Read keeps being called, and stopping it collapses the browser estimate.
func videoReadShouldStop(err error, ended bool) bool {
	if err == nil {
		return false
	}
	if ended || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "closed") || strings.Contains(text, "eof")
}

// rejectUplinkPictureSizeChangeLocked tracks the SPS picture size. In the
// default accept mode a change is taken and the forwarder holds the new size
// until its IDR (armUplinkPictureSizeChangeLocked); this never returns true.
// With SIP_UPLINK_PICTURE_SIZE_CHANGE=reject the first size is kept for the
// call. A later SPS with different cropped dimensions is how Chrome tells
// the phone to reallocate its YUV buffer; TTRS then overflowed in
// glTexSubImage2D. The caller holds s.mu. True means this packet must not be
// forwarded. After a change, every later uplink video packet is dropped too,
// because those slices belong to the new picture size.
func (s *Session) rejectUplinkPictureSizeChangeLocked(payload []byte) bool {
	if s.uplinkSPSDimensionBlocked {
		return true
	}
	for _, sps := range h264SPSUnits(payload) {
		width, height, ok := h264SPSPictureSize(sps)
		if !ok {
			continue
		}
		if s.uplinkSPSWidth == 0 {
			s.uplinkSPSWidth = width
			s.uplinkSPSHeight = height
			fmt.Printf("[%s] uplink_sps picture=%dx%d bytes=%d\n", s.ID, width, height, len(sps))
			continue
		}
		if width == s.uplinkSPSWidth && height == s.uplinkSPSHeight {
			continue
		}
		if s.acceptUplinkPictureSizeChange() {
			s.armUplinkPictureSizeChangeLocked(width, height, time.Now())
			continue
		}
		if s.allowNextUplinkPictureSize {
			fmt.Printf("[%s] uplink_sps_dimension_switch accepted %dx%d was %dx%d\n",
				s.ID, width, height, s.uplinkSPSWidth, s.uplinkSPSHeight)
			s.uplinkSPSWidth = width
			s.uplinkSPSHeight = height
			s.allowNextUplinkPictureSize = false
			s.uplinkSPSDimensionBlocked = false
			s.uplinkSPSDimensionLogged = false
			continue
		}
		s.uplinkSPSDimensionBlocked = true
		if !s.uplinkSPSDimensionLogged {
			s.uplinkSPSDimensionLogged = true
			fmt.Printf("[%s] uplink_sps_dimension_change dropped %dx%d kept %dx%d\n",
				s.ID, width, height, s.uplinkSPSWidth, s.uplinkSPSHeight)
		}
		return true
	}
	return false
}

// recordUplinkParameterSetIfForwarded notes in-band SPS/PPS only when this
// packet is actually sent. A set dropped while waiting for an IDR must not
// suppress the injection that the following IDR needs. The caller holds s.mu.
func (s *Session) recordUplinkParameterSetIfForwarded(held bool, timestamp uint32, sps, pps bool) {
	if held {
		return
	}
	s.noteUplinkInBandParameterSetLocked(timestamp, sps, pps)
}

// noteUplinkInBandParameterSetLocked records SPS/PPS already forwarded in this
// RTP timestamp. The caller holds s.mu.
func (s *Session) noteUplinkInBandParameterSetLocked(timestamp uint32, sps, pps bool) {
	if !s.uplinkInBandTimestampSet || s.uplinkInBandTimestamp != timestamp {
		s.uplinkInBandTimestamp = timestamp
		s.uplinkInBandTimestampSet = true
		s.uplinkInBandSPS = false
		s.uplinkInBandPPS = false
	}
	if sps {
		s.uplinkInBandSPS = true
	}
	if pps {
		s.uplinkInBandPPS = true
	}
}

// uplinkKeyframeParameterSetCopiesLocked reports how many SPS+PPS pairs to
// send before this IDR packet. The caller holds s.mu. Zero means this packet
// continues an access unit that already received parameter sets, or the
// browser already sent SPS and PPS for this timestamp. The first access unit
// that arrives without in-band sets gets two pairs; later ones get one.
func (s *Session) uplinkKeyframeParameterSetCopiesLocked(timestamp uint32) int {
	if s.uplinkSPSPPSTimestampSet && s.uplinkSPSPPSTimestamp == timestamp {
		return 0
	}
	s.uplinkSPSPPSTimestamp = timestamp
	s.uplinkSPSPPSTimestampSet = true
	if s.uplinkInBandTimestampSet && s.uplinkInBandTimestamp == timestamp && s.uplinkInBandSPS && s.uplinkInBandPPS {
		// Chrome mode 0 already emitted SPS and PPS as their own NALs. Another
		// pair in front of each IDR restarts TTRS OpenH264; the GL upload then
		// reads past the previous YUV allocation.
		s.uplinkSPSPPSWarmupDone = true
		return 0
	}
	if !s.uplinkSPSPPSWarmupDone {
		s.uplinkSPSPPSWarmupDone = true
		return 2
	}
	return 1
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
