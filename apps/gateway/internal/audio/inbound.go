package audio

import (
	"fmt"

	"github.com/pion/rtp"

	"webrtc-sip-gateway/internal/translator"
)

// InboundGainProcessor decodes Opus RTP, applies PCM gain, and re-encodes Opus.
type InboundGainProcessor struct {
	codec translator.OpusCodec
	gain  float32
}

func NewInboundGainProcessor(codec translator.OpusCodec, gain float32) *InboundGainProcessor {
	return &InboundGainProcessor{
		codec: codec,
		gain:  gain,
	}
}

// Process returns a new RTP packet with gained Opus payload.
func (p *InboundGainProcessor) Process(packet *rtp.Packet) (*rtp.Packet, error) {
	if packet == nil || len(packet.Payload) == 0 {
		return nil, fmt.Errorf("empty rtp packet")
	}

	pcm, err := p.codec.Decode(packet.Payload)
	if err != nil {
		return nil, err
	}

	ApplyGain(pcm, p.gain)

	opusOut, err := p.codec.Encode(pcm)
	if err != nil {
		return nil, err
	}

	return &rtp.Packet{
		Header: rtp.Header{
			Version:        packet.Header.Version,
			Padding:        packet.Header.Padding,
			Extension:      packet.Header.Extension,
			Marker:         packet.Header.Marker,
			PayloadType:    packet.Header.PayloadType,
			SequenceNumber: packet.Header.SequenceNumber,
			Timestamp:      packet.Header.Timestamp,
			SSRC:           packet.Header.SSRC,
			CSRC:           packet.Header.CSRC,
			Extensions:     packet.Header.Extensions,
		},
		Payload: opusOut,
	}, nil
}

func (p *InboundGainProcessor) Close() {
	if p.codec != nil {
		p.codec.Close()
		p.codec = nil
	}
}
