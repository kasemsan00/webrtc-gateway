package sip

import (
	"testing"

	"k2-gateway/internal/session"
)

func TestNotifySessionStateChange_ForwardsConnectingRingingAndDedupes(t *testing.T) {
	notifier := &midCallStateNotifierStub{}
	srv := &Server{stateNotifier: notifier}
	sess := &session.Session{ID: "sess-progress", State: session.StateConnecting}

	srv.notifySessionStateChange(sess, session.StateConnecting)
	if notifier.calls != 1 || notifier.state != session.StateConnecting {
		t.Fatalf("expected connecting notify, got calls=%d state=%s", notifier.calls, notifier.state)
	}

	srv.notifySessionStateChange(sess, session.StateConnecting)
	if notifier.calls != 1 {
		t.Fatalf("expected connecting dedupe, got calls=%d", notifier.calls)
	}

	srv.notifySessionStateChange(sess, session.StateRinging)
	if notifier.calls != 2 || notifier.state != session.StateRinging {
		t.Fatalf("expected ringing notify, got calls=%d state=%s", notifier.calls, notifier.state)
	}

	srv.notifySessionStateChange(sess, session.StateActive)
	if notifier.calls != 3 || notifier.state != session.StateActive {
		t.Fatalf("expected active notify, got calls=%d state=%s", notifier.calls, notifier.state)
	}
}

func TestNotifySessionStateChange_IgnoresNonProgressStates(t *testing.T) {
	notifier := &midCallStateNotifierStub{}
	srv := &Server{stateNotifier: notifier}
	sess := &session.Session{ID: "sess-progress", State: session.StateIncoming}

	srv.notifySessionStateChange(sess, session.StateIncoming)
	srv.notifySessionStateChange(sess, session.StateNew)
	if notifier.calls != 0 {
		t.Fatalf("expected non-progress states ignored, got calls=%d", notifier.calls)
	}
}
