package session

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"

	"webrtc-sip-gateway/internal/config"
)

func TestSwitchVideoGateInactivePassThroughRejectsOnlyAcceptedStaleGeneration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{SwitchVideoGateAcceptedGeneration: 7}

	stale := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(6, false, false, 1), now)
	current := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(7, false, false, 1), now)
	newer := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(8, false, false, 1), now)

	if stale.Emit || stale.Reason != "stale-accepted-generation" || stale.Generation != 7 {
		t.Fatalf("stale inactive decision = %+v", stale)
	}
	if !current.Emit || current.Reason != "inactive" {
		t.Fatalf("current inactive decision = %+v", current)
	}
	if !newer.Emit || newer.Reason != "inactive" {
		t.Fatalf("newer inactive decision = %+v", newer)
	}
}

func TestSwitchVideoGateReadyIDRReservesUntilCommit(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 4}
	if !sess.StartSwitchVideoGate(4, start, "agent-switch") {
		t.Fatal("gate did not start")
	}

	ready := gateTestAU(4, true, true, 99)
	ready.InjectedParameterSets = true
	reserved := sess.EvaluateSwitchVideoAccessUnit(ready, start.Add(time.Second))
	if !reserved.Emit || reserved.Reason != "complete-idr-reserved" || reserved.Generation != 4 || reserved.Reservation == 0 {
		t.Fatalf("reservation decision = %+v", reserved)
	}
	if !sess.IsSwitchVideoGateActive() {
		t.Fatal("reservation cleared gate before commit")
	}

	blocked := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(4, false, true, 99), start.Add(2*time.Second))
	if blocked.Emit || blocked.Reason != "release-in-progress" {
		t.Fatalf("P-frame passed before commit: %+v", blocked)
	}
	if sess.CommitSwitchVideoGateRelease(3, reserved.Reservation, start.Add(3*time.Second)) {
		t.Fatal("wrong generation committed reservation")
	}
	if !sess.IsSwitchVideoGateActive() {
		t.Fatal("wrong commit cleared gate")
	}
	if !sess.CommitSwitchVideoGateRelease(4, reserved.Reservation, start.Add(3*time.Second)) {
		t.Fatal("correct generation failed to commit")
	}
	if sess.IsSwitchVideoGateActive() {
		t.Fatal("committed gate remained active")
	}
	if sess.SwitchVideoGateAcceptedGeneration != 4 {
		t.Fatalf("accepted generation = %d, want 4", sess.SwitchVideoGateAcceptedGeneration)
	}
}

func TestSwitchVideoGateAbortReturnsToAwaitingAndLaterIDRCanReserve(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 2}
	sess.StartSwitchVideoGate(2, now, "switch")
	first := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(2, true, true, 1), now)
	if !first.Emit {
		t.Fatalf("first IDR did not reserve: %+v", first)
	}
	if sess.AbortSwitchVideoGateRelease(1, first.Reservation, "wrong-generation") {
		t.Fatal("wrong generation aborted reservation")
	}
	if blocked := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(2, true, true, 1), now); blocked.Emit {
		t.Fatalf("wrong abort opened reservation: %+v", blocked)
	}
	if !sess.AbortSwitchVideoGateRelease(2, first.Reservation, "write-failed") {
		t.Fatal("correct generation failed to abort")
	}
	if pframe := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(2, false, true, 1), now); pframe.Emit {
		t.Fatalf("abort failed open to P-frame: %+v", pframe)
	}
	second := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(2, true, true, 1), now.Add(time.Second))
	if !second.Emit || second.Reason != "complete-idr-reserved" {
		t.Fatalf("later IDR could not reserve: %+v", second)
	}
}

func TestSwitchVideoGateRejectsUnsafeAndStaleAccessUnitsWithoutTimeoutFailOpen(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 10}
	sess.StartSwitchVideoGate(10, start, "switch")

	tests := []struct {
		name   string
		au     NormalizedH264AccessUnit
		reason string
	}{
		{name: "stale generation", au: gateTestAU(9, true, true, 4242), reason: "stale-generation"},
		{name: "current non IDR", au: gateTestAU(10, false, true, 4242), reason: "non-idr"},
		{name: "IDR without parameter sets", au: gateTestAU(10, true, false, 4242), reason: "parameter-sets-not-ready"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := sess.EvaluateSwitchVideoAccessUnit(tt.au, start.Add(time.Duration(i)*time.Second))
			if decision.Emit || decision.Reason != tt.reason || decision.Generation != 10 {
				t.Fatalf("decision = %+v, want blocked reason %q", decision, tt.reason)
			}
		})
	}

	decision := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(10, false, true, 4242), start.Add(31*time.Second))
	if decision.Emit || !sess.IsSwitchVideoGateActive() {
		t.Fatalf("stalled gate failed open: decision=%+v", decision)
	}
}

func TestSwitchVideoGateSameSSRCDoesNotOverrideGeneration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 10}
	sess.StartSwitchVideoGate(10, now, "switch")

	stale := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(9, true, true, 4242), now)
	current := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(10, true, true, 4242), now)
	if stale.Emit || stale.Reason != "stale-generation" {
		t.Fatalf("same-SSRC stale AU = %+v", stale)
	}
	if !current.Emit || current.Reason != "complete-idr-reserved" {
		t.Fatalf("same-SSRC current AU = %+v", current)
	}
}

func TestSwitchVideoGateStaleStartAndStopCannotAffectNewerGeneration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 1, PLISent: 8}
	if !sess.StartSwitchVideoGate(1, now, "first") {
		t.Fatal("first valid gate start failed")
	}
	sess.SwitchGeneration = 2
	if !sess.StartSwitchVideoGate(2, now.Add(time.Second), "second") {
		t.Fatal("valid gate start failed")
	}
	if sess.StartSwitchVideoGate(1, now.Add(2*time.Second), "stale") {
		t.Fatal("stale start replaced newer generation")
	}
	if sess.StopSwitchVideoGate(1, now.Add(3*time.Second), "stale-stop") {
		t.Fatal("stale stop cleared newer generation")
	}
	sess.mu.RLock()
	generation := sess.SwitchVideoGateGeneration
	baseline := sess.SwitchVideoGateFeedbackBaseline
	startedAt := sess.SwitchVideoGateStartedAt
	sess.mu.RUnlock()
	if generation != 2 || baseline != 8 || !startedAt.Equal(now.Add(time.Second)) {
		t.Fatalf("newer gate changed: generation=%d baseline=%d startedAt=%s", generation, baseline, startedAt)
	}
	if !sess.StopSwitchVideoGate(2, now.Add(4*time.Second), "cancelled") {
		t.Fatal("current generation stop failed")
	}
}

func TestSwitchVideoGateStartRequiresAuthoritativeSessionGeneration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 2}
	if !sess.StartSwitchVideoGate(2, now, "current") {
		t.Fatal("authoritative generation did not start")
	}
	if !sess.StopSwitchVideoGate(2, now, "ordinary-stop") {
		t.Fatal("authoritative generation did not stop")
	}
	if sess.StartSwitchVideoGate(1, now, "delayed-old") || sess.IsSwitchVideoGateActive() {
		t.Fatal("delayed older generation restarted inactive gate")
	}
	if sess.StartSwitchVideoGate(3, now, "impossible-future") || sess.IsSwitchVideoGateActive() {
		t.Fatal("future generation started before session authority advanced")
	}
	sess.SwitchGeneration = 3
	if !sess.StartSwitchVideoGate(3, now, "advanced") {
		t.Fatal("generation did not start after session authority advanced")
	}
}

