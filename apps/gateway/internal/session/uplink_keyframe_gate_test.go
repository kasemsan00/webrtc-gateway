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

func TestOutboundIDRBeforeSSRCLearnKicks(t *testing.T) {
	// Browser IDR is forwarded just after 200 OK. Android's video SSRC is
	// learned about two seconds later, so that IDR never reached the decoder.
	sess := &Session{ID: "pre-learn-idr"}
	answer := time.Now().Add(-2 * time.Second)
	idrAt := answer.Add(57 * time.Millisecond)
	sess.LastUplinkKeyframe = idrAt
	sess.uplinkIDRCompleteAt = idrAt

	learned := time.Now()
	sess.remoteVideoSSRCLearnedAt = learned
	sess.NoteSIPFarEndVideoSeen(learned)

	send, reason := sess.DecideSessionUplinkKeyframeRequest("remote-ssrc-learn", learned)
	if !send || reason != "" {
		t.Fatalf("remote-ssrc-learn after pre-learn IDR: send=%v reason=%s", send, reason)
	}
	if !sess.KickUplinkKeyframeOnRemoteJoinIfNeeded() {
		t.Fatal("remote-ssrc-learn kick did not fire")
	}
	sess.mu.Lock()
	sess.State = StateEnded
	sess.mu.Unlock()

	// First SIP RTCP still has only the pre-learn IDR, so it kicks too.
	send, reason = sess.DecideSessionUplinkKeyframeRequest("sip-rtcp-first", learned.Add(3*time.Second))
	if !send || reason != "" {
		t.Fatalf("sip-rtcp-first with only a pre-learn IDR: send=%v reason=%s", send, reason)
	}
	if !sess.KickUplinkKeyframeForSIPDecoder("sip-rtcp-first") {
		t.Fatal("sip-rtcp-first kick did not fire")
	}

	// An IDR forwarded after learn satisfies the next check.
	post := time.Now()
	sess.mu.Lock()
	sess.LastUplinkKeyframe = post
	sess.uplinkIDRCompleteAt = post
	sess.mu.Unlock()
	send, reason = sess.DecideSessionUplinkKeyframeRequest("remote-ssrc-learn", post.Add(20*time.Millisecond))
	if send || reason != "idr-fresh" {
		t.Fatalf("post-learn IDR: send=%v reason=%s, want idr-fresh", send, reason)
	}
}

func TestIDRAfterSSRCLearnSkipsJoinKicks(t *testing.T) {
	sess := &Session{ID: "post-learn-idr"}
	learned := time.Now().Add(-400 * time.Millisecond)
	sess.remoteVideoSSRCLearnedAt = learned
	sess.NoteSIPFarEndVideoSeen(learned.Add(-time.Millisecond))
	idrAt := learned.Add(50 * time.Millisecond)
	sess.LastUplinkKeyframe = idrAt
	sess.uplinkIDRCompleteAt = idrAt

	for _, kind := range []string{"remote-ssrc-learn", "sip-rtcp-first"} {
		send, reason := sess.DecideSessionUplinkKeyframeRequest(kind, time.Now())
		if send || reason != "idr-fresh" {
			t.Fatalf("%s: send=%v reason=%s, want idr-fresh", kind, send, reason)
		}
		if sess.KickUplinkKeyframeForSIPDecoder(kind) {
			t.Fatalf("%s kick fired after a post-learn IDR", kind)
		}
	}
}

func TestIDRBeforeFirstSIPRTCPDoesNotCountAsFresh(t *testing.T) {
	sess := &Session{ID: "pre-rtcp-idr"}
	idrAt := time.Now().Add(-3 * time.Second)
	sess.LastUplinkKeyframe = idrAt
	sess.uplinkIDRCompleteAt = idrAt
	sess.NoteSIPFarEndVideoSeen(time.Now())

	send, reason := sess.DecideSessionUplinkKeyframeRequest("sip-rtcp-first", time.Now())
	if !send || reason != "" {
		t.Fatalf("sip-rtcp-first: send=%v reason=%s", send, reason)
	}
	// SSRC was never learned. remote-ssrc-learn still ignores the early IDR
	// because SIP video RTCP has already been seen.
	send, reason = sess.DecideSessionUplinkKeyframeRequest("remote-ssrc-learn", time.Now())
	if !send || reason != "" {
		t.Fatalf("remote-ssrc-learn before SSRC with RTCP seen: send=%v reason=%s", send, reason)
	}
}

