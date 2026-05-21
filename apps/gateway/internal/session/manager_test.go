package session

import (
	"testing"

	"k2-gateway/internal/config"
)

func TestDeleteSessionIsIdempotent(t *testing.T) {
	mgr := NewManager(&config.Config{})
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	mgr.DeleteSession(sess.ID)
	mgr.DeleteSession(sess.ID)

	if _, ok := mgr.GetSession(sess.ID); ok {
		t.Fatalf("expected session to be removed")
	}
}