func TestSwitchVideoGateAcceptedGenerationSurvivesStopAndForceStopResetsIt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 5}
	sess.StartSwitchVideoGate(5, now, "switch")
	reserved := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, true, true, 1), now)
	sess.CommitSwitchVideoGateRelease(5, reserved.Reservation, now)
	if !sess.StartSwitchVideoGate(5, now, "same-generation") || !sess.StopSwitchVideoGate(5, now, "ordinary-stop") {
		t.Fatal("same accepted generation could not start and stop")
	}
	if sess.SwitchVideoGateAcceptedGeneration != 5 {
		t.Fatalf("ordinary stop lost accepted generation %d", sess.SwitchVideoGateAcceptedGeneration)
	}

	if sess.StartSwitchVideoGate(4, now, "stale") {
		t.Fatal("start older than accepted generation succeeded")
	}
	if decision := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(4, true, true, 1), now); decision.Emit {
		t.Fatalf("stale AU passed after commit: %+v", decision)
	}
	sess.ForceStopSwitchVideoGate(now, "media-reset")
	if sess.SwitchVideoGateAcceptedGeneration != 0 {
		t.Fatalf("force stop retained accepted generation %d", sess.SwitchVideoGateAcceptedGeneration)
	}
	if !sess.StartSwitchVideoGate(5, now, "after-reset") {
		t.Fatal("gate did not start after force reset")
	}
	afterReset := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, true, true, 1), now)
	if afterReset.Reservation <= reserved.Reservation {
		t.Fatalf("force reset reused reservation: before=%d after=%d", reserved.Reservation, afterReset.Reservation)
	}
}

func TestSwitchVideoGateStartRequiresNormalizationAndPreservesAudioState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{SwitchGeneration: 3, AudioSSRC: 11, RemoteAudioSSRC: 22, AudioSeq: 33}
	if sess.StartSwitchVideoGate(3, now, "disabled") || sess.IsSwitchVideoGateActive() {
		t.Fatal("gate started while normalization disabled")
	}
	sess.VideoAUNormalizeEnabled = true
	sess.StartSwitchVideoGate(3, now, "enabled")
	sess.EvaluateSwitchVideoAccessUnit(gateTestAU(3, false, true, 1), now)
	reserved := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(3, true, true, 1), now)
	sess.CommitSwitchVideoGateRelease(3, reserved.Reservation, now)
	if sess.AudioSSRC != 11 || sess.RemoteAudioSSRC != 22 || sess.AudioSeq != 33 {
		t.Fatalf("audio changed: local=%d remote=%d seq=%d", sess.AudioSSRC, sess.RemoteAudioSSRC, sess.AudioSeq)
	}
}

func TestSwitchVideoGateConcurrentEvaluateCreatesOneReservation(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 7}
	sess.StartSwitchVideoGate(7, now, "switch")
	au := gateTestAU(7, true, true, 1)

	var reservations atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sess.EvaluateSwitchVideoAccessUnit(au, now).Emit {
				reservations.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := reservations.Load(); got != 1 {
		t.Fatalf("reservations = %d, want one", got)
	}
}