func TestInFlightIDRBeforeLearnDoesNotBlockJoinKick(t *testing.T) {
	sess := &Session{ID: "pre-learn-inflight"}
	sess.uplinkIDRInFlight = true
	sess.LastUplinkKeyframe = time.Now().Add(-2 * time.Second)
	sess.remoteVideoSSRCLearnedAt = time.Now()
	sess.NoteSIPFarEndVideoSeen(sess.remoteVideoSSRCLearnedAt)

	send, reason := sess.DecideSessionUplinkKeyframeRequest("remote-ssrc-learn", time.Now())
	if !send || reason != "" {
		t.Fatalf("in-flight pre-learn IDR: send=%v reason=%s", send, reason)
	}

	sess.LastUplinkKeyframe = time.Now()
	send, reason = sess.DecideSessionUplinkKeyframeRequest("remote-ssrc-learn", time.Now())
	if send || reason != "idr-in-flight" {
		t.Fatalf("in-flight after learn: send=%v reason=%s, want idr-in-flight", send, reason)
	}
}

func TestDestReadyStillTreatsRecentIDRAsFresh(t *testing.T) {
	sess := &Session{ID: "dest-ready-fresh"}
	idrAt := time.Now().Add(-2 * time.Second)
	sess.LastUplinkKeyframe = idrAt
	sess.uplinkIDRCompleteAt = idrAt
	sess.remoteVideoSSRCLearnedAt = time.Now()
	sess.NoteSIPFarEndVideoSeen(sess.remoteVideoSSRCLearnedAt)

	send, reason := sess.DecideSessionUplinkKeyframeRequest("sip-video-dest-ready", time.Now())
	if send || reason != "idr-fresh" {
		t.Fatalf("dest-ready: send=%v reason=%s, want idr-fresh", send, reason)
	}
}

func TestDecoderJoinPLIFollowUp(t *testing.T) {
	attempts, gap := decoderJoinPLIFollowUp(false)
	if attempts != 3 || gap != 300*time.Millisecond {
		t.Fatalf("default follow-up = (%d, %s)", attempts, gap)
	}
	attempts, gap = decoderJoinPLIFollowUp(true)
	if attempts != 1 || gap != webrtcPLIOverallMinInterval {
		t.Fatalf("pre-learn follow-up = (%d, %s), want one retry at %s", attempts, gap, webrtcPLIOverallMinInterval)
	}
}

func TestRemoteVideoSSRCLearnedAtSticks(t *testing.T) {
	sess := &Session{ID: "ssrc-learn-at"}
	sess.SetRemoteVideoSSRC(0)
	if !sess.remoteVideoSSRCLearnedAt.IsZero() {
		t.Fatal("SSRC 0 recorded a learn time")
	}
	sess.SetRemoteVideoSSRC(111)
	learned := sess.remoteVideoSSRCLearnedAt
	if learned.IsZero() {
		t.Fatal("first real SSRC did not record learn time")
	}
	time.Sleep(2 * time.Millisecond)
	sess.SetRemoteVideoSSRC(222)
	if !sess.remoteVideoSSRCLearnedAt.Equal(learned) {
		t.Fatal("later SSRC change moved the first learn time")
	}
}

func TestNoteSIPFarEndVideoSeenKeepsFirst(t *testing.T) {
	sess := &Session{ID: "far-end-seen"}
	first := time.Now().Add(-time.Second)
	sess.NoteSIPFarEndVideoSeen(first)
	sess.NoteSIPVideoRTP(time.Now())
	got := sess.sipFarEndVideoSeenAt()
	if !got.Equal(first) {
		t.Fatalf("far-end seen = %s, want %s", got, first)
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
