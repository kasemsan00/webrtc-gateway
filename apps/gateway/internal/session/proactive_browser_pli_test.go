package session

import (
	"context"
	"testing"
	"time"
)

func TestDecideProactiveBrowserPLIStopsAfterFirstIDR(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	initial := 2 * time.Second
	safety := 10 * time.Second

	startup := DecideProactiveBrowserPLI(time.Time{}, time.Time{}, now, initial, safety)
	if !startup.Send || startup.Reason != "startup" {
		t.Fatalf("no IDR: %+v", startup)
	}
	waited := DecideProactiveBrowserPLI(time.Time{}, now.Add(-time.Second), now, initial, safety)
	if waited.Send || waited.Reason != "startup-wait" {
		t.Fatalf("startup PLI 1s ago: %+v", waited)
	}
	fresh := DecideProactiveBrowserPLI(now, time.Time{}, now.Add(4*time.Second), initial, safety)
	if fresh.Send || fresh.Reason != "idr-fresh" {
		t.Fatalf("IDR 4s ago still sent a 4s cadence PLI: %+v", fresh)
	}
	net := DecideProactiveBrowserPLI(now, time.Time{}, now.Add(safety), initial, safety)
	if !net.Send || net.Reason != "safety-net" {
		t.Fatalf("IDR 10s ago: %+v", net)
	}
	waiting := DecideProactiveBrowserPLI(now, now.Add(safety), now.Add(safety+time.Second), initial, safety)
	if waiting.Send || waiting.Reason != "safety-wait" {
		t.Fatalf("safety PLI 1s ago: %+v", waiting)
	}
	if ProactiveBrowserPLIInitialInterval != 2*time.Second ||
		ProactiveBrowserPLISafetyInterval != 10*time.Second ||
		BrowserKeyframeSatisfyWindow != time.Second {
		t.Fatal("intervals drifted from 2s startup, 10s safety, 1s dedupe")
	}
}

func TestProactiveBrowserPLINotGatedByRTPFlowing(t *testing.T) {
	now := time.Now()
	decision := DecideKeyframeWatchdog(KeyframeWatchdogInput{
		Now:          now,
		LastKeyframe: now.Add(-6 * time.Second),
		LastRTP:      now,
		Stale:        4 * time.Second,
		FIRStale:     8 * time.Second,
		Interval:     2 * time.Second,
		HealthyIDR:   true,
	})
	if decision.Action != KeyframeWatchdogNone || decision.Reason != "rtp-flowing" {
		t.Fatalf("watchdog = %+v, want rtp-flowing skip", decision)
	}
	got := DecideProactiveBrowserPLI(time.Time{}, time.Time{}, now, ProactiveBrowserPLIInitialInterval, ProactiveBrowserPLISafetyInterval)
	if !got.Send || got.Reason != "startup" {
		t.Fatalf("proactive startup skipped while rtp is flowing: %+v", got)
	}
}

func TestWebRTCPLIThrottle(t *testing.T) {
	now := time.Now()
	last := now.Add(-200 * time.Millisecond)
	if !webrtcPLIThrottled(false, last, now) {
		t.Fatal("unforced PLI inside 400ms must stay throttled")
	}
	if !webrtcPLIThrottled(true, last, now) {
		t.Fatal("forced PLI inside 1s must stay throttled")
	}
	last = now.Add(-500 * time.Millisecond)
	if webrtcPLIThrottled(false, last, now) {
		t.Fatal("unforced PLI after 400ms is a real SIP forward and must be allowed")
	}
	if !webrtcPLIThrottled(true, last, now) {
		t.Fatal("forced PLI at 500ms is still inside the 1s overall cap")
	}
	last = now.Add(-webrtcPLIOverallMinInterval)
	if webrtcPLIThrottled(true, last, now) {
		t.Fatal("forced PLI at 1s is the proactive slot and must be allowed")
	}
	if webrtcPLIThrottled(true, time.Time{}, now) {
		t.Fatal("first PLI must be allowed")
	}
}

