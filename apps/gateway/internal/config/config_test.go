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
