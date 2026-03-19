package api

import (
	"testing"

	"k2-gateway/internal/observability"
)

func TestWSBackpressure_IncrementsQueueFullMetric(t *testing.T) {
	srv := &Server{
		metrics: observability.NewRegistry(nil),
	}
	client := &WSClient{
		sessionID: "session-1",
		send:      make(chan []byte, 1),
	}
	// Fill buffer to force non-blocking send failure.
	client.send <- []byte(`{"type":"already-queued"}`)

	srv.sendWSMessage(client, WSMessage{Type: "state", SessionID: "session-1", State: "active"})

	snap := srv.metrics.Snapshot()
	if snap.Counters[observability.MetricWSSendQueueFullTotal] != 1 {
		t.Fatalf("expected queue-full metric to increment to 1, got %v", snap.Counters[observability.MetricWSSendQueueFullTotal])
	}
}
