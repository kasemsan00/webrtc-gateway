package logstore

import (
	"context"
	"testing"

	"k2-gateway/internal/config"
)

func TestNoopStoreResolveTrunkByCredentials(t *testing.T) {
	store, err := New(config.DBConfig{Enable: false})
	if err != nil {
		t.Fatalf("failed to init noop store: %v", err)
	}

	trunkID, leaseOwner, leaseUntil, found, err := store.ResolveTrunkByCredentials(context.Background(), "sip.example.com", 5060, "1001", "secret")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	if trunkID != 0 {
		t.Fatalf("expected trunkID=0, got %d", trunkID)
	}
	if leaseOwner != nil {
		t.Fatalf("expected leaseOwner=nil")
	}
	if leaseUntil != nil {
		t.Fatalf("expected leaseUntil=nil")
	}
}
