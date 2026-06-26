package translator

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"

	"k2-gateway/internal/translator/pb"
)

type S2SPipeline struct {
	client            *Client
	codec             OpusCodec
	srcLang           string
	tgtLang           string
	ttsVoice          string
	srcPort           int
	stats             S2SStats
	statsMu           sync.Mutex
	running           atomic.Bool
	cancel            context.CancelFunc
	codecMu           sync.Mutex
	bufferMu          sync.Mutex
	pendingOpusFrames [][]byte
	streamMu          sync.Mutex
	stream            pb.SpeechTranslator_TranslateClient
	streamCancel      context.CancelFunc
	sendBuffer        []byte
}

const (
	translatorSampleRate = 16000
	translatorChunkBytes = 1280
	translatorChannels   = 1
	translatorSampleBits = 16
)

type S2SStats struct {
	PacketsIn    int64
	PacketsOut   int64
	DecodeErrors int64
	EncodeErrors int64
	SendErrors   int64
	RecvErrors   int64
	BytesIn      int64
	BytesOut     int64
	LastPacketAt time.Time
	LastError    string
	LastErrorAt  time.Time
}

func NewS2SPipeline(client *Client, codec OpusCodec, srcLang, tgtLang, ttsVoice string) *S2SPipeline {
	if srcLang == "" {
		srcLang = client.cfg.SourceLang
	}
	if tgtLang == "" {
		tgtLang = client.cfg.TargetLang
	}
	if ttsVoice == "" {
		ttsVoice = client.cfg.TTSVoice
	}
	return &S2SPipeline{
		client:   client,
		codec:    codec,
		srcLang:  srcLang,
		tgtLang:  tgtLang,
		ttsVoice: ttsVoice,
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

	if out := p.popPendingFrame(original); out != nil {
		return out, nil
	}

	p.statsMu.Lock()
	p.stats.PacketsIn++
	p.stats.BytesIn += int64(len(original.Payload))
	p.stats.LastPacketAt = time.Now()
	p.statsMu.Unlock()

	p.codecMu.Lock()
	pcm, err := p.codec.Decode(original.Payload)
	p.codecMu.Unlock()
	if err != nil {
		p.statsMu.Lock()
		p.stats.DecodeErrors++
		p.stats.LastError = fmt.Sprintf("decode: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		return nil, err
	}

	pcmBytes := int16SliceToBytes(resamplePCM(pcm, p.codec.SampleRate(), translatorSampleRate))
	if err := p.sendPCMToStream(pcmBytes); err != nil {
		return nil, err
	}

	return p.popPendingFrame(original), nil
}

func (p *S2SPipeline) popPendingFrame(original *rtp.Packet) *rtp.Packet {
	p.bufferMu.Lock()
	defer p.bufferMu.Unlock()

	if len(p.pendingOpusFrames) == 0 {
		return nil
	}
	payload := p.pendingOpusFrames[0]
	p.pendingOpusFrames[0] = nil
	p.pendingOpusFrames = p.pendingOpusFrames[1:]

	return &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    original.PayloadType,
			SequenceNumber: original.SequenceNumber,
			Timestamp:      original.Timestamp,
			SSRC:           original.SSRC,
			Marker:         original.Marker,
		},
		Payload: payload,
	}
}

func (p *S2SPipeline) sendPCMToStream(pcmBytes []byte) error {
	if len(pcmBytes) == 0 {
		return nil
	}
	p.streamMu.Lock()
	defer p.streamMu.Unlock()

	if err := p.ensureStreamLocked(); err != nil {
		return err
	}

	p.sendBuffer = append(p.sendBuffer, pcmBytes...)
	for len(p.sendBuffer) >= translatorChunkBytes {
		chunk := append([]byte(nil), p.sendBuffer[:translatorChunkBytes]...)
		p.sendBuffer = p.sendBuffer[translatorChunkBytes:]
		if err := p.sendAudioChunkLocked(chunk); err != nil {
			p.closeStreamLocked()
			return err
		}
	}
	return nil
}

