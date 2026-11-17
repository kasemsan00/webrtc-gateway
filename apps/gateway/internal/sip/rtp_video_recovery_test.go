package sip

import (
	"testing"
	"time"
)

func TestSipVideoShouldRecoverGapIgnoresReorderedHoles(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if sipVideoShouldRecoverGap(1, time.Time{}, now) {
		t.Fatal("single unrecovered packet must not request a keyframe")
	}
	if sipVideoShouldRecoverGap(sipVideoBurstGapTrigger-1, time.Time{}, now) {
		t.Fatal("sub-threshold skip must not request a keyframe")
	}
	if !sipVideoShouldRecoverGap(sipVideoBurstGapTrigger, time.Time{}, now) {
		t.Fatal("first unrecovered burst should request a keyframe")
	}
	if sipVideoShouldRecoverGap(sipVideoBurstGapTrigger, now, now.Add(sipVideoGapRecoveryMinInterval-time.Millisecond)) {
		t.Fatal("throttled burst must not request another keyframe")
	}
	if !sipVideoShouldRecoverGap(11, now, now.Add(sipVideoGapRecoveryMinInterval)) {
		t.Fatal("burst after throttle interval should request a keyframe")
	}
}

func TestSipVideoTimestampJumpedDetectsPreAnswerToEndpoint(t *testing.T) {
	if sipVideoTimestampJumped(false, 0, 2700) {
		t.Fatal("first timestamp is not a jump")
	}
	if sipVideoTimestampJumped(true, 2700, 2700+3000) {
		t.Fatal("normal 30fps step is not a jump")
	}
	if !sipVideoTimestampJumped(true, 2700, 1_000_188_990) {
		t.Fatal("pre-answer still to endpoint clock should be a jump")
	}
}

// A backward sequence step is only a restart when its timestamp is on another
// timeline. A late packet a frame behind must stay "out of order".
func TestSipVideoTimestampFar(t *testing.T) {
	cases := []struct {
		prev, next uint32
		far        bool
	}{
		{1_000_000_000, 1_000_000_000 - 3000, false}, // late packet, previous frame
		{1_000_000_000, 1_000_000_000 + 3000, false}, // next frame
		{1_000_000_000, 2700, true},                  // restart on a lower timeline
		{2700, 1_000_000_000, true},                  // restart on a higher timeline
		{10, ^uint32(0) - 10, false},                 // wrap, 21 ticks apart
	}
	for _, c := range cases {
		if got := sipVideoTimestampFar(c.prev, c.next); got != c.far {
			t.Fatalf("sipVideoTimestampFar(%d, %d) = %v, want %v", c.prev, c.next, got, c.far)
		}
	}
}
