package session

import (
	"fmt"

	"github.com/pion/rtp"

	"k2-gateway/internal/audio"
)

// EnableInboundGain activates Opus decode/gain/encode for SIP → WebRTC audio (thread-safe).
func (s *Session) EnableInboundGain(gain float32, opusBitrate int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.InboundGainEnabled {
		return
	}
	codec, err := createOpusCodec(opusBitrate)
	if err != nil {
		fmt.Printf("[%s] Failed to create Opus codec for inbound gain: %v\n", s.ID, err)
		return
	}
	s.inboundGainProc = audio.NewInboundGainProcessor(codec, gain)
	s.InboundGainEnabled = true
	s.InboundGain = gain
	fmt.Printf("[%s] 🔊 Inbound audio gain enabled: %.2f\n", s.ID, gain)
}

// DisableInboundGain deactivates inbound gain processing (thread-safe).
func (s *Session) DisableInboundGain() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inboundGainProc != nil {
		s.inboundGainProc.Close()
		s.inboundGainProc = nil
	}
	if s.InboundGainEnabled {
		fmt.Printf("[%s] 🔊 Inbound audio gain disabled\n", s.ID)
	}
	s.InboundGainEnabled = false
}

// IsInboundGainEnabled returns whether inbound gain is active (thread-safe).
func (s *Session) IsInboundGainEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.InboundGainEnabled
}

// ProcessInboundGain applies configured gain to an inbound Opus RTP packet.
func (s *Session) ProcessInboundGain(packet *rtp.Packet) (*rtp.Packet, error) {
	s.mu.RLock()
	proc := s.inboundGainProc
	s.mu.RUnlock()
	if proc == nil {
		return nil, nil
	}
	return proc.Process(packet)
}