package session

import (
	"testing"
	"time"
)

// Disconnect, recover, disconnect again: the first episode's timer must not
// end the call during the second grace period.
func TestICEReconnectGrace_StaleEpisodeDoesNotEndCall(t *testing.T) {
	s := &Session{ID: "grace-episode", State: StateActive}

	s.mu.Lock()
	if !s.enterICEReconnectGraceLocked(time.Hour, "disconnected") {
		t.Fatal("expected first disconnect to enter grace")
	}
	first := s.iceGraceEpisode
	s.State = StateActive // ICE reconnected
	if !s.enterICEReconnectGraceLocked(time.Hour, "disconnected") {
		t.Fatal("expected second disconnect to enter grace")
	}

	if ended, _ := s.expireICEReconnectGraceLocked(first); ended {
		t.Fatal("stale episode timer ended the call")
	}
	if s.State != StateReconnecting {
		t.Fatalf("expected still reconnecting, got %s", s.State)
	}
	if ended, _ := s.expireICEReconnectGraceLocked(s.iceGraceEpisode); !ended {
		t.Fatal("current episode timer should end the call")
	}
	if s.State != StateEnded {
		t.Fatalf("expected ended, got %s", s.State)
	}
	s.mu.Unlock()
}

func TestICEReconnectGrace_ExpiresAfterGracePeriod(t *testing.T) {
	s := &Session{ID: "grace-expiry", State: StateActive}

	s.mu.Lock()
	s.enterICEReconnectGraceLocked(20*time.Millisecond, "disconnected")
	s.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for s.GetState() != StateEnded {
		if time.Now().After(deadline) {
			t.Fatalf("grace did not expire, state=%s", s.GetState())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// When the grace runs out the handler must run (BYE toward SIP and the
// client's ended state live outside this package).
func TestICEReconnectGrace_ExpiryRunsEndedHandler(t *testing.T) {
	s := &Session{ID: "grace-handler", State: StateActive}
	reasons := make(chan string, 1)
	s.SetICEEndedHandler(func(sess *Session, reason string) {
		if sess != s {
			t.Errorf("handler got another session")
		}
		reasons <- reason
	})

	s.mu.Lock()
	s.enterICEReconnectGraceLocked(10*time.Millisecond, "disconnected")
	s.mu.Unlock()

	select {
	case reason := <-reasons:
		if reason != "ice_grace_expired" {
			t.Fatalf("reason = %q, want ice_grace_expired", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ICE ended handler did not run")
	}
}
