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

	index := int(seq % uint16(s.VideoRTPHistorySize))

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

// ClearVideoRTPHistory invalidates cached RTP packets.
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

	s.videoRTPHistoryMu.RLock()
	if s.VideoRTPHistorySeq[index] != seq || len(s.VideoRTPHistoryPackets[index]) == 0 {
		s.videoRTPHistoryMu.RUnlock()
		return nil
	}
	copyBuf := make([]byte, len(s.VideoRTPHistoryPackets[index]))
	copy(copyBuf, s.VideoRTPHistoryPackets[index])
	s.videoRTPHistoryMu.RUnlock()

	return copyBuf
}

// RetransmitVideoNACK attempts to resend cached RTP packets in response to NACKs.
// Returns (sent, missing).
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
			packet := s.getCachedVideoRTPPacket(seq)
			if len(packet) == 0 {
				missing++
				continue
			}
			if _, err := s.WriteVideoToWebRTC(packet); err == nil {
				sent++
			} else {
				missing++
			}
		}
	}

	return sent, missing
}
