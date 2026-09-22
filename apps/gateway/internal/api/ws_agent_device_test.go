package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"webrtc-sip-gateway/internal/auth"
	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

func newAgentDeviceTestServer(t *testing.T, tm *agentTrunkManagerStub) *Server {
	t.Helper()
	mgr := session.NewManager(&config.Config{})
	return &Server{
		sessionMgr:         mgr,
		trunkManager:       tm,
		config:             config.APIConfig{EnableAgentDeviceWS: true},
		wsClients:          make(map[string]*WSClient),
		wsConnections:      make(map[*WSClient]struct{}),
		agentTrunkBindings: make(map[int64]map[*WSClient]struct{}),
		wsClientStreams:    make(map[int]chan []byte),
	}
}

func newAgentDeviceWSClient() *WSClient {
	return &WSClient{
		clientID:        "agent-device-1",
		send:            make(chan []byte, 8),
		ownedSessionIDs: make(map[string]struct{}),
		pendingIncoming: make(map[string]struct{}),
		availability:    clientAvailabilityIdle,
		callState:       string(session.StateNew),
		ConnectedAt:     time.Now(),
		agentDeviceOnly: true,
		devicePlatform:  devicePlatformAndroid,
		authClaims:      &auth.VerifiedClaims{Subject: "ems-sub-1", Realm: auth.TokenRealmEmployee},
	}
}

func TestAgentDeviceWSFlagOffReturns404(t *testing.T) {
	srv := NewServer(config.APIConfig{EnableAgentDeviceWS: false}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/ws-agent-device", nil)
	rr := httptest.NewRecorder()
	srv.handleAgentDeviceWebSocket(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAgentDeviceWSRequiresJWTAndPlatform(t *testing.T) {
	srv := NewServer(config.APIConfig{EnableAgentDeviceWS: true}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, _ auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw == "valid-token" {
				return &auth.VerifiedClaims{Subject: "ems-sub-1", Realm: auth.TokenRealmEmployee}, nil
			}
			return nil, context.DeadlineExceeded
		},
	})
	provisioner := &mobileSIPProvisionerStub{}
	srv.SetMobileSIPProvisioner(provisioner)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleAgentDeviceWebSocket))
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	assertUnauthorized := func(query string) {
		t.Helper()
		conn, resp, err := dialer.Dial(wsURL+query, nil)
		if conn != nil {
			_ = conn.Close()
		}
		if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("expected 401 for %q, err=%v status=%d", query, err, status)
		}
	}

	assertUnauthorized("")
	assertUnauthorized("?access_token=valid-token")
	assertUnauthorized("?devicePlatform=android")
	assertUnauthorized("?access_token=bad&devicePlatform=android")
	assertUnauthorized("?access_token=valid-token&devicePlatform=mobile")
	if provisioner.called {
		t.Fatalf("mobile provisioner must not be invoked for /ws-agent-device")
	}

	conn, resp, err := dialer.Dial(wsURL+"?access_token=valid-token&devicePlatform=android", nil)
	if err != nil {
		t.Fatalf("expected accepted upgrade, err=%v status=%v", err, resp)
	}
	_ = conn.Close()
	if provisioner.called {
		t.Fatalf("mobile provisioner must not be invoked after accepted agent-device upgrade")
	}
}

func TestDeviceRegisterSuccessDoesNotEchoPassword(t *testing.T) {
	tm := &agentTrunkManagerStub{}
	srv := newAgentDeviceTestServer(t, tm)
	client := newAgentDeviceWSClient()

	srv.handleWSDeviceRegister(client, WSMessage{
		Type:        "device_register",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "super-secret",
		MultiCall:   true,
	})

	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if msgs[0].SIPPassword != "" || msgs[0].PNToken != "" {
		t.Fatalf("trunk_resolved must not echo secrets: %+v", msgs[0])
	}
	if !client.trunkResolved || client.resolvedTrunkID != 100 {
		t.Fatalf("expected client bound to trunk 100")
	}
	if tm.registerCount != 1 {
		t.Fatalf("expected first bind to REGISTER once, got %d", tm.registerCount)
	}
	if tm.upsertPayload.Password != "super-secret" {
		t.Fatalf("expected upsert to receive password")
	}
	raw, _ := json.Marshal(msgs[0])
	if strings.Contains(string(raw), "super-secret") {
		t.Fatalf("password leaked in trunk_resolved payload")
	}
}

