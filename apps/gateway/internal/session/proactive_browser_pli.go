package session

import (
	"context"
	"fmt"
	"time"
)

const (
	// ProactiveBrowserPLIInitialInterval is how often the gateway asks the
	// browser for an IDR during the opening window of a call whose SIP leg
	// has never sent PLI or FIR.
	ProactiveBrowserPLIInitialInterval = 2 * time.Second
	// ProactiveBrowserPLIInitialWindow is how long the faster interval lasts.
	ProactiveBrowserPLIInitialWindow = 10 * time.Second
	// ProactiveBrowserPLISteadyInterval is the 3–4s pace used after the opening window.
	ProactiveBrowserPLISteadyInterval = 4 * time.Second
	// SwitchKeyframePLIFollowUp is the first retry after a switch request.
	SwitchKeyframePLIFollowUp = 300 * time.Millisecond
	// SwitchKeyframePLISecond is the second retry after a switch request.
	SwitchKeyframePLISecond = time.Second
)

// ProactiveBrowserPLIInterval is the wait before the next browser PLI.
// callAge is how long the schedule has already been running. The rtp-flowing
// watchdog does not change this interval.
func ProactiveBrowserPLIInterval(callAge time.Duration) time.Duration {
	if callAge < ProactiveBrowserPLIInitialWindow {
		return ProactiveBrowserPLIInitialInterval
	}
	return ProactiveBrowserPLISteadyInterval
}

// NoteSIPKeyframeRequest records that the SIP leg itself sent PLI or FIR.
// The proactive browser-PLI schedule stops after the first one.
func (s *Session) NoteSIPKeyframeRequest(kind string) {
	s.mu.Lock()
	already := s.sipKeyframeRequestSeen
	s.sipKeyframeRequestSeen = true
	s.mu.Unlock()
	if !already {
		fmt.Printf("[%s] sip_keyframe_request_seen kind=%s\n", s.ID, kind)
	}
}

// SendProactivePLIToWebRTC writes one PLI to the browser. It bypasses the
// 400ms generic throttle and still refuses to send when any browser PLI was
// written in the last second. The bool is true only after WriteRTCP succeeds.
func (s *Session) SendProactivePLIToWebRTC() bool {
	s.mu.Lock()
	snapshot := s.prepareWebRTCFeedbackLocked("pli", true, nil)
	id := s.ID
	s.mu.Unlock()
	if snapshot.throttled || !snapshot.ready {
		return false
	}
	return sendPreparedWebRTCFeedback(id, snapshot)
}

// ProactiveBrowserPLIWait is how long the proactive loop should sleep.
// A switch request holds the next proactive PLI until 2s after that request,
// including when the call is already on the 4s steady interval.
func (s *Session) ProactiveBrowserPLIWait(callAge time.Duration, now time.Time) time.Duration {
	s.mu.Lock()
	notBefore := s.proactivePLINotBefore
	s.mu.Unlock()
	if notBefore.After(now) {
		return notBefore.Sub(now)
	}
	return ProactiveBrowserPLIInterval(callAge)
}

// ConsumeProactivePLIHold clears a switch reset once its 2s point has passed
// so the following wait uses the call-age interval again.
func (s *Session) ConsumeProactivePLIHold(now time.Time) {
	s.mu.Lock()
	if !s.proactivePLINotBefore.IsZero() && !now.Before(s.proactivePLINotBefore) {
		s.proactivePLINotBefore = time.Time{}
	}
	s.mu.Unlock()
}

func (s *Session) proactivePLIWakeChan() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proactivePLIWake == nil {
		s.proactivePLIWake = make(chan struct{}, 1)
	}
	return s.proactivePLIWake
}

func (s *Session) signalProactivePLIWake() {
	ch := s.proactivePLIWakeChan()
	select {
	case ch <- struct{}{}:
	default:
	}
}

// ProactivePLIWakeChan is the interrupt for the SIP-side proactive loop.
func (s *Session) ProactivePLIWakeChan() <-chan struct{} {
	return s.proactivePLIWakeChan()
}

// armSwitchKeyframeRequestLocked records a client request for a browser IDR.
// The caller holds s.mu. The returned generation cancels an older follow-up burst.
func (s *Session) armSwitchKeyframeRequestLocked(now time.Time) uint64 {
	s.switchKeyframeBurstGen++
	s.switchKeyframeRequestedAt = now
	s.switchKeyframeIDRPending = true
	s.allowNextUplinkPictureSize = true
	s.uplinkSPSDimensionBlocked = false
	s.proactivePLINotBefore = now.Add(ProactiveBrowserPLIInitialInterval)
	return s.switchKeyframeBurstGen
}

