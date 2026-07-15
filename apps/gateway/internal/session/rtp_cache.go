package session

import "github.com/pion/rtcp"

const videoRTPHistorySize = 1024

func (s *Session) initVideoRTPHistory() {
	s.VideoRTPHistorySize = videoRTPHistorySize
	s.VideoRTPHistoryPackets = make([][]byte, videoRTPHistorySize)
	s.VideoRTPHistorySeq = make([]uint16, videoRTPHistorySize)
}

// CacheVideoRTPPacket stores a copy of the RTP packet for NACK-based retransmission.
func (s *Session) CacheVideoRTPPacket(seq uint16, data []byte) {
	if len(data) == 0 || s.VideoRTPHistorySize == 0 {
		return
	}
	packetSSRC, ok := videoRTPPacketSSRC(data)
	if !ok {
		return
	}

	index := int(seq % uint16(s.VideoRTPHistorySize))

	s.mu.RLock()
	defer s.mu.RUnlock()
	if packetSSRC != s.WebRTCVideoEgressSSRC {
		return
	}

	s.videoRTPHistoryMu.Lock()
	if cap(s.VideoRTPHistoryPackets[index]) < len(data) {
		s.VideoRTPHistoryPackets[index] = make([]byte, len(data))
	} else {
		s.VideoRTPHistoryPackets[index] = s.VideoRTPHistoryPackets[index][:len(data)]
	}
	copy(s.VideoRTPHistoryPackets[index], data)
	s.VideoRTPHistorySeq[index] = seq
	s.videoRTPHistoryMu.Unlock()
}

// ClearVideoRTPHistory invalidates packets cached under the previous egress
// SSRC so a later NACK cannot retransmit them after an SSRC remap.
func (s *Session) ClearVideoRTPHistory() {
	s.videoRTPHistoryMu.Lock()
	clear(s.VideoRTPHistoryPackets)
	clear(s.VideoRTPHistorySeq)
	s.videoRTPHistoryMu.Unlock()
}

func (s *Session) getCachedVideoRTPPacket(seq uint16) []byte {
	if s.VideoRTPHistorySize == 0 {
		return nil
	}

	index := int(seq % uint16(s.VideoRTPHistorySize))

	s.mu.RLock()
	defer s.mu.RUnlock()
	s.videoRTPHistoryMu.RLock()
	if s.VideoRTPHistorySeq[index] != seq || len(s.VideoRTPHistoryPackets[index]) == 0 {
		s.videoRTPHistoryMu.RUnlock()
		return nil
	}
	original := s.VideoRTPHistoryPackets[index]
	packetSSRC, ok := videoRTPPacketSSRC(original)
	if !ok || packetSSRC != s.WebRTCVideoEgressSSRC {
		s.videoRTPHistoryMu.RUnlock()
		return nil
	}
	copyBuf := make([]byte, len(original))
	copy(copyBuf, original)
	s.videoRTPHistoryMu.RUnlock()

	return copyBuf
}

func videoRTPPacketSSRC(data []byte) (uint32, bool) {
	if len(data) < 12 || data[0]>>6 != 2 {
		return 0, false
	}
	return uint32(data[8])<<24 |
		uint32(data[9])<<16 |
		uint32(data[10])<<8 |
		uint32(data[11]), true
}

// RetransmitVideoNACK attempts to resend cached RTP packets in response to NACKs.
// Returns (sent, missing).
//
// Lock ordering: mu.RLock then videoRTPHistoryMu.RLock, held from cache lookup
// and egress SSRC validation through VideoTrack.Write. Remap takes mu.Lock then
// videoRTPHistoryMu.Lock (ClearVideoRTPHistory), so it blocks until retransmit
// finishes and cannot interleave a history clear mid-write.
func (s *Session) RetransmitVideoNACK(nacks []rtcp.NackPair) (int, int) {
	if s.VideoTrack == nil || len(nacks) == 0 {
		return 0, len(nacks)
	}

	sent := 0
	missing := 0

	for _, pair := range nacks {
		seqs := []uint16{pair.PacketID}
		for i := 0; i < 16; i++ {
			if (pair.LostPackets & (1 << i)) != 0 {
				seqs = append(seqs, pair.PacketID+uint16(i)+1)
			}
		}

		for _, seq := range seqs {
			if s.VideoRTPHistorySize == 0 {
				missing++
				continue
			}

			index := int(seq % uint16(s.VideoRTPHistorySize))

			s.mu.RLock()
			s.videoRTPHistoryMu.RLock()

			wrote := false
			if s.VideoRTPHistorySeq[index] == seq && len(s.VideoRTPHistoryPackets[index]) > 0 {
				original := s.VideoRTPHistoryPackets[index]
				packetSSRC, ok := videoRTPPacketSSRC(original)
				if ok && packetSSRC == s.WebRTCVideoEgressSSRC {
					packet := make([]byte, len(original))
					copy(packet, original)
					if _, err := s.VideoTrack.Write(packet); err == nil {
						wrote = true
					}
				}
			}

			s.videoRTPHistoryMu.RUnlock()
			s.mu.RUnlock()

			if wrote {
				sent++
			} else {
				missing++
			}
		}
	}

	return sent, missing
}
