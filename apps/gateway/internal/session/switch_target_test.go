package session

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPrepareSwitchVideoTargetIgnoresDuplicateInsideDebounce(t *testing.T) {
	sess := newBurstTestSession("switch-target-dup")
	now := time.Now()
	sess.RemoteVideoSSRC = 1111
	sess.SIPVideoRTPSource = "203.0.113.10:4000"

	first, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	if first.Ignore {
		t.Fatalf("expected first switch to be honored")
	}
	if first.Generation != 1 {
		t.Fatalf("expected first generation 1, got %d", first.Generation)
	}

	dup, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now.Add(30*time.Second), time.Minute, true)
	if !dup.Ignore {
		t.Fatalf("expected duplicate switch to be ignored")
	}
	if dup.Generation != first.Generation {
		t.Fatalf("expected ignored duplicate to keep generation %d, got %d", first.Generation, dup.Generation)
	}
	if dup.DuplicateCount != 1 {
		t.Fatalf("expected duplicate count 1, got %d", dup.DuplicateCount)
	}
}

func TestPrepareSwitchVideoTargetHonorsAfterDebounce(t *testing.T) {
	sess := newBurstTestSession("switch-target-expired")
	now := time.Now()
	sess.RemoteVideoSSRC = 1111
	sess.SIPVideoRTPSource = "203.0.113.10:4000"

	first, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	next, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now.Add(61*time.Second), time.Minute, true)

	if next.Ignore {
		t.Fatalf("expected switch after debounce window to be honored")
	}
	if next.Generation != first.Generation+1 {
		t.Fatalf("expected generation to advance, got first=%d next=%d", first.Generation, next.Generation)
	}
	if next.Reason != "debounce-window-expired" {
		t.Fatalf("expected debounce-window-expired reason, got %q", next.Reason)
	}
}

func TestPrepareSwitchVideoTargetHonorsMediaGenerationChange(t *testing.T) {
	sess := newBurstTestSession("switch-target-media-generation")
	now := time.Now()
	sess.RemoteVideoSSRC = 1111
	sess.SIPVideoRTPSource = "203.0.113.10:4000"

	first, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	sess.RemoteVideoSSRC = 2222
	sess.SIPVideoRTPSource = "203.0.113.10:4010"
	next, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now.Add(30*time.Second), time.Minute, true)

	if next.Ignore {
		t.Fatalf("expected same target with changed media generation to be honored")
	}
	if next.Generation != first.Generation+1 {
		t.Fatalf("expected generation to advance, got first=%d next=%d", first.Generation, next.Generation)
	}
	if next.Reason != "media-generation-changed" {
		t.Fatalf("expected media-generation-changed reason, got %q", next.Reason)
	}
}

func TestPrepareAndActivateSwitchVideoTargetPublishesGateAndClearsSIPCacheAtomically(t *testing.T) {
	sess := newBurstTestSession("switch-target-atomic")
	sess.VideoAUNormalizeEnabled = true
	sess.CacheSIPSPS([]byte{0x67, 0x64, 0x00, 0x28})
	sess.CacheSIPPPS([]byte{0x68, 0xee, 0x3c, 0x80})

	decision, activation := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", time.Now(), time.Minute, true)

	if decision.Ignore || decision.Generation != 1 {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if !activation.Active || activation.Outcome != SwitchVideoGateActivationActive {
		t.Fatalf("expected active gate result, got %+v", activation)
	}
	if !sess.IsSwitchVideoAuthority(decision.Generation, decision.MediaEpoch) {
		t.Fatalf("accepted token is not authoritative: %+v", decision)
	}
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	if sess.SwitchGeneration != decision.Generation || !sess.SwitchVideoGateActive ||
		sess.SwitchVideoGateGeneration != decision.Generation {
		t.Fatalf("generation published without matching active gate: switch=%d active=%v gate=%d",
			sess.SwitchGeneration, sess.SwitchVideoGateActive, sess.SwitchVideoGateGeneration)
	}
	if len(sess.SIPCachedSPS) != 0 || len(sess.SIPCachedPPS) != 0 {
		t.Fatalf("accepted switch retained SIP cache: SPS=%x PPS=%x", sess.SIPCachedSPS, sess.SIPCachedPPS)
	}
}

func TestPrepareAndActivateSwitchVideoTargetHasNoPublishedGenerationWithoutGate(t *testing.T) {
	sess := newBurstTestSession("switch-target-publication")
	sess.VideoAUNormalizeEnabled = true
	stop := make(chan struct{})
	start := make(chan struct{})
	errCh := make(chan string, 1)
	var readers sync.WaitGroup
	var ready sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		ready.Add(1)
		go func() {
			defer readers.Done()
			ready.Done()
			<-start
			for {
				select {
				case <-stop:
					return
				default:
				}
				sess.mu.RLock()
				generation := sess.SwitchGeneration
				active := sess.SwitchVideoGateActive
				gateGeneration := sess.SwitchVideoGateGeneration
				sess.mu.RUnlock()
				if generation > 0 && (!active || gateGeneration != generation) {
					select {
					case errCh <- fmt.Sprintf("switch=%d active=%v gate=%d", generation, active, gateGeneration):
					default:
					}
					return
				}
			}
		}()
	}
	ready.Wait()
	close(start)

	for generation := 1; generation <= 100; generation++ {
		decision, activation := sess.PrepareAndActivateSwitchVideoTarget("14131", fmt.Sprintf("%05d", generation), time.Now(), time.Minute, true)
		if decision.Generation != generation || !activation.Active {
			t.Fatalf("transition %d failed: decision=%+v activation=%+v", generation, decision, activation)
		}
	}
	close(stop)
	readers.Wait()
	select {
	case observation := <-errCh:
		t.Fatalf("observed non-atomic publication: %s", observation)
	default:
	}
}

