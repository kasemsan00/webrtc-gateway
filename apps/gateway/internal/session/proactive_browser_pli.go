package session

import (
	"context"
	"fmt"
	"time"
)

const (
	// ProactiveBrowserPLIInitialInterval is how often the gateway asks the
	// browser for an IDR until the first one is forwarded to SIP.
	ProactiveBrowserPLIInitialInterval = 2 * time.Second
	// ProactiveBrowserPLISafetyInterval is the steady-state floor: a
	// proactive PLI is sent only if no IDR has been forwarded in this long.
	// SIP INFO FIR and RTCP FIR/PLI handle recovery in between.
	ProactiveBrowserPLISafetyInterval = 10 * time.Second
	// BrowserKeyframeSatisfyWindow is the shared keyframe-request dedupe.
	// A new PLI that lands within this window of a forwarded IDR, or of a
	// PLI that is still waiting for one, is already satisfied.
	BrowserKeyframeSatisfyWindow = time.Second
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
	// BrowserIDRRequestMinGap is the closest two browser IDR requests may be,
	// whatever asked: SIP INFO, its retry, mode0 recovery, a switch, or the
	// proactive schedule. Chrome ignores a PLI inside this window after an IDR.
	BrowserIDRRequestMinGap = 500 * time.Millisecond
	// SIPInfoFIRRetryCap bounds how many times one VFU is retried while the
	// browser still has not forwarded a newer IDR.
	SIPInfoFIRRetryCap = 6
)

// ProactiveBrowserPLIDecision is whether the proactive loop should send.
type ProactiveBrowserPLIDecision struct {
	Send   bool
	Reason string
}

// DecideProactiveBrowserPLI is the fake-clock decision for the proactive
// browser-PLI loop. Startup PLIs stop once any IDR has been forwarded.
// After that only the safety net remains, and it fires only when no IDR
// (and no unanswered safety PLI) has been seen for safety.
func DecideProactiveBrowserPLI(lastIDR, lastPLI, now time.Time, initial, safety time.Duration) ProactiveBrowserPLIDecision {
	if initial <= 0 {
		initial = ProactiveBrowserPLIInitialInterval
	}
	if safety <= 0 {
		safety = ProactiveBrowserPLISafetyInterval
	}
	if lastIDR.IsZero() {
		if !lastPLI.IsZero() && now.Sub(lastPLI) < initial {
			return ProactiveBrowserPLIDecision{Reason: "startup-wait"}
		}
		return ProactiveBrowserPLIDecision{Send: true, Reason: "startup"}
	}
	if now.Sub(lastIDR) < safety {
		return ProactiveBrowserPLIDecision{Reason: "idr-fresh"}
	}
	if !lastPLI.IsZero() && now.Sub(lastPLI) < safety {
		return ProactiveBrowserPLIDecision{Reason: "safety-wait"}
	}
	return ProactiveBrowserPLIDecision{Send: true, Reason: "safety-net"}
}

// BrowserKeyframeRequestSatisfied reports that another browser IDR request
// would not help: one was forwarded within BrowserKeyframeSatisfyWindow, or
// a PLI is still outstanding and waiting for that IDR.
func BrowserKeyframeRequestSatisfied(lastIDR, lastPLI, now time.Time) (bool, string) {
	if !lastIDR.IsZero() {
		age := now.Sub(lastIDR)
		if age >= 0 && age < BrowserKeyframeSatisfyWindow {
			return true, "idr-fresh"
		}
	}
	if !lastPLI.IsZero() && (lastIDR.IsZero() || lastIDR.Before(lastPLI)) {
		age := now.Sub(lastPLI)
		if age >= 0 && age < BrowserKeyframeSatisfyWindow {
			return true, "pli-pending"
		}
	}
	return false, ""
}

// IDRSatisfiesKeyframeRequest is true when an IDR was forwarded at or after
// the request. Leftover slices of a picture still in flight count.
func IDRSatisfiesKeyframeRequest(lastIDR, requestedAt time.Time) bool {
	return !lastIDR.IsZero() && !lastIDR.Before(requestedAt)
}

// NoteSIPKeyframeRequest records that the SIP leg itself sent PLI or FIR.
// Proactive PLI no longer exits on this; SIP INFO FIR and the safety net
// keep asking when the phone is silent.
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
	now := time.Now()
	if !browserIDRRequestAllowed(s.lastBrowserIDRRequest, now) {
		id := s.ID
		s.mu.Unlock()
		fmt.Printf("[%s] browser_pli coalesced reason=proactive\n", id)
		return false
	}
	snapshot := s.prepareWebRTCFeedbackLocked("pli", true, nil)
	if snapshot.ready && !snapshot.throttled {
		s.lastBrowserIDRRequest = now
	}
	id := s.ID
	s.mu.Unlock()
	if snapshot.throttled || !snapshot.ready {
		return false
	}
	return sendPreparedWebRTCFeedback(id, snapshot)
}

