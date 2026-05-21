package push

import (
	"testing"
	"time"
)

func TestBuildIncomingCallPushDataIncludesBackgroundCallMetadata(t *testing.T) {
	now := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)

	data := buildIncomingCallPushData("sess-1", "1001", "1002", "android_abc", now, true)

	if data["type"] != "incoming_call" {
		t.Fatalf("expected incoming_call type, got %q", data["type"])
	}
	if data["sessionId"] != "sess-1" || data["caller"] != "1001" || data["callee"] != "1002" {
		t.Fatalf("unexpected call data: %#v", data)
	}
	if data["platform"] != "android" {
		t.Fatalf("expected android platform, got %q", data["platform"])
	}
	if data["ringTimeoutSeconds"] != "30" {
		t.Fatalf("expected ring timeout 30, got %q", data["ringTimeoutSeconds"])
	}
	if data["hasVideo"] != "true" {
		t.Fatalf("expected hasVideo true, got %q", data["hasVideo"])
	}
	if data["expiresAt"] != now.Add(30*time.Second).Format(time.RFC3339) {
		t.Fatalf("unexpected expiresAt: %q", data["expiresAt"])
	}
}

func TestBuildIncomingCallPushDataDetectsIOSFallbackPlatform(t *testing.T) {
	data := buildIncomingCallPushData("sess-2", "1001", "1002", "ios_abc", time.Unix(0, 0).UTC(), false)

	if data["platform"] != "ios" {
		t.Fatalf("expected ios platform, got %q", data["platform"])
	}
	if data["hasVideo"] != "false" {
		t.Fatalf("expected hasVideo false, got %q", data["hasVideo"])
	}
}
