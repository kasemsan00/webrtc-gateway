package sip

import (
	"fmt"
	"sort"
	"time"
)

const (
	registerRetryBaseDelay = 5 * time.Second
	registerRetryMaxDelay  = 5 * time.Minute
)

// registerRetry is the backoff state of an owned trunk whose last REGISTER
// failed. Without it a trunk that failed at boot (registrar down) stayed
// unregistered until an admin refresh, and a failed refresh waited a full
// 0.8 x Expires.
type registerRetry struct {
	failures int
	next     time.Time
	inFlight bool
}

// registerRetryBackoff is 5s, 10s, 20s ... capped at 5 minutes.
func registerRetryBackoff(failures int) time.Duration {
	delay := registerRetryBaseDelay
	for i := 1; i < failures && delay < registerRetryMaxDelay; i++ {
		delay *= 2
	}
	if delay > registerRetryMaxDelay {
		delay = registerRetryMaxDelay
	}
	return delay
}

// nextRegistrarGenerationLocked starts a registrar operation for trunkID.
// Caller must hold tm.mu.
func (tm *TrunkManager) nextRegistrarGenerationLocked(trunkID int64) uint64 {
	if tm.registrarGenerations == nil {
		tm.registrarGenerations = make(map[int64]uint64)
	}
	tm.registrarGenerations[trunkID]++
	return tm.registrarGenerations[trunkID]
}

// registerOutcomeStillWanted reports, for a REGISTER that just succeeded,
// whether this instance still owns the trunk and whether no newer registrar
// operation started since. A wanted success also clears retry backoff.
func (tm *TrunkManager) registerOutcomeStillWanted(trunkID int64, generation uint64) (owned, current bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	owned = tm.ownedLeases[trunkID]
	current = tm.registrarGenerations[trunkID] == generation
	if owned && current {
		delete(tm.registerRetries, trunkID)
	}
	return owned, current
}

// noteRegisterFailure schedules the next REGISTER attempt for an owned trunk.
func (tm *TrunkManager) noteRegisterFailure(trunkID int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if !tm.ownedLeases[trunkID] {
		delete(tm.registerRetries, trunkID)
		return
	}
	if tm.registerRetries == nil {
		tm.registerRetries = make(map[int64]*registerRetry)
	}
	r := tm.registerRetries[trunkID]
	if r == nil {
		r = &registerRetry{}
		tm.registerRetries[trunkID] = r
	}
	r.failures++
	r.next = time.Now().Add(registerRetryBackoff(r.failures))
}

// dueRegisterRetries claims the trunks whose retry is due and drops entries
// for trunks this instance no longer owns.
func (tm *TrunkManager) dueRegisterRetries(now time.Time) []int64 {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	var due []int64
	for id, r := range tm.registerRetries {
		if !tm.ownedLeases[id] {
			delete(tm.registerRetries, id)
			continue
		}
		if r.inFlight || now.Before(r.next) {
			continue
		}
		r.inFlight = true
		due = append(due, id)
	}
	sort.Slice(due, func(i, j int) bool { return due[i] < due[j] })
	return due
}

func (tm *TrunkManager) finishRegisterRetry(trunkID int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if r := tm.registerRetries[trunkID]; r != nil {
		r.inFlight = false
	}
}

// retryFailedRegistrations re-sends REGISTER for owned trunks whose last
// attempt failed. Each retry runs in its own goroutine so a slow registrar
// cannot delay lease renewal.
func (tm *TrunkManager) retryFailedRegistrations(now time.Time) {
	for _, id := range tm.dueRegisterRetries(now) {
		tm.wg.Add(1)
		go func(trunkID int64) {
			defer tm.wg.Done()
			defer tm.finishRegisterRetry(trunkID)
			if err := tm.registerTrunk(trunkID); err != nil {
				fmt.Printf("⚠️ [TrunkManager] Trunk %d REGISTER retry failed: %v\n", trunkID, err)
				return
			}
			fmt.Printf("✅ [TrunkManager] Trunk %d REGISTER retry succeeded\n", trunkID)
		}(id)
	}
}
