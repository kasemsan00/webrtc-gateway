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
	client             *Client
	codec              OpusCodec
	srcLang            string
	tgtLang            string
	ttsVoice           string
	srcPort            int
	stats              S2SStats
	statsMu            sync.Mutex
	running            atomic.Bool
	cancel             context.CancelFunc
	debugLabel         string
	codecMu            sync.Mutex
	bufferMu           sync.Mutex
	pendingOpusFrames  [][]byte
	streamMu           sync.Mutex
	stream             pb.SpeechTranslator_TranslateClient
	streamCancel       context.CancelFunc
	sendBuffer         []byte
	captionMu          sync.RWMutex
	captionHandler     CaptionHandler
	partialMu          sync.Mutex
	lastPartialT2SAt   time.Time
	lastPartialT2SText string
}

const (
	translatorSampleRate  = 16000
	translatorChunkBytes  = 1280
	translatorChannels    = 1
	translatorSampleBits  = 16
	partialT2SMinInterval = 3 * time.Second
	partialT2SMinChars    = 18
)

type CaptionEvent struct {
	SourceLang     string
	TargetLang     string
	RecognizedText string
	TranslatedText string
	IsFinal        bool
}

type CaptionHandler func(CaptionEvent)

type S2SStats struct {
	PacketsIn            int64
	PacketsOut           int64
	PassthroughNoFrame   int64
	Responses            int64
	AudioResponses       int64
	EmptyAudioResponses  int64
	FallbackT2SAttempts  int64
	FallbackT2SSuccesses int64
	FallbackT2SErrors    int64
	FallbackT2SSkips     int64
	PartialT2SAttempts   int64
	PartialT2SSuccesses  int64
	PartialT2SErrors     int64
	PartialT2SSkips      int64
	DecodeErrors         int64
	EncodeErrors         int64
	SendErrors           int64
	RecvErrors           int64
	BytesIn              int64
	BytesOut             int64
	LastPacketAt         time.Time
	LastResponseAt       time.Time
	LastError            string
	LastErrorAt          time.Time
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

func (p *S2SPipeline) SetCaptionHandler(handler CaptionHandler) {
	p.captionMu.Lock()
	defer p.captionMu.Unlock()
	p.captionHandler = handler
}

func (p *S2SPipeline) SetDebugLabel(label string) {
	p.debugLabel = strings.TrimSpace(label)
}

func (p *S2SPipeline) CaptionHandlerSet() bool {
	p.captionMu.RLock()
	defer p.captionMu.RUnlock()
	return p.captionHandler != nil
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

	out := p.popPendingFrame(original)
	if out == nil {
		p.statsMu.Lock()
		p.stats.PassthroughNoFrame++
		passthroughs := p.stats.PassthroughNoFrame
		packetsIn := p.stats.PacketsIn
		packetsOut := p.stats.PacketsOut
		responses := p.stats.Responses
		audioResponses := p.stats.AudioResponses
		emptyAudioResponses := p.stats.EmptyAudioResponses
		p.statsMu.Unlock()
		if passthroughs <= 5 || passthroughs%1000 == 0 {
			p.logf("translator passthrough_no_frame count=%d packets_in=%d packets_out=%d responses=%d audio_responses=%d empty_audio_responses=%d",
				passthroughs, packetsIn, packetsOut, responses, audioResponses, emptyAudioResponses)
		}
	}
	return out, nil
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

		event, hasCaption := captionEventFromResult(resp, p.srcLang, p.tgtLang)
		if hasCaption {
			p.emitCaption(event)
		}

		audioData := resp.GetAudioData()
		respNo, shouldLogResponse := p.recordResponse(audioData, event, hasCaption)
		if shouldLogResponse {
			p.logResponse(respNo, audioData, event, hasCaption)
		}

		if len(audioData) == 0 && hasCaption && shouldFallbackT2S(event) {
			audioData = p.synthesizeFallbackAudio(event)
		} else if len(audioData) == 0 && hasCaption && shouldFallbackPartialT2S(event) {
			p.maybeSynthesizePartialFallback(event)
		} else if len(audioData) == 0 && hasCaption && event.IsFinal && strings.TrimSpace(event.TranslatedText) != "" {
			p.statsMu.Lock()
			p.stats.FallbackT2SSkips++
			skips := p.stats.FallbackT2SSkips
			p.statsMu.Unlock()
			if skips <= 5 || skips%50 == 0 {
				p.logf("translation fallback_t2s skipped count=%d reason=same-or-empty recognized=%q translated=%q",
					skips,
					truncateForLog(event.RecognizedText, 80),
					truncateForLog(event.TranslatedText, 80))
			}
		}

		if len(audioData) == 0 {
			continue
		}

		if err := p.enqueueAudioData(audioData); err != nil {
			p.logf("translation audio enqueue error: %v", err)
		}
	}
}

