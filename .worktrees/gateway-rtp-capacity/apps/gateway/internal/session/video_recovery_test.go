package session

import (
	"testing"
	"time"
)

func TestAnalyzeSTAPAPayload_InvalidChunkStopsGracefully(t *testing.T) {
	// Declares 16-byte NAL but only 2 bytes remain.
	payload := []byte{0, 16, 0x67, 0x64}
	got := analyzeSTAPAPayload(payload)
	if got == nil {
		t.Fatalf("expected non-nil result")
	}
	if got.containsIDR || got.containsSPS || got.containsPPS {
		t.Fatalf("expected no detected nal types for invalid chunk, got %+v", got)
	}
}

func newBurstTestSession(id string) *Session {
	return &Session{
		ID:                         id,
		VideoRecoveryBurstEnabled:  true,
		VideoRecoveryBurstWindow:   12 * time.Second,
		VideoRecoveryBurstInterval: 800 * time.Millisecond,
		VideoRecoveryBurstStale:    1200 * time.Millisecond,
		VideoRecoveryBurstFIRStale: 2500 * time.Millisecond,
	}
}

func TestVideoRecoveryBurstPolicyLifecycle(t *testing.T) {
	sess := newBurstTestSession("burst-policy")

	baseInterval := 3 * time.Second
	baseStale := 5 * time.Second
	baseFirStale := 10 * time.Second

	interval, stale, firStale, active := sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFirStale)
	if active {
		t.Fatalf("expected burst policy inactive before start")
	}
	if interval != baseInterval || stale != baseStale || firStale != baseFirStale {
		t.Fatalf("expected base policy before burst, got interval=%s stale=%s firStale=%s", interval, stale, firStale)
	}

	sess.StartVideoRecoveryBurst("unit-test")

	interval, stale, firStale, active = sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFirStale)
	if !active {
		t.Fatalf("expected burst policy active after start")
	}
	if interval != 800*time.Millisecond || stale != 1200*time.Millisecond || firStale != 2500*time.Millisecond {
		t.Fatalf("unexpected burst policy: interval=%s stale=%s firStale=%s", interval, stale, firStale)
	}
	if !sess.ShouldUseVideoRTCPFallback() {
		t.Fatalf("expected RTCP fallback active during burst window")
	}

	sess.StopVideoRecoveryBurstIfActive("unit-test-stop")
	interval, stale, firStale, active = sess.GetVideoRecoveryPolicy(baseInterval, baseStale, baseFirStale)
	if active {
		t.Fatalf("expected burst policy inactive after stop")
	}
	if interval != baseInterval || stale != baseStale || firStale != baseFirStale {
		t.Fatalf("expected base policy after stop, got interval=%s stale=%s firStale=%s", interval, stale, firStale)
	}
	if sess.ShouldUseVideoRTCPFallback() {
		t.Fatalf("expected RTCP fallback disabled after burst stop")
	}
}

func TestSendBrowserRecoveryToAsterisk_UsesBothInBurstForWSKeyframe(t *testing.T) {
	sess := newBurstTestSession("burst-ws-request")
	sess.StartVideoRecoveryBurst("unit-test")

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	if action != "both" {
		t.Fatalf("expected action=both during burst ws-request_keyframe, got %s", action)
	}
}

func TestRecordKeyframe_StopsVideoRecoveryBurst(t *testing.T) {
	sess := newBurstTestSession("burst-keyframe")
	sess.StartVideoRecoveryBurst("unit-test")

	sess.RecordKeyframe()

	_, _, _, active := sess.GetVideoRecoveryPolicy(3*time.Second, 5*time.Second, 10*time.Second)
	if active {
		t.Fatalf("expected burst policy to stop after keyframe recovery")
	}
}