func TestSwitchVideoGateReservationTokenPreventsSameGenerationABA(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 5, PLISent: 3}
	sess.StartSwitchVideoGate(5, now, "switch")
	a := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, true, true, 1), now)
	if a.Reservation == 0 {
		t.Fatal("reservation A has zero token")
	}

	if !sess.StartSwitchVideoGate(5, now.Add(time.Second), "duplicate-start") {
		t.Fatal("same-generation idempotent start returned false")
	}
	sess.mu.RLock()
	activeAfterStart := sess.SwitchVideoGateReservation
	startedAt := sess.SwitchVideoGateStartedAt
	baseline := sess.SwitchVideoGateFeedbackBaseline
	sess.mu.RUnlock()
	if activeAfterStart != a.Reservation || !startedAt.Equal(now) || baseline != 3 {
		t.Fatalf("same-generation start mutated lease: reservation=%d startedAt=%s baseline=%d", activeAfterStart, startedAt, baseline)
	}

	if !sess.AbortSwitchVideoGateRelease(5, a.Reservation, "retry") {
		t.Fatal("exact reservation A abort failed")
	}
	b := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, true, true, 1), now.Add(2*time.Second))
	if b.Reservation == 0 || b.Reservation == a.Reservation {
		t.Fatalf("reservation B token=%d, A=%d", b.Reservation, a.Reservation)
	}
	if sess.CommitSwitchVideoGateRelease(5, a.Reservation, now.Add(3*time.Second)) {
		t.Fatal("stale reservation A committed reservation B")
	}
	if sess.AbortSwitchVideoGateRelease(5, a.Reservation, "stale") {
		t.Fatal("stale reservation A aborted reservation B")
	}
	if !sess.CommitSwitchVideoGateRelease(5, b.Reservation, now.Add(3*time.Second)) {
		t.Fatal("exact reservation B commit failed")
	}
}

func TestSwitchVideoGateNewGenerationInvalidatesOlderReservation(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 5}
	sess.StartSwitchVideoGate(5, now, "first")
	a := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, true, true, 1), now)

	sess.SwitchGeneration = 6
	if !sess.StartSwitchVideoGate(6, now.Add(time.Second), "newer") {
		t.Fatal("newer generation did not supersede reserved gate")
	}
	b := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(6, true, true, 1), now.Add(time.Second))
	if b.Reservation == 0 || b.Reservation == a.Reservation {
		t.Fatalf("new generation reservation B=%d A=%d", b.Reservation, a.Reservation)
	}
	if sess.CommitSwitchVideoGateRelease(5, a.Reservation, now.Add(2*time.Second)) {
		t.Fatal("superseded generation committed")
	}
	if sess.AbortSwitchVideoGateRelease(5, a.Reservation, "stale") {
		t.Fatal("superseded generation aborted current reservation")
	}
	if !sess.CommitSwitchVideoGateRelease(6, b.Reservation, now.Add(2*time.Second)) {
		t.Fatal("current generation exact reservation did not commit")
	}
}

func TestSwitchVideoGateGloballyThrottlesAlternatingReasons(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 6}
	sess.StartSwitchVideoGate(6, now, "switch")
	sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, true, true, 1), now)
	sess.mu.RLock()
	firstLogAt := sess.SwitchVideoGateLastRejectLogAt
	sess.mu.RUnlock()
	for i := 1; i <= 8; i++ {
		generation := 5
		idr := true
		if i%2 != 0 {
			generation = 6
			idr = false
		}
		sess.EvaluateSwitchVideoAccessUnit(gateTestAU(generation, idr, true, 1), now.Add(time.Duration(i)*100*time.Millisecond))
	}
	sess.mu.RLock()
	lastLogAt := sess.SwitchVideoGateLastRejectLogAt
	sess.mu.RUnlock()
	if !lastLogAt.Equal(firstLogAt) {
		t.Fatalf("alternating reasons bypassed throttle: first=%s last=%s", firstLogAt, lastLogAt)
	}
	sess.EvaluateSwitchVideoAccessUnit(gateTestAU(6, false, true, 1), now.Add(time.Second))
	sess.mu.RLock()
	resumedAt := sess.SwitchVideoGateLastRejectLogAt
	sess.mu.RUnlock()
	if !resumedAt.Equal(now.Add(time.Second)) {
		t.Fatalf("logging did not resume after interval: %s", resumedAt)
	}
}

func TestSwitchVideoGateElapsedClampsTimeRegression(t *testing.T) {
	start := time.Unix(1_700_000_001, 0)
	if elapsed := switchVideoGateElapsed(start, start.Add(-time.Second)); elapsed != 0 {
		t.Fatalf("regressed elapsed = %s, want zero", elapsed)
	}
}