func (p *S2SPipeline) emitCaption(event CaptionEvent) {
	p.captionMu.RLock()
	handler := p.captionHandler
	p.captionMu.RUnlock()
	if handler == nil {
		return
	}

	handler(event)
}

func (p *S2SPipeline) recordResponse(audioData []byte, event CaptionEvent, hasCaption bool) (int64, bool) {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	p.stats.Responses++
	respNo := p.stats.Responses
	p.stats.LastResponseAt = time.Now()
	if len(audioData) == 0 {
		p.stats.EmptyAudioResponses++
	} else {
		p.stats.AudioResponses++
	}
	shouldLog := respNo <= 10 || respNo%50 == 0 || (hasCaption && event.IsFinal)
	return respNo, shouldLog
}

func (p *S2SPipeline) synthesizeFallbackAudio(event CaptionEvent) []byte {
	p.statsMu.Lock()
	p.stats.FallbackT2SAttempts++
	attempt := p.stats.FallbackT2SAttempts
	p.statsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	audioData, err := p.client.SynthesizeText(ctx, event.TargetLang, event.TranslatedText, p.ttsVoice)
	cancel()
	if err != nil {
		p.statsMu.Lock()
		p.stats.FallbackT2SErrors++
		errors := p.stats.FallbackT2SErrors
		p.stats.LastError = fmt.Sprintf("fallback t2s: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		if errors <= 5 || errors%50 == 0 {
			p.logf("translation fallback_t2s error attempt=%d errors=%d target=%s voice=%q err=%v",
				attempt, errors, event.TargetLang, p.ttsVoice, err)
		}
		return nil
	}
	if len(audioData) == 0 {
		p.logf("translation fallback_t2s empty attempt=%d target=%s voice=%q translated=%q",
			attempt,
			event.TargetLang,
			p.ttsVoice,
			truncateForLog(event.TranslatedText, 80))
		return nil
	}

	p.statsMu.Lock()
	p.stats.FallbackT2SSuccesses++
	successes := p.stats.FallbackT2SSuccesses
	p.statsMu.Unlock()
	p.logf("translation fallback_t2s audio generated attempt=%d successes=%d bytes=%d target=%s voice=%q translated=%q",
		attempt,
		successes,
		len(audioData),
		event.TargetLang,
		p.ttsVoice,
		truncateForLog(event.TranslatedText, 80))
	return audioData
}

func (p *S2SPipeline) maybeSynthesizePartialFallback(event CaptionEvent) {
	now := time.Now()
	text := strings.TrimSpace(event.TranslatedText)
	p.partialMu.Lock()
	if now.Sub(p.lastPartialT2SAt) < partialT2SMinInterval {
		p.partialMu.Unlock()
		p.recordPartialSkip()
		return
	}
	if sameCaptionText(text, p.lastPartialT2SText) || strings.Contains(strings.ToLower(p.lastPartialT2SText), strings.ToLower(text)) {
		p.partialMu.Unlock()
		p.recordPartialSkip()
		return
	}
	p.lastPartialT2SAt = now
	p.lastPartialT2SText = text
	p.partialMu.Unlock()

	go p.synthesizeAndEnqueuePartialFallback(event)
}

func (p *S2SPipeline) synthesizeAndEnqueuePartialFallback(event CaptionEvent) {
	p.statsMu.Lock()
	p.stats.PartialT2SAttempts++
	attempt := p.stats.PartialT2SAttempts
	p.statsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	audioData, err := p.client.SynthesizeText(ctx, event.TargetLang, event.TranslatedText, p.ttsVoice)
	cancel()
	if err != nil {
		p.statsMu.Lock()
		p.stats.PartialT2SErrors++
		errors := p.stats.PartialT2SErrors
		p.stats.LastError = fmt.Sprintf("partial t2s: %v", err)
		p.stats.LastErrorAt = time.Now()
		p.statsMu.Unlock()
		if errors <= 5 || errors%50 == 0 {
			p.logf("translation partial_t2s error attempt=%d errors=%d target=%s voice=%q err=%v",
				attempt, errors, event.TargetLang, p.ttsVoice, err)
		}
		return
	}
	if len(audioData) == 0 {
		p.logf("translation partial_t2s empty attempt=%d target=%s voice=%q translated=%q",
			attempt,
			event.TargetLang,
			p.ttsVoice,
			truncateForLog(event.TranslatedText, 80))
		return
	}
	if !p.running.Load() {
		return
	}
	if err := p.enqueueAudioData(audioData); err != nil {
		p.logf("translation partial_t2s enqueue error attempt=%d err=%v", attempt, err)
		return
	}

	p.statsMu.Lock()
	p.stats.PartialT2SSuccesses++
	successes := p.stats.PartialT2SSuccesses
	p.statsMu.Unlock()
	p.logf("translation partial_t2s audio generated attempt=%d successes=%d bytes=%d target=%s voice=%q translated=%q",
		attempt,
		successes,
		len(audioData),
		event.TargetLang,
		p.ttsVoice,
		truncateForLog(event.TranslatedText, 80))
}