func (s *Session) proactiveInitialInterval() time.Duration {
	if s != nil && s.proactiveBrowserPLIInitial > 0 {
		return s.proactiveBrowserPLIInitial
	}
	return ProactiveBrowserPLIInitialInterval
}

func (s *Session) proactiveSafetyInterval() time.Duration {
	if s != nil && s.proactiveBrowserPLISafety > 0 {
		return s.proactiveBrowserPLISafety
	}
	return ProactiveBrowserPLISafetyInterval
}

func (s *Session) lastForwardedIDRTimeLocked() time.Time {
	t := s.LastUplinkKeyframe
	if s.uplinkVideoLastIDR.After(t) {
		t = s.uplinkVideoLastIDR
	}
	return t
}

// ProactiveBrowserPLIInitial is the configured startup interval, or the default.
func (s *Session) ProactiveBrowserPLIInitial() time.Duration {
	return s.proactiveInitialInterval()
}

// ProactiveBrowserPLISafety is the configured safety-net interval, or the default.
func (s *Session) ProactiveBrowserPLISafety() time.Duration {
	return s.proactiveSafetyInterval()
}

// DecideSessionProactiveBrowserPLI is DecideProactiveBrowserPLI using session clocks.
func (s *Session) DecideSessionProactiveBrowserPLI(now time.Time) ProactiveBrowserPLIDecision {
	s.mu.RLock()
	lastIDR := s.lastForwardedIDRTimeLocked()
	lastPLI := s.lastBrowserIDRRequest
	initial := s.proactiveInitialInterval()
	safety := s.proactiveSafetyInterval()
	s.mu.RUnlock()
	return DecideProactiveBrowserPLI(lastIDR, lastPLI, now, initial, safety)
}

// ProactiveBrowserPLIWait is how long the proactive loop should sleep.
// After the first forwarded IDR this is the remaining safety-net time,
// not a 4s cadence. FIR and switch no longer reset it to 2s.
func (s *Session) ProactiveBrowserPLIWait(now time.Time) time.Duration {
	s.mu.RLock()
	lastIDR := s.lastForwardedIDRTimeLocked()
	lastPLI := s.lastBrowserIDRRequest
	initial := s.proactiveInitialInterval()
	safety := s.proactiveSafetyInterval()
	s.mu.RUnlock()
	if lastIDR.IsZero() {
		if !lastPLI.IsZero() {
			next := lastPLI.Add(initial)
			if next.After(now) {
				return next.Sub(now)
			}
		}
		if initial < time.Millisecond {
			return time.Millisecond
		}
		return initial
	}
	next := lastIDR.Add(safety)
	if lastPLI.After(lastIDR) {
		if pliNext := lastPLI.Add(safety); pliNext.After(next) {
			next = pliNext
		}
	}
	if next.After(now) {
		return next.Sub(now)
	}
	return time.Millisecond
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
	for i, at := range []time.Duration{0, SwitchKeyframePLIFollowUp, SwitchKeyframePLISecond} {
		if at > 0 {
			timer := time.NewTimer(time.Until(start.Add(at)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		now := time.Now()
		if stop, reason := s.switchKeyframeBurstShouldStop(gen, i > 0, now); stop {
			fmt.Printf("[%s] switch_keyframe_burst_stop reason=%s\n", s.ID, reason)
			return
		}
		s.SendBrowserPLI("switch")
	}
}

// switchKeyframeBurstShouldStop is the fake-clock gate for one shot of the
// post-switch PLI burst. followUp is false for the immediate shot.
func (s *Session) switchKeyframeBurstShouldStop(gen uint64, followUp bool, now time.Time) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.switchKeyframeBurstGen != gen {
		return true, "superseded"
	}
	lastIDR := s.lastForwardedIDRTimeLocked()
	requestedAt := s.switchKeyframeRequestedAt
	if IDRSatisfiesKeyframeRequest(lastIDR, requestedAt) || !s.switchKeyframeIDRPending {
		return true, "idr-forwarded"
	}
	if followUp {
		return false, ""
	}
	if skip, reason := BrowserKeyframeRequestSatisfied(lastIDR, s.lastBrowserIDRRequest, now); skip {
		return true, reason
	}
	return false, ""
}

// HandleSIPInfoPictureFastUpdate is one Linphone VFU. Repeats inside
// SIPInfoFIRDebounce do not send another PLI. A VFU that arrives while Chrome
// is still inside its post-IDR ignore window does not send immediately; it
// schedules sip_info_fir_retry for when that window ends.
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
	s.firIDRSliceLog = true
	s.firIDRSliceTS = 0
	s.firIDRSliceTSSet = false
	wait, retry := SIPInfoFIRRetryWait(s.uplinkVideoLastIDR, now)
	var gen uint64
	if retry && !s.sipInfoFIRRetryActive {
		s.sipInfoFIRRetryGen++
		gen = s.sipInfoFIRRetryGen
		s.sipInfoFIRRetryActive = true
	}
	id := s.ID
	s.mu.Unlock()
	s.signalProactivePLIWake()
	if retry {
		fmt.Printf("[%s] sip_info_fir deferred retry_in=%s\n", id, wait.Round(time.Millisecond))
		if gen != 0 {
			go s.runSIPInfoFIRRetry(gen, now, wait)
		}
		return true
	}
	s.SendBrowserPLIImmediate("sip_info_fir")
	return true
}

// SIPInfoFIRRetryWait is the delay before the first retry when fir landed
// inside Chrome's post-IDR window. The retry itself is one IDR request and
// still has to pass BrowserIDRRequestMinGap.
func SIPInfoFIRRetryWait(lastIDR, fir time.Time) (time.Duration, bool) {
	if lastIDR.IsZero() || !fir.After(lastIDR) {
		return 0, false
	}
	since := fir.Sub(lastIDR)
	if since >= BrowserIDRRequestMinGap {
		return 0, false
	}
	return BrowserIDRRequestMinGap - since, true
}

func browserIDRRequestAllowed(last, now time.Time) bool {
	if last.IsZero() {
		return true
	}
	return now.Sub(last) >= BrowserIDRRequestMinGap
}

func (s *Session) runSIPInfoFIRRetry(gen uint64, firAt time.Time, firstWait time.Duration) {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		s.mu.Lock()
		if s.sipInfoFIRRetryGen == gen {
			s.sipInfoFIRRetryActive = false
		}
		s.mu.Unlock()
	}()
	timer := time.NewTimer(firstWait)
	defer timer.Stop()
	for n := 0; n < SIPInfoFIRRetryCap; n++ {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		send, reason := s.sipInfoFIRRetryShouldSend(gen, firAt, time.Now())
		if !send {
			if reason == "idr-forwarded" {
				fmt.Printf("[%s] sip_info_fir_retry cancelled reason=%s\n", s.ID, reason)
			}
			return
		}
		s.SendBrowserPLIImmediate("sip_info_fir_retry")
		timer.Reset(SIPInfoFIRRetryGap())
	}
}

