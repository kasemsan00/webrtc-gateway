package translator

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestNormalizeSpeechLocale(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "en-US"},
		{in: "en", want: "en-US"},
		{in: "th", want: "th-TH"},
		{in: "en_US", want: "en-US"},
		{in: "ja-JP", want: "ja-JP"},
	}

	for _, tt := range tests {
		if got := normalizeSpeechLocale(tt.in); got != tt.want {
			t.Fatalf("normalizeSpeechLocale(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeTranslationTarget(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "th"},
		{in: "TH", want: "th"},
		{in: "en-US", want: "en"},
		{in: "th_TH", want: "th"},
	}

	for _, tt := range tests {
		if got := normalizeTranslationTarget(tt.in); got != tt.want {
			t.Fatalf("normalizeTranslationTarget(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResamplePCMDownAndUp(t *testing.T) {
	in := make([]int16, 480)
	for i := range in {
		in[i] = int16(i)
	}

	down := resamplePCM(in, 48000, 16000)
	if len(down) != 160 {
		t.Fatalf("downsample length = %d, want 160", len(down))
	}
	if down[0] != 0 || down[1] != 3 || down[2] != 6 {
		t.Fatalf("unexpected downsample values: %v", down[:3])
	}

	up := resamplePCM(down, 16000, 48000)
	if len(up) != 480 {
		t.Fatalf("upsample length = %d, want 480", len(up))
	}
	if up[0] != 0 || up[1] != 0 || up[2] != 0 || up[3] != 3 {
		t.Fatalf("unexpected upsample values: %v", up[:4])
	}
}

func TestDecodeTranslatorAudioRawPCM(t *testing.T) {
	audio := []byte{1, 0, 2, 0}

	pcm, sampleRate, channels := decodeTranslatorAudio(audio)

	if !bytes.Equal(pcm, audio) {
		t.Fatalf("raw pcm changed: got %v want %v", pcm, audio)
	}
	if sampleRate != translatorSampleRate || channels != translatorChannels {
		t.Fatalf("format = %d/%d, want %d/%d", sampleRate, channels, translatorSampleRate, translatorChannels)
	}
}

func TestDecodeTranslatorAudioWAV(t *testing.T) {
	pcmData := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	wav := makePCM16WAV(16000, 1, pcmData)

	pcm, sampleRate, channels := decodeTranslatorAudio(wav)

	if !bytes.Equal(pcm, pcmData) {
		t.Fatalf("pcm = %v, want %v", pcm, pcmData)
	}
	if sampleRate != 16000 || channels != 1 {
		t.Fatalf("format = %d/%d, want 16000/1", sampleRate, channels)
	}
}

func makePCM16WAV(sampleRate, channels int, pcm []byte) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(pcm)))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	byteRate := sampleRate * channels * 2
	_ = binary.Write(&b, binary.LittleEndian, uint32(byteRate))
	blockAlign := channels * 2
	_ = binary.Write(&b, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}
