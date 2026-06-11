package sip

import (
	"strings"
	"testing"
)

func TestSIPAdvertisedCapabilitiesMatchImplementedMidCallFeatures(t *testing.T) {
	allow := sipAllowHeaderValue()
	supported := sipSupportedHeaderValue()

	for _, method := range []string{"INVITE", "ACK", "CANCEL", "OPTIONS", "BYE", "MESSAGE", "UPDATE"} {
		if !strings.Contains(allow, method) {
			t.Fatalf("expected Allow to include %s, got %q", method, allow)
		}
	}
	for _, unsupported := range []string{"REFER", "PRACK", "SUBSCRIBE", "NOTIFY"} {
		if strings.Contains(allow, unsupported) {
			t.Fatalf("did not expect Allow to advertise %s before first-class support, got %q", unsupported, allow)
		}
	}
	if strings.Contains(supported, "100rel") || strings.Contains(supported, "timer") {
		t.Fatalf("did not expect Supported to advertise unimplemented extensions, got %q", supported)
	}
	if !strings.Contains(supported, "outbound") {
		t.Fatalf("expected Supported to preserve outbound, got %q", supported)
	}
}