// sipInfoFIRRetryShouldSend is the fake-clock gate for one VFU retry. An IDR
// forwarded after firAt, including leftover slices of the picture that was
// already in flight, cancels the retry.
func (s *Session) sipInfoFIRRetryShouldSend(gen uint64, firAt, now time.Time) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sipInfoFIRRetryGen != gen {
		return false, "stale"
	}
	lastIDR := s.lastForwardedIDRTimeLocked()
	if IDRSatisfiesKeyframeRequest(lastIDR, firAt) || !s.switchKeyframeIDRPending {
		return false, "idr-forwarded"
	}
	_ = now
	return true, ""
}

// SIPInfoFIRRetryGap is the spacing between retry PLIs after the first one
// has waited out Chrome's post-IDR window.
func SIPInfoFIRRetryGap() time.Duration {
	return BrowserIDRRequestMinGap
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
	now := time.Now()
	if !browserIDRRequestAllowed(s.lastBrowserIDRRequest, now) {
		id := s.ID
		s.mu.Unlock()
		fmt.Printf("[%s] browser_pli coalesced reason=%s\n", id, reason)
		return false
	}
	s.lastBrowserPLIReason = reason
	saved := s.LastWebRTCPLISent
	s.LastWebRTCPLISent = time.Time{}
	snapshot := s.prepareWebRTCFeedbackLocked("pli", true, nil)
	if snapshot.ready && !snapshot.throttled {
		s.lastBrowserIDRRequest = now
	}
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
	now := time.Now()
	if !browserIDRRequestAllowed(s.lastBrowserIDRRequest, now) {
		id := s.ID
		s.mu.Unlock()
		fmt.Printf("[%s] browser_pli coalesced reason=%s\n", id, reason)
		return false
	}
	s.lastBrowserPLIReason = reason
	snapshot := s.prepareWebRTCFeedbackLocked("pli", true, nil)
	if snapshot.ready && !snapshot.throttled {
		s.lastBrowserIDRRequest = now
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
	if isIDR {
		if s.LastUplinkKeyframe.Before(now) {
			s.LastUplinkKeyframe = now
		}
	}
	if isIDR && s.switchKeyframeIDRPending {
		logAfterSwitch = true
		switchLatency = now.Sub(s.switchKeyframeRequestedAt)
		s.switchKeyframeIDRPending = false
		s.allowNextUplinkPictureSize = false
		s.switchKeyframeBurstGen++
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
