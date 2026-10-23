package session

import (
	"context"
	"testing"
	"time"
)

func TestDecideUplinkKeyframeRequestStartup(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	send, reason := DecideUplinkKeyframeRequest("startup", true, time.Time{}, time.Time{}, now)
	if send || reason != "idr-in-flight" {
		t.Fatalf("in-flight: send=%v reason=%s", send, reason)
	}
	complete := now.Add(-1499 * time.Millisecond)
	send, reason = DecideUplinkKeyframeRequest("startup", false, complete, time.Time{}, now)
	if send || reason != "idr-fresh" {
		t.Fatalf("1.499s after complete: send=%v reason=%s", send, reason)
	}
	send, reason = DecideUplinkKeyframeRequest("startup", false, now.Add(-StartupUplinkKeyframeFresh), time.Time{}, now)
	if !send || reason != "" {
		t.Fatalf("1.5s after complete: send=%v reason=%s", send, reason)
	}
	send, reason = DecideUplinkKeyframeRequest("periodic", false, time.Time{}, time.Time{}, now)
	if !send {
		t.Fatalf("no IDR yet: send=%v reason=%s", send, reason)
	}
}

func TestDecideUplinkKeyframeRequestDecoderJoin(t *testing.T) {
	now := time.Unix(1_800_000_100, 0)
	complete := now.Add(-3 * time.Second)
	for _, kind := range []string{"sip-rtcp-first", "sip-video-dest-ready", "remote-ssrc-learn", "sip-video-rtp-learn"} {
		send, reason := DecideUplinkKeyframeRequest(kind, false, complete, time.Time{}, now)
		if send || reason != "idr-fresh" {
			t.Fatalf("%s 3s after IDR: send=%v reason=%s", kind, send, reason)
		}
	}
	send, reason := DecideUplinkKeyframeRequest("sip-rtcp-first", false, now.Add(-DecoderJoinUplinkKeyframeFresh), time.Time{}, now)
	if !send || reason != "" {
		t.Fatalf("10s after IDR: send=%v reason=%s", send, reason)
	}
	send, reason = DecideUplinkKeyframeRequest("sip-rtcp-first", true, time.Time{}, time.Time{}, now)
	if send || reason != "idr-in-flight" {
		t.Fatalf("in-flight join kick: send=%v reason=%s", send, reason)
	}
}

func TestDecideUplinkKeyframeRequestSwitchAlwaysSends(t *testing.T) {
	now := time.Unix(1_800_000_200, 0)
	send, reason := DecideUplinkKeyframeRequest("switch", true, now, now, now)
	if !send || reason != "" {
		t.Fatalf("switch in-flight: send=%v reason=%s", send, reason)
	}
	send, reason = DecideUplinkKeyframeRequest("implicit-switch", false, now.Add(-time.Second), now, now)
	if !send {
		t.Fatal("implicit-switch was skipped")
	}
}

func TestDecideUplinkKeyframeRequestSIPCoalesce(t *testing.T) {
	now := time.Unix(1_800_000_300, 0)
	send, reason := DecideUplinkKeyframeRequest("sip_info_fir", true, time.Time{}, time.Time{}, now)
	if send || reason != "idr-in-flight" {
		t.Fatalf("SIP INFO in-flight: send=%v reason=%s", send, reason)
	}
	send, reason = DecideUplinkKeyframeRequest("sip_info_fir", false, now.Add(-3*time.Second), now.Add(-200*time.Millisecond), now)
	if send || reason != "coalesced" {
		t.Fatalf("SIP INFO 200ms after PLI: send=%v reason=%s", send, reason)
	}
	send, reason = DecideUplinkKeyframeRequest("sip_info_fir", false, now.Add(-3*time.Second), now.Add(-SIPKeyframePLICoalesce), now)
	if !send || reason != "" {
		t.Fatalf("SIP INFO at 1s: send=%v reason=%s", send, reason)
	}
	send, reason = DecideUplinkKeyframeRequest("fir", false, now.Add(-8*time.Second), time.Time{}, now)
	if !send {
		t.Fatal("SIP FIR with a stale IDR was skipped")
	}
}

func TestKickUplinkKeyframeSkipsRecentCompleteIDR(t *testing.T) {
	sess := &Session{ID: "kick-skip"}
	now := time.Now()
	sess.uplinkIDRCompleteAt = now.Add(-4 * time.Second)
	if sess.KickUplinkKeyframeForSIPDecoder("sip-rtcp-first") {
		t.Fatal("sip-rtcp-first ran with a 4s-old complete IDR")
	}
	if sess.KickUplinkKeyframeForSIPDecoder("sip-video-dest-ready") {
		t.Fatal("dest-ready ran with a 4s-old complete IDR")
	}
	if sess.KickUplinkKeyframeForSIPDecoder("remote-ssrc-learn") {
		t.Fatal("ssrc-learn ran with a 4s-old complete IDR")
	}
	if !sess.KickUplinkKeyframeForSIPDecoder("switch") {
		t.Fatal("switch kick was skipped")
	}
}

func TestStartupPLIStopsOnceIDRIsInFlight(t *testing.T) {
	sess := &Session{ID: "startup-stop"}
	if sess.ShouldStopStartupBrowserPLI() {
		t.Fatal("empty session stopped startup PLI")
	}
	sess.ObserveUplinkVideoForwardMarked(true, 9, time.Unix(50, 0), false)
	if !sess.UplinkIDRInFlight() {
		t.Fatal("first IDR slice did not mark in-flight")
	}
	if !sess.ShouldStopStartupBrowserPLI() {
		t.Fatal("startup PLI continued while an IDR was in flight")
	}
	sess.ObserveUplinkVideoForwardMarked(true, 9, time.Unix(50, 0).Add(80*time.Millisecond), true)
	if sess.UplinkIDRInFlight() || !sess.HasCompleteUplinkIDR() {
		t.Fatal("marker slice did not complete the IDR")
	}
	skip, reason := sess.SkipUplinkKeyframeRequest("startup", time.Unix(50, 0).Add(time.Second))
	if !skip || reason != "idr-fresh" {
		t.Fatalf("startup 1s after complete: skip=%v reason=%s", skip, reason)
	}
}

func TestSIPInfoWaitsForInFlightIDR(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := &Session{ID: "info-wait", ctx: ctx}
	now := time.Unix(60, 0)
	sess.ObserveUplinkVideoForwardMarked(true, 3, now, false)
	if !sess.HandleSIPInfoPictureFastUpdate(now.Add(20 * time.Millisecond)) {
		t.Fatal("INFO during in-flight IDR was dropped")
	}
	if sess.LastBrowserPLIReason() == "sip_info_fir" {
		t.Fatal("INFO sent a PLI while an IDR was in flight")
	}
	send, reason := sess.sipInfoFIRRetryShouldSend(1, now.Add(20*time.Millisecond), now.Add(40*time.Millisecond))
	if send || reason != "idr-in-flight" {
		t.Fatalf("retry during in-flight: send=%v reason=%s", send, reason)
	}
}
