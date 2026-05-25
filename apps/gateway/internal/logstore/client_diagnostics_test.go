package logstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNoopStoreClientDiagnosticReturnsDisabled(t *testing.T) {
	t.Parallel()

	store := &noopStore{}
	err := store.StoreClientDiagnostic(context.Background(), &ClientDiagnosticRecord{
		Timestamp:     time.Now().UTC(),
		ClientTraceID: "trace-1",
		Source:        "app",
		Level:         "info",
		Name:          "app.boot",
	})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}