func TestDeviceDisconnectKeepsRegisterAndFCM(t *testing.T) {
	token := "fcm-stored"
	tm := &agentTrunkManagerStub{
		trunk: &sip.Trunk{
			ID:       100,
			PublicID: "agent-device-public-100",
			Name:     "sipclient-agent-device-1001@sip.example.com:5060",
			FcmToken: &token,
			Enabled:  true,
		},
		owned: true,
	}
	srv := newAgentDeviceTestServer(t, tm)
	client := newAgentDeviceWSClient()
	client.trunkResolved = true
	client.resolvedTrunkID = 100
	_, _, _, _ = srv.bindAgentClient(client, 100)

	sess, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sess.UpdateState(session.StateActive)
	srv.bindClientSession(client, sess.ID)

	srv.cleanupAgentDevicePresence(client)
	if tm.unregisterCount != 0 {
		t.Fatalf("disconnect must not UNREGISTER, got %d", tm.unregisterCount)
	}
	if tm.fcmCleared {
		t.Fatalf("disconnect must not clear FCM")
	}
	if _, ok := srv.sessionMgr.GetSession(sess.ID); ok {
		t.Fatalf("expected owned session ended on disconnect")
	}
}

func TestDeviceUnregisterClearsFCMAndUnregisters(t *testing.T) {
	token := "fcm-stored"
	tm := &agentTrunkManagerStub{
		trunk: &sip.Trunk{
			ID:       100,
			PublicID: "agent-device-public-100",
			Name:     "sipclient-agent-device-1001@sip.example.com:5060",
			FcmToken: &token,
			Enabled:  true,
		},
		owned: true,
	}
	srv := newAgentDeviceTestServer(t, tm)
	client := newAgentDeviceWSClient()
	_, _, _, _ = srv.bindAgentClient(client, 100)

	srv.handleWSDeviceUnregister(client, WSMessage{Type: "unregister"})
	if tm.unregisterCount != 1 || tm.unregisterID != 100 {
		t.Fatalf("expected device UNREGISTER, count=%d id=%d", tm.unregisterCount, tm.unregisterID)
	}
	if !tm.fcmCleared {
		t.Fatalf("expected FCM cleared on logout")
	}
	if tm.notifyUserID != nil {
		t.Fatalf("expected notify user cleared")
	}
	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 1 || msgs[0].Type != "unregistered" {
		t.Fatalf("expected unregistered ack, got %+v", msgs)
	}
	if msgs[0].PNToken != "" || msgs[0].SIPPassword != "" {
		t.Fatalf("unregistered must not echo secrets")
	}
}

func TestAgentLastDisconnectDoesNotUnregisterDeviceTrunk(t *testing.T) {
	device := &sip.Trunk{ID: 200, Name: "sipclient-agent-device-1001@sip.example.com:5060", Enabled: true}
	agent := &sip.Trunk{ID: 99, Name: "sipclient-agent-1001@sip.example.com:5060", Enabled: true}
	tm := &agentTrunkManagerStub{trunk: agent, owned: true, deviceTrunks: []*sip.Trunk{device}}
	srv := newAgentTestServer(t, tm)
	client := newAgentWSClient()
	_, _, _, _ = srv.bindAgentClient(client, 99)

	srv.cleanupAgentPresence(client)
	if tm.unregisterCount != 1 || tm.unregisterID != 99 {
		t.Fatalf("agent last disconnect should unregister agent trunk only, count=%d id=%d", tm.unregisterCount, tm.unregisterID)
	}
}

