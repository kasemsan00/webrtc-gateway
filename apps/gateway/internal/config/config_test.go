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
		{name: "invalid falls back", value: "disabled", want: SIPSwitchVideoTransitionPreserve},
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
