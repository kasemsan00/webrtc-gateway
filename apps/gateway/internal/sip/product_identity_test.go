package sip

import (
	"strings"
	"testing"
)

func TestStaticRegisterRequestUsesCanonicalProductUserAgent(t *testing.T) {
	server := &Server{
		publicAddress: "127.0.0.1",
		sipPort:       5060,
	}

	req, err := server.createRegisterRequestWithParams(RegisterParams{
		Domain:   "127.0.0.1",
		Username: "gateway-user",
		Port:     5060,
	})
	if err != nil {
		t.Fatalf("create register request: %v", err)
	}

	headers := req.GetHeaders("User-Agent")
	if len(headers) != 1 {
		t.Fatalf("expected one User-Agent header, got %d", len(headers))
	}
	got := headers[0].Value()
	if got != "WebRTC-SIP-Gateway/1.0" {
		t.Fatalf("User-Agent = %q, want canonical product token", got)
	}
	if strings.Contains(strings.ToLower(got), "k2") {
		t.Fatalf("User-Agent must not retain temporary branding: %q", got)
	}
}
