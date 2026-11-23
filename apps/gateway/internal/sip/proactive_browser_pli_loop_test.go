package sip

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtcp"

	"webrtc-sip-gateway/internal/session"
)

func captureStdoutForTest(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(copyDone)
	}()

	defer func() {
		os.Stdout = orig
	}()
	fn()
	_ = w.Close()
	<-copyDone
	_ = r.Close()
	return buf.String()
}

func TestStartProactiveBrowserPLIBacksOffWithoutBurningThrottleWhenNotReady(t *testing.T) {
	srv := &Server{}
	sess := &session.Session{ID: "pli-backoff-test"}

	var attempts atomic.Int32
	sess.SetWriteWebRTCRTCPForTest(func(_ []rtcp.Packet) error {
		attempts.Add(1)
		return errors.New("dtls not ready")
	})

	done := make(chan struct{})
	out := captureStdoutForTest(t, func() {
		go func() {
			defer close(done)
			srv.startProactiveBrowserPLIForSession(sess)
		}()

		time.Sleep(320 * time.Millisecond)

		gotAttempts := attempts.Load()
		if gotAttempts < 2 {
			t.Errorf("expected at least 2 attempts during 320ms backoff window, got %d", gotAttempts)
		}
		if gotAttempts > 5 {
			t.Errorf("proactive PLI loop spun too fast: %d attempts in 320ms (expected backoff >= 100ms)", gotAttempts)
		}

		// Failed/unready attempts must not stamp lastBrowserIDRRequest or burn the throttle.
		decision := sess.DecideSessionProactiveBrowserPLI(time.Now())
		if !decision.Send || decision.Reason != "startup" {
			t.Errorf("expected startup decision to remain sendable without burning throttle, got %+v", decision)
		}

		sess.SetState(session.StateEnded)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("proactive PLI goroutine did not exit promptly on session end")
		}
	})

	throttledLogs := strings.Count(out, "proactive_browser_pli throttled")
	if throttledLogs != 1 {
		t.Fatalf("expected throttled log spam to be suppressed to 1 line within 2s, got %d:\n%s", throttledLogs, out)
	}
}

func TestStartProactiveBrowserPLIWakesPromptlyOnSignal(t *testing.T) {
	srv := &Server{}
	sess := &session.Session{ID: "pli-wake-test"}

	firstFailed := make(chan struct{}, 1)
	sent := make(chan time.Time, 1)
	var ready atomic.Bool

	sess.SetWriteWebRTCRTCPForTest(func(_ []rtcp.Packet) error {
		if !ready.Load() {
			select {
			case firstFailed <- struct{}{}:
			default:
			}
			return errors.New("track not ready")
		}
		select {
		case sent <- time.Now():
		default:
		}
		return nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Use a long backoff (1s) so waking within <200ms proves SignalProactivePLIWake bypassed it.
		srv.runProactiveBrowserPLILoop(sess, time.Second, 2*time.Second)
	}()

	select {
	case <-firstFailed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for initial unready PLI attempt")
	}

	// Let the loop enter the 1s backoff timer, then mark ready and signal wake.
	time.Sleep(15 * time.Millisecond)
	ready.Store(true)
	wakeAt := time.Now()
	sess.SignalProactivePLIWake()

	select {
	case sentAt := <-sent:
		if elapsed := sentAt.Sub(wakeAt); elapsed >= 250*time.Millisecond {
			t.Fatalf("proactive PLI took %s after wake signal, expected prompt wakeup (<250ms)", elapsed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for proactive PLI after SignalProactivePLIWake")
	}

	sess.SetState(session.StateEnded)
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("proactive PLI loop did not stop promptly when session ended")
	}
}
