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
