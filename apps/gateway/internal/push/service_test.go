package push

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubFCM struct {
	err     error
	token   string
	calls   int
	display bool
}

func (s *stubFCM) SendPush(_ context.Context, token, _, _ string, _ map[string]string, _ string) error {
	s.calls++
	s.token = token
	return s.err
}

func (s *stubFCM) SendDisplayPush(ctx context.Context, token, title, body string, data map[string]string, device string) error {
	s.display = true
	return s.SendPush(ctx, token, title, body, data, device)
}

func TestNormalizeTestPushStyle(t *testing.T) {
	style, err := NormalizeTestPushStyle("")
	if err != nil || style != TestPushStyleData {
		t.Fatalf("empty should default to data, got %q %v", style, err)
	}
	style, err = NormalizeTestPushStyle("MESSAGE")
	if err != nil || style != TestPushStyleMessage {
		t.Fatalf("expected message, got %q %v", style, err)
	}
	if _, err := NormalizeTestPushStyle("banner"); err == nil {
		t.Fatal("expected invalid style error")
	}
}

func TestSendIncomingCallFCMTokenReturnsProviderError(t *testing.T) {
	if err := (*Service)(nil).SendIncomingCallFCMToken("tok", "sess", "from", "to", false); !errors.Is(err, ErrPushNotConfigured) {
		t.Fatalf("expected not configured, got %v", err)
	}

	svc := NewServiceWithFCM(nil, &stubFCM{}, nil)
	if err := svc.SendIncomingCallFCMToken("  ", "sess", "from", "to", false); !errors.Is(err, ErrEmptyPushToken) {
		t.Fatalf("expected empty token, got %v", err)
	}

	fcm := &stubFCM{err: errors.New("denied")}
	svc = NewServiceWithFCM(nil, fcm, nil)
	if err := svc.SendIncomingCallFCMToken("tok-1", "sess", "from", "to", true); err == nil || err.Error() != "denied" {
		t.Fatalf("expected provider error, got %v", err)
	}
	if fcm.calls != 1 || fcm.token != "tok-1" {
		t.Fatalf("unexpected send: calls=%d token=%q", fcm.calls, fcm.token)
	}

	fcm.err = nil
	if err := svc.SendIncomingCallFCMToken("tok-2", "sess", "from", "to", false); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

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
