package session

import (
	"testing"
	"time"
)

func TestProactiveBrowserPLIInterval(t *testing.T) {
	if got := ProactiveBrowserPLIInterval(0); got != ProactiveBrowserPLIInitialInterval {
		t.Fatalf("age 0: got %s", got)
	}
	if got := ProactiveBrowserPLIInterval(9 * time.Second); got != ProactiveBrowserPLIInitialInterval {
		t.Fatalf("age 9s: got %s", got)
	}
	if got := ProactiveBrowserPLIInterval(ProactiveBrowserPLIInitialWindow); got != ProactiveBrowserPLISteadyInterval {
		t.Fatalf("age 10s: got %s", got)
	}
	if got := ProactiveBrowserPLIInterval(30 * time.Second); got != ProactiveBrowserPLISteadyInterval {
		t.Fatalf("age 30s: got %s", got)
	}
	if ProactiveBrowserPLIInitialInterval != 2*time.Second ||
		ProactiveBrowserPLIInitialWindow != 10*time.Second ||
		ProactiveBrowserPLISteadyInterval != 4*time.Second {
		t.Fatal("intervals drifted from 2s for 10s, then 4s")
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
	if got := ProactiveBrowserPLIInterval(2 * time.Second); got != ProactiveBrowserPLIInitialInterval {
		t.Fatalf("proactive interval changed while rtp is flowing: %s", got)
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
