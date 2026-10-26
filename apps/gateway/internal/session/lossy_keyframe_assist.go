package session

import (
	"context"
	"fmt"
	"time"
)

// Lossy keyframe assist (SIP_LOSSY_KEYFRAME_ASSIST). Android Linphone legs
// lose 5-20% of video. A 25-30 packet IDR then rarely arrives whole, the
// phone keeps showing the queue picture, and it only asks again every ~5s.
// When the SIP video leg is lossy the gateway sends one extra browser IDR
// after each early-call picture_fast_update, and runs the safety net at a
// shorter interval. A leg below the threshold behaves exactly as before.

const (
	sipVideoLossBucket      = time.Second
	sipVideoLossBuckets     = 5
	sipVideoLossMinExpected = 50
	// sipVideoLossRebaseJump is a forward sequence jump treated as a new
	// stream (switch, SSRC change) rather than lost packets.
	sipVideoLossRebaseJump = 3000
	// lossyExtraIDRInFlightWait is how long the extra request re-checks
	// while a browser IDR is still being forwarded.
	lossyExtraIDRInFlightWait = 80 * time.Millisecond
	// lossyExtraIDRMaxChecks bounds those re-checks (~1s) before giving up.
	lossyExtraIDRMaxChecks = 12
)

type sipVideoLossBucketStat struct {
	start    time.Time
	expected int
	received int
}

// sipVideoLossWindow is RTCP-style loss (expected - received) over the last
// sipVideoLossBuckets-1 complete seconds of SIP video sequence numbers. A
// reordered packet that arrives late still counts as received, so reorder
// is not loss. The bucket still filling is left out of the figure.
type sipVideoLossWindow struct {
	haveBase bool
	highest  uint16
	cur      int
	buckets  [sipVideoLossBuckets]sipVideoLossBucketStat
}

// observe adds one packet. rolled is true when this packet started a new
// bucket, which is when the caller re-evaluates the lossy state.
func (w *sipVideoLossWindow) observe(now time.Time, seq uint16) (rolled bool) {
	b := &w.buckets[w.cur]
	switch {
	case b.start.IsZero():
		b.start = now
	case now.Sub(b.start) >= sipVideoLossBucket:
		steps := int(now.Sub(b.start) / sipVideoLossBucket)
		if steps > sipVideoLossBuckets {
			steps = sipVideoLossBuckets
		}
		for i := 0; i < steps; i++ {
			w.cur = (w.cur + 1) % sipVideoLossBuckets
			w.buckets[w.cur] = sipVideoLossBucketStat{}
		}
		w.buckets[w.cur].start = now
		b = &w.buckets[w.cur]
		rolled = true
	}
	if !w.haveBase {
		w.haveBase = true
		w.highest = seq
		b.expected++
		b.received++
		return rolled
	}
	delta := uint16(seq - w.highest)
	switch {
	case delta == 0:
		// duplicate
	case delta < 0x8000 && delta > sipVideoLossRebaseJump:
		w.highest = seq
		b.expected++
		b.received++
	case delta < 0x8000:
		w.highest = seq
		b.expected += int(delta)
		b.received++
	default:
		// Late packet: its slot was already counted as expected.
		b.received++
	}
	return rolled
}

func (w *sipVideoLossWindow) totals() (expected, lost int) {
	received := 0
	for i, b := range w.buckets {
		if i == w.cur {
			continue
		}
		expected += b.expected
		received += b.received
	}
	lost = expected - received
	if lost < 0 {
		lost = 0
	}
	return expected, lost
}

func (s *Session) lossySafetyInterval() time.Duration {
	if s.lossySafety > 0 {
		return s.lossySafety
	}
	return 3 * time.Second
}

// NoteSIPVideoSeq feeds one SIP video RTP sequence number into the loss
// window. It is a no-op with the assist off. The lossy state changes at most
// once per bucket; a change wakes the proactive loop so it picks up the
// new safety interval.
func (s *Session) NoteSIPVideoSeq(now time.Time, seq uint16) {
	if s == nil || !s.lossyAssist {
		return
	}
	s.lossyMu.Lock()
	if !s.lossyLoss.observe(now, seq) {
		s.lossyMu.Unlock()
		return
	}
	expected, lost := s.lossyLoss.totals()
	if expected < sipVideoLossMinExpected {
		s.lossyMu.Unlock()
		return
	}
	pct := 100 * float64(lost) / float64(expected)
	s.lossyPct = pct
	lossy := pct >= float64(s.lossyThresholdPct)
	changed := lossy != s.lossyActive
	s.lossyActive = lossy
	s.lossyMu.Unlock()
	if !changed {
		return
	}
	normal := s.proactiveSafetyInterval()
	if lossy {
		fmt.Printf("[%s] lossy_keyframe_assist lossy_detected loss_pct=%.1f expected=%d lost=%d threshold_pct=%d\n",
			s.ID, pct, expected, lost, s.lossyThresholdPct)
		fmt.Printf("[%s] lossy_keyframe_assist safety_interval switched from=%s to=%s reason=lossy\n",
			s.ID, normal, s.lossySafetyInterval())
	} else {
		fmt.Printf("[%s] lossy_keyframe_assist recovered loss_pct=%.1f expected=%d lost=%d threshold_pct=%d\n",
			s.ID, pct, expected, lost, s.lossyThresholdPct)
		fmt.Printf("[%s] lossy_keyframe_assist safety_interval switched from=%s to=%s reason=recovered\n",
			s.ID, s.lossySafetyInterval(), normal)
	}
	s.signalProactivePLIWake()
}

