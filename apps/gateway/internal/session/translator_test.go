package session

import (
	"testing"
	"time"

	"k2-gateway/internal/translator"
)

type fakeTranslatorCodec struct {
	closed bool
}

func (c *fakeTranslatorCodec) Decode(opusData []byte) ([]int16, error) {
	return []int16{1}, nil
}

func (c *fakeTranslatorCodec) Encode(pcm []int16) ([]byte, error) {
	return []byte{1}, nil
}

func (c *fakeTranslatorCodec) FrameDuration() time.Duration {
	return 20 * time.Millisecond
}

func (c *fakeTranslatorCodec) SampleRate() int {
	return 48000
}

func (c *fakeTranslatorCodec) Close() {
	c.closed = true
}

func TestEnableTranslatorCreatesBidirectionalPipelines(t *testing.T) {
	origCreateOpusCodec := createOpusCodec
	defer func() {
		createOpusCodec = origCreateOpusCodec
	}()

	codecCalls := 0
	createOpusCodec = func(bitrate int) (translator.OpusCodec, error) {
		codecCalls++
		if bitrate != 24000 {
			t.Fatalf("unexpected bitrate: %d", bitrate)
		}
		return &fakeTranslatorCodec{}, nil
	}

	sess := &Session{ID: "translator-bidir"}
	client := translator.NewClient(translator.Config{
		SourceLang: "en",
		TargetLang: "th",
		TTSVoice:   "th-TH-PremwadeeNeural",
	})

	sess.SetTranslator(client, "en", "th", "th-TH-PremwadeeNeural", "en-US-AriaNeural")
	sess.SetTranslationCaptionHandler(func(TranslationCaptionEvent) {})
	sess.EnableTranslator()

	if codecCalls != 2 {
		t.Fatalf("expected two codec instances, got %d", codecCalls)
	}
	if !sess.TranslatorEnabled {
		t.Fatal("expected translator to be enabled")
	}
	if sess.Translator == nil {
		t.Fatal("expected outbound translator pipeline")
	}
	if sess.InboundTranslator == nil {
		t.Fatal("expected inbound translator pipeline")
	}
	if sess.InboundTranslatorSrcLang != "th" || sess.InboundTranslatorTgtLang != "en" {
		t.Fatalf("unexpected inbound direction: %s -> %s", sess.InboundTranslatorSrcLang, sess.InboundTranslatorTgtLang)
	}
	if sess.InboundTranslatorTTSVoice != "en-US-AriaNeural" {
		t.Fatalf("unexpected inbound voice: %s", sess.InboundTranslatorTTSVoice)
	}
	if sess.Translator.CaptionHandlerSet() {
		t.Fatal("expected outbound translator to have no caption handler")
	}
	if !sess.InboundTranslator.CaptionHandlerSet() {
		t.Fatal("expected inbound translator to have caption handler")
	}

	sess.DisableTranslator()

	if sess.TranslatorEnabled {
		t.Fatal("expected translator to be disabled")
	}
	if sess.Translator != nil {
		t.Fatal("expected outbound translator to be cleared")
	}
	if sess.InboundTranslator != nil {
		t.Fatal("expected inbound translator to be cleared")
	}
}

func TestEnableTranslatorDoesNotAttachCaptionHandlerWhenUnset(t *testing.T) {
	origCreateOpusCodec := createOpusCodec
	defer func() {
		createOpusCodec = origCreateOpusCodec
	}()

	createOpusCodec = func(bitrate int) (translator.OpusCodec, error) {
		return &fakeTranslatorCodec{}, nil
	}

	sess := &Session{ID: "translator-no-caption"}
	client := translator.NewClient(translator.Config{
		SourceLang: "en",
		TargetLang: "th",
	})

	sess.SetTranslator(client, "en", "th", "", "")
	sess.EnableTranslator()

	if sess.Translator == nil || sess.InboundTranslator == nil {
		t.Fatal("expected translator pipelines")
	}
	if sess.Translator.CaptionHandlerSet() {
		t.Fatal("expected outbound translator to have no caption handler")
	}
	if sess.InboundTranslator.CaptionHandlerSet() {
		t.Fatal("expected inbound translator to have no caption handler when session handler is unset")
	}
}
