package session

import (
	"fmt"

	"github.com/pion/rtp"
)

func (s *Session) GetWebRTCVideoEgressSSRC() uint32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.WebRTCVideoEgressSSRC
}

func (s *Session) EnsureWebRTCVideoEgressSSRC(packetSSRC uint32) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.WebRTCVideoEgressSSRC != 0 {
		return s.WebRTCVideoEgressSSRC
	}
	if packetSSRC != 0 {
		s.WebRTCVideoEgressSSRC = packetSSRC
	} else {
		s.WebRTCVideoEgressSSRC = generateSSRC()
		if s.WebRTCVideoEgressSSRC == 0 {
			s.WebRTCVideoEgressSSRC = 1
		}
	}
	fmt.Printf("[%s] webrtc_video_egress_ssrc_init ssrc=%d source=%s\n",
		s.ID, s.WebRTCVideoEgressSSRC, map[bool]string{true: "packet", false: "generated"}[packetSSRC != 0])
	return s.WebRTCVideoEgressSSRC
}

func (s *Session) RemapWebRTCVideoEgressSSRC(reason string) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remapWebRTCVideoEgressSSRCLocked(reason)
}

func (s *Session) remapWebRTCVideoEgressSSRCLocked(reason string) uint32 {
	old := s.WebRTCVideoEgressSSRC
	next := generateSSRC()
	if next == 0 || next == old {
		next = generateSSRC()
	}
	if next == 0 {
		next = old + 1
		if next == 0 {
			next = 1
		}
	}
	s.WebRTCVideoEgressSSRC = next
	fmt.Printf("[%s] switch_webrtc_ssrc_remap generation=%d mediaEpoch=%d old=%d new=%d sipSsrc=%d reason=%s\n",
		s.ID, s.SwitchGeneration, s.MediaEpoch, old, next, s.RemoteVideoSSRC, reason)
	return next
}

func (s *Session) ApplyWebRTCVideoEgressSSRC(packet *rtp.Packet) {
	if packet == nil {
		return
	}
	ssrc := s.EnsureWebRTCVideoEgressSSRC(packet.SSRC)
	packet.SSRC = ssrc
}
