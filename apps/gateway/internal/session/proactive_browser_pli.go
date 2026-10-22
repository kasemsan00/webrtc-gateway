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
	// SIPInfoFIRDebounce collapses Linphone's repeated picture_fast_update
	// INFOs. The SDK sends another VFU while the decoder is still broken.
	SIPInfoFIRDebounce = 250 * time.Millisecond
	// Mode0DropPLIRetry is the earliest a second mode0_drop PLI may be sent
	// while the gateway is still waiting for the IDR from the first one.
	Mode0DropPLIRetry = 2 * time.Second
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

// HandleSIPInfoPictureFastUpdate is one Linphone VFU. Repeats inside
// SIPInfoFIRDebounce do not send another PLI. An accepted request sends one
// forced PLI and pulls the next proactive PLI forward to 2s.
func (s *Session) HandleSIPInfoPictureFastUpdate(now time.Time) bool {
	s.mu.Lock()
	if SIPInfoFIRDebounced(s.sipInfoFIRAt, now) {
		id := s.ID
		s.mu.Unlock()
		fmt.Printf("[%s] sip_info_fir debounced\n", id)
		return false
	}
	s.sipInfoFIRAt = now
	s.armBrowserIDRLocked(now)
	s.mu.Unlock()
	s.signalProactivePLIWake()
	s.SendBrowserPLIImmediate("sip_info_fir")
	return true
}

// HandleSIPReinviteKeyframe asks the browser for an IDR after a SIP re-INVITE.
func (s *Session) HandleSIPReinviteKeyframe(now time.Time) {
	s.mu.Lock()
	s.armBrowserIDRLocked(now)
	s.mu.Unlock()
	s.signalProactivePLIWake()
	fmt.Printf("[%s] sip_reinvite pli\n", s.ID)
	s.SendBrowserPLIImmediate("sip_reinvite")
}

func (s *Session) armBrowserIDRLocked(now time.Time) {
	s.switchKeyframeRequestedAt = now
	s.switchKeyframeIDRPending = true
	s.proactivePLINotBefore = now.Add(ProactiveBrowserPLIInitialInterval)
}

// SIPInfoFIRDebounced reports whether this VFU is a repeat inside the window.
func SIPInfoFIRDebounced(last, now time.Time) bool {
	return !last.IsZero() && now.Sub(last) < SIPInfoFIRDebounce
}

// SIPInfoKeyframePending reports that an INFO picture_fast_update is waiting
// for the next uplink IDR.
func (s *Session) SIPInfoKeyframePending() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.switchKeyframeIDRPending
}

// SendBrowserPLIImmediate writes one browser PLI even when one was sent in
// the last second. Forced sends still skip the 400ms window.
func (s *Session) SendBrowserPLIImmediate(reason string) bool {
	s.mu.Lock()
	s.lastBrowserPLIReason = reason
	saved := s.LastWebRTCPLISent
	s.LastWebRTCPLISent = time.Time{}
	snapshot := s.prepareWebRTCFeedbackLocked("pli", true, nil)
	if !snapshot.ready {
		s.LastWebRTCPLISent = saved
	}
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

// SendBrowserPLI writes one PLI to the browser. Forced sends bypass the 400ms
// window and still honor the 1s overall cap. True only after WriteRTCP succeeds.
func (s *Session) SendBrowserPLI(reason string) bool {
	s.mu.Lock()
	s.lastBrowserPLIReason = reason
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

// NoteMode0Drop records one bad mode-0 frame. One PLI is sent for the wait.
// Further broken frames do not send another until Mode0DropPLIRetry has passed
// with no IDR, or the IDR arrives and a later frame breaks. The caller sends
// the PLI off the RTP read goroutine.
func (s *Session) NoteMode0Drop(now time.Time, reason string, nalBytes int) bool {
	if s == nil || !mode0DropArmsRecovery(reason) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uplinkStallSince.IsZero() {
		s.uplinkStallSince = now
	}
	s.uplinkStallReason = reason
	s.mode0NeedsIDR = true
	if reason == "oversized-fua" {
		fmt.Printf("[%s] h264_mode0_oversized nal_bytes=%d\n", s.ID, nalBytes)
	}
	if !s.mode0PLIAt.IsZero() && now.Sub(s.mode0PLIAt) < Mode0DropPLIRetry {
		return false
	}
	s.mode0PLIAt = now
	s.lastBrowserPLIReason = "mode0_drop"
	return true
}

// UplinkHeldForMode0Recovery reports whether this video packet must wait for
// the next IDR after a mode-0 drop. An IDR clears the hold.
func (s *Session) UplinkHeldForMode0Recovery(isIDR bool) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.holdUplinkForMode0Locked(isIDR)
}

func (s *Session) holdUplinkForMode0Locked(isIDR bool) bool {
	if !s.mode0NeedsIDR {
		return false
	}
	if !isIDR {
		return true
	}
	s.mode0NeedsIDR = false
	s.mode0PLIAt = time.Time{}
	s.uplinkStallSince = time.Time{}
	s.uplinkStallReason = ""
	s.uplinkStallLogAt = time.Time{}
	return false
}

// MaybeLogUplinkVideoStall prints one stall line per second while mode-0
// recovery or the SPS size lock is dropping uplink video.
func (s *Session) MaybeLogUplinkVideoStall(now time.Time, browserRTPPerSec int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	reason := s.uplinkStallReason
	since := s.uplinkStallSince
	if s.uplinkSPSDimensionBlocked {
		reason = "sps-dimension"
		if since.IsZero() {
			since = now
			s.uplinkStallSince = now
		}
	}
	if reason == "" || since.IsZero() {
		s.mu.Unlock()
		return
	}
	if !s.uplinkStallLogAt.IsZero() && now.Sub(s.uplinkStallLogAt) < time.Second {
		s.mu.Unlock()
		return
	}
	s.uplinkStallLogAt = now
	id := s.ID
	line := formatUplinkVideoStalled(now.Sub(since), browserRTPPerSec, reason)
	s.mu.Unlock()
	fmt.Printf("[%s] %s\n", id, line)
}

func formatUplinkVideoStalled(since time.Duration, browserRTPPerSec int, reason string) string {
	if since < 0 {
		since = 0
	}
	return fmt.Sprintf("uplink_video_stalled since=%s browser_rtp_per_s=%d reason=%s", since.Round(100*time.Millisecond), browserRTPPerSec, reason)
}

// LastBrowserPLIReason is the most recent browser-PLI reason this session attempted.
func (s *Session) LastBrowserPLIReason() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastBrowserPLIReason
}

// RememberSIPAnswer stores the SDP body we last answered with.
func (s *Session) RememberSIPAnswer(sdp []byte) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sipAnswerSDP = append([]byte(nil), sdp...)
	s.mu.Unlock()
}

// SIPAnswerSDP returns a copy of the last SIP SDP answer, or nil.
func (s *Session) SIPAnswerSDP() []byte {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.sipAnswerSDP) == 0 {
		return nil
	}
	return append([]byte(nil), s.sipAnswerSDP...)
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
