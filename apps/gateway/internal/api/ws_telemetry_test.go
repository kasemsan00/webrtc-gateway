package api

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"webrtc-sip-gateway/internal/telemetry"
)

func TestFullWebSocketSendBufferDropsNewestAndEmitsSafeEvent(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256),
	}))
	client := &WSClient{sessionID: "safe-session", send: make(chan []byte, 1)}
	client.send <- []byte("existing")
	server := &Server{}
	server.sendWSMessage(client, WSMessage{Type: "offer", Body: "body-secret", SIPPassword: "password-secret", PNToken: "push-token-secret"})
	if len(client.send) != 1 || string(<-client.send) != "existing" {
		t.Fatal("full send buffer did not preserve the earlier message")
	}
	text := output.String()
	if !strings.Contains(text, `"event.name":"websocket.message.dropped"`) || !strings.Contains(text, `"reason":"queue_full"`) {
		t.Fatalf("missing bounded drop event: %s", text)
	}
	for _, forbidden := range []string{"body-secret", "password-secret", "push-token-secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("drop telemetry leaked %q: %s", forbidden, text)
		}
	}
}

func TestWebSocketReconnectEmitsBoundedConnectDisconnectEvents(t *testing.T) {
	var output bytes.Buffer
	telemetry.SetStructuredLogger(telemetry.NewStructuredLogger(&output, telemetry.StructuredLogConfig{
		Format: telemetry.LogFormatJSON, Sanitizer: telemetry.NewSanitizer(256),
	}))
	ctx := context.Background()
	telemetry.RecordWebSocketConnection(ctx, true)
	_ = telemetry.Log(ctx, telemetry.LogEvent{Severity: telemetry.SeverityInfo, Component: "websocket", Name: "websocket.connected", Outcome: "success", Reason: "none"})
	telemetry.RecordWebSocketConnection(ctx, false)
	_ = telemetry.Log(ctx, telemetry.LogEvent{Severity: telemetry.SeverityInfo, Component: "websocket", Name: "websocket.disconnected", Outcome: "success", Reason: "peer_closed"})
	telemetry.RecordWebSocketConnection(ctx, true)
	_ = telemetry.Log(ctx, telemetry.LogEvent{Severity: telemetry.SeverityInfo, Component: "websocket", Name: "websocket.connected", Outcome: "success", Reason: "none"})
	text := output.String()
	if strings.Count(text, `"event.name":"websocket.connected"`) != 2 || strings.Count(text, `"event.name":"websocket.disconnected"`) != 1 {
		t.Fatalf("unexpected reconnect events: %s", text)
	}
	for _, forbidden := range []string{"client-id", "session-secret", "10.0.0.1"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("reconnect telemetry leaked %q: %s", forbidden, text)
		}
	}
}
