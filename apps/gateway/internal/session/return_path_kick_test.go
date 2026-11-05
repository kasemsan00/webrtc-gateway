package session

import (
	"testing"
	"time"
)

// fakeReturnPathClock drives runReturnPathUplinkKicks. onSleep runs before
// the clock advances, with the fake "now" at which the previous tick ran.
type fakeReturnPathClock struct {
	t       time.Time
	sess    *Session
	kicks   []time.Duration
	start   time.Time
	seen    int
	onSleep func(now time.Time, kicks int)
}

func (c *fakeReturnPathClock) clock() returnPathKickClock {
	return returnPathKickClock{
		now: func() time.Time { return c.t },
		sleep: func(d time.Duration) {
			c.sess.mu.RLock()
			n := c.sess.returnRecoveryUplinkKicks
			c.sess.mu.RUnlock()
			for ; c.seen < n; c.seen++ {
				c.kicks = append(c.kicks, c.t.Sub(c.start))
			}
			if c.onSleep != nil {
				c.onSleep(c.t, n)
			}
			c.t = c.t.Add(d)
		},
	}
}

// KVt9sOuRsPuy: SIP 200 OK, the first uplink IDR is forwarded in the same
// millisecond as the remote video SSRC learn, and Echo never returns it.
func newEchoLearnSession(t0 time.Time) *Session {
	sess := &Session{ID: "echo-learn", RemoteVideoSSRC: 99, State: StateActive}
	sess.remoteVideoSSRCLearnedAt = t0
	sess.LastUplinkKeyframe = t0
	sess.uplinkIDRCompleteAt = t0
	return sess
}

func TestReturnPathKickFiresAt500msAndStopsOnReturnIDR(t *testing.T) {
	t0 := time.Unix(1_900_000_000, 0)
	sess := newEchoLearnSession(t0)
	c := &fakeReturnPathClock{t: t0, start: t0, sess: sess}
	c.onSleep = func(now time.Time, kicks int) {
		// The phone answers the second kick; Echo returns its IDR 55ms later.
		if kicks == 2 && sess.LastKeyframe.IsZero() {
			sess.LastUplinkKeyframe = now.Add(50 * time.Millisecond)
			sess.LastKeyframe = now.Add(105 * time.Millisecond)
		}
	}
	stop := sess.runReturnPathUplinkKicks(sess.MediaEpoch, c.clock())
	if stop != "return-keyframe" {
		t.Fatalf("stop=%q, want return-keyframe", stop)
	}
	want := []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond}
	if len(c.kicks) != len(want) {
		t.Fatalf("kicks at %v, want %v", c.kicks, want)
	}
	for i := range want {
		if c.kicks[i] != want[i] {
			t.Fatalf("kicks at %v, want %v", c.kicks, want)
		}
	}
}

func TestReturnPathKickRespectsCapAndSpacing(t *testing.T) {
	t0 := time.Unix(1_900_000_000, 0)
	sess := newEchoLearnSession(t0)
	c := &fakeReturnPathClock{t: t0, start: t0, sess: sess}
	lastKicks := 0
	c.onSleep = func(now time.Time, kicks int) {
		// The phone sends an IDR 100ms after every kick; Echo never returns it.
		if kicks != lastKicks {
			lastKicks = kicks
			sess.LastUplinkKeyframe = now.Add(100 * time.Millisecond)
		}
	}
	stop := sess.runReturnPathUplinkKicks(sess.MediaEpoch, c.clock())
	if stop != "max" {
		t.Fatalf("stop=%q, want max", stop)
	}
	if len(c.kicks) != returnRecoveryUplinkKickMax {
		t.Fatalf("kicks=%d (%v), want cap %d", len(c.kicks), c.kicks, returnRecoveryUplinkKickMax)
	}
	if c.kicks[0] != returnPathKickFirstDelay {
		t.Fatalf("first kick at %v, want %v", c.kicks[0], returnPathKickFirstDelay)
	}
	for i := 1; i < len(c.kicks); i++ {
		if gap := c.kicks[i] - c.kicks[i-1]; gap < returnPathKickInterval || gap > returnPathKickInterval+returnPathKickPoll {
			t.Fatalf("kick gap %v at %d (%v), want ~%v", gap, i, c.kicks, returnPathKickInterval)
		}
	}
}

