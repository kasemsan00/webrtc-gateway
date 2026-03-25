package sip

import (
	"errors"
	"fmt"
	"testing"

	gosip "github.com/emiago/sipgo/sip"
)

func TestHandleLeaseRenewResult_TransientFailureKeepsOwnership(t *testing.T) {
	t.Parallel()

	stopCh := make(chan struct{})
	tm := &TrunkManager{
		ownedLeases:    map[int64]bool{1: true},
		registrations:  map[int64]*gosip.ClientTransaction{1: nil},
		refreshWorkers: map[int64]chan struct{}{1: stopCh},
		leaseRetryRuns: map[int64]int{},
	}

	leaseLost, retryCount := tm.handleLeaseRenewResult(1, fmt.Errorf("%w: db timeout", ErrTrunkLeaseRetry))
	if leaseLost {
		t.Fatalf("expected leaseLost=false for transient error")
	}
	if retryCount != 1 {
		t.Fatalf("expected retryCount=1, got %d", retryCount)
	}
	if !tm.ownedLeases[1] {
		t.Fatalf("expected ownership to be kept on transient failure")
	}
	if _, ok := tm.refreshWorkers[1]; !ok {
		t.Fatalf("expected refresh worker to remain on transient failure")
	}
	if got := tm.leaseRetryRuns[1]; got != 1 {
		t.Fatalf("expected leaseRetryRuns[1]=1, got %d", got)
	}
}

func TestHandleLeaseRenewResult_LostLeaseDropsOwnershipAndWorker(t *testing.T) {
	t.Parallel()

	stopCh := make(chan struct{})
	tm := &TrunkManager{
		ownedLeases:    map[int64]bool{1: true},
		registrations:  map[int64]*gosip.ClientTransaction{1: nil},
		refreshWorkers: map[int64]chan struct{}{1: stopCh},
		leaseRetryRuns: map[int64]int{1: 2},
	}

	leaseLost, retryCount := tm.handleLeaseRenewResult(1, fmt.Errorf("%w: held by another instance", ErrTrunkLeaseLost))
	if !leaseLost {
		t.Fatalf("expected leaseLost=true when lease is lost")
	}
	if retryCount != 0 {
		t.Fatalf("expected retryCount=0 when lease is lost, got %d", retryCount)
	}
	if _, ok := tm.ownedLeases[1]; ok {
		t.Fatalf("expected ownership to be dropped after lease loss")
	}
	if _, ok := tm.refreshWorkers[1]; ok {
		t.Fatalf("expected refresh worker to be removed after lease loss")
	}
	if _, ok := tm.registrations[1]; ok {
		t.Fatalf("expected registration cache to be removed after lease loss")
	}
	if _, ok := tm.leaseRetryRuns[1]; ok {
		t.Fatalf("expected retry counter to be cleared after lease loss")
	}

	select {
	case <-stopCh:
		// expected closed
	default:
		t.Fatalf("expected stop channel to be closed after lease loss")
	}
}

func TestHandleLeaseRenewResult_TransientThenRecoverResetsRetryCounter(t *testing.T) {
	t.Parallel()

	stopCh := make(chan struct{})
	tm := &TrunkManager{
		ownedLeases:    map[int64]bool{1: true},
		registrations:  map[int64]*gosip.ClientTransaction{1: nil},
		refreshWorkers: map[int64]chan struct{}{1: stopCh},
		leaseRetryRuns: map[int64]int{},
	}

	leaseLost, retryCount := tm.handleLeaseRenewResult(1, errors.New("temporary network error"))
	if leaseLost {
		t.Fatalf("expected leaseLost=false for transient error")
	}
	if retryCount != 1 {
		t.Fatalf("expected retryCount=1 after transient error, got %d", retryCount)
	}

	leaseLost, retryCount = tm.handleLeaseRenewResult(1, nil)
	if leaseLost {
		t.Fatalf("expected leaseLost=false for success")
	}
	if retryCount != 0 {
		t.Fatalf("expected retryCount=0 for success, got %d", retryCount)
	}
	if _, ok := tm.leaseRetryRuns[1]; ok {
		t.Fatalf("expected retry counter to be reset after recovery")
	}
	if !tm.ownedLeases[1] {
		t.Fatalf("expected ownership to remain after recovery")
	}
	if _, ok := tm.refreshWorkers[1]; !ok {
		t.Fatalf("expected refresh worker to remain after recovery")
	}
}
