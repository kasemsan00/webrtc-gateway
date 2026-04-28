package session

import (
	"testing"
	"time"
)

func TestSwitchBlackout_HoldsUntilKeyframeThenReleases(t *testing.T) {
	sess := &Session{ID: "switch-blackout-1", SwitchVideoBlackoutEnabled: true}
	blackout := 50 * time.Millisecond
	maxWait := 200 * time.Millisecond

	sess.StartSwitchVideoBlackout(blackout, maxWait, "unit-test")

	now := time.Now()
	if !sess.ShouldHoldSwitchVideoPacket(now, false) {
		t.Fatalf("expected hold during initial blackout")
	}

	time.Sleep(60 * time.Millisecond)
	now = time.Now()
	if !sess.ShouldHoldSwitchVideoPacket(now, false) {
		t.Fatalf("expected hold after blackout until keyframe")
	}

	if sess.ShouldHoldSwitchVideoPacket(time.Now(), true) {
		t.Fatalf("expected release on keyframe")
	}

	if sess.ShouldHoldSwitchVideoPacket(time.Now(), false) {
		t.Fatalf("expected hold disabled after keyframe release")
	}
}

func TestSwitchBlackout_ReleasesOnMaxWaitTimeout(t *testing.T) {
	sess := &Session{ID: "switch-blackout-2", SwitchVideoBlackoutEnabled: true}

	sess.StartSwitchVideoBlackout(30*time.Millisecond, 80*time.Millisecond, "unit-test")

	if !sess.ShouldHoldSwitchVideoPacket(time.Now(), false) {
		t.Fatalf("expected hold active initially")
	}

	time.Sleep(100 * time.Millisecond)
	if sess.ShouldHoldSwitchVideoPacket(time.Now(), false) {
		t.Fatalf("expected hold released after max wait timeout")
	}
}
