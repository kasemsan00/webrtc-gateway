package sip

import (
	"testing"
	"time"
)

func TestRegisterRetryBackoffDoublesAndCaps(t *testing.T) {
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 5 * time.Minute, 5 * time.Minute}
	for i, w := range want {
		if got := registerRetryBackoff(i + 1); got != w {
			t.Fatalf("failures=%d: got %s want %s", i+1, got, w)
		}
	}
}

func TestNoteRegisterFailure_OnlyOwnedTrunksRetry(t *testing.T) {
	tm := &TrunkManager{ownedLeases: map[int64]bool{1: true}}

	tm.noteRegisterFailure(1)
	tm.noteRegisterFailure(2)

	if tm.registerRetries[1] == nil || tm.registerRetries[1].failures != 1 {
		t.Fatalf("expected owned trunk 1 scheduled, got %+v", tm.registerRetries[1])
	}
	if _, ok := tm.registerRetries[2]; ok {
		t.Fatal("trunk 2 is not owned and must not be retried")
	}
}

func TestDueRegisterRetries_ClaimsDueAndDropsLostLeases(t *testing.T) {
	now := time.Now()
	tm := &TrunkManager{
		ownedLeases: map[int64]bool{1: true, 2: true},
		registerRetries: map[int64]*registerRetry{
			1: {failures: 1, next: now.Add(-time.Second)},
			2: {failures: 1, next: now.Add(time.Minute)},
			3: {failures: 1, next: now.Add(-time.Second)}, // lease lost
		},
	}

	due := tm.dueRegisterRetries(now)
	if len(due) != 1 || due[0] != 1 {
		t.Fatalf("expected only trunk 1 due, got %v", due)
	}
	if !tm.registerRetries[1].inFlight {
		t.Fatal("claimed retry must be marked in flight")
	}
	if again := tm.dueRegisterRetries(now); len(again) != 0 {
		t.Fatalf("in-flight retry must not be claimed twice, got %v", again)
	}
	if _, ok := tm.registerRetries[3]; ok {
		t.Fatal("retry for a trunk no longer owned must be dropped")
	}

	tm.finishRegisterRetry(1)
	if tm.registerRetries[1].inFlight {
		t.Fatal("finished retry must be claimable again")
	}
}

// A REGISTER that succeeds after UnregisterTrunk (lease dropped) or after a
// newer REGISTER started must not be reported as the trunk's registration.
func TestRegisterOutcomeStillWanted(t *testing.T) {
	tm := &TrunkManager{
		ownedLeases:          map[int64]bool{1: true},
		registrarGenerations: map[int64]uint64{1: 4, 2: 1},
		registerRetries:      map[int64]*registerRetry{1: {failures: 2}},
	}

	if owned, current := tm.registerOutcomeStillWanted(1, 3); !owned || current {
		t.Fatalf("superseded generation: owned=%v current=%v", owned, current)
	}
	if tm.registerRetries[1] == nil {
		t.Fatal("a superseded success must not clear the retry state")
	}
	if owned, current := tm.registerOutcomeStillWanted(2, 1); owned || !current {
		t.Fatalf("unowned trunk: owned=%v current=%v", owned, current)
	}
	if owned, current := tm.registerOutcomeStillWanted(1, 4); !owned || !current {
		t.Fatalf("current success: owned=%v current=%v", owned, current)
	}
	if _, ok := tm.registerRetries[1]; ok {
		t.Fatal("a wanted success must clear retry backoff")
	}
}
