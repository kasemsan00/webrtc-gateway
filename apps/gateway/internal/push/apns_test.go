package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func TestAPNSBaseURLSelectsEnvironment(t *testing.T) {
	if got := apnsBaseURL("sandbox"); got != apnsSandboxBaseURL {
		t.Fatalf("expected sandbox URL, got %q", got)
	}
	if got := apnsBaseURL("production"); got != apnsProductionBaseURL {
		t.Fatalf("expected production URL, got %q", got)
	}
}

func TestNormalizeAPNSTopicDefaultsToVoIPTopic(t *testing.T) {
	if got := normalizeAPNSTopic("th.or.nectec.kasemsan.softphone", ""); got != "th.or.nectec.kasemsan.softphone.voip" {
		t.Fatalf("unexpected topic: %q", got)
	}
	if got := normalizeAPNSTopic("ignored", "custom.topic.voip"); got != "custom.topic.voip" {
		t.Fatalf("unexpected custom topic: %q", got)
	}
}

func TestBuildAPNSVoIPPayloadIncludesIncomingMetadata(t *testing.T) {
	data := buildIncomingCallPushData("sess-1", "1001", "1002", "ios_pushkit", time.Unix(0, 0).UTC(), true)
	payload := buildAPNSVoIPPayload(data)

	aps, ok := payload["aps"].(map[string]interface{})
	if !ok || aps["content-available"] != 1 {
		t.Fatalf("expected content-available aps payload, got %#v", payload["aps"])
	}
	if payload["type"] != "incoming_call" || payload["sessionId"] != "sess-1" || payload["hasVideo"] != "true" {
		t.Fatalf("unexpected APNs payload: %#v", payload)
	}
}

func TestAPNSRequestHeaders(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sender := &APNSSender{
		keyID:      "KEY123",
		teamID:     "TEAM123",
		topic:      "th.or.nectec.kasemsan.softphone.voip",
		privateKey: key,
		baseURL:    "https://example.invalid",
	}

	req, err := sender.buildRequest(context.Background(), "abcdef", []byte(`{}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if req.URL.String() != "https://example.invalid/3/device/abcdef" {
		t.Fatalf("unexpected URL: %s", req.URL.String())
	}
	if req.Header.Get("apns-push-type") != "voip" || req.Header.Get("apns-topic") != sender.topic || req.Header.Get("apns-priority") != "10" {
		t.Fatalf("unexpected APNs headers: %#v", req.Header)
	}
	if !strings.HasPrefix(req.Header.Get("authorization"), "bearer ") {
		t.Fatalf("missing bearer auth header")
	}
}