func (p *S2SPipeline) ensureStreamLocked() error {
	if p.stream != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := p.client.TranslateStream(ctx)
	if err != nil {
		cancel()
		p.statsMu.Lock()
		p.stats.SendErrors++
		p.stats.LastError = fmt.Sprintf("stream open: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		return err
	}

	if err := stream.Send(&pb.TranslationRequest{
		SourceLanguage: normalizeSpeechLocale(p.srcLang),
		TargetLanguage: normalizeTranslationTarget(p.tgtLang),
		ReturnAudio:    true,
		TTSVoiceName:   p.ttsVoice,
		Mode:           pb.TranslationMode_MODE_S2S,
	}); err != nil {
		stream.CloseSend()
		cancel()
		p.statsMu.Lock()
		p.stats.SendErrors++
		p.stats.LastError = fmt.Sprintf("send config: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		return err
	}

	p.stream = stream
	p.streamCancel = cancel
	go p.readResponses(stream)
	return nil
}

func (p *S2SPipeline) sendAudioChunkLocked(chunk []byte) error {
	req := &pb.TranslationRequest{
		SourceLanguage: normalizeSpeechLocale(p.srcLang),
		TargetLanguage: normalizeTranslationTarget(p.tgtLang),
		ReturnAudio:    true,
		TTSVoiceName:   p.ttsVoice,
		AudioData:      chunk,
		Mode:           pb.TranslationMode_MODE_S2S,
	}
	if err := p.stream.Send(req); err != nil {
		p.statsMu.Lock()
		p.stats.SendErrors++
		p.stats.LastError = fmt.Sprintf("send audio: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		return err
	}
	return nil
}

func (p *S2SPipeline) readResponses(stream pb.SpeechTranslator_TranslateClient) {
	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				p.statsMu.Lock()
				p.stats.LastError = "recv: stream EOF"
				p.stats.LastErrorAt = time.Now()
				p.statsMu.Unlock()
				return
			}
			p.statsMu.Lock()
			p.stats.RecvErrors++
			p.stats.LastError = fmt.Sprintf("recv: %v", err)
			p.stats.LastErrorAt = time.Now()
			p.statsMu.Unlock()
			p.streamMu.Lock()
			if p.stream == stream {
				p.closeStreamLocked()
			}
			p.streamMu.Unlock()
			return
		}

		if len(resp.AudioData) == 0 {
			continue
		}

		pcmBytesOut, sampleRate, channels := decodeTranslatorAudio(resp.AudioData)
		if channels > 1 {
			pcmBytesOut = downmixPCMBytesToMono(pcmBytesOut, channels)
		}
		pcmOut := resamplePCM(bytesToInt16Slice(pcmBytesOut), sampleRate, p.codec.SampleRate())
		p.codecMu.Lock()
		opusFrames, err := p.encodePCMFrames(pcmOut)
		p.codecMu.Unlock()
		if err != nil {
			p.statsMu.Lock()
			p.stats.LastError = fmt.Sprintf("encode response: %v", err)
			p.stats.LastErrorAt = time.Now()
			p.statsMu.Unlock()
			continue
		}

		var bytesOut int64
		for _, frame := range opusFrames {
			bytesOut += int64(len(frame))
		}
		p.statsMu.Lock()
		p.stats.PacketsOut += int64(len(opusFrames))
		p.stats.BytesOut += bytesOut
		p.statsMu.Unlock()

		p.bufferMu.Lock()
		p.pendingOpusFrames = append(p.pendingOpusFrames, opusFrames...)
		p.bufferMu.Unlock()
	}
}

func (p *S2SPipeline) closeStreamLocked() {
	if p.stream != nil {
		_ = p.stream.CloseSend()
		p.stream = nil
	}
	if p.streamCancel != nil {
		p.streamCancel()
		p.streamCancel = nil
	}
	p.sendBuffer = nil
}

func (p *S2SPipeline) encodePCMFrames(pcmOut []int16) ([][]byte, error) {
	frameSamples := int(float64(p.codec.SampleRate()) * p.codec.FrameDuration().Seconds())
	if frameSamples <= 0 {
		frameSamples = len(pcmOut)
	}

	frames := make([][]byte, 0, (len(pcmOut)+frameSamples-1)/frameSamples)
	for start := 0; start < len(pcmOut); start += frameSamples {
		end := start + frameSamples
		frame := pcmOut[start:min(end, len(pcmOut))]
		if len(frame) < frameSamples {
			padded := make([]int16, frameSamples)
			copy(padded, frame)
			frame = padded
		}
		opusOut, err := p.codec.Encode(frame)
		if err != nil {
			p.statsMu.Lock()
			p.stats.EncodeErrors++
			p.stats.LastError = fmt.Sprintf("encode: %v", err)
			p.stats.LastErrorAt = time.Now()
			p.statsMu.Unlock()
			return nil, err
		}
		frames = append(frames, opusOut)
	}
	return frames, nil
}

