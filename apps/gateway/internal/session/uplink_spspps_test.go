package session

import "testing"

func TestUplinkKeyframeParameterSetsOncePerAccessUnit(t *testing.T) {
	sess := &Session{ID: "spspps"}

	if got := sess.uplinkKeyframeParameterSetCopiesLocked(90000); got != 2 {
		t.Fatalf("first access unit copies=%d, want 2", got)
	}
	if got := sess.uplinkKeyframeParameterSetCopiesLocked(90000); got != 0 {
		t.Fatalf("same timestamp copies=%d, want 0", got)
	}
	if got := sess.uplinkKeyframeParameterSetCopiesLocked(180000); got != 1 {
		t.Fatalf("later access unit copies=%d, want 1", got)
	}

	sess.ResetMediaState()
	if got := sess.uplinkKeyframeParameterSetCopiesLocked(90000); got != 2 {
		t.Fatalf("after media reset copies=%d, want 2", got)
	}
}

func TestHeldParameterSetDoesNotCountAsInBand(t *testing.T) {
	sess := &Session{ID: "held-sps", mode0NeedsIDR: true}
	if !sess.holdUplinkForMode0Locked(false) {
		t.Fatal("SPS during mode-0 recovery should be held")
	}
	sess.recordUplinkParameterSetIfForwarded(true, 90000, true, false)
	sess.recordUplinkParameterSetIfForwarded(true, 90000, false, true)
	if got := sess.uplinkKeyframeParameterSetCopiesLocked(90000); got != 2 {
		t.Fatalf("IDR copies=%d, want 2 after a dropped SPS/PPS", got)
	}
}

func TestUplinkSkipsInjectionWhenAccessUnitAlreadyHasParameterSets(t *testing.T) {
	sess := &Session{ID: "spspps-inband"}
	sess.noteUplinkInBandParameterSetLocked(90000, true, false)
	sess.noteUplinkInBandParameterSetLocked(90000, false, true)
	if got := sess.uplinkKeyframeParameterSetCopiesLocked(90000); got != 0 {
		t.Fatalf("in-band access unit copies=%d, want 0", got)
	}
	if got := sess.uplinkKeyframeParameterSetCopiesLocked(90000); got != 0 {
		t.Fatalf("same in-band timestamp copies=%d, want 0", got)
	}
	if got := sess.uplinkKeyframeParameterSetCopiesLocked(180000); got != 1 {
		t.Fatalf("later access unit without in-band sets copies=%d, want 1", got)
	}
}
