package sip

import (
	"testing"
)

func TestBuildPublicAccountKey(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		username string
		port     int
		expected string
	}{
		{
			name:     "Hostname with port 5060 - port omitted",
			domain:   "sipclient.ttrs.or.th",
			username: "0000177005714",
			port:     5060,
			expected: "0000177005714@sipclient.ttrs.or.th",
		},
		{
			name:     "Hostname with port 5070 - port still omitted (DNS SRV)",
			domain:   "sip.example.com",
			username: "user123",
			port:     5070,
			expected: "user123@sip.example.com",
		},
		{
			name:     "Hostname with port 0 - port omitted",
			domain:   "pbx.local",
			username: "alice",
			port:     0,
			expected: "alice@pbx.local",
		},
		{
			name:     "IPv4 with port 5060 - port included",
			domain:   "192.168.1.100",
			username: "bob",
			port:     5060,
			expected: "bob@192.168.1.100:5060",
		},
		{
			name:     "IPv4 with port 0 - port omitted",
			domain:   "10.0.0.1",
			username: "charlie",
			port:     0,
			expected: "charlie@10.0.0.1",
		},
		{
			name:     "IPv6 with port 5060 - port included with brackets",
			domain:   "2001:db8::1",
			username: "dave",
			port:     5060,
			expected: "dave@[2001:db8::1]:5060",
		},
		{
			name:     "IPv6 with port 0 - port omitted",
			domain:   "fe80::1",
			username: "eve",
			port:     0,
			expected: "eve@fe80::1",
		},
		{
			name:     "IPv6 loopback with port 5080 - brackets included",
			domain:   "::1",
			username: "frank",
			port:     5080,
			expected: "frank@[::1]:5080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildPublicAccountKey(tt.domain, tt.username, tt.port)
			if result != tt.expected {
				t.Errorf("buildPublicAccountKey(%q, %q, %d) = %q; want %q",
					tt.domain, tt.username, tt.port, result, tt.expected)
			}
		})
	}
}
