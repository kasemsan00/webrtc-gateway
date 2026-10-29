package config

import (
	"testing"
	"time"
)

func TestWebRTCPrepareTTLFromEnv(t *testing.T) {
	cases := map[string]time.Duration{
		"":      DefaultWebRTCPrepareTTL,
		"45":    45 * time.Second,
		"90s":   90 * time.Second,
		"2m":    2 * time.Minute,
		"0":     DefaultWebRTCPrepareTTL,
		"-5s":   DefaultWebRTCPrepareTTL,
		"later": DefaultWebRTCPrepareTTL,
	}
	for value, want := range cases {
		t.Setenv("WEBRTC_PREPARE_TTL", value)
		if got := getEnvAsDurationSeconds("WEBRTC_PREPARE_TTL", DefaultWebRTCPrepareTTL); got != want {
			t.Fatalf("WEBRTC_PREPARE_TTL=%q -> %s, want %s", value, got, want)
		}
	}
}