func TestSwitchKeyframeDoesNotRestartProactiveTwoSecondTimer(t *testing.T) {
	sess := &Session{ID: "switch-pli"}
	now := time.Unix(1_700_000_000, 0)
	sess.LastUplinkKeyframe = now.Add(-3 * time.Second)
	sess.mu.Lock()
	sess.armSwitchKeyframeRequestLocked(now)
	sess.mu.Unlock()
	if !sess.switchKeyframeIDRPending || !sess.allowNextUplinkPictureSize {
		t.Fatal("switch request did not arm the next uplink IDR")
	}
	if got := sess.ProactiveBrowserPLIWait(now); got != 7*time.Second {
		t.Fatalf("wait after switch = %s, want remaining 7s safety net, not a 2s restart", got)
	}
	later := now.Add(180 * time.Millisecond)
	sess.ObserveUplinkVideoForward(true, 55, later)
	if sess.switchKeyframeIDRPending || sess.allowNextUplinkPictureSize {
		t.Fatal("forwarded IDR left the switch arm set")
	}
	if got := sess.ProactiveBrowserPLIWait(later); got != ProactiveBrowserPLISafetyInterval {
		t.Fatalf("wait after IDR = %s, want 10s safety net", got)
	}
}

func TestSIPInfoFIRDebounceCollapsesRepeats(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if SIPInfoFIRDebounced(time.Time{}, now) {
		t.Fatal("first VFU must not be debounced")
	}
	if !SIPInfoFIRDebounced(now, now.Add(249*time.Millisecond)) {
		t.Fatal("repeat inside 250ms must be debounced")
	}
	if SIPInfoFIRDebounced(now, now.Add(SIPInfoFIRDebounce)) {
		t.Fatal("repeat at 250ms is a new VFU")
	}
	sess := &Session{ID: "info-debounce"}
	if !sess.HandleSIPInfoPictureFastUpdate(now) {
		t.Fatal("first INFO was debounced")
	}
	if sess.HandleSIPInfoPictureFastUpdate(now.Add(100 * time.Millisecond)) {
		t.Fatal("repeat INFO sent another PLI")
	}
	if sess.LastBrowserPLIReason() != "sip_info_fir" {
		t.Fatalf("reason %q", sess.LastBrowserPLIReason())
	}
	if !sess.HandleSIPInfoPictureFastUpdate(now.Add(SIPInfoFIRDebounce)) {
		t.Fatal("INFO after the debounce window was ignored")
	}
}

func TestSIPInfoFIRRetryWait(t *testing.T) {
	idr := time.Unix(1_700_000_000, 0)
	wait, ok := SIPInfoFIRRetryWait(idr, idr.Add(200*time.Millisecond))
	if !ok || wait != 300*time.Millisecond {
		t.Fatalf("wait=%s ok=%v, want 300ms", wait, ok)
	}
	if _, ok = SIPInfoFIRRetryWait(idr, idr.Add(3*time.Second)); ok {
		t.Fatal("FIR well after the IDR must not be retried")
	}
	if _, ok = SIPInfoFIRRetryWait(idr, idr.Add(BrowserIDRRequestMinGap)); ok {
		t.Fatal("FIR at the end of the window must not be retried")
	}
	if _, ok = SIPInfoFIRRetryWait(time.Time{}, idr); ok {
		t.Fatal("FIR before any IDR must not be retried")
	}
}

func TestBrowserIDRRequestGapIsShared(t *testing.T) {
	now := time.Unix(50, 0)
	if !browserIDRRequestAllowed(time.Time{}, now) {
		t.Fatal("first request was coalesced")
	}
	if browserIDRRequestAllowed(now, now.Add(499*time.Millisecond)) {
		t.Fatal("request inside 500ms was allowed")
	}
	if !browserIDRRequestAllowed(now, now.Add(BrowserIDRRequestMinGap)) {
		t.Fatal("request at 500ms was coalesced")
	}
}

func TestPLISourcesCoalesceToOneRequest(t *testing.T) {
	sess := &Session{ID: "coalesce"}
	sess.lastBrowserIDRRequest = time.Now()
	if sess.SendBrowserPLIImmediate("sip_info_fir") ||
		sess.SendBrowserPLI("switch") ||
		sess.SendProactivePLIToWebRTC() ||
		sess.SendBrowserPLIImmediate("mode0_drop") ||
		sess.SendBrowserPLIImmediate("sip_info_fir_retry") {
		t.Fatal("a second IDR request inside 500ms was sent")
	}
	if sess.LastBrowserPLIReason() != "" {
		t.Fatalf("coalesced request recorded reason %q", sess.LastBrowserPLIReason())
	}
}

