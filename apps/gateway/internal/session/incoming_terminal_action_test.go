package session

import "testing"

func TestIncomingTerminalActionRejectsStaleRingingState(t *testing.T) {
	for _, action := range []string{"caller_cancel", "timeout", "reject"} {
		t.Run(action, func(t *testing.T) {
			s := &Session{ID: "incoming-terminal", State: StateIncoming}
			// A handler observed ringing before accept finished and cleared its guard.
			if s.GetState() != StateIncoming {
				t.Fatal("expected ringing")
			}
			s.SetState(StateActive)
			s.ClearTerminalAction()
			if s.TryBeginIncomingTerminalAction(action) {
				t.Fatal("stale incoming action claimed an answered call")
			}
			if s.GetState() != StateActive || s.GetTerminalAction() != "" {
				t.Fatal("stale incoming action changed the answered call")
			}
		})
	}
}

func TestIncomingTerminalActionOnlyOneWinner(t *testing.T) {
	s := &Session{ID: "incoming-terminal", State: StateIncoming}
	if !s.TryBeginIncomingTerminalAction("caller_cancel") {
		t.Fatal("first incoming action must win")
	}
	for _, action := range []string{"timeout", "reject", "caller_cancel"} {
		if s.TryBeginIncomingTerminalAction(action) {
			t.Fatalf("second action %s must lose", action)
		}
	}
	if s.GetTerminalAction() != "caller_cancel" {
		t.Fatal("loser overwrote the terminal action")
	}
}
