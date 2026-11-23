package push

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
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

func (s *stubFCM) SendDataPush(_ context.Context, token string, _ map[string]string, _ DataPushOptions) error {
	s.calls++
	s.token = token
	return s.err
}

func TestCallEndedPushDataNeverLooksLikeARingableCall(t *testing.T) {
	started := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)
	data := buildCallEndedPushData(CallEnded{SessionID: "s1", From: "1000", To: "2000", Reason: CallEndedCallerCancelled, StartedAt: started})
	want := map[string]string{"type": "call_ended", "sessionId": "s1", "reason": "caller_cancelled", "caller": "1000", "callee": "2000", "startedAt": "2026-11-09T03:00:00Z"}
	for key, value := range want {
		if data[key] != value {
			t.Fatalf("%s = %q, want %q", key, data[key], value)
		}
	}
	for _, key := range []string{"expiresAt", "ringTimeoutSeconds"} {
		if _, found := data[key]; found {
			t.Fatalf("%s would make SDKs before 0.1.24 persist the push as a call to ring", key)
		}
	}
}

func TestCallEndedPushOptions(t *testing.T) {
	if got := callEndedPushOptions(CallEndedNoAnswer); got.Priority != "high" || got.TTL != "86400s" {
		t.Fatalf("missed call options: %+v", got)
	}
	if got := callEndedPushOptions(CallEndedAnsweredElsewhere); got.Priority != "normal" || got.TTL != "30s" {
		t.Fatalf("answered elsewhere options: %+v", got)
	}
}

func TestBuildFCMDataPayloadHasNoNotification(t *testing.T) {
	payload := buildFCMDataPayload("tok", map[string]string{"type": "call_ended"}, DataPushOptions{Priority: "high", TTL: "86400s"})
	if payload.Message.Notification != nil || payload.Message.Android == nil || payload.Message.Android.TTL != "86400s" {
		t.Fatalf("unexpected payload: %+v", payload.Message)
	}
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

func TestMaskToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "<redacted>"},
		{"short-tok", "<redacted>"},
		{"12345678901234", "<redacted>"},
		{"123456789012345", "123456...2345"},
		{"  abcdef_SECRET_MIDDLE_wxyz  ", "abcdef...wxyz"},
	}
	for _, tc := range cases {
		if got := maskToken(tc.in); got != tc.want {
			t.Fatalf("maskToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNotifyIncomingCallRedactsFCMTokensInLogs(t *testing.T) {
	longToken := "eXk9Ab_SUPER_SECRET_FCM_TOKEN_PAYLOAD_7890"
	shortToken := "shortSecret12"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"status":"ok","data":[`+
			`{"user_id":"u-42","service_id":%q,"token":%q,"mobile_device":"android_dev1"},`+
			`{"user_id":"u-42","service_id":%q,"token":%q,"mobile_device":"ios_dev2"}`+
			`]}`, pushServiceID, longToken, pushServiceID, shortToken)
	}))
	defer srv.Close()

	ttrs := &TTRSClient{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}
	fcm := &stubFCM{}
	svc := NewServiceWithFCM(ttrs, fcm, nil)

	var logBuf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOut)

	svc.NotifyIncomingCall("u-42", "sess-99", "1001", "2002", true)

	logs := logBuf.String()
	if fcm.calls != 2 {
		t.Fatalf("expected 2 FCM sends, got %d", fcm.calls)
	}
	for _, forbidden := range []string{longToken, shortToken, "SUPER_SECRET_FCM_TOKEN", "FCM token:", "FCM tokens:"} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("sensitive or verbose token dump %q leaked into push logs:\n%s", forbidden, logs)
		}
	}
	if !strings.Contains(logs, "token=eXk9Ab...7890") {
		t.Fatalf("expected masked long token in logs, got:\n%s", logs)
	}
	if !strings.Contains(logs, "token=<redacted>") {
		t.Fatalf("expected redacted short token in logs, got:\n%s", logs)
	}

	// Verify NotificationEntry.String() also redacts when formatted with %v.
	entryStr := fmt.Sprintf("%v", NotificationEntry{
		UserID:       "u-42",
		ServiceID:    pushServiceID,
		Token:        longToken,
		MobileDevice: "android_dev1",
	})
	if strings.Contains(entryStr, longToken) {
		t.Fatalf("NotificationEntry.String() leaked plaintext token: %s", entryStr)
	}
}
