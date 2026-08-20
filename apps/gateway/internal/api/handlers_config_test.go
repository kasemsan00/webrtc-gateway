package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"webrtc-sip-gateway/internal/config"
)

func TestHandleGetConfigRedactsSecrets(t *testing.T) {
	srv, _ := newAPIHandlerTestServer(t, nil, nil, nil)
	srv.gatewayConfig = config.GatewayConfig{InstanceID: "gw-test-1"}
	srv.SetRuntimeConfig(&config.Config{
		TURN: config.TURNConfig{
			Server:   "turn:example.com",
			Username: "alice",
			Password: "turn-secret",
		},
		SIP: config.SIPConfig{
			Password: "sip-secret",
			Port:     5060,
		},
		DB: config.DBConfig{
			Enable: true,
			DSN:    "postgres://user:db-secret@db.internal:5432/webrtc_sip_gateway",
		},
		PushNotification: config.PushNotificationConfig{
			TTRSClientSecret: "oauth-secret",
		},
		API: config.APIConfig{
			Port:     8000,
			EnableWS: true,
		},
	})

	rr := doRequest(t, srv.handleGetConfig, http.MethodGet, "/config", "", nil)
	resp := assertJSONDecode[GatewayConfigResponse](t, rr, http.StatusOK)

	if resp.InstanceID != "gw-test-1" {
		t.Fatalf("expected instanceId gw-test-1, got %q", resp.InstanceID)
	}
	if resp.Source != "env" {
		t.Fatalf("expected source env, got %q", resp.Source)
	}
	if resp.Sections.API.Port != 8000 {
		t.Fatalf("expected API port 8000, got %d", resp.Sections.API.Port)
	}
	if resp.Sections.TURN.Password != config.RedactedValue {
		t.Fatalf("expected redacted TURN password, got %q", resp.Sections.TURN.Password)
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(raw)
	for _, secret := range []string{"turn-secret", "sip-secret", "db-secret", "oauth-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked secret %q: %s", secret, body)
		}
	}
}

func TestHandleGetConfigUnavailableWithoutRuntimeConfig(t *testing.T) {
	srv, _ := newAPIHandlerTestServer(t, nil, nil, nil)
	rr := doRequest(t, srv.handleGetConfig, http.MethodGet, "/config", "", nil)
	assertJSONError(t, rr, http.StatusServiceUnavailable, "runtime config not available")
}
