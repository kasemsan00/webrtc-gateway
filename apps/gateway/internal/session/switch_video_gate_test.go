package session

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
)

func TestEvaluateSwitchVideoAccessUnitPassesThroughWhenGateInactive(t *testing.T) {
	sess := &Session{}

	decision := sess.EvaluateSwitchVideoAccessUnit(NormalizedH264AccessUnit{Generation: 7}, time.Now())

	if !decision.Emit || decision.Released {
		t.Fatalf("inactive decision = %+v, want emit without release", decision)
	}
}

func TestSwitchVideoGateReleaseConditions(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 4}
	sess.StartSwitchVideoGate(start, "agent-switch")

	tests := []struct {
		name   string
		au     NormalizedH264AccessUnit
		reason string
	}{
		{name: "stale generation", au: gateTestAU(3, true, true, 99), reason: "stale-generation"},
		{name: "non IDR", au: gateTestAU(4, false, true, 99), reason: "non-idr"},
		{name: "IDR without parameter sets", au: gateTestAU(4, true, false, 99), reason: "parameter-sets-not-ready"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := sess.EvaluateSwitchVideoAccessUnit(tt.au, start.Add(time.Duration(i)*time.Second))
			if decision.Emit || decision.Released || decision.Reason != tt.reason || decision.Generation != 4 {
				t.Fatalf("decision = %+v, want blocked reason %q generation 4", decision, tt.reason)
			}
			if !sess.IsSwitchVideoGateActive() {
				t.Fatal("unsafe access unit cleared gate")
			}
		})
	}

	decision := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(4, true, true, 99), start.Add(4*time.Second))
	if !decision.Emit || !decision.Released || decision.Reason != "complete-idr" || decision.Generation != 4 {
		t.Fatalf("release decision = %+v", decision)
	}
	if sess.IsSwitchVideoGateActive() {
		t.Fatal("released gate remained active")
	}
}

func TestSwitchVideoGateFailsClosedAfterThirtySeconds(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 2}
	sess.StartSwitchVideoGate(start, "switch")

	decision := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(2, false, true, 7), start.Add(31*time.Second))
	if decision.Emit || !sess.IsSwitchVideoGateActive() {
		t.Fatalf("stalled unsafe AU failed open: decision=%+v active=%v", decision, sess.IsSwitchVideoGateActive())
	}
}

func TestSwitchVideoGateGenerationIsAuthoritativeWithSameSSRC(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 10}
	sess.StartSwitchVideoGate(start, "switch")

	stale := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(9, true, true, 4242), start)
	current := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(10, true, true, 4242), start.Add(time.Millisecond))
	if stale.Emit || stale.Reason != "stale-generation" {
		t.Fatalf("same-SSRC stale AU decision = %+v", stale)
	}
	if !current.Emit || !current.Released {
		t.Fatalf("same-SSRC current AU decision = %+v", current)
	}
}

func TestNewerSwitchVideoGateSupersedesEarlierGeneration(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 1}
	sess.StartSwitchVideoGate(start, "first")
	sess.SwitchGeneration = 2
	sess.PLISent = 8
	sess.StartSwitchVideoGate(start.Add(time.Second), "second")

	if decision := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(1, true, true, 1), start.Add(2*time.Second)); decision.Emit {
		t.Fatalf("superseded generation emitted: %+v", decision)
	}
	sess.mu.RLock()
	generation, baseline, reason, startedAt := sess.SwitchVideoGateGeneration, sess.SwitchVideoGatePLIBaseline, sess.SwitchVideoGateStartReason, sess.SwitchVideoGateStartedAt
	sess.mu.RUnlock()
	if generation != 2 || baseline != 8 || reason != "second" || !startedAt.Equal(start.Add(time.Second)) {
		t.Fatalf("new gate snapshot = generation %d baseline %d reason %q startedAt %s", generation, baseline, reason, startedAt)
	}
}

func TestSwitchVideoGateReleaseAtomicallyClearsGate(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 7}
	sess.StartSwitchVideoGate(now, "switch")
	au := gateTestAU(7, true, true, 1)

	var released atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sess.EvaluateSwitchVideoAccessUnit(au, now).Released {
				released.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := released.Load(); got != 1 {
		t.Fatalf("released decisions = %d, want exactly one", got)
	}
}

