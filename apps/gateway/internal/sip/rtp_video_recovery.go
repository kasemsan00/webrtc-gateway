package sip

import "time"

const (
	sipVideoBurstGapTrigger        = 8
	sipVideoGapRecoveryMinInterval = 1200 * time.Millisecond
	// 10s at the 90 kHz video clock. Pre-answer Asterisk stills use timestamps
	// near 2700; the endpoint's own stream is ~1e9 (S5CDifyVuoul).
	sipVideoTimestampJumpThreshold = uint32(900000)
)

// sipVideoShouldRecoverGap reports whether an unrecovered post-reorder skip
// should request a SIP keyframe. Arrival-order holes that the reorder buffer
// later fills must not call this.
func sipVideoShouldRecoverGap(skipped int, lastRecovery, now time.Time) bool {
	if skipped < sipVideoBurstGapTrigger {
		return false
	}
	return lastRecovery.IsZero() || now.Sub(lastRecovery) >= sipVideoGapRecoveryMinInterval
}

// sipVideoTimestampFar reports a timestamp more than the jump threshold away
// in either direction. Unlike sipVideoTimestampJumped it does not treat a
// slightly older timestamp (a late packet) as a jump.
func sipVideoTimestampFar(prev, next uint32) bool {
	d := next - prev
	return d > sipVideoTimestampJumpThreshold && d < ^uint32(0)-sipVideoTimestampJumpThreshold
}

func sipVideoTimestampJumped(havePrev bool, prev, next uint32) bool {
	if !havePrev {
		return false
	}
	return next-prev > sipVideoTimestampJumpThreshold
}