// SIPVideoLossy reports whether the assist is on and the SIP video leg is
// currently above the loss threshold, with the last measured loss.
func (s *Session) SIPVideoLossy() (bool, float64) {
	if s == nil || !s.lossyAssist {
		return false, 0
	}
	s.lossyMu.Lock()
	defer s.lossyMu.Unlock()
	return s.lossyActive, s.lossyPct
}

// proactiveSafetyForCallLocked is the safety-net interval and whether an
// answered SIP keyframe request may still suppress it. On a lossy leg the
// phone's answered VFU is not proof its decoder recovered: the IDR may
// have been damaged on the way, so the shorter safety net keeps running.
// The caller holds s.mu (read or write); lossyMu nests inside it.
func (s *Session) proactiveSafetyForCallLocked(sipSeen bool) (time.Duration, bool) {
	if lossy, _ := s.SIPVideoLossy(); lossy {
		return s.lossySafetyInterval(), false
	}
	return s.proactiveSafetyInterval(), sipSeen
}

// noteCallAnsweredLocked starts the early-call window. The caller holds s.mu.
func (s *Session) noteCallAnsweredLocked(now time.Time) {
	if s.lossyAnsweredAt.IsZero() {
		s.lossyAnsweredAt = now
	}
}

// resetLossyKeyframeAssistLocked clears per-call state. The caller holds s.mu.
func (s *Session) resetLossyKeyframeAssistLocked() {
	s.lossyAnsweredAt = time.Time{}
	s.lossyExtraPending = false
	s.lossyExtraGen++
	s.lossyMu.Lock()
	s.lossyLoss = sipVideoLossWindow{}
	s.lossyActive = false
	s.lossyPct = 0
	s.lossyMu.Unlock()
}

// maybeScheduleLossyExtraIDR runs after a picture_fast_update was answered.
// Within the early window after answer, on a lossy leg, it schedules one
// extra browser IDR request. A request already scheduled absorbs this one.
func (s *Session) maybeScheduleLossyExtraIDR(now time.Time) {
	lossy, pct := s.SIPVideoLossy()
	if !lossy {
		return
	}
	s.mu.Lock()
	id := s.ID
	answeredAt := s.lossyAnsweredAt
	if answeredAt.IsZero() || now.Sub(answeredAt) > s.lossyEarlyWindow {
		s.mu.Unlock()
		age := "none"
		if !answeredAt.IsZero() {
			age = now.Sub(answeredAt).Round(time.Millisecond).String()
		}
		fmt.Printf("[%s] lossy_extra_idr skipped reason=outside-early-window call_age=%s loss_pct=%.1f\n", id, age, pct)
		return
	}
	if s.lossyExtraPending {
		s.mu.Unlock()
		fmt.Printf("[%s] lossy_extra_idr coalesced reason=already-scheduled loss_pct=%.1f\n", id, pct)
		return
	}
	s.lossyExtraPending = true
	s.lossyExtraGen++
	gen := s.lossyExtraGen
	delay := s.lossyExtraDelay
	s.mu.Unlock()
	fmt.Printf("[%s] lossy_extra_idr scheduled delay=%s loss_pct=%.1f call_age=%s\n",
		id, delay, pct, now.Sub(answeredAt).Round(time.Millisecond))
	go s.runLossyExtraIDR(gen, delay)
}

// lossyExtraIDRDecision is the gate when the extra request fires. send
// sends now; wait > 0 re-checks after wait; otherwise the request is
// dropped with reason. The caller holds s.mu.
func (s *Session) lossyExtraIDRDecisionLocked(gen uint64, now time.Time) (send bool, wait time.Duration, reason string) {
	if s.lossyExtraGen != gen || s.State == StateEnded {
		return false, 0, "stale"
	}
	if s.sipInfoFIRRetryActive {
		// A VFU retry is still waiting for its IDR; that IDR is the next one.
		return false, 0, "sip-retry-pending"
	}
	lastReq := s.lastBrowserIDRRequest
	lastIDR := s.uplinkVideoLastIDR
	if !lastReq.IsZero() && (lastIDR.IsZero() || lastIDR.Before(lastReq)) && now.Sub(lastReq) < BrowserKeyframeSatisfyWindow {
		return false, 0, "pli-pending"
	}
	if s.uplinkIDRInFlight {
		return false, lossyExtraIDRInFlightWait, "idr-in-flight"
	}
	// Chrome ignores a PLI right after an IDR; wait until it would act on it.
	for _, t := range []time.Time{lastReq, lastIDR} {
		if !t.IsZero() {
			if remain := BrowserIDRRequestMinGap - now.Sub(t); remain > wait {
				wait = remain
			}
		}
	}
	if wait > 0 {
		return false, wait, "min-gap"
	}
	return true, 0, ""
}

func (s *Session) runLossyExtraIDR(gen uint64, delay time.Duration) {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		s.mu.Lock()
		if s.lossyExtraGen == gen {
			s.lossyExtraPending = false
		}
		s.mu.Unlock()
	}()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for checks := 0; ; checks++ {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.mu.Lock()
		send, wait, reason := s.lossyExtraIDRDecisionLocked(gen, time.Now())
		sendFn := s.lossyExtraSend
		s.mu.Unlock()
		if send {
			if sendFn == nil {
				sendFn = s.SendBrowserPLIImmediate
			}
			if sendFn("lossy_extra_idr") {
				fmt.Printf("[%s] lossy_extra_idr sent\n", s.ID)
			} else {
				fmt.Printf("[%s] lossy_extra_idr coalesced reason=browser-pli-gate\n", s.ID)
			}
			return
		}
		if wait <= 0 || checks >= lossyExtraIDRMaxChecks {
			if reason != "stale" {
				fmt.Printf("[%s] lossy_extra_idr coalesced reason=%s\n", s.ID, reason)
			}
			return
		}
		timer.Reset(wait)
	}
}
