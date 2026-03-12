package session

import (
	"net"
	"testing"
	"time"

	"github.com/pion/rtcp"
)

func TestGetVideoFeedbackTargets_RTPMode(t *testing.T) {
	sess := &Session{VideoFeedbackTransport: "rtp"}
	dest := &net.UDPAddr{IP: net.ParseIP("203.150.245.42"), Port: 18576}

	targets := sess.getVideoFeedbackTargets(dest, nil, true)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Kind != "rtp" || targets[0].Addr.Port != 18576 || !targets[0].IsPrimary {
		t.Fatalf("unexpected target: %+v", targets[0])
	}
}

func TestGetVideoFeedbackTargets_DualMode_RTPFirst(t *testing.T) {
	sess := &Session{VideoFeedbackTransport: "dual"}
	dest := &net.UDPAddr{IP: net.ParseIP("203.150.245.42"), Port: 18576}

	targets := sess.getVideoFeedbackTargets(dest, nil, false)
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if targets[0].Kind != "rtp" || targets[0].Addr.Port != 18576 {
		t.Fatalf("expected first target to RTP port, got %+v", targets[0])
	}
	if targets[1].Kind != "rtcp" || targets[1].Addr.Port != 18577 {
		t.Fatalf("expected second target to RTCP port, got %+v", targets[1])
	}
}

func TestGetVideoFeedbackTargets_AutoMode_LegacyFallback(t *testing.T) {
	sess := &Session{VideoFeedbackTransport: "auto"}
	dest := &net.UDPAddr{IP: net.ParseIP("203.150.245.42"), Port: 18576}
	learned := &net.UDPAddr{IP: net.ParseIP("203.150.245.42"), Port: 19000}

	targets := sess.getVideoFeedbackTargets(dest, learned, true)
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets in auto fallback mode, got %d", len(targets))
	}
	if !targets[0].IsPrimary || targets[0].Kind != "rtcp" || targets[0].Addr.Port != 19000 {
		t.Fatalf("unexpected primary target: %+v", targets[0])
	}
	if targets[1].Kind != "rtcp" || targets[1].Addr.Port != 18577 {
		t.Fatalf("unexpected rtcp fallback target: %+v", targets[1])
	}
	if targets[2].Kind != "rtp" || targets[2].Addr.Port != 18576 {
		t.Fatalf("unexpected rtp fallback target: %+v", targets[2])
	}
}

func TestSuppressBrowserNACK_DedupeWindow(t *testing.T) {
	sess := &Session{}
	sig := buildNACKSignature([]rtcp.NackPair{{PacketID: 12012, LostPackets: 31}})
	now := time.Now()

	if sess.shouldSuppressBrowserNACK(sig, now) {
		t.Fatalf("first NACK should not be suppressed")
	}
	if !sess.shouldSuppressBrowserNACK(sig, now.Add(50*time.Millisecond)) {
		t.Fatalf("duplicate NACK within dedupe window should be suppressed")
	}
	if sess.shouldSuppressBrowserNACK(sig, now.Add(200*time.Millisecond)) {
		t.Fatalf("duplicate NACK after dedupe window should not be suppressed")
	}
}

func TestSuppressNACKHandledLog_Throttle(t *testing.T) {
	sess := &Session{}
	now := time.Now()
	sig := "12012:31;"

	if sess.shouldSuppressNACKHandledLog(sig, 0, 25, now) {
		t.Fatalf("first log should not be suppressed")
	}
	if !sess.shouldSuppressNACKHandledLog(sig, 0, 25, now.Add(200*time.Millisecond)) {
		t.Fatalf("duplicate log should be suppressed within throttle window")
	}
	if sess.shouldSuppressNACKHandledLog(sig, 0, 25, now.Add(1500*time.Millisecond)) {
		t.Fatalf("duplicate log should not be suppressed after throttle window")
	}
	if sess.shouldSuppressNACKHandledLog(sig, 1, 0, now.Add(1600*time.Millisecond)) {
		t.Fatalf("successful retransmit logs should not be throttled")
	}
}
