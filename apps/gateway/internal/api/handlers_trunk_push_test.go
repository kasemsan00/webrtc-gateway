package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/push"
	"webrtc-sip-gateway/internal/sip"
)

type stubFCMClient struct {
	err     error
	token   string
	title   string
	body    string
	data    map[string]string
	device  string
	calls   int
	display bool
}

func (s *stubFCMClient) SendPush(_ context.Context, token, title, notificationBody string, data map[string]string, mobileDevice string) error {
	s.calls++
	s.token = token
	s.title = title
	s.body = notificationBody
	s.data = data
	s.device = mobileDevice
	return s.err
}

func (s *stubFCMClient) SendDisplayPush(ctx context.Context, token, title, notificationBody string, data map[string]string, mobileDevice string) error {
	s.display = true
	return s.SendPush(ctx, token, title, notificationBody, data, mobileDevice)
}

func TestHandleTrunkTestIncomingPush_StoredFCMSuccess(t *testing.T) {
	fcmToken := "fcm-secret-token-do-not-echo"
	tm := &apiHandlerTrunkManagerStub{
		byID: map[int64]*sip.Trunk{
			11: {
				ID:        11,
				Name:      "agent-device:1001",
				Domain:    "sip.example.com",
				Username:  "1001",
				FcmToken:  &fcmToken,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}
	fcm := &stubFCMClient{}
	srv, _ := newAPIHandlerTestServer(t, tm, nil, nil)
	srv.SetPushService(push.NewServiceWithFCM(nil, fcm, nil))

	rr := doRequest(t, srv.handleTrunkTestIncomingPush, http.MethodPost, "/trunk/11/test-incoming-push", "", map[string]string{"id": "11"})
	out := assertJSONDecode[map[string]interface{}](t, rr, http.StatusOK)
	if out["status"] != "sent" || out["trunkId"] != float64(11) {
		t.Fatalf("unexpected response: %+v", out)
	}
	sessionID, _ := out["sessionId"].(string)
	if !strings.HasPrefix(sessionID, "push-test-") {
		t.Fatalf("expected synthetic sessionId, got %q", sessionID)
	}
	channels, _ := out["channels"].([]interface{})
	if len(channels) != 1 || channels[0] != "fcm" {
		t.Fatalf("expected fcm channel, got %#v", out["channels"])
	}
	if fcm.calls != 1 || fcm.token != fcmToken || fcm.display {
		t.Fatalf("expected one data-only FCM send, calls=%d display=%v", fcm.calls, fcm.display)
	}
	if out["style"] != "data" {
		t.Fatalf("expected default style data, got %#v", out["style"])
	}
	if fcm.data["type"] != "incoming_call" || fcm.data["sessionId"] != sessionID {
		t.Fatalf("unexpected push data: %#v", fcm.data)
	}
	if strings.Contains(rr.Body.String(), fcmToken) {
		t.Fatalf("FCM token leaked in JSON: %s", rr.Body.String())
	}
}

func TestHandleTrunkTestIncomingPush_MessageStyleUsesDisplayPush(t *testing.T) {
	fcmToken := "fcm-secret-token-do-not-echo"
	tm := &apiHandlerTrunkManagerStub{
		byID: map[int64]*sip.Trunk{
			15: {ID: 15, Username: "1001", FcmToken: &fcmToken, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
	}
	fcm := &stubFCMClient{}
	srv, _ := newAPIHandlerTestServer(t, tm, nil, nil)
	srv.SetPushService(push.NewServiceWithFCM(nil, fcm, nil))

	rr := doRequest(t, srv.handleTrunkTestIncomingPush, http.MethodPost, "/trunk/15/test-incoming-push", `{"style":"message"}`, map[string]string{"id": "15"})
	out := assertJSONDecode[map[string]interface{}](t, rr, http.StatusOK)
	if out["style"] != "message" || !fcm.display {
		t.Fatalf("expected message display push, resp=%+v display=%v", out, fcm.display)
	}
}

func TestHandleTrunkTestIncomingPush_InvalidStyle(t *testing.T) {
	fcmToken := "fcm-secret-token"
	tm := &apiHandlerTrunkManagerStub{
		byID: map[int64]*sip.Trunk{
			16: {ID: 16, Username: "1001", FcmToken: &fcmToken, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
	}
	srv, _ := newAPIHandlerTestServer(t, tm, nil, nil)
	srv.SetPushService(push.NewServiceWithFCM(nil, &stubFCMClient{}, nil))

	rr := doRequest(t, srv.handleTrunkTestIncomingPush, http.MethodPost, "/trunk/16/test-incoming-push", `{"style":"banner"}`, map[string]string{"id": "16"})
	assertJSONError(t, rr, http.StatusBadRequest, "style must be data or message")
}

func TestHandleTrunkTestIncomingPush_NoTargetConflict(t *testing.T) {
	tm := &apiHandlerTrunkManagerStub{
		byID: map[int64]*sip.Trunk{
			12: {
				ID:        12,
				Name:      "plain",
				Username:  "1002",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}
	srv, _ := newAPIHandlerTestServer(t, tm, nil, nil)
	srv.SetPushService(push.NewServiceWithFCM(nil, &stubFCMClient{}, nil))

	rr := doRequest(t, srv.handleTrunkTestIncomingPush, http.MethodPost, "/trunk/12/test-incoming-push", "", map[string]string{"id": "12"})
	assertJSONError(t, rr, http.StatusConflict, "no incoming push target")
}

func TestHandleTrunkTestIncomingPush_SenderMissing(t *testing.T) {
	fcmToken := "fcm-secret-token"
	tm := &apiHandlerTrunkManagerStub{
		byID: map[int64]*sip.Trunk{
			13: {ID: 13, Username: "1003", FcmToken: &fcmToken, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
	}
	srv, _ := newAPIHandlerTestServer(t, tm, nil, nil)

	rr := doRequest(t, srv.handleTrunkTestIncomingPush, http.MethodPost, "/trunk/13/test-incoming-push", "", map[string]string{"id": "13"})
	assertJSONError(t, rr, http.StatusServiceUnavailable, "Push service not configured")

	srv.SetPushService(push.NewService(nil, nil))
	rr = doRequest(t, srv.handleTrunkTestIncomingPush, http.MethodPost, "/trunk/13/test-incoming-push", "", map[string]string{"id": "13"})
	assertJSONError(t, rr, http.StatusServiceUnavailable, "Push sender not configured")
}

func TestHandleTrunkTestIncomingPush_SendFailed(t *testing.T) {
	fcmToken := "fcm-secret-token"
	tm := &apiHandlerTrunkManagerStub{
		byID: map[int64]*sip.Trunk{
			14: {ID: 14, Username: "1004", FcmToken: &fcmToken, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
	}
	srv, _ := newAPIHandlerTestServer(t, tm, nil, nil)
	srv.SetPushService(push.NewServiceWithFCM(nil, &stubFCMClient{err: errors.New("fcm down")}, nil))

	rr := doRequest(t, srv.handleTrunkTestIncomingPush, http.MethodPost, "/trunk/14/test-incoming-push", "", map[string]string{"id": "14"})
	assertJSONError(t, rr, http.StatusBadGateway, "Failed to send test push")
	if strings.Contains(rr.Body.String(), fcmToken) {
		t.Fatalf("FCM token leaked in error JSON: %s", rr.Body.String())
	}
}

func TestHandleTrunkTestIncomingPush_InvalidAndMissingTrunk(t *testing.T) {
	tm := &apiHandlerTrunkManagerStub{byID: map[int64]*sip.Trunk{}}
	srv, _ := newAPIHandlerTestServer(t, tm, nil, nil)
	srv.SetPushService(push.NewServiceWithFCM(nil, &stubFCMClient{}, nil))

	rr := doRequest(t, srv.handleTrunkTestIncomingPush, http.MethodPost, "/trunk/x/test-incoming-push", "", map[string]string{"id": "x"})
	assertJSONError(t, rr, http.StatusBadRequest, "Invalid trunk ID")

	rr = doRequest(t, srv.handleTrunkTestIncomingPush, http.MethodPost, "/trunk/99/test-incoming-push", "", map[string]string{"id": "99"})
	assertJSONError(t, rr, http.StatusNotFound, "Trunk not found")
}