func TestBrowserKeyframeRequestSatisfied(t *testing.T) {
	now := time.Unix(80, 0)
	if skip, _ := BrowserKeyframeRequestSatisfied(time.Time{}, time.Time{}, now); skip {
		t.Fatal("empty clocks were satisfied")
	}
	if skip, reason := BrowserKeyframeRequestSatisfied(now.Add(-999*time.Millisecond), time.Time{}, now); !skip || reason != "idr-fresh" {
		t.Fatalf("IDR 999ms ago: skip=%v reason=%s", skip, reason)
	}
	if skip, _ := BrowserKeyframeRequestSatisfied(now.Add(-BrowserKeyframeSatisfyWindow), time.Time{}, now); skip {
		t.Fatal("IDR at 1s was still treated as fresh")
	}
	if skip, reason := BrowserKeyframeRequestSatisfied(time.Time{}, now.Add(-200*time.Millisecond), now); !skip || reason != "pli-pending" {
		t.Fatalf("pending PLI: skip=%v reason=%s", skip, reason)
	}
	if skip, _ := BrowserKeyframeRequestSatisfied(now.Add(-50*time.Millisecond), now.Add(-200*time.Millisecond), now); !skip {
		t.Fatal("IDR after the pending PLI was not fresh")
	}
}

func TestIDRSatisfiesKeyframeRequest(t *testing.T) {
	fir := time.Unix(90, 0)
	if IDRSatisfiesKeyframeRequest(time.Time{}, fir) {
		t.Fatal("no IDR satisfied the FIR")
	}
	if IDRSatisfiesKeyframeRequest(fir.Add(-time.Millisecond), fir) {
		t.Fatal("IDR before the FIR cancelled the retry")
	}
	if !IDRSatisfiesKeyframeRequest(fir, fir) {
		t.Fatal("IDR at FIR time must cancel the retry")
	}
	if !IDRSatisfiesKeyframeRequest(fir.Add(5*time.Millisecond), fir) {
		t.Fatal("IDR 5ms after FIR must cancel the retry")
	}
}

func TestSIPInfoFIRRetryCancelsWhenIDRSliceIsForwarded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := &Session{ID: "fir-cancel", ctx: ctx}
	now := time.Unix(50, 0)
	sess.uplinkVideoLastIDR = now
	sess.uplinkVideoIDRTimestampSeen = true
	sess.uplinkVideoLastIDRTimestamp = 111
	sess.LastUplinkKeyframe = now
	fir := now.Add(200 * time.Millisecond)
	if !sess.HandleSIPInfoPictureFastUpdate(fir) {
		t.Fatal("FIR inside the window was ignored")
	}
	send, reason := sess.sipInfoFIRRetryShouldSend(1, fir, fir.Add(300*time.Millisecond))
	if !send {
		t.Fatalf("retry skipped before any post-FIR IDR, reason=%s", reason)
	}
	sess.ObserveUplinkVideoForward(true, 111, fir.Add(5*time.Millisecond))
	send, reason = sess.sipInfoFIRRetryShouldSend(1, fir, fir.Add(400*time.Millisecond))
	if send || reason != "idr-forwarded" {
		t.Fatalf("retry after leftover IDR slice: send=%v reason=%s", send, reason)
	}
}

func TestSwitchKeyframeFollowUpStopsAfterIDR(t *testing.T) {
	sess := &Session{ID: "switch-stop"}
	now := time.Unix(1_700_000_000, 0)
	sess.mu.Lock()
	gen := sess.armSwitchKeyframeRequestLocked(now)
	sess.mu.Unlock()
	if stop, reason := sess.switchKeyframeBurstShouldStop(gen, false, now); stop {
		t.Fatalf("immediate switch PLI stopped: %s", reason)
	}
	sess.ObserveUplinkVideoForward(true, 9, now.Add(50*time.Millisecond))
	stop, reason := sess.switchKeyframeBurstShouldStop(gen, true, now.Add(time.Second))
	if !stop || (reason != "superseded" && reason != "idr-forwarded") {
		t.Fatalf("+1s after IDR: stop=%v reason=%s", stop, reason)
	}
	sess.mu.Lock()
	gen2 := sess.switchKeyframeBurstGen
	sess.mu.Unlock()
	if gen2 == gen {
		t.Fatal("forwarded IDR did not cancel the switch burst generation")
	}
}

func TestSwitchKeyframeFirstShotSatisfiedByFreshIDR(t *testing.T) {
	sess := &Session{ID: "switch-fresh"}
	now := time.Unix(1_700_000_100, 0)
	sess.LastUplinkKeyframe = now.Add(-400 * time.Millisecond)
	sess.mu.Lock()
	gen := sess.armSwitchKeyframeRequestLocked(now)
	sess.mu.Unlock()
	stop, reason := sess.switchKeyframeBurstShouldStop(gen, false, now)
	if !stop || reason != "idr-fresh" {
		t.Fatalf("switch after a 400ms-old IDR: stop=%v reason=%s", stop, reason)
	}
}

