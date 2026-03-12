package sip

import (
	"strings"
	"testing"
)

func TestResolveSIPDestination(t *testing.T) {
	tests := []struct {
		name      string
		domain    string
		port      int
		transport string
		wantMatch string // substring that should be in result
		wantErr   bool
	}{
		{
			name:      "IPv4 with explicit port",
			domain:    "192.168.1.100",
			port:      5060,
			transport: "tcp",
			wantMatch: "192.168.1.100:5060",
			wantErr:   false,
		},
		{
			name:      "IPv4 with port 0 - defaults to 5060",
			domain:    "10.0.0.1",
			port:      0,
			transport: "tcp",
			wantMatch: "10.0.0.1:5060",
			wantErr:   false,
		},
		{
			name:      "IPv6 with explicit port - should use brackets",
			domain:    "2001:db8::1",
			port:      5070,
			transport: "tcp",
			wantMatch: "[2001:db8::1]:5070",
			wantErr:   false,
		},
		{
			name:      "IPv6 with port 0 - defaults to 5060 with brackets",
			domain:    "::1",
			port:      0,
			transport: "tcp",
			wantMatch: "[::1]:5060",
			wantErr:   false,
		},
		{
			name:      "Hostname with explicit port",
			domain:    "sip.example.com",
			port:      5080,
			transport: "tcp",
			wantMatch: "sip.example.com:5080",
			wantErr:   false,
		},
		{
			name:      "Hostname with port 0 - tries SRV, falls back to 5060",
			domain:    "nonexistent.invalid",
			port:      0,
			transport: "tcp",
			wantMatch: ":5060", // should fallback to domain:5060
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolveSIPDestination(tt.domain, tt.port, tt.transport)

			if (err != nil) != tt.wantErr {
				t.Errorf("resolveSIPDestination() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && !strings.Contains(result, tt.wantMatch) {
				t.Errorf("resolveSIPDestination() = %q, want to contain %q", result, tt.wantMatch)
			}

			// Additional validation: result should always contain a colon (host:port)
			if !tt.wantErr && !strings.Contains(result, ":") {
				t.Errorf("resolveSIPDestination() = %q, should contain ':' (host:port)", result)
			}
		})
	}
}

func TestTrimTrailingDot(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"example.com.", "example.com"},
		{"example.com", "example.com"},
		{".", ""},
		{"", ""},
		{"sub.domain.com.", "sub.domain.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := trimTrailingDot(tt.input); got != tt.want {
				t.Errorf("trimTrailingDot(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
