package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicViewRedactsSecrets(t *testing.T) {
	cfg := &Config{
		TURN: TURNConfig{
			Server:   "turn:example.com",
			Username: "alice",
			Password: "super-secret-turn",
		},
		SIP: SIPConfig{
			Domain:   "sip.example.com",
			Username: "bob",
			Password: "super-secret-sip",
			Port:     5060,
		},
		DB: DBConfig{
			Enable: true,
			DSN:    "postgres://gateway_user:db-pass-123@db.internal:5432/k2",
		},
		PushNotification: PushNotificationConfig{
			Enable:                  true,
			TTRSClientSecret:        "oauth-secret",
			FirebaseCredentialsFile: "/secrets/firebase.json",
			APNSKeyFile:             "/secrets/apns.p8",
			APNSCertFile:            "/secrets/apns.pem",
			APNSCertKeyFile:         "/secrets/apns-key.pem",
		},
		API: APIConfig{
			Port:     8000,
			EnableWS: true,
		},
		Auth: AuthConfig{
			FrontendPassword: "admin-plain-password",
		},
	}

	view := cfg.PublicView()
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(raw)

	secrets := []string{
		"super-secret-turn",
		"super-secret-sip",
		"db-pass-123",
		"oauth-secret",
		"/secrets/firebase.json",
		"/secrets/apns.p8",
		"/secrets/apns.pem",
		"/secrets/apns-key.pem",
		"admin-plain-password",
	}
	for _, secret := range secrets {
		if strings.Contains(body, secret) {
			t.Fatalf("public view leaked secret %q in %s", secret, body)
		}
	}

	if view.Sections.TURN.Password != RedactedValue {
		t.Fatalf("expected redacted TURN password, got %q", view.Sections.TURN.Password)
	}
	if view.Sections.SIP.Password != RedactedValue {
		t.Fatalf("expected redacted SIP password, got %q", view.Sections.SIP.Password)
	}
	if !strings.Contains(view.Sections.DB.DSN, ":****@") {
		t.Fatalf("expected redacted DSN password, got %q", view.Sections.DB.DSN)
	}
	if view.Sections.Push.TTRSClientSecret != RedactedValue {
		t.Fatalf("expected redacted TTRS client secret, got %q", view.Sections.Push.TTRSClientSecret)
	}
	if view.Sections.Auth.FrontendPassword != RedactedValue {
		t.Fatalf("expected redacted frontend password, got %q", view.Sections.Auth.FrontendPassword)
	}
}

func TestPublicViewPreservesNonSecretValues(t *testing.T) {
	cfg := &Config{
		TURN: TURNConfig{
			Server:   "turn:example.com",
			Username: "alice",
		},
		API: APIConfig{
			Port:     8080,
			EnableWS: true,
		},
		Gateway: GatewayConfig{
			InstanceID:  "gw-test-1",
			PublicWSURL: "wss://gw.example.com/ws",
		},
	}

	view := cfg.PublicView()
	if view.Sections.TURN.Server != "turn:example.com" {
		t.Fatalf("expected TURN server to be preserved, got %q", view.Sections.TURN.Server)
	}
	if view.Sections.API.Port != 8080 {
		t.Fatalf("expected API port 8080, got %d", view.Sections.API.Port)
	}
	if view.Sections.Gateway.InstanceID != "gw-test-1" {
		t.Fatalf("expected gateway instance ID to be preserved, got %q", view.Sections.Gateway.InstanceID)
	}
}

func TestPublicViewNilConfig(t *testing.T) {
	var cfg *Config
	view := cfg.PublicView()
	if view.Sections.API.Port != 0 {
		t.Fatalf("expected empty view for nil config, got API port %d", view.Sections.API.Port)
	}
}

func TestRedactDSNForPublic(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "postgres url",
			dsn:  "postgres://user:secret@host:5432/db",
			want: "postgres://user:****@host:5432/db",
		},
		{
			name: "empty",
			dsn:  "",
			want: "",
		},
		{
			name: "non url",
			dsn:  "host=db password=secret",
			want: RedactedValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactDSNForPublic(tt.dsn); got != tt.want {
				t.Fatalf("redactDSNForPublic(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}