// HandleSwitchKeyframeRequest arms the post-switch IDR path and starts the
// PLI burst. now is the receipt time used for the latency log.
func (s *Session) HandleSwitchKeyframeRequest(now time.Time) {
	s.mu.Lock()
	gen := s.armSwitchKeyframeRequestLocked(now)
	id := s.ID
	s.mu.Unlock()
	fmt.Printf("[%s] switch_keyframe_request received\n", id)
	s.signalProactivePLIWake()
	go s.runSwitchKeyframeBurst(gen)
}

func (s *Session) runSwitchKeyframeBurst(gen uint64) {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	for _, at := range []time.Duration{0, SwitchKeyframePLIFollowUp, SwitchKeyframePLISecond} {
		if at > 0 {
			timer := time.NewTimer(time.Until(start.Add(at)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		s.mu.Lock()
		current := s.switchKeyframeBurstGen
		s.mu.Unlock()
		if current != gen {
			return
		}
		s.SendBrowserPLI("switch")
	}
}

// SendBrowserPLI writes one PLI to the browser. Forced sends bypass the 400ms
// window and still honor the 1s overall cap. True only after WriteRTCP succeeds.
func (s *Session) SendBrowserPLI(reason string) bool {
	s.mu.Lock()
	snapshot := s.prepareWebRTCFeedbackLocked("pli", true, nil)
	id := s.ID
	throttled := snapshot.throttled
	ready := snapshot.ready
	s.mu.Unlock()
	if throttled {
		fmt.Printf("[%s] browser_pli throttled reason=%s\n", id, reason)
		return false
	}
	if !ready || !sendPreparedWebRTCFeedback(id, snapshot) {
		return false
	}
	fmt.Printf("[%s] browser_pli sent reason=%s\n", id, reason)
	return true
}

// HasSIPKeyframeRequest reports whether SIP RTCP has ever carried PLI or FIR.
func (s *Session) HasSIPKeyframeRequest() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sipKeyframeRequestSeen
}

// ObserveUplinkVideoForward counts one WebRTC→SIP video packet that was written.
// A new IDR timestamp logs the gap since the previous IDR. Once a second it
// logs the forwarded packet rate. Must not be called while holding s.mu.
func (s *Session) ObserveUplinkVideoForward(isIDR bool, rtpTimestamp uint32, now time.Time) {
	s.mu.Lock()
	if s.uplinkVideoWindowStart.IsZero() {
		s.uplinkVideoWindowStart = now
	}
	s.uplinkVideoPackets++
	elapsed := now.Sub(s.uplinkVideoWindowStart)
	currentRate := s.lastUplinkPacketsPerSec
	if elapsed > 0 && s.uplinkVideoPackets > 0 {
		currentRate = int(float64(s.uplinkVideoPackets) / elapsed.Seconds())
	}

	logIDR := false
	var idrGap time.Duration
	logAfterSwitch := false
	var switchLatency time.Duration
	if isIDR && s.switchKeyframeIDRPending {
		logAfterSwitch = true
		switchLatency = now.Sub(s.switchKeyframeRequestedAt)
		s.switchKeyframeIDRPending = false
		s.allowNextUplinkPictureSize = false
	}
	if isIDR && (!s.uplinkVideoIDRTimestampSeen || rtpTimestamp != s.uplinkVideoLastIDRTimestamp) {
		if !s.uplinkVideoLastIDR.IsZero() {
			idrGap = now.Sub(s.uplinkVideoLastIDR)
		}
		s.uplinkVideoLastIDR = now
		s.uplinkVideoLastIDRTimestamp = rtpTimestamp
		s.uplinkVideoIDRTimestampSeen = true
		logIDR = true
	}

	logRate := false
	rate := 0
	if elapsed >= time.Second {
		rate = currentRate
		s.lastUplinkPacketsPerSec = rate
		s.uplinkVideoPackets = 0
		s.uplinkVideoWindowStart = now
		logRate = true
	}
	id := s.ID
	s.mu.Unlock()

	if logAfterSwitch {
		fmt.Printf("[%s] uplink_idr_after_switch forwarded latency_ms=%d\n", id, switchLatency.Milliseconds())
	}
	if logIDR {
		fmt.Printf("[%s] uplink_idr_forwarded gap_s=%.1f packets_per_s=%d\n", id, idrGap.Seconds(), currentRate)
	}
	if logRate {
		fmt.Printf("[%s] uplink_video_forward packets_per_s=%d\n", id, rate)
	}
}