func TestSwitchVideoGateClockRegressionStartsFreshLogWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 6}
	sess.StartSwitchVideoGate(6, now, "switch")
	au := gateTestAU(6, false, true, 1)
	sess.EvaluateSwitchVideoAccessUnit(au, now)

	regressed := now.Add(-time.Second)
	sess.EvaluateSwitchVideoAccessUnit(au, regressed)
	sess.mu.RLock()
	lastLogAt := sess.SwitchVideoGateLastRejectLogAt
	sess.mu.RUnlock()
	if !lastLogAt.Equal(regressed) {
		t.Fatalf("regressed clock did not open fresh log window: %s", lastLogAt)
	}
}

func TestSwitchVideoGateStallSnapshotIsDelayedAndThrottled(t *testing.T) {
	now := time.Unix(900, 0)
	sess := &Session{ID: "gate-stall", VideoAUNormalizeEnabled: true, SwitchGeneration: 9}
	sess.StartSwitchVideoGate(9, now, "switch")
	summary := VideoRecoverySummary{Packets: 300, Gaps: 4, Missing: 9, OutOfOrder: 3, ReorderTimedOut: 2}

	if _, ok := sess.ObserveSwitchVideoGateStall(now.Add(time.Second), summary); ok {
		t.Fatal("stall reported before two-second threshold")
	}
	first, ok := sess.ObserveSwitchVideoGateStall(now.Add(2*time.Second), summary)
	if !ok || first.Generation != 9 || first.Summary.Missing != 9 || first.Elapsed != 2*time.Second {
		t.Fatalf("unexpected first stall snapshot: %+v ok=%v", first, ok)
	}
	if _, ok := sess.ObserveSwitchVideoGateStall(now.Add(3*time.Second), summary); ok {
		t.Fatal("stall report was not throttled")
	}
	if _, ok := sess.ObserveSwitchVideoGateStall(now.Add(4*time.Second), summary); !ok {
		t.Fatal("expected later stall report")
	}
}

func TestSwitchVideoGateBlocksReleaseDuringBlackoutMinimum(t *testing.T) {
	start := time.Now()
	sess := &Session{
		VideoAUNormalizeEnabled:    true,
		SwitchGeneration:           4,
		SwitchVideoBlackoutEnabled: true,
		SwitchVideoTransitionMode:  config.SIPSwitchVideoTransitionBlackout,
	}
	sess.StartSwitchVideoTransitionHold(config.SIPSwitchVideoTransitionBlackout, 300*time.Millisecond, 1200*time.Millisecond, "switch")
	if !sess.StartSwitchVideoGate(4, start, "agent-switch") {
		t.Fatal("gate did not start")
	}

	ready := gateTestAU(4, true, true, 1)
	blocked := sess.EvaluateSwitchVideoAccessUnit(ready, start.Add(100*time.Millisecond))
	if blocked.Emit || blocked.Reason != "blackout-hold" {
		t.Fatalf("expected blackout hold before minimum elapsed, got %+v", blocked)
	}

	released := sess.EvaluateSwitchVideoAccessUnit(ready, start.Add(400*time.Millisecond))
	if !released.Emit || released.Reason != "complete-idr-reserved" {
		t.Fatalf("expected release after blackout minimum, got %+v", released)
	}
}

func TestSwitchVideoGateCommitStopsTransitionHold(t *testing.T) {
	start := time.Now()
	sess := &Session{
		ID:                         "gate-hold-stop",
		VideoAUNormalizeEnabled:    true,
		SwitchGeneration:           3,
		SwitchVideoBlackoutEnabled: true,
		SwitchVideoTransitionMode:  config.SIPSwitchVideoTransitionBlackout,
	}
	sess.StartSwitchVideoTransitionHold(config.SIPSwitchVideoTransitionBlackout, 300*time.Millisecond, 1200*time.Millisecond, "switch")
	sess.StartSwitchVideoGate(3, start, "switch")
	reserved := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(3, true, true, 1), start.Add(400*time.Millisecond))
	if !reserved.Emit {
		t.Fatalf("expected reservation, got %+v", reserved)
	}
	if !sess.CommitSwitchVideoGateRelease(3, reserved.Reservation, start.Add(500*time.Millisecond)) {
		t.Fatal("commit failed")
	}
	sess.mu.RLock()
	holdActive := isSwitchVideoTransitionActiveLocked(sess)
	sess.mu.RUnlock()
	if holdActive {
		t.Fatal("expected transition hold cleared on gate release")
	}
}

