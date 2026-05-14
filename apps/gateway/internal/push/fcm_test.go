package push

import "testing"

func TestBuildFCMPayloadAndroidIsDataOnlyHighPriority(t *testing.T) {
	payload := buildFCMPayload(
		"token-1",
		"Incoming",
		"Call from 1001",
		map[string]string{"type": "incoming_call"},
		"android_abc",
	)

	if payload.Message.Notification != nil {
		t.Fatal("expected android payload to omit notification body")
	}
	if payload.Message.Android == nil {
		t.Fatal("expected android config")
	}
	if payload.Message.Android.Priority != "high" || payload.Message.Android.TTL != "30s" {
		t.Fatalf("unexpected android config: %#v", payload.Message.Android)
	}
}

func TestBuildFCMPayloadIOSKeepsNotificationFallback(t *testing.T) {
	payload := buildFCMPayload(
		"token-2",
		"Incoming",
		"Call from 1001",
		map[string]string{"type": "incoming_call"},
		"ios_abc",
	)

	if payload.Message.Notification == nil {
		t.Fatal("expected iOS fallback notification payload")
	}
	if payload.Message.Android != nil {
		t.Fatal("did not expect android config for iOS fallback")
	}
	if payload.Message.Notification.Title != "Incoming" {
		t.Fatalf("unexpected notification title: %q", payload.Message.Notification.Title)
	}
}
