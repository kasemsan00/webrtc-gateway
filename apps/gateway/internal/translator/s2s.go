package translator

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"

	"k2-gateway/internal/translator/pb"
)

type S2SPipeline struct {
	client    *Client
	codec     OpusCodec
	srcPort   int
	stats     S2SStats
	statsMu   sync.Mutex
	running   atomic.Bool
	cancel    context.CancelFunc
}

type S2SStats struct {
	PacketsIn     int64
	PacketsOut    int64
	DecodeErrors  int64
	EncodeErrors  int64
	SendErrors    int64
	RecvErrors    int64
	BytesIn       int64
	BytesOut      int64
	LastPacketAt  time.Time
	LastError     string
	LastErrorAt   time.Time
}

func NewS2SPipeline(client *Client, codec OpusCodec) *S2SPipeline {
	return &S2SPipeline{
		client: client,
		codec:  codec,
	}
}

func (p *S2SPipeline) Running() bool {
	return p.running.Load()
}

func (p *S2SPipeline) Stats() S2SStats {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats
}

// Process processes a single Opus RTP packet through the S2S pipeline.
// Returns the translated Opus RTP packet and true if successful.
// Returns nil, false if the packet should be skipped (passthrough).
func (p *S2SPipeline) Process(original *rtp.Packet) (*rtp.Packet, error) {
	if !p.running.Load() {
		return nil, nil
	}

	p.statsMu.Lock()
	p.stats.PacketsIn++
	p.stats.BytesIn += int64(len(original.Payload))
	p.stats.LastPacketAt = time.Now()
	p.statsMu.Unlock()

	pcm, err := p.codec.Decode(original.Payload)
	if err != nil {
		p.statsMu.Lock()
		p.stats.DecodeErrors++
		p.stats.LastError = fmt.Sprintf("decode: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := p.client.TranslateStream(ctx)
	if err != nil {
		p.statsMu.Lock()
		p.stats.SendErrors++
		p.stats.LastError = fmt.Sprintf("stream open: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		return nil, err
	}

	pcmBytes := int16SliceToBytes(pcm)
	req := &pb.TranslationRequest{
		SourceLanguage: p.client.cfg.SourceLang,
		TargetLanguage: p.client.cfg.TargetLang,
		ReturnAudio:    true,
		TTSVoiceName:   p.client.cfg.TTSVoice,
		AudioData:      pcmBytes,
		Mode:           pb.TranslationMode_MODE_S2S,
	}

	if err := stream.Send(req); err != nil {
		stream.CloseSend()
		p.statsMu.Lock()
		p.stats.SendErrors++
		p.stats.LastError = fmt.Sprintf("send: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		return nil, err
	}

	resp, err := stream.Recv()
	if err != nil {
		stream.CloseSend()
		if err == io.EOF {
			err = fmt.Errorf("stream EOF")
		}
		p.statsMu.Lock()
		p.stats.RecvErrors++
		p.stats.LastError = fmt.Sprintf("recv: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		return nil, err
	}
	stream.CloseSend()

	if len(resp.AudioData) == 0 {
		return nil, fmt.Errorf("empty audio response")
	}

	pcmOut := bytesToInt16Slice(resp.AudioData)
	opusOut, err := p.codec.Encode(pcmOut)
	if err != nil {
		p.statsMu.Lock()
		p.stats.EncodeErrors++
		p.stats.LastError = fmt.Sprintf("encode: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		return nil, err
	}

	outPacket := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    original.PayloadType,
			SequenceNumber: original.SequenceNumber,
			Timestamp:      original.Timestamp,
			SSRC:           original.SSRC,
			Marker:         original.Marker,
		},
		Payload: opusOut,
	}

	p.statsMu.Lock()
	p.stats.PacketsOut++
	p.stats.BytesOut += int64(len(opusOut))
	p.statsMu.Unlock()

	return outPacket, nil
}

func (p *S2SPipeline) Start() {
	p.running.Store(true)
}

func (p *S2SPipeline) Stop() {
	p.running.Store(false)
}

func int16SliceToBytes(s []int16) []byte {
	b := make([]byte, len(s)*2)
	for i, v := range s {
		b[i*2] = byte(v)
		b[i*2+1] = byte(v >> 8)
	}
	return b
}

func bytesToInt16Slice(b []byte) []int16 {
	s := make([]int16, len(b)/2)
	for i := range s {
		s[i] = int16(b[i*2]) | int16(b[i*2+1])<<8
	}
	return s
}