func TestSwitchVideoGateHoldsUndersizedIDRUntilFullGOP(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 3}
	sess.StartSwitchVideoGate(3, start, "switch")

	tiny := gateTestAUWithPackets(3, true, true, 99, 5)
	held := sess.EvaluateSwitchVideoAccessUnit(tiny, start.Add(75*time.Millisecond))
	if held.Emit || held.Reason != "undersized-idr" {
		t.Fatalf("tiny IDR should be held, got %+v", held)
	}
	if !sess.IsSwitchVideoGateActive() {
		t.Fatal("undersized IDR released the gate")
	}

	pframe := sess.EvaluateSwitchVideoAccessUnit(gateTestAUWithPackets(3, false, true, 99, 4), start.Add(200*time.Millisecond))
	if pframe.Emit || pframe.Reason != "non-idr" {
		t.Fatalf("P-frame passed while holding: %+v", pframe)
	}

	full := gateTestAUWithPackets(3, true, true, 99, MinSwitchVideoGateIDRPackets)
	reserved := sess.EvaluateSwitchVideoAccessUnit(full, start.Add(400*time.Millisecond))
	if !reserved.Emit || reserved.Reason != "complete-idr-reserved" || reserved.Reservation == 0 {
		t.Fatalf("full IDR did not reserve: %+v", reserved)
	}
}

func TestSwitchVideoGateHolds22PacketPLIFlushIDR(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 3}
	sess.StartSwitchVideoGate(3, start, "switch")

	// CAoRE65fuUZG: 800ms PLI produced a 22-packet IDR that blacked n1669.
	flush := gateTestAUWithPackets(3, true, true, 99, 22)
	held := sess.EvaluateSwitchVideoAccessUnit(flush, start.Add(882*time.Millisecond))
	if held.Emit || held.Reason != "undersized-idr" {
		t.Fatalf("22-packet PLI flush should be held, got %+v", held)
	}

	next := gateTestAUWithPackets(3, true, true, 99, 27)
	reserved := sess.EvaluateSwitchVideoAccessUnit(next, start.Add(1900*time.Millisecond))
	if !reserved.Emit || reserved.Reason != "complete-idr-reserved" {
		t.Fatalf("27-packet GOP should reserve: %+v", reserved)
	}
}

func TestSwitchVideoGateAcceptsUndersizedIDRAfterStall(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 3}
	sess.StartSwitchVideoGate(3, start, "switch")

	tiny := gateTestAUWithPackets(3, true, true, 99, 5)
	held := sess.EvaluateSwitchVideoAccessUnit(tiny, start.Add(time.Second))
	if held.Emit || held.Reason != "undersized-idr" {
		t.Fatalf("tiny IDR before stall should be held: %+v", held)
	}

	late := sess.EvaluateSwitchVideoAccessUnit(tiny, start.Add(switchVideoGateStallThreshold))
	if !late.Emit || late.Reason != "complete-idr-reserved" {
		t.Fatalf("tiny IDR after stall did not reserve: %+v", late)
	}
}

