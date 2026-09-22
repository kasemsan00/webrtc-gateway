package config

import "testing"

func TestNormalizeSwitchVideoTransitionMode(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "preserve", value: "preserve", want: SIPSwitchVideoTransitionPreserve},
		{name: "blackout", value: "blackout", want: SIPSwitchVideoTransitionBlackout},
		{name: "trim and lower", value: " Preserve ", want: SIPSwitchVideoTransitionPreserve},
		{name: "invalid falls back", value: "disabled", want: SIPSwitchVideoTransitionBlackout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSwitchVideoTransitionMode(tt.value); got != tt.want {
				t.Fatalf("normalizeSwitchVideoTransitionMode(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestMidCallRenegotiationConfigDefault(t *testing.T) {
	t.Setenv("SIP_MIDCALL_RENEGOTIATION_ENABLE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.SIP.MidCallRenegotiationEnable {
		t.Fatalf("expected SIP mid-call renegotiation to be enabled by default")
	}
}

func TestMidCallRenegotiationConfigCanBeDisabled(t *testing.T) {
	t.Setenv("SIP_MIDCALL_RENEGOTIATION_ENABLE", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SIP.MidCallRenegotiationEnable {
		t.Fatalf("expected SIP mid-call renegotiation to be disabled by env")
	}
}

func TestTrunkPNAppIDConfigDefault(t *testing.T) {
	t.Setenv("PUSH_TRUNK_PN_APP_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.API.TrunkPNAppID != DefaultTrunkPNAppID {
		t.Fatalf("expected default trunk PN app ID %q, got %q", DefaultTrunkPNAppID, cfg.API.TrunkPNAppID)
	}
}

func TestTrunkPNAppIDConfigCanBeCustomizedAndTrimmed(t *testing.T) {
	t.Setenv("PUSH_TRUNK_PN_APP_ID", " th.or.ttrs.video.staging ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.API.TrunkPNAppID != "th.or.ttrs.video.staging" {
		t.Fatalf("expected custom trunk PN app ID to be trimmed, got %q", cfg.API.TrunkPNAppID)
	}
}

func TestPublicWebSocketConfigDefaultDisabled(t *testing.T) {
	t.Setenv("API_ENABLE_PUBLIC_WS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.API.EnablePublicWS {
		t.Fatalf("expected public WebSocket endpoint to be disabled by default")
	}
}

func TestPublicWebSocketConfigCanBeEnabled(t *testing.T) {
	t.Setenv("API_ENABLE_PUBLIC_WS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.API.EnablePublicWS {
		t.Fatalf("expected public WebSocket endpoint to be enabled by env")
	}
}

func TestAgentWebSocketConfigDefaultDisabled(t *testing.T) {
	t.Setenv("API_ENABLE_AGENT_WS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.API.EnableAgentWS {
		t.Fatalf("expected agent WebSocket endpoint to be disabled by default")
	}
}

func TestAgentWebSocketConfigCanBeEnabled(t *testing.T) {
	t.Setenv("API_ENABLE_AGENT_WS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.API.EnableAgentWS {
		t.Fatalf("expected agent WebSocket endpoint to be enabled by env")
	}
}

func TestAgentDeviceWebSocketConfigDefaultDisabled(t *testing.T) {
	t.Setenv("API_ENABLE_AGENT_DEVICE_WS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.API.EnableAgentDeviceWS {
		t.Fatalf("expected agent-device WebSocket endpoint to be disabled by default")
	}
}

func TestAgentDeviceWebSocketConfigCanBeEnabled(t *testing.T) {
	t.Setenv("API_ENABLE_AGENT_DEVICE_WS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.API.EnableAgentDeviceWS {
		t.Fatalf("expected agent-device WebSocket endpoint to be enabled by env")
	}
}

func TestVideoAUNormalizationConfigDefaultsEnabled(t *testing.T) {
	t.Setenv("SIP_VIDEO_AU_NORMALIZE_ENABLE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.SIP.VideoAUNormalizeEnabled {
		t.Fatal("expected SIP video AU normalization to be enabled by default")
	}
}

func TestVideoAUNormalizationConfigCanBeDisabled(t *testing.T) {
	t.Setenv("SIP_VIDEO_AU_NORMALIZE_ENABLE", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SIP.VideoAUNormalizeEnabled {
		t.Fatal("expected SIP video AU normalization rollback switch to disable the path")
	}
}

func TestFrontendPasswordConfigIsTrimmed(t *testing.T) {
	t.Setenv("FRONTEND_PASSWORD", " ops-secret ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.FrontendPassword != "ops-secret" {
		t.Fatalf("expected trimmed FRONTEND_PASSWORD, got %q", cfg.Auth.FrontendPassword)
	}
}

func TestChatImageConfigDefaults(t *testing.T) {
	t.Setenv("CHAT_IMAGE_ENABLE", "")
	t.Setenv("CHAT_IMAGE_DIR", "")
	t.Setenv("CHAT_IMAGE_PUBLIC_BASE_URL", "")
	t.Setenv("CHAT_IMAGE_MAX_BYTES", "")
	t.Setenv("CHAT_IMAGE_MAX_PER_SESSION", "")
	t.Setenv("CHAT_IMAGE_TTL_SECONDS", "")
	t.Setenv("CHAT_IMAGE_CLEANUP_INTERVAL_SECONDS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.ChatImage.Enable {
		t.Fatal("expected chat images enabled by default")
	}
	if cfg.ChatImage.Dir != DefaultChatImageDir {
		t.Fatalf("dir = %q, want %q", cfg.ChatImage.Dir, DefaultChatImageDir)
	}
	if cfg.ChatImage.MaxBytes != DefaultChatImageMaxBytes {
		t.Fatalf("maxBytes = %d", cfg.ChatImage.MaxBytes)
	}
	if cfg.ChatImage.MaxPerSession != DefaultChatImageMaxPerSession {
		t.Fatalf("maxPerSession = %d", cfg.ChatImage.MaxPerSession)
	}
	if cfg.ChatImage.TTLSeconds != DefaultChatImageTTLSeconds {
		t.Fatalf("ttl = %d", cfg.ChatImage.TTLSeconds)
	}
}

func TestChatImageConfigCanBeCustomized(t *testing.T) {
	t.Setenv("CHAT_IMAGE_ENABLE", "false")
	t.Setenv("CHAT_IMAGE_DIR", " /var/lib/webrtc-gateway/chat-images ")
	t.Setenv("CHAT_IMAGE_PUBLIC_BASE_URL", " https://k2-gateway.kasemsan.com/ ")
	t.Setenv("CHAT_IMAGE_MAX_BYTES", "1048576")
	t.Setenv("CHAT_IMAGE_MAX_PER_SESSION", "5")
	t.Setenv("CHAT_IMAGE_TTL_SECONDS", "3600")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ChatImage.Enable {
		t.Fatal("expected chat images disabled")
	}
	if cfg.ChatImage.Dir != "/var/lib/webrtc-gateway/chat-images" {
		t.Fatalf("dir = %q", cfg.ChatImage.Dir)
	}
	if cfg.ChatImage.PublicBaseURL != "https://k2-gateway.kasemsan.com" {
		t.Fatalf("publicBaseURL = %q", cfg.ChatImage.PublicBaseURL)
	}
	if cfg.ChatImage.MaxBytes != 1048576 || cfg.ChatImage.MaxPerSession != 5 || cfg.ChatImage.TTLSeconds != 3600 {
		t.Fatalf("unexpected chat image limits %#v", cfg.ChatImage)
	}
}

func TestChatImageConfigPromotesLegacyTwoMegabyteLimit(t *testing.T) {
	t.Setenv("CHAT_IMAGE_MAX_BYTES", "2097152")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ChatImage.MaxBytes != DefaultChatImageMaxBytes {
		t.Fatalf("maxBytes = %d, want %d", cfg.ChatImage.MaxBytes, DefaultChatImageMaxBytes)
	}
}
