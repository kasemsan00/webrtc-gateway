package session

import (
	"testing"
	"time"
)

func newDisorderTestSession(id string) *Session {
	sess := newBurstTestSession(id)
	sess.VideoRTPDisorderMonitorEnabled = true
	sess.VideoRTPDisorderMinPacketDelta = 300
	sess.VideoRTPDisorderMaxGapDelta = 45
	sess.VideoRTPDisorderMaxMissingDelta = 80
	sess.VideoRTPDisorderMaxOutOfOrderDelta = 80
	sess.VideoRTPDisorderMaxReorderTimeout = 20
	sess.VideoRTPDisorderConsecutiveWindows = 3
	sess.VideoRTPDisorderLogInterval = time.Millisecond
	return sess
}

func TestObserveSIPVideoRTPDisorderDetectsSustainedWindows(t *testing.T) {
	sess := newDisorderTestSession("rtp-disorder-sustained")
	sess.SwitchGeneration = 2
	now := time.Now()

	obs := sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{Packets: 300}, now)
	if obs.BadWindow || obs.Sustained {
		t.Fatalf("expected first observation to seed baseline")
	}

	for i := 1; i <= 2; i++ {
		obs = sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{
			Packets:         300 * (i + 1),
			Gaps:            50 * i,
			Missing:         90 * i,
			OutOfOrder:      90 * i,
			ReorderTimedOut: int64(2 * i),
		}, now.Add(time.Duration(i)*time.Second))
		if !obs.BadWindow {
			t.Fatalf("expected bad window %d", i)
		}
		if obs.Sustained {
			t.Fatalf("did not expect sustained disorder before consecutive threshold")
		}
	}

	obs = sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{
		Packets:         1200,
		Gaps:            150,
		Missing:         270,
		OutOfOrder:      270,
		ReorderTimedOut: 6,
	}, now.Add(3*time.Second))
	if !obs.Sustained {
		t.Fatalf("expected sustained disorder on third consecutive bad window")
	}
	if obs.SwitchGeneration != 2 {
		t.Fatalf("expected observation to include switch generation 2, got %d", obs.SwitchGeneration)
	}
}

func TestObserveSIPVideoRTPDisorderNormalWindowsDoNotEmit(t *testing.T) {
	sess := newDisorderTestSession("rtp-disorder-normal")
	now := time.Now()

	sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{Packets: 300}, now)
	for i := 1; i <= 5; i++ {
		obs := sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{
			Packets:    300 * (i + 1),
			Gaps:       5 * i,
			Missing:    8 * i,
			OutOfOrder: 8 * i,
		}, now.Add(time.Duration(i)*time.Second))
		if obs.BadWindow || obs.Sustained {
			t.Fatalf("expected normal window %d to stay quiet, got %+v", i, obs)
		}
	}
	if !sess.VideoRTPDisorderContainmentUntil.IsZero() {
		t.Fatalf("expected containment to remain inactive for normal windows")
	}
}

func TestObserveSIPVideoRTPDisorderStartsBoundedContainmentWhenEnabled(t *testing.T) {
	sess := newDisorderTestSession("rtp-disorder-containment")
	sess.VideoRTPDisorderConsecutiveWindows = 1
	sess.VideoRTPDisorderContainmentEnabled = true
	sess.VideoRTPDisorderContainmentDuration = 50 * time.Millisecond
	now := time.Now()

	sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{Packets: 300}, now)
	obs := sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{
		Packets:    700,
		Gaps:       50,
		Missing:    90,
		OutOfOrder: 90,
	}, now.Add(time.Second))

	if !obs.Sustained || !obs.ContainmentStarted {
		t.Fatalf("expected sustained disorder to start bounded containment, got %+v", obs)
	}
	if sess.VideoRTPDisorderContainmentUntil.IsZero() {
		t.Fatalf("expected containment deadline to be set")
	}
	if sess.VideoRTPDisorderContainmentReason != "gap-delta" {
		t.Fatalf("expected containment reason to be recorded, got %q", sess.VideoRTPDisorderContainmentReason)
	}
}

func TestObserveSIPVideoRTPDisorderLeavesContainmentDisabledByDefault(t *testing.T) {
	sess := newDisorderTestSession("rtp-disorder-containment-disabled")
	sess.VideoRTPDisorderConsecutiveWindows = 1
	now := time.Now()

	sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{Packets: 300}, now)
	obs := sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{
		Packets:    700,
		Gaps:       50,
		Missing:    90,
		OutOfOrder: 90,
	}, now.Add(time.Second))

	if !obs.Sustained {
		t.Fatalf("expected sustained disorder to be detected")
	}
	if obs.ContainmentStarted || !sess.VideoRTPDisorderContainmentUntil.IsZero() {
		t.Fatalf("expected containment to stay disabled by default, got %+v", obs)
	}
}

func TestObserveSIPVideoRTPDisorderEndsContainmentWhenMetricsRecover(t *testing.T) {
	sess := newDisorderTestSession("rtp-disorder-containment-recovered")
	sess.VideoRTPDisorderConsecutiveWindows = 1
	sess.VideoRTPDisorderContainmentEnabled = true
	sess.VideoRTPDisorderContainmentDuration = time.Minute
	now := time.Now()

	sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{Packets: 300}, now)
	sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{
		Packets:    700,
		Gaps:       50,
		Missing:    90,
		OutOfOrder: 90,
	}, now.Add(time.Second))
	obs := sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{
		Packets:    1100,
		Gaps:       55,
		Missing:    95,
		OutOfOrder: 95,
	}, now.Add(2*time.Second))

	if !obs.ContainmentEnded {
		t.Fatalf("expected containment to end when RTP disorder metrics recover, got %+v", obs)
	}
	if !sess.VideoRTPDisorderContainmentUntil.IsZero() {
		t.Fatalf("expected containment deadline to be cleared")
	}
	if sess.VideoRTPDisorderContainmentReason != "" {
		t.Fatalf("expected containment reason to be cleared, got %q", sess.VideoRTPDisorderContainmentReason)
	}
}

func TestObserveSIPVideoRTPDisorderEndsContainmentAfterExpiry(t *testing.T) {
	sess := newDisorderTestSession("rtp-disorder-containment-expired")
	sess.VideoRTPDisorderConsecutiveWindows = 1
	sess.VideoRTPDisorderContainmentEnabled = true
	sess.VideoRTPDisorderContainmentDuration = 50 * time.Millisecond
	now := time.Now()

	sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{Packets: 300}, now)
	sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{
		Packets:    700,
		Gaps:       50,
		Missing:    90,
		OutOfOrder: 90,
	}, now.Add(time.Second))
	obs := sess.ObserveSIPVideoRTPDisorder(VideoRecoverySummary{
		Packets:    1100,
		Gaps:       100,
		Missing:    180,
		OutOfOrder: 180,
	}, now.Add(2*time.Second))

	if !obs.ContainmentEnded || !obs.ContainmentStarted {
		t.Fatalf("expected expired containment to end and restart for ongoing disorder, got %+v", obs)
	}
	if sess.VideoRTPDisorderContainmentUntil.IsZero() {
		t.Fatalf("expected restarted containment deadline to be set")
	}
}