func TestSwitchVideoGateSchedulesRetryPLIOnStart(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 3, LastSipPLISent: now}
	sess.StartSwitchVideoGate(3, now, "switch")
	if !sess.SwitchVideoUndersizedIDRPLIScheduled || sess.SwitchVideoUndersizedIDRPLIGeneration != 3 {
		t.Fatalf("expected gate start to schedule retry PLI, scheduled=%v gen=%d",
			sess.SwitchVideoUndersizedIDRPLIScheduled, sess.SwitchVideoUndersizedIDRPLIGeneration)
	}
	if delay, ok := sess.scheduleUndersizedIDRPLI(3, now.Add(75*time.Millisecond)); ok {
		t.Fatalf("second schedule after gate start, delay=%s", delay)
	}

	if !sess.StopSwitchVideoGate(3, now.Add(time.Second), "test") {
		t.Fatal("expected gate stop")
	}
	if sess.SwitchVideoUndersizedIDRPLIScheduled || sess.SwitchVideoUndersizedIDRPLIGeneration != 0 {
		t.Fatalf("expected schedule cleared with gate, scheduled=%v gen=%d",
			sess.SwitchVideoUndersizedIDRPLIScheduled, sess.SwitchVideoUndersizedIDRPLIGeneration)
	}
}

func TestSwitchVideoGateDoesNotScheduleUndersizedIDRPLIWhenInactive(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 3}
	if _, ok := sess.scheduleUndersizedIDRPLI(3, now); ok {
		t.Fatal("inactive gate scheduled undersized IDR PLI")
	}
}

func gateTestAU(generation int, idr, parameterSetsReady bool, ssrc uint32) NormalizedH264AccessUnit {
	return gateTestAUWithPackets(generation, idr, parameterSetsReady, ssrc, MinSwitchVideoGateIDRPackets)
}

func TestSwitchVideoGateHoldsIDRWhileSwitchRenegotiationPending(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 3}
	sess.StartSwitchVideoGate(3, start, "switch")
	if !sess.TryClaimSwitchVideoRenegotiation(3) {
		t.Fatal("expected renegotiation claim")
	}

	full := gateTestAUWithPackets(3, true, true, 99, MinSwitchVideoGateIDRPackets)
	held := sess.EvaluateSwitchVideoAccessUnit(full, start.Add(400*time.Millisecond))
	if held.Emit || held.Reason != "renegotiate-pending" {
		t.Fatalf("expected renegotiate-pending hold, got %+v", held)
	}

	started, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		Source: MidCallRenegotiationSourceSwitchMessage,
		Method: "WS",
	})
	if !ok {
		t.Fatal("expected pending switch renegotiation")
	}
	if !sess.CompleteMidCallRenegotiation(started.ID, "v=0") {
		t.Fatal("expected complete")
	}

	reserved := sess.EvaluateSwitchVideoAccessUnit(full, start.Add(500*time.Millisecond))
	if !reserved.Emit || reserved.Reason != "complete-idr-reserved" {
		t.Fatalf("expected IDR after client answer, got %+v", reserved)
	}
}

func TestSwitchVideoGateDoesNotHoldForSIPReinvite(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 3}
	sess.StartSwitchVideoGate(3, start, "switch")
	if _, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		Source: "sip_reinvite",
		Method: "INVITE",
	}); !ok {
		t.Fatal("expected SIP re-INVITE pending")
	}

	full := gateTestAUWithPackets(3, true, true, 99, MinSwitchVideoGateIDRPackets)
	reserved := sess.EvaluateSwitchVideoAccessUnit(full, start.Add(400*time.Millisecond))
	if !reserved.Emit || reserved.Reason != "complete-idr-reserved" {
		t.Fatalf("SIP re-INVITE must not block @switch IDR, got %+v", reserved)
	}
}

func gateTestAUWithPackets(generation int, idr, parameterSetsReady bool, ssrc uint32, n int) NormalizedH264AccessUnit {
	packets := make([]*rtp.Packet, n)
	for i := range packets {
		packets[i] = &rtp.Packet{Header: rtp.Header{SSRC: ssrc}}
	}
	return NormalizedH264AccessUnit{
		Packets:            packets,
		IsIDR:              idr,
		ParameterSetsReady: parameterSetsReady,
		Generation:         generation,
	}
}
