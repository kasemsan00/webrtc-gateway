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