func TestSwitchKeyframeFirstShotSatisfiedByPendingPLI(t *testing.T) {
	sess := &Session{ID: "switch-pending"}
	now := time.Unix(1_700_000_200, 0)
	sess.lastBrowserIDRRequest = now.Add(-80 * time.Millisecond)
	sess.mu.Lock()
	gen := sess.armSwitchKeyframeRequestLocked(now)
	sess.mu.Unlock()
	stop, reason := sess.switchKeyframeBurstShouldStop(gen, false, now)
	if !stop || reason != "pli-pending" {
		t.Fatalf("switch while PLI pending: stop=%v reason=%s", stop, reason)
	}
}

func TestFIRDoesNotRestartProactiveTwoSecondTimer(t *testing.T) {
	sess := &Session{ID: "fir-proactive"}
	now := time.Unix(10, 0)
	sess.LastUplinkKeyframe = now
	sess.uplinkVideoLastIDR = now
	sess.uplinkVideoIDRTimestampSeen = true
	if !sess.HandleSIPInfoPictureFastUpdate(now.Add(3 * time.Second)) {
		t.Fatal("late FIR was ignored")
	}
	wait := sess.ProactiveBrowserPLIWait(now.Add(3 * time.Second))
	if wait < 6*time.Second || wait > 8*time.Second {
		t.Fatalf("FIR restarted proactive wait to %s, want remaining safety net", wait)
	}
}

func TestSIPInfoFIRInsideKeyframeWindowDefersPLI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := &Session{ID: "fir-retry", ctx: ctx}
	fir := time.Now()
	sess.uplinkVideoLastIDR = fir.Add(-200 * time.Millisecond)
	sess.uplinkVideoIDRTimestampSeen = true
	sess.uplinkVideoLastIDRTimestamp = 111
	if !sess.HandleSIPInfoPictureFastUpdate(fir) {
		t.Fatal("FIR inside the window was ignored")
	}
	sess.mu.Lock()
	active := sess.sipInfoFIRRetryActive
	gen := sess.sipInfoFIRRetryGen
	logged := sess.firIDRSliceLog
	sess.mu.Unlock()
	if !active || gen != 1 || !logged {
		t.Fatalf("active=%v gen=%d sliceLog=%v", active, gen, logged)
	}
	if sess.LastBrowserPLIReason() == "sip_info_fir" {
		t.Fatal("immediate PLI was sent inside Chrome's ignore window")
	}
	if sess.HandleSIPInfoPictureFastUpdate(fir.Add(100 * time.Millisecond)) {
		t.Fatal("repeat FIR started another retry")
	}
	sess.mu.Lock()
	if sess.sipInfoFIRRetryGen != 1 {
		t.Fatalf("retry gen=%d, want 1", sess.sipInfoFIRRetryGen)
	}
	sess.mu.Unlock()
	sess.ResetMediaState()
}

func TestSIPInfoFIRAfterKeyframeWindowSendsImmediately(t *testing.T) {
	sess := &Session{ID: "fir-late"}
	fir := time.Now()
	sess.uplinkVideoLastIDR = fir.Add(-3 * time.Second)
	sess.uplinkVideoIDRTimestampSeen = true
	if !sess.HandleSIPInfoPictureFastUpdate(fir) {
		t.Fatal("late FIR was ignored")
	}
	if sess.LastBrowserPLIReason() != "sip_info_fir" {
		t.Fatalf("reason %q, want sip_info_fir", sess.LastBrowserPLIReason())
	}
	sess.mu.Lock()
	active := sess.sipInfoFIRRetryActive
	sess.mu.Unlock()
	if active {
		t.Fatal("late FIR armed a retry")
	}
}

func TestSwitchBrowserPLIRespectsOneSecondCap(t *testing.T) {
	sess := &Session{ID: "switch-cap"}
	sess.LastWebRTCPLISent = time.Now()
	if sess.SendBrowserPLI("switch") {
		t.Fatal("switch PLI inside the 1s cap was reported sent")
	}
	sess.LastWebRTCPLISent = time.Now().Add(-500 * time.Millisecond)
	if sess.SendBrowserPLI("switch") {
		t.Fatal("switch PLI at 500ms still has to honor the 1s cap")
	}
	sess.LastWebRTCPLISent = time.Time{}
	if sess.SendBrowserPLI("switch") {
		t.Fatal("switch PLI without a peer connection was reported sent")
	}
}

