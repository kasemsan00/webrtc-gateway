package session

import (
	"testing"
	"time"
)

func TestPostGateRecoverySoftening(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sess := &Session{
		VideoAUNormalizeEnabled:         true,
		SwitchGeneration:                2,
		VideoRecoveryBurstEnabled:       true,
		SwitchVideoRecoveryStableWindow: 750 * time.Millisecond,
		SwitchVideoRTPMinPacketDelta:    30,
	}
	sess.startSwitchVideoRecovery(0, 0, false, 5*time.Second, 750*time.Millisecond)
	sess.mu.Lock()
	sess.applyPostGateRecoverySofteningLocked(now)
	stable := sess.SwitchVideoRecoveryStableWindow
	minPackets := sess.SwitchVideoRTPMinPacketDelta
	sess.mu.Unlock()

	if stable != postGateRecoveryStableWindow {
		t.Fatalf("stable window = %s, want %s", stable, postGateRecoveryStableWindow)
	}
	if minPackets != postGateRTPMinPacketDelta {
		t.Fatalf("min packet delta = %d, want %d", minPackets, postGateRTPMinPacketDelta)
	}
}
