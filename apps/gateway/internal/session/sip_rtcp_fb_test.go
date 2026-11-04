package session

import (
	"sync/atomic"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
)

func TestNormalizeSIPKeyframeRequestMode(t *testing.T) {
	cases := map[string]string{
		"":     config.SIPKeyframeRequestModeRTCP,
		"INFO": config.SIPKeyframeRequestModeInfo,
		"rtcp": config.SIPKeyframeRequestModeRTCP,
		"both": config.SIPKeyframeRequestModeBoth,
		"nope": config.SIPKeyframeRequestModeInfo,
	}
	for in, want := range cases {
		if got := NormalizeSIPKeyframeRequestMode(in); got != want {
			t.Fatalf("mode %q = %q, want %q", in, got, want)
		}
	}
}

func TestDecideSIPKeyframeFeedbackModeInfoNeverSendsRTCP(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	got := DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeInfo, Kind: "pli", Now: now,
		LastPacketMarker: true,
	})
	if got.SendRTCP || !got.SendINFO || got.Reason != "mode-info" {
		t.Fatalf("info mode: %+v", got)
	}
	got = DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeInfo, Kind: "fir", Now: now, InFlight: true,
	})
	if got.SendRTCP || !got.SendINFO {
		t.Fatalf("info mode in-flight still INFO, not RTCP: %+v", got)
	}
}

func TestDecideSIPKeyframeFeedbackSkipsInFlightAndGuard(t *testing.T) {
	now := time.Unix(1_900_000_100, 0)
	got := DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeRTCP, Kind: "fir", InFlight: true, Now: now,
		Guard: 1500 * time.Millisecond, MinInterval: 2 * time.Second,
	})
	if got.SendRTCP || got.SendINFO || got.Reason != "idr-in-flight" {
		t.Fatalf("in-flight: %+v", got)
	}
	got = DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeBoth, Kind: "pli", Now: now,
		CompleteAt: now.Add(-1499 * time.Millisecond),
		Guard:      1500 * time.Millisecond, MinInterval: 2 * time.Second,
		LastPacketMarker: true,
	})
	if got.SendRTCP || !got.SendINFO || got.Reason != "idr-guard" {
		t.Fatalf("guard: %+v", got)
	}
	got = DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeRTCP, Kind: "pli", Now: now,
		CompleteAt: now.Add(-1500 * time.Millisecond),
		Guard:      1500 * time.Millisecond, LastPacketMarker: true,
	})
	if !got.SendRTCP || got.Reason != "" {
		t.Fatalf("guard elapsed: %+v", got)
	}
}

func TestDecideSIPKeyframeFeedbackRateLimit(t *testing.T) {
	now := time.Unix(1_900_000_200, 0)
	got := DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeRTCP, Kind: "pli", Now: now,
		LastRTCP: now.Add(-1999 * time.Millisecond), MinInterval: 2 * time.Second,
		LastPacketMarker: true,
	})
	if got.SendRTCP || got.Reason != "rate-limit" {
		t.Fatalf("inside 2s: %+v", got)
	}
	got = DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeRTCP, Kind: "fir", Now: now,
		LastRTCP: now.Add(-2 * time.Second), MinInterval: 2 * time.Second,
		LastPacketMarker: true,
	})
	if !got.SendRTCP || got.Reason != "" {
		t.Fatalf("at 2s: %+v", got)
	}
}

func TestDecideSIPKeyframeFeedbackWaitsForMarker(t *testing.T) {
	now := time.Unix(1_900_000_300, 0)
	got := DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeRTCP, Kind: "pli", Now: now,
		PacketSeen: true, LastPacketMarker: false,
	})
	if got.SendRTCP || !got.PendingRTCP || got.Reason != "mid-frame" {
		t.Fatalf("mid-frame: %+v", got)
	}
	got = DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeRTCP, Kind: "pli", Now: now,
		PacketSeen: true, LastPacketMarker: true,
	})
	if !got.SendRTCP || got.PendingRTCP {
		t.Fatalf("after marker: %+v", got)
	}
	got = DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeRTCP, Kind: "pli", Now: now,
		PacketSeen: false, LastPacketMarker: false,
	})
	if !got.SendRTCP {
		t.Fatalf("no packets yet should not wait for a marker: %+v", got)
	}
}

