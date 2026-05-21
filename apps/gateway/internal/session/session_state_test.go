package session

import "testing"

func TestTerminalCleanupStateIgnoresICETransitions(t *testing.T) {
	tests := []struct {
		name           string
		state          SessionState
		terminalAction string
		want           bool
	}{
		{name: "ended", state: StateEnded, want: true},
		{name: "bye", state: StateActive, terminalAction: "bye", want: true},
		{name: "reject", state: StateIncoming, terminalAction: "reject", want: true},
		{name: "cancel", state: StateRinging, terminalAction: "cancel", want: true},
		{name: "end", state: StateActive, terminalAction: "end", want: true},
		{name: "active", state: StateActive, want: false},
		{name: "connecting", state: StateConnecting, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTerminalCleanupState(tt.state, tt.terminalAction); got != tt.want {
				t.Fatalf("isTerminalCleanupState(%q, %q)=%v want %v", tt.state, tt.terminalAction, got, tt.want)
			}
		})
	}
}