func TestSendProactivePLIWithoutPeerConnection(t *testing.T) {
	sess := &Session{ID: "call"}
	if sess.SendProactivePLIToWebRTC() {
		t.Fatal("proactive PLI reported sent without a peer connection")
	}
	sess.LastWebRTCPLISent = time.Now()
	if sess.SendProactivePLIToWebRTC() {
		t.Fatal("proactive PLI reported sent inside the 1s cap")
	}
}

func TestNoteSIPKeyframeRequest(t *testing.T) {
	sess := &Session{ID: "call"}
	if sess.HasSIPKeyframeRequest() {
		t.Fatal("new session already saw a SIP keyframe request")
	}
	sess.NoteSIPKeyframeRequest("pli")
	if !sess.HasSIPKeyframeRequest() {
		t.Fatal("PLI was not recorded")
	}
	sess.NoteSIPKeyframeRequest("fir")
	if !sess.HasSIPKeyframeRequest() {
		t.Fatal("later FIR cleared the flag")
	}
}

func TestObserveUplinkVideoForward(t *testing.T) {
	sess := &Session{ID: "call"}
	start := time.Now()
	sess.ObserveUplinkVideoForward(true, 1000, start)
	if !sess.uplinkVideoIDRTimestampSeen || sess.uplinkVideoPackets != 1 {
		t.Fatalf("first IDR was not counted: seen=%v packets=%d", sess.uplinkVideoIDRTimestampSeen, sess.uplinkVideoPackets)
	}
	sess.ObserveUplinkVideoForward(true, 1000, start.Add(20*time.Millisecond))
	if sess.uplinkVideoPackets != 2 {
		t.Fatalf("same-timestamp IDR slice was not counted: %d", sess.uplinkVideoPackets)
	}
	if !sess.uplinkVideoLastIDR.Equal(start) {
		t.Fatal("same-timestamp slice moved the IDR clock")
	}
	sess.ObserveUplinkVideoForward(false, 2000, start.Add(time.Second))
	if sess.uplinkVideoPackets != 0 || sess.lastUplinkPacketsPerSec == 0 {
		t.Fatalf("one-second window did not roll: packets=%d rate=%d", sess.uplinkVideoPackets, sess.lastUplinkPacketsPerSec)
	}
	second := start.Add(4800 * time.Millisecond)
	sess.ObserveUplinkVideoForward(true, 3000, second)
	gap := second.Sub(sess.uplinkVideoLastIDR)
	if gap != 0 {
		t.Fatalf("IDR clock was not updated: gap=%s", gap)
	}
	if !sess.uplinkVideoLastIDR.Equal(second) {
		t.Fatalf("second IDR at %s, clock %s", second, sess.uplinkVideoLastIDR)
	}
}

func TestMode0DropPLIIsOnePerIDRWait(t *testing.T) {
	sess := &Session{ID: "mode0"}
	now := time.Unix(1_700_000_100, 0)
	if !sess.NoteMode0Drop(now, "discontinuous-fua", 0) {
		t.Fatal("first discontinuous FU-A did not request a PLI")
	}
	if sess.NoteMode0Drop(now.Add(260*time.Millisecond), "discontinuous-fua", 0) {
		t.Fatal("second drop inside the IDR wait requested another PLI")
	}
	if sess.NoteMode0Drop(now.Add(520*time.Millisecond), "malformed-fua", 0) {
		t.Fatal("non-frame reason requested a PLI")
	}
	if sess.LastBrowserPLIReason() != "mode0_drop" {
		t.Fatalf("PLI reason %q", sess.LastBrowserPLIReason())
	}
	if !sess.UplinkHeldForMode0Recovery(false) {
		t.Fatal("P-frame was forwarded while the broken frame was outstanding")
	}
	if !sess.NoteMode0Drop(now.Add(Mode0DropPLIRetry), "discontinuous-fua", 0) {
		t.Fatal("retry after the IDR timeout was ignored")
	}
	if sess.UplinkHeldForMode0Recovery(true) {
		t.Fatal("IDR was held after mode-0 recovery")
	}
	if sess.UplinkHeldForMode0Recovery(false) {
		t.Fatal("P-frame stayed held after the IDR")
	}
	if !sess.NoteMode0Drop(now.Add(Mode0DropPLIRetry+time.Millisecond), "oversized-fua", 90000) {
		t.Fatal("a new broken frame after the IDR did not request a PLI")
	}
}

func TestUplinkVideoStalledLine(t *testing.T) {
	line := formatUplinkVideoStalled(1400*time.Millisecond, 270, "discontinuous-fua")
	if line != "uplink_video_stalled since=1.4s browser_rtp_per_s=270 reason=discontinuous-fua" {
		t.Fatalf("stall line %q", line)
	}
}