func (p *S2SPipeline) recordPartialSkip() {
	p.statsMu.Lock()
	p.stats.PartialT2SSkips++
	skips := p.stats.PartialT2SSkips
	p.statsMu.Unlock()
	if skips <= 5 || skips%50 == 0 {
		p.logf("translation partial_t2s skipped count=%d", skips)
	}
}

func (p *S2SPipeline) enqueueAudioData(audioData []byte) error {
	pcmBytesOut, sampleRate, channels := decodeTranslatorAudio(audioData)
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
		return err
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
	return nil
}

func (p *S2SPipeline) logResponse(respNo int64, audioData []byte, event CaptionEvent, hasCaption bool) {
	if !hasCaption {
		p.logf("translation response #%d audio_bytes=%d caption=false", respNo, len(audioData))
		return
	}
	p.logf("translation response #%d audio_bytes=%d final=%t same_text=%t source=%s target=%s recognized=%q translated=%q",
		respNo,
		len(audioData),
		event.IsFinal,
		sameCaptionText(event.RecognizedText, event.TranslatedText),
		event.SourceLang,
		event.TargetLang,
		truncateForLog(event.RecognizedText, 80),
		truncateForLog(event.TranslatedText, 80))
}

func (p *S2SPipeline) logf(format string, args ...any) {
	label := p.debugLabel
	if label == "" {
		label = "translator"
	}
	fmt.Printf("[%s] "+format+"\n", append([]any{label}, args...)...)
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

func parseCaptionText(value string) (string, bool) {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasSuffix(value, "+++"):
		return strings.TrimSpace(strings.TrimSuffix(value, "+++")), false
	case strings.HasSuffix(value, "###"):
		return strings.TrimSpace(strings.TrimSuffix(value, "###")), true
	default:
		return value, true
	}
}

func captionEventFromResult(resp *pb.TranslationResult, sourceLang, targetLang string) (CaptionEvent, bool) {
	if resp == nil {
		return CaptionEvent{}, false
	}
	recognizedText, recognizedFinal := parseCaptionText(resp.GetRecognizedText())
	translatedText, translatedFinal := parseCaptionText(resp.GetTranslatedText())
	if recognizedText == "" && translatedText == "" {
		return CaptionEvent{}, false
	}
	return CaptionEvent{
		SourceLang:     sourceLang,
		TargetLang:     targetLang,
		RecognizedText: recognizedText,
		TranslatedText: translatedText,
		IsFinal:        recognizedFinal && translatedFinal,
	}, true
}

func shouldFallbackT2S(event CaptionEvent) bool {
	if !event.IsFinal || strings.TrimSpace(event.TranslatedText) == "" {
		return false
	}
	if strings.TrimSpace(event.RecognizedText) == "" {
		return true
	}
	return !sameCaptionText(event.RecognizedText, event.TranslatedText)
}

func shouldFallbackPartialT2S(event CaptionEvent) bool {
	translated := strings.TrimSpace(event.TranslatedText)
	if event.IsFinal || len([]rune(translated)) < partialT2SMinChars {
		return false
	}
	if strings.TrimSpace(event.RecognizedText) == "" {
		return true
	}
	return !sameCaptionText(event.RecognizedText, translated)
}

func sameCaptionText(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func truncateForLog(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
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
	case "zh":
		return "zh-CN"
	case "ko":
		return "ko-KR"
	case "ja":
		return "ja-JP"
	case "ru":
		return "ru-RU"
	case "hi":
		return "hi-IN"
	case "de":
		return "de-DE"
	case "fr":
		return "fr-FR"
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
