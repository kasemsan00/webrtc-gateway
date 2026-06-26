package api

import "testing"

func TestTranslatorVoiceForTargetLang(t *testing.T) {
	tests := []struct {
		name       string
		targetLang string
		fallback   string
		want       string
	}{
		{name: "english base", targetLang: "en", fallback: "fallback", want: "en-US-AriaNeural"},
		{name: "english locale", targetLang: "en-US", fallback: "fallback", want: "en-US-AriaNeural"},
		{name: "thai base", targetLang: "th", fallback: "fallback", want: "th-TH-PremwadeeNeural"},
		{name: "thai locale underscore", targetLang: "th_TH", fallback: "fallback", want: "th-TH-PremwadeeNeural"},
		{name: "chinese base", targetLang: "zh", fallback: "fallback", want: "zh-CN-XiaoxiaoNeural"},
		{name: "chinese locale", targetLang: "zh-CN", fallback: "fallback", want: "zh-CN-XiaoxiaoNeural"},
		{name: "korean base", targetLang: "ko", fallback: "fallback", want: "ko-KR-SunHiNeural"},
		{name: "japanese base", targetLang: "ja", fallback: "fallback", want: "ja-JP-NanamiNeural"},
		{name: "russian base", targetLang: "ru", fallback: "fallback", want: "ru-RU-SvetlanaNeural"},
		{name: "hindi base", targetLang: "hi", fallback: "fallback", want: "hi-IN-SwaraNeural"},
		{name: "german base", targetLang: "de", fallback: "fallback", want: "de-DE-KatjaNeural"},
		{name: "french base", targetLang: "fr", fallback: "fallback", want: "fr-FR-DeniseNeural"},
		{name: "unknown uses fallback", targetLang: "es", fallback: "es-ES-ElviraNeural", want: "es-ES-ElviraNeural"},
		{name: "unknown without fallback uses english", targetLang: "es", fallback: "", want: "en-US-AriaNeural"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translatorVoiceForTargetLang(tt.targetLang, tt.fallback)
			if got != tt.want {
				t.Fatalf("translatorVoiceForTargetLang(%q, %q) = %q, want %q", tt.targetLang, tt.fallback, got, tt.want)
			}
		})
	}
}
