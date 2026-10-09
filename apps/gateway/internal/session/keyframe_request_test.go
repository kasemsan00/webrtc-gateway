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
