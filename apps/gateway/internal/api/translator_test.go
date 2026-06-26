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
		{name: "thai base", targetLang: "th", fallback: "fallback", want: "th-TH-Sarawut"},
		{name: "thai locale underscore", targetLang: "th_TH", fallback: "fallback", want: "th-TH-Sarawut"},
		{name: "unknown uses fallback", targetLang: "ja", fallback: "ja-JP-NanamiNeural", want: "ja-JP-NanamiNeural"},
		{name: "unknown without fallback uses english", targetLang: "ja", fallback: "", want: "en-US-AriaNeural"},
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
