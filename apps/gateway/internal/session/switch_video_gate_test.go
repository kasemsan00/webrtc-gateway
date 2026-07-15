package session

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
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
	if !current.Emit || current.Released || current.Reason != "inactive" {
		t.Fatalf("current inactive decision = %+v", current)
	}
	if !newer.Emit || newer.Released || newer.Reason != "inactive" {
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
	if !reserved.Emit || reserved.Released || reserved.Reason != "complete-idr-reserved" || reserved.Generation != 4 {
		t.Fatalf("reservation decision = %+v", reserved)
	}
	if !sess.IsSwitchVideoGateActive() {
		t.Fatal("reservation cleared gate before commit")
	}

	blocked := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(4, false, true, 99), start.Add(2*time.Second))
	if blocked.Emit || blocked.Reason != "release-in-progress" {
		t.Fatalf("P-frame passed before commit: %+v", blocked)
	}
	if sess.CommitSwitchVideoGateRelease(3, start.Add(3*time.Second)) {
		t.Fatal("wrong generation committed reservation")
	}
	if !sess.IsSwitchVideoGateActive() {
		t.Fatal("wrong commit cleared gate")
	}
	if !sess.CommitSwitchVideoGateRelease(4, start.Add(3*time.Second)) {
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
	if sess.AbortSwitchVideoGateRelease(1, "wrong-generation") {
		t.Fatal("wrong generation aborted reservation")
	}
	if blocked := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(2, true, true, 1), now); blocked.Emit {
		t.Fatalf("wrong abort opened reservation: %+v", blocked)
	}
	if !sess.AbortSwitchVideoGateRelease(2, "write-failed") {
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
			if decision.Emit || decision.Released || decision.Reason != tt.reason || decision.Generation != 10 {
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
	sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, true, true, 1), now)
	sess.CommitSwitchVideoGateRelease(5, now)
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
	sess.EvaluateSwitchVideoAccessUnit(gateTestAU(3, true, true, 1), now)
	sess.CommitSwitchVideoGateRelease(3, now)
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

func gateTestAU(generation int, idr, parameterSetsReady bool, ssrc uint32) NormalizedH264AccessUnit {
	return NormalizedH264AccessUnit{
		Packets:            []*rtp.Packet{{Header: rtp.Header{SSRC: ssrc}}},
		IsIDR:              idr,
		ParameterSetsReady: parameterSetsReady,
		Generation:         generation,
	}
}