func TestPrepareSwitchVideoTargetRemapsWebRTCEgressSSRC(t *testing.T) {
	sess := newBurstTestSession("switch-egress-remap")
	now := time.Now()
	sess.RemoteVideoSSRC = 1111
	sess.SIPVideoRTPSource = "203.0.113.10:4000"
	sess.WebRTCVideoEgressSSRC = 2222

	first, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	if first.Ignore {
		t.Fatal("expected first switch honored")
	}
	if sess.GetWebRTCVideoEgressSSRC() == 0 || sess.GetWebRTCVideoEgressSSRC() == 2222 {
		t.Fatalf("expected remapped egress SSRC, got %d", sess.GetWebRTCVideoEgressSSRC())
	}
	if sess.RemoteVideoSSRC != 1111 {
		t.Fatalf("SIP RemoteVideoSSRC must stay 1111, got %d", sess.RemoteVideoSSRC)
	}
	afterFirst := sess.GetWebRTCVideoEgressSSRC()

	dup, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now.Add(30*time.Second), time.Minute, true)
	if !dup.Ignore {
		t.Fatal("expected duplicate ignored")
	}
	if sess.GetWebRTCVideoEgressSSRC() != afterFirst {
		t.Fatalf("duplicate must not remap egress SSRC, before=%d after=%d", afterFirst, sess.GetWebRTCVideoEgressSSRC())
	}

	second, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now.Add(61*time.Second), time.Minute, true)
	if second.Ignore || second.Reason != "debounce-window-expired" {
		t.Fatalf("expected second switch after debounce honored, got %+v", second)
	}
	afterSecond := sess.GetWebRTCVideoEgressSSRC()
	if afterSecond == 0 || afterSecond == afterFirst {
		t.Fatalf("second accepted switch did not remap egress SSRC, first=%d second=%d", afterFirst, afterSecond)
	}
	if sess.RemoteVideoSSRC != 1111 {
		t.Fatalf("SIP RemoteVideoSSRC must stay 1111, got %d", sess.RemoteVideoSSRC)
	}
}

func TestPrepareAndActivateSwitchVideoTargetNormalizationDisabledUsesLegacyOutcome(t *testing.T) {
	sess := newBurstTestSession("switch-target-disabled")
	sess.VideoAUNormalizeEnabled = false

	decision, activation := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", time.Now(), time.Minute, true)

	if decision.Ignore || activation.Active || activation.Outcome != SwitchVideoGateActivationDisabled {
		t.Fatalf("expected accepted legacy outcome, decision=%+v activation=%+v", decision, activation)
	}
	if !sess.IsSwitchVideoAuthority(decision.Generation, decision.MediaEpoch) {
		t.Fatalf("disabled-normalization switch token should remain authoritative")
	}
}