func TestSwitchVideoGateStartRequiresNormalizationAndStopPassesThrough(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{SwitchGeneration: 3}
	sess.StartSwitchVideoGate(now, "disabled")
	if sess.IsSwitchVideoGateActive() {
		t.Fatal("gate started while AU normalization disabled")
	}

	sess.VideoAUNormalizeEnabled = true
	sess.StartSwitchVideoGate(now, "enabled")
	sess.StopSwitchVideoGate("hangup")
	if sess.IsSwitchVideoGateActive() {
		t.Fatal("explicit stop left gate active")
	}
	if decision := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(1, false, false, 1), now); !decision.Emit || decision.Released {
		t.Fatalf("stopped gate did not pass through: %+v", decision)
	}
}

func TestSwitchVideoGateDoesNotMutateAudioState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{
		VideoAUNormalizeEnabled: true,
		SwitchGeneration:        5,
		AudioSSRC:               11,
		RemoteAudioSSRC:         22,
		AudioSeq:                33,
	}
	sess.StartSwitchVideoGate(now, "switch")
	sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, false, true, 11), now)
	sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, true, true, 11), now.Add(time.Second))
	if sess.AudioSSRC != 11 || sess.RemoteAudioSSRC != 22 || sess.AudioSeq != 33 {
		t.Fatalf("audio state changed: local=%d remote=%d seq=%d", sess.AudioSSRC, sess.RemoteAudioSSRC, sess.AudioSeq)
	}
}

func TestSwitchVideoGateThrottlesRepeatedRejectionReason(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 6}
	sess.StartSwitchVideoGate(now, "switch")
	au := gateTestAU(6, false, true, 1)

	sess.EvaluateSwitchVideoAccessUnit(au, now)
	sess.mu.RLock()
	firstLogAt := sess.SwitchVideoGateLastRejectLogAt
	sess.mu.RUnlock()
	sess.EvaluateSwitchVideoAccessUnit(au, now.Add(500*time.Millisecond))
	sess.mu.RLock()
	throttledLogAt := sess.SwitchVideoGateLastRejectLogAt
	rejected := sess.SwitchVideoGateRejectedCount
	sess.mu.RUnlock()
	if !throttledLogAt.Equal(firstLogAt) || rejected != 2 {
		t.Fatalf("throttled state: first=%s second=%s rejected=%d", firstLogAt, throttledLogAt, rejected)
	}

	sess.EvaluateSwitchVideoAccessUnit(au, now.Add(time.Second))
	sess.mu.RLock()
	resumedLogAt := sess.SwitchVideoGateLastRejectLogAt
	sess.mu.RUnlock()
	if !resumedLogAt.Equal(now.Add(time.Second)) {
		t.Fatalf("logging did not resume after throttle window: %s", resumedLogAt)
	}
}

func TestSwitchVideoGateThrottlesAlternatingRejectionReasonsGlobally(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{VideoAUNormalizeEnabled: true, SwitchGeneration: 6}
	sess.StartSwitchVideoGate(now, "switch")

	sess.EvaluateSwitchVideoAccessUnit(gateTestAU(5, true, true, 1), now)
	sess.mu.RLock()
	firstLogAt := sess.SwitchVideoGateLastRejectLogAt
	sess.mu.RUnlock()

	for i := 1; i <= 8; i++ {
		var au NormalizedH264AccessUnit
		if i%2 == 0 {
			au = gateTestAU(5, true, true, 1)
		} else {
			au = gateTestAU(6, false, true, 1)
		}
		sess.EvaluateSwitchVideoAccessUnit(au, now.Add(time.Duration(i)*100*time.Millisecond))
	}

	sess.mu.RLock()
	throttledLogAt := sess.SwitchVideoGateLastRejectLogAt
	rejected := sess.SwitchVideoGateRejectedCount
	sess.mu.RUnlock()
	if !throttledLogAt.Equal(firstLogAt) || rejected != 9 {
		t.Fatalf("alternating rejection throttle: first=%s last=%s rejected=%d", firstLogAt, throttledLogAt, rejected)
	}

	sess.EvaluateSwitchVideoAccessUnit(gateTestAU(6, false, true, 1), now.Add(time.Second))
	sess.mu.RLock()
	resumedLogAt := sess.SwitchVideoGateLastRejectLogAt
	sess.mu.RUnlock()
	if !resumedLogAt.Equal(now.Add(time.Second)) {
		t.Fatalf("global logging did not resume after throttle window: %s", resumedLogAt)
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
