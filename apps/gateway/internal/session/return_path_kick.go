package session

import (
	"fmt"
	"time"
)

const (
	// returnPathKickFirstDelay is how long after the first remote video SSRC
	// learn the gateway waits for a return keyframe before asking the browser
	// encoder for a new IDR. A loopback far end (Asterisk Echo on 9999)
	// reflects only an uplink IDR sent after it started echoing; the IDR that
	// opened the call often lands in the same millisecond as the learn and is
	// never returned (KVt9sOuRsPuy).
	returnPathKickFirstDelay = 500 * time.Millisecond
	// returnPathKickInterval spaces return-path kicks while the call has no
	// return keyframe yet.
	returnPathKickInterval = time.Second
	// returnPathKickIDRFresh is the minimum gap after a forwarded uplink IDR:
	// an echo returns it in ~50ms, so 500ms without a return keyframe means it
	// will not come back.
	returnPathKickIDRFresh = 500 * time.Millisecond
	// returnPathKickPoll is how often the learn schedule re-checks the gates.
	returnPathKickPoll = 250 * time.Millisecond
	// returnPathKickWindow bounds the learn schedule. The shared attempt cap
	// (returnRecoveryUplinkKickMax) normally ends it first.
	returnPathKickWindow = 15 * time.Second
)

// returnPathKickClock lets tests drive the learn schedule on a fake clock.
type returnPathKickClock struct {
	now   func() time.Time
	sleep func(time.Duration)
}

var realReturnPathKickClock = returnPathKickClock{now: time.Now, sleep: time.Sleep}

// StartReturnPathUplinkKicks arms the SSRC-learn return-path schedule once
// per call: returnPathKickFirstDelay after the first remote video SSRC learn,
// and then every returnPathKickInterval while the call is answered and no
// SIP→WebRTC keyframe has arrived, the browser is asked for an IDR (forced
// FIR + PLI, `uplink_keyframe_kick reason=return-recovery trigger=ssrc-learn`).
// It shares the gates and the attempt cap with the client-PLI return-recovery
// kick, skips while an uplink IDR is in flight or one was forwarded in the last
// returnPathKickIDRFresh, and stops at the first return keyframe. Returns true
// when the schedule was armed.
func (s *Session) StartReturnPathUplinkKicks() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if s.returnPathKickArmed {
		s.mu.Unlock()
		return false
	}
	s.returnPathKickArmed = true
	epoch := s.MediaEpoch
	s.mu.Unlock()
	go s.runReturnPathUplinkKicks(epoch, realReturnPathKickClock)
	return true
}

// runReturnPathUplinkKicks is the learn schedule loop. Returns the stop reason.
func (s *Session) runReturnPathUplinkKicks(epoch uint64, clock returnPathKickClock) string {
	start := clock.now()
	clock.sleep(returnPathKickFirstDelay)
	for {
		now := clock.now()
		stop := s.returnPathKickTick(epoch, now)
		if stop == "" && now.Sub(start) >= returnPathKickWindow {
			stop = "window"
		}
		if stop != "" {
			fmt.Printf("[%s] return_path_kick_schedule_stop reason=%s after=%s\n", s.ID, stop, now.Sub(start).Round(time.Millisecond))
			return stop
		}
		clock.sleep(returnPathKickPoll)
	}
}

// returnPathKickTick runs one schedule step: it sends a kick when the gates
// allow and returns a non-empty reason when the schedule should stop.
func (s *Session) returnPathKickTick(epoch uint64, now time.Time) string {
	if s.GetState() == StateEnded {
		return "ended"
	}
	s.mu.RLock()
	sameCall := s.MediaEpoch == epoch
	returnKeyframe := !s.returnKeyframeThisCallLocked().IsZero()
	kicks := s.returnRecoveryUplinkKicks
	s.mu.RUnlock()
	switch {
	case !sameCall:
		return "new-call"
	case returnKeyframe:
		return "return-keyframe"
	case kicks >= returnRecoveryUplinkKickMax:
		return "max"
	}
	// Only after SIP 200 OK: early or waiting media is not the echo path.
	if s.GetState() != StateActive {
		return ""
	}
	if _, skip := s.tryReturnRecoveryUplinkKick("ssrc-learn", now); skip == "max" {
		return "max"
	}
	return ""
}