func TestReturnPathKickWaitsForAnswerAndInFlightIDR(t *testing.T) {
	t0 := time.Unix(1_900_000_000, 0)
	sess := newEchoLearnSession(t0)
	sess.State = StateRinging
	sess.uplinkIDRInFlight = true
	c := &fakeReturnPathClock{t: t0, start: t0, sess: sess}
	c.onSleep = func(now time.Time, kicks int) {
		if now.Sub(t0) >= 1500*time.Millisecond {
			sess.mu.Lock()
			sess.State = StateActive
			sess.mu.Unlock()
		}
		if now.Sub(t0) >= 2200*time.Millisecond {
			sess.uplinkIDRInFlight = false
		}
		if kicks == 1 {
			sess.LastKeyframe = now
		}
	}
	if stop := sess.runReturnPathUplinkKicks(sess.MediaEpoch, c.clock()); stop != "return-keyframe" {
		t.Fatalf("stop=%q", stop)
	}
	if len(c.kicks) != 1 || c.kicks[0] < 2200*time.Millisecond {
		t.Fatalf("kicks at %v, want one after answer and in-flight IDR end (>=2.2s)", c.kicks)
	}
}

func TestReturnPathKickStopsOnEndAndNewCall(t *testing.T) {
	t0 := time.Unix(1_900_000_000, 0)
	sess := newEchoLearnSession(t0)
	sess.State = StateEnded
	c := &fakeReturnPathClock{t: t0, start: t0, sess: sess}
	if stop := sess.runReturnPathUplinkKicks(sess.MediaEpoch, c.clock()); stop != "ended" || len(c.kicks) != 0 {
		t.Fatalf("ended: stop=%q kicks=%v", stop, c.kicks)
	}
	sess = newEchoLearnSession(t0)
	c = &fakeReturnPathClock{t: t0, start: t0, sess: sess}
	if stop := sess.runReturnPathUplinkKicks(sess.MediaEpoch+1, c.clock()); stop != "new-call" || len(c.kicks) != 0 {
		t.Fatalf("new call: stop=%q kicks=%v", stop, c.kicks)
	}
}

// LastKeyframe survives ResetMediaState: a keyframe from the previous call
// must not count as this call's return keyframe.
func TestReturnPathKickIgnoresPreviousCallKeyframe(t *testing.T) {
	t0 := time.Unix(1_900_000_000, 0)
	sess := newEchoLearnSession(t0)
	sess.LastKeyframe = t0.Add(-30 * time.Second)
	c := &fakeReturnPathClock{t: t0, start: t0, sess: sess}
	c.onSleep = func(now time.Time, kicks int) {
		if kicks == 1 {
			sess.LastKeyframe = now
		}
	}
	if stop := sess.runReturnPathUplinkKicks(sess.MediaEpoch, c.clock()); stop != "return-keyframe" {
		t.Fatalf("stop=%q", stop)
	}
	if len(c.kicks) != 1 || c.kicks[0] != returnPathKickFirstDelay {
		t.Fatalf("kicks at %v, want one at %v", c.kicks, returnPathKickFirstDelay)
	}
}

func TestStartReturnPathUplinkKicksArmsOncePerCall(t *testing.T) {
	sess := &Session{ID: "arm", State: StateEnded}
	if !sess.StartReturnPathUplinkKicks() {
		t.Fatal("first learn did not arm the schedule")
	}
	if sess.StartReturnPathUplinkKicks() {
		t.Fatal("second learn in the same call re-armed the schedule")
	}
	sess.ResetMediaState()
	if !sess.StartReturnPathUplinkKicks() {
		t.Fatal("new call did not re-arm the schedule")
	}
}