func (p *S2SPipeline) Start() {
	p.running.Store(true)
}

func (p *S2SPipeline) Stop() {
	p.running.Store(false)
	p.streamMu.Lock()
	p.closeStreamLocked()
	p.streamMu.Unlock()
	p.bufferMu.Lock()
	p.pendingOpusFrames = nil
	p.bufferMu.Unlock()
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

func resamplePCM(in []int16, fromRate, toRate int) []int16 {
	if len(in) == 0 {
		return nil
	}
	if fromRate <= 0 || toRate <= 0 || fromRate == toRate {
		return append([]int16(nil), in...)
	}
	outLen := len(in) * toRate / fromRate
	if outLen <= 0 {
		outLen = 1
	}
	out := make([]int16, outLen)
	for i := range out {
		src := i * fromRate / toRate
		if src >= len(in) {
			src = len(in) - 1
		}
		out[i] = in[src]
	}
	return out
}

func downmixPCMBytesToMono(in []byte, channels int) []byte {
	if channels <= 1 || len(in) < 2*channels {
		return in
	}
	frameBytes := channels * 2
	frames := len(in) / frameBytes
	out := make([]byte, frames*2)
	for frame := 0; frame < frames; frame++ {
		base := frame * frameBytes
		var sum int
		for ch := 0; ch < channels; ch++ {
			sum += int(int16(binary.LittleEndian.Uint16(in[base+ch*2:])))
		}
		avg := int16(sum / channels)
		binary.LittleEndian.PutUint16(out[frame*2:], uint16(avg))
	}
	return out
}

func decodeTranslatorAudio(audio []byte) ([]byte, int, int) {
	if len(audio) < 12 || !bytes.Equal(audio[0:4], []byte("RIFF")) || !bytes.Equal(audio[8:12], []byte("WAVE")) {
		return audio, translatorSampleRate, translatorChannels
	}

	sampleRate := translatorSampleRate
	channels := translatorChannels
	var (
		offset        = 12
		fmtFound      bool
		dataFound     bool
		pcmData       []byte
		formatTag     uint16
		bitsPerSample uint16
	)

	for offset+8 <= len(audio) {
		chunkID := audio[offset : offset+4]
		chunkSize := int(binary.LittleEndian.Uint32(audio[offset+4 : offset+8]))
		offset += 8
		if chunkSize < 0 || offset+chunkSize > len(audio) {
			break
		}
		chunk := audio[offset : offset+chunkSize]
		switch string(chunkID) {
		case "fmt ":
			if len(chunk) >= 16 {
				formatTag = binary.LittleEndian.Uint16(chunk[0:2])
				channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
				sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
				bitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])
				fmtFound = true
			}
		case "data":
			pcmData = chunk
			dataFound = true
		}
		offset += chunkSize
		if chunkSize%2 == 1 {
			offset++
		}
	}

	if fmtFound && dataFound && formatTag == 1 && bitsPerSample == translatorSampleBits && sampleRate > 0 && channels > 0 {
		return pcmData, sampleRate, channels
	}
	return audio, translatorSampleRate, translatorChannels
}

func normalizeSpeechLocale(lang string) string {
	lang = strings.TrimSpace(strings.ReplaceAll(lang, "_", "-"))
	if lang == "" {
		return "en-US"
	}
	if strings.Contains(lang, "-") {
		return lang
	}
	switch strings.ToLower(lang) {
	case "en":
		return "en-US"
	case "th":
		return "th-TH"
	default:
		return lang
	}
}

func normalizeTranslationTarget(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(lang, "_", "-")))
	if lang == "" {
		return "th"
	}
	if idx := strings.Index(lang, "-"); idx > 0 {
		return lang[:idx]
	}
	return lang
}