func TestDecideSIPKeyframeFeedbackIgnoresNACK(t *testing.T) {
	got := DecideSIPKeyframeFeedback(SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeInfo, Kind: "nack", InFlight: true,
	})
	if !got.SendRTCP || got.SendINFO {
		t.Fatalf("NACK must stay RTCP: %+v", got)
	}
}

func TestSendSIPFeedbackModeInfoRedirectsToINFO(t *testing.T) {
	sess := newBurstTestSession("sip-fb-info")
	makeSIPVideoRecoveryReady(t, sess)
	sess.sipKeyframeRequestMode = config.SIPKeyframeRequestModeInfo
	sess.sipRTCPFBMinInterval = 2 * time.Second
	sess.sipRTCPFBGuard = 1500 * time.Millisecond
	var infos atomic.Int32
	sess.SetSIPKeyframeINFOSender(func(reason string) {
		infos.Add(1)
	})

	sess.SendPLIToAsteriskForced("browser-pli")
	sess.SendFIRToAsterisk()
	if !sess.LastSipPLISent.IsZero() || !sess.LastSipFIRSent.IsZero() {
		t.Fatal("info mode must not send SIP RTCP PLI/FIR")
	}
	waitFor(t, time.Second, func() bool { return infos.Load() >= 1 })
	if infos.Load() < 1 {
		t.Fatal("expected SIP INFO redirect")
	}

	sess.SendPLIToAsteriskForced("browser-pli")
	if infos.Load() > 1 {
		t.Fatalf("INFO should coalesce, got %d", infos.Load())
	}
}

func TestSendSIPFeedbackSkipsRTCPDuringUplinkIDR(t *testing.T) {
	sess := newBurstTestSession("sip-fb-idr")
	makeSIPVideoRecoveryReady(t, sess)
	sess.sipKeyframeRequestMode = config.SIPKeyframeRequestModeRTCP
	sess.sipRTCPFBMinInterval = 2 * time.Second
	sess.sipRTCPFBGuard = 1500 * time.Millisecond
	sess.uplinkIDRInFlight = true

	sess.SendFIRToAsterisk()
	if !sess.LastSipFIRSent.IsZero() {
		t.Fatal("RTCP FIR must not go out while an uplink IDR is in flight")
	}

	sess.uplinkIDRInFlight = false
	sess.uplinkIDRCompleteAt = time.Now()
	sess.SendPLIToAsteriskForced("ssrc-learn")
	if !sess.LastSipPLISent.IsZero() {
		t.Fatal("RTCP PLI must not go out during the post-IDR guard")
	}

	sess.uplinkIDRCompleteAt = time.Now().Add(-1500 * time.Millisecond)
	sess.lastUplinkPacketMarker = true
	sess.uplinkVideoPacketSeen = true
	sess.SendPLIToAsteriskForced("ssrc-learn")
	if sess.LastSipPLISent.IsZero() {
		t.Fatal("expected RTCP PLI after the guard")
	}
	first := sess.LastSipPLISent
	sess.SendFIRToAsterisk()
	if !sess.LastSipFIRSent.IsZero() {
		t.Fatal("second RTCP inside 2s must be rate-limited")
	}
	if !sess.LastSipPLISent.Equal(first) {
		t.Fatal("rate-limit must not refresh LastSipPLISent")
	}
}

func TestSendSIPFeedbackWaitsForUplinkMarker(t *testing.T) {
	sess := newBurstTestSession("sip-fb-marker")
	makeSIPVideoRecoveryReady(t, sess)
	sess.sipKeyframeRequestMode = config.SIPKeyframeRequestModeRTCP
	sess.uplinkVideoPacketSeen = true
	sess.lastUplinkPacketMarker = false

	sess.SendPLIToAsteriskForced("browser-pli")
	if !sess.LastSipPLISent.IsZero() {
		t.Fatal("mid-frame RTCP must wait for a marker")
	}

	sess.ObserveUplinkVideoForwardMarked(false, 90000, time.Now(), true)
	if sess.LastSipPLISent.IsZero() {
		t.Fatal("pending RTCP should flush on the next marker packet")
	}
}

func waitFor(t *testing.T, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
