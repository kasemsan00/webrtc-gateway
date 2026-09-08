package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizerClassifiedValuesNeverAppear(t *testing.T) {
	t.Parallel()

	sanitizer := NewSanitizer(128)
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "authorization header mixed case", key: "AuThOrIzAtIoN", value: "Bearer auth-secret-fixture"},
		{name: "proxy authorization punctuation", key: "Proxy_Authorization", value: "Digest username=alice,response=proxy-secret-fixture"},
		{name: "standalone bearer token", key: "failure", value: "Bearer standalone-bearer-fixture"},
		{name: "jwt in ordinary text", key: "error", value: "rejected eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJqd3Qtc2VjcmV0LWZpeHR1cmUifQ.c2lnbmF0dXJlRml4dHVyZQ"},
		{name: "sip password", key: "sip-password", value: "sip-password-secret-fixture"},
		{name: "turn credential", key: "TURN_CREDENTIAL", value: "turn-credential-secret-fixture"},
		{name: "dsn", key: "database_dsn", value: "postgres://gateway:dsn-secret-fixture@db.example/gateway"},
		{name: "push token", key: "pushToken", value: "push-token-secret-fixture"},
		{name: "raw sip", key: "diagnostic", value: "INVITE sip:bob@example.test SIP/2.0\r\nAuthorization: Bearer raw-sip-secret-fixture\r\n\r\nhello"},
		{name: "sdp", key: "diagnostic", value: "v=0\r\no=- 1 1 IN IP4 192.0.2.10\r\nm=audio 49170 RTP/AVP 111\r\na=ice-pwd:sdp-secret-fixture"},
		{name: "ipv4", key: "peer", value: "connected to 203.0.113.77:5060"},
		{name: "ipv6", key: "peer", value: "connected to [2001:db8:85a3::8a2e:370:7334]"},
		{name: "sip identity", key: "peer", value: "sip:private-user@example.test"},
		{name: "message body", key: "message_body", value: "message-body-secret-fixture"},
		{name: "transcript", key: "Transcript", value: "transcript-secret-fixture"},
		{name: "device token suffix", key: "apns_device_token", value: "device-token-secret-fixture"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clean := sanitizer.Sanitize(test.key, test.value)
			encoded, err := json.Marshal(clean)
			if err != nil {
				t.Fatalf("marshal sanitized value: %v", err)
			}
			if strings.Contains(string(encoded), test.value) {
				t.Fatalf("sanitized output contains raw fixture %q: %s", test.value, encoded)
			}
		})
	}
}

func TestSanitizerRecursivelySanitizesArbitraryMap(t *testing.T) {
	t.Parallel()

	fixtures := []string{
		"nested-password-fixture",
		"nested-auth-fixture",
		"nested-transcript-fixture",
		"198.51.100.24",
		"keysecretfixture0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	}
	input := map[string]any{
		"safe_count": 42,
		"nested": map[string]any{
			"PASSWORD":      fixtures[0],
			"authorization": "Bearer " + fixtures[1],
			"items": []any{
				map[string]any{"transcript_body": fixtures[2]},
				"peer=" + fixtures[3],
			},
		},
		fixtures[4]: "value",
	}

	clean := NewSanitizer(128).SanitizeMap(input)
	encoded, err := json.Marshal(clean)
	if err != nil {
		t.Fatalf("marshal sanitized map: %v", err)
	}
	for _, fixture := range fixtures {
		if strings.Contains(string(encoded), fixture) {
			t.Errorf("sanitized map contains raw fixture %q: %s", fixture, encoded)
		}
	}
	if clean["safe_count"] != 42 {
		t.Errorf("safe typed measurement changed: got %#v", clean["safe_count"])
	}
}

func TestSanitizerBoundsStringsAndOmitsUnknownContainers(t *testing.T) {
	t.Parallel()

	sanitizer := NewSanitizer(32)
	long := strings.Repeat("safe value ", 20)
	clean, ok := sanitizer.Sanitize("detail", long).(string)
	if !ok {
		t.Fatalf("sanitized string has type %T", clean)
	}
	if len(clean) > 32 || !strings.HasSuffix(clean, TruncatedValue) {
		t.Fatalf("oversized value was not bounded: len=%d value=%q", len(clean), clean)
	}
	if got := NewSanitizer(5).SanitizeText(long); len(got) != 5 {
		t.Fatalf("small maximum was not honored: len=%d value=%q", len(got), got)
	}

	type unreviewed struct{ Secret string }
	if got := sanitizer.Sanitize("metadata", unreviewed{Secret: "struct-secret-fixture"}); got != OmittedValue {
		t.Fatalf("unreviewed structure = %q, want %q", got, OmittedValue)
	}
}

func TestSanitizeTextRedactsInlineCredentials(t *testing.T) {
	t.Parallel()

	raw := "request failed Authorization: Bearer inline-auth-fixture password=inline-password-fixture peer=192.0.2.44"
	clean := NewSanitizer(256).SanitizeText(raw)
	for _, fixture := range []string{"inline-auth-fixture", "inline-password-fixture", "192.0.2.44"} {
		if strings.Contains(clean, fixture) {
			t.Errorf("sanitized legacy text contains %q: %q", fixture, clean)
		}
	}
}