func TestAgentDeviceRejectsAgentAndTrunkMessages(t *testing.T) {
	srv := newAgentDeviceTestServer(t, &agentTrunkManagerStub{})
	client := newAgentDeviceWSClient()
	client.trunkResolved = true
	client.resolvedTrunkID = 100

	for _, msgType := range []string{"agent_register", "trunk_resolve", "trunk_push_token", "translate"} {
		ok, reason := srv.allowAgentDeviceWSMessage(client, WSMessage{Type: msgType})
		if ok {
			t.Fatalf("expected %s rejected", msgType)
		}
		if reason == "" {
			t.Fatalf("expected reject reason for %s", msgType)
		}
	}
	if ok, _ := srv.allowAgentDeviceWSMessage(client, WSMessage{Type: "resume", SessionID: "sess-1"}); !ok {
		t.Fatalf("resume should be allowed on agent-device")
	}
}

func TestDevicePushTokenStoresFCMWithoutEcho(t *testing.T) {
	tm := &agentTrunkManagerStub{
		trunk: &sip.Trunk{ID: 100, PublicID: "agent-device-public-100", Enabled: true},
	}
	srv := newAgentDeviceTestServer(t, tm)
	client := newAgentDeviceWSClient()
	_, _, _, _ = srv.bindAgentClient(client, 100)

	srv.handleWSDevicePushToken(client, WSMessage{
		Type:           "device_push_token",
		PNType:         "fcm",
		PNToken:        "fcm-secret-token",
		DevicePlatform: "android",
	})
	if tm.fcmToken != "fcm-secret-token" {
		t.Fatalf("expected stored FCM token")
	}
	msgs := readAgentWSMessages(t, client)
	for _, msg := range msgs {
		raw, _ := json.Marshal(msg)
		if strings.Contains(string(raw), "fcm-secret-token") {
			t.Fatalf("FCM token leaked in WS reply: %s", raw)
		}
	}
}

func TestDeviceRegisterReplaysPendingIncoming(t *testing.T) {
	tm := &agentTrunkManagerStub{
		trunk: &sip.Trunk{
			ID:       100,
			PublicID: "agent-device-public-100",
			Name:     "sipclient-agent-device-1001@sip.example.com:5060",
			Enabled:  true,
		},
		identityGroup: map[int64][]int64{100: {10}, 10: {100}},
	}
	srv := newAgentDeviceTestServer(t, tm)
	incoming, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	incoming.SetState(session.StateIncoming)
	incoming.SetCallInfo("inbound", "sip:2002@example.com", "sip:1001@example.com", "sip-call-device")
	incoming.SetSIPAuthContext("trunk", "", 10, "sip.example.com", "1001", "secret", 5060)

	client := newAgentDeviceWSClient()
	srv.handleWSDeviceRegister(client, WSMessage{
		Type:        "device_register",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "secret",
	})

	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 2 {
		t.Fatalf("expected trunk_resolved + incoming, got %+v", msgs)
	}
	if msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved first, got %s", msgs[0].Type)
	}
	if msgs[1].Type != "incoming" || msgs[1].SessionID != incoming.ID {
		t.Fatalf("expected replayed incoming, got %+v", msgs[1])
	}
}

func TestDeviceRebindUnregistersPreviousIdentity(t *testing.T) {
	oldID := "ems-sub-1"
	old := &sip.Trunk{
		ID:           50,
		Name:         "sipclient-agent-device-old@sip.example.com:5060",
		NotifyUserID: &oldID,
		Enabled:      true,
	}
	tm := &agentTrunkManagerStub{
		trunk: &sip.Trunk{
			ID:       100,
			PublicID: "agent-device-public-100",
			Name:     "sipclient-agent-device-1001@sip.example.com:5060",
			Enabled:  true,
		},
		deviceTrunks: []*sip.Trunk{old},
	}
	srv := newAgentDeviceTestServer(t, tm)
	client := newAgentDeviceWSClient()

	srv.handleWSDeviceRegister(client, WSMessage{
		Type:        "device_register",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "secret",
	})
	if tm.unregisterID != 50 {
		t.Fatalf("expected old device trunk unregistered, got %d", tm.unregisterID)
	}
	if tm.fcmClearID != 50 {
		t.Fatalf("expected old FCM cleared, got %d", tm.fcmClearID)
	}
}
