package session

import (
	"bytes"
	"strings"
	"testing"

	"webrtc-sip-gateway/internal/telemetry"
)

func TestCallTelemetryLifecycleIsExactlyOnce(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256),
	}))
	sess := &Session{ID: "safe-session", State: StateNew}
	sess.SetCallInfo("outbound", "sensitive-from", "sensitive-to", "sensitive-call-id")
	sess.SetState(StateConnecting)
	sess.SetState(StateConnecting)
	sess.SetState(StateActive)
	sess.SetState(StateActive)
	sess.SetState(StateEnded)
	sess.SetState(StateEnded)

	text := output.String()
	for event, want := range map[string]int{"call.started": 1, "call.setup.completed": 1, "call.completed": 1} {
		if got := strings.Count(text, `"event.name":"`+event+`"`); got != want {
			t.Fatalf("event %s count = %d, want %d; output=%s", event, got, want, text)
		}
	}
	if !strings.Contains(text, `"session.id":"safe-session"`) {
		t.Fatalf("call telemetry omitted session correlation: %s", text)
	}
	for _, forbidden := range []string{"sensitive-from", "sensitive-to", "sensitive-call-id"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("call telemetry leaked %q: %s", forbidden, text)
		}
	}
}

func TestIncomingCallTelemetryOwnershipTransfersWithoutDoubleCount(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256),
	}))
	incoming := &Session{ID: "incoming", State: StateIncoming}
	incoming.SetCallInfo("inbound", "from", "to", "call")
	active := &Session{ID: "active", State: StateConnecting}
	active.CopyIncomingInviteFrom(incoming)
	active.SetCallInfo("inbound", "from", "to", "call")
	incoming.SetState(StateEnded)
	active.SetState(StateActive)
	active.SetState(StateEnded)

	text := output.String()
	if got := strings.Count(text, `"event.name":"call.started"`); got != 1 {
		t.Fatalf("started count = %d, want 1: %s", got, text)
	}
	if got := strings.Count(text, `"event.name":"call.completed"`); got != 1 {
		t.Fatalf("completed count = %d, want 1: %s", got, text)
	}
}

func TestTerminalTelemetryOutcomeIsBounded(t *testing.T) {
	for _, test := range []struct{ action, terminalReason, outcome, reason string }{
		{action: "reject", outcome: "rejected", reason: "remote_rejected"},
		{action: "timeout", outcome: "timeout", reason: "deadline_exceeded"},
		{action: "cancel", outcome: "cancelled", reason: "none"},
		{terminalReason: "ice_failed", outcome: "failure", reason: "peer_closed"},
		{action: "raw secret action", terminalReason: "raw error", outcome: "success", reason: "none"},
	} {
		outcome, reason := terminalTelemetryOutcome(test.action, test.terminalReason)
		if outcome != test.outcome || reason != test.reason {
			t.Errorf("terminalTelemetryOutcome(%q,%q) = %q,%q", test.action, test.terminalReason, outcome, reason)
		}
	}
}
