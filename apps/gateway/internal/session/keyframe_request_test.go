package session

import (
	"testing"
	"time"
)

func TestDecidePeriodicBrowserPLI(t *testing.T) {
	now := time.Now()
	deadline := now.Add(PeriodicBrowserPLISafetyNet)

	tests := []struct {
		name   string
		hasIDR bool
		now    time.Time
		stop   bool
		reason string
	}{
		{name: "continue before idr", hasIDR: false, now: now, stop: false},
		{name: "stop on uplink idr", hasIDR: true, now: now, stop: true, reason: "uplink-idr-forwarded"},
		{name: "idr wins even after deadline", hasIDR: true, now: deadline.Add(time.Second), stop: true, reason: "uplink-idr-forwarded"},
		{name: "safety net at deadline", hasIDR: false, now: deadline, stop: true, reason: "no-uplink-idr"},
		{name: "safety net after deadline", hasIDR: false, now: deadline.Add(time.Millisecond), stop: true, reason: "no-uplink-idr"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stop, reason := DecidePeriodicBrowserPLI(tc.hasIDR, tc.now, deadline)
			if stop != tc.stop || reason != tc.reason {
				t.Fatalf("DecidePeriodicBrowserPLI() = (%v, %q), want (%v, %q)", stop, reason, tc.stop, tc.reason)
			}
		})
	}
}

func TestAllowSIPOriginatedBrowserKeyframe(t *testing.T) {
	sess := newBurstTestSession("sip-keyframe-gate")
	allow, reason := sess.AllowSIPOriginatedBrowserKeyframe("pli")
	if !allow || reason != "" {
		t.Fatalf("fresh session = (%v, %q), want allow", allow, reason)
	}

	sess.RecordUplinkKeyframe()
	allow, reason = sess.AllowSIPOriginatedBrowserKeyframe("pli")
	if allow || reason != "uplink-idr-fresh" {
		t.Fatalf("fresh uplink IDR = (%v, %q), want uplink-idr-fresh", allow, reason)
	}

	sess.LastUplinkKeyframe = time.Now().Add(-sipOriginatedBrowserKeyframeMinInterval - time.Millisecond)
	sess.LastWebRTCPLISent = time.Now()
	allow, reason = sess.AllowSIPOriginatedBrowserKeyframe("fir")
	if allow || reason != "rate-limit" {
		t.Fatalf("recent PLI = (%v, %q), want rate-limit", allow, reason)
	}

	sess.LastWebRTCPLISent = time.Now().Add(-sipOriginatedBrowserKeyframeMinInterval - time.Millisecond)
	sess.LastWebRTCFIRSent = time.Now()
	allow, reason = sess.AllowSIPOriginatedBrowserKeyframe("pli")
	if allow || reason != "rate-limit" {
		t.Fatalf("recent FIR = (%v, %q), want rate-limit", allow, reason)
	}

	sess.LastWebRTCFIRSent = time.Now().Add(-sipOriginatedBrowserKeyframeMinInterval - time.Millisecond)
	allow, reason = sess.AllowSIPOriginatedBrowserKeyframe("pli")
	if !allow || reason != "" {
		t.Fatalf("stale request and IDR = (%v, %q), want allow", allow, reason)
	}
}

func TestImplicitSwitchKicksOnceWithoutSwitchMessage(t *testing.T) {
	sess := newBurstTestSession("implicit-switch-once")
	now := time.Now()
	if !sess.TryKickUplinkKeyframeForSwitch("implicit-switch", now) {
		t.Fatal("expected one implicit-switch kick")
	}
	if sess.TryKickUplinkKeyframeForSwitch("implicit-switch", now.Add(10*time.Millisecond)) {
		t.Fatal("expected a second implicit-switch inside the window to be skipped")
	}
}

func TestDiscontinuityAndSwitchKickOnce(t *testing.T) {
	now := time.Now()
	orders := [][2]string{
		{"implicit-switch", "switch"},
		{"switch", "implicit-switch"},
	}
	for _, order := range orders {
		sess := newBurstTestSession(order[0] + "-then-" + order[1])
		if !sess.TryKickUplinkKeyframeForSwitch(order[0], now) {
			t.Fatalf("%s: expected the first kick", order[0])
		}
		if sess.TryKickUplinkKeyframeForSwitch(order[1], now.Add(videoDiscontinuityIDRCredit)) {
			t.Fatalf("%s then %s at 150ms: expected a single kick", order[0], order[1])
		}
		if !sess.TryKickUplinkKeyframeForSwitch(order[1], now.Add(videoDiscontinuityIDRCredit+time.Millisecond)) {
			t.Fatalf("%s: expected a kick after the dedupe window", order[1])
		}
	}
}

func TestFirstPostSwitchSIPPLIIsForwarded(t *testing.T) {
	sess := newBurstTestSession("post-switch-sip-pli")
	sess.RecordUplinkKeyframe()
	sess.LastWebRTCPLISent = time.Now()
	now := time.Now()
	if !sess.TryKickUplinkKeyframeForSwitch("implicit-switch", now) {
		t.Fatal("expected implicit-switch kick")
	}
	if sess.TryKickUplinkKeyframeForSwitch("switch", now.Add(20*time.Millisecond)) {
		t.Fatal("expected @switch inside the window to be skipped")
	}

	allow, reason := sess.AllowSIPOriginatedBrowserKeyframe("pli")
	if !allow || reason != "" {
		t.Fatalf("first post-switch SIP PLI = (%v, %q), want forward", allow, reason)
	}
	allow, reason = sess.AllowSIPOriginatedBrowserKeyframe("fir")
	if allow || reason != "uplink-idr-fresh" {
		t.Fatalf("second post-switch SIP FIR = (%v, %q), want uplink-idr-fresh", allow, reason)
	}
}
