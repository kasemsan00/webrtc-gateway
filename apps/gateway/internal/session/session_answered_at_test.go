package session

import (
	"testing"
	"time"
)

func TestAnsweredAtStaysZeroUntilActive(t *testing.T) {
	sess := &Session{Direction: "outbound"}
	sess.SetState(StateConnecting)
	sess.SetState(StateRinging)
	if got := sess.AnsweredAt(); !got.IsZero() {
		t.Fatalf("AnsweredAt()=%v before answer, want zero", got)
	}

	before := time.Now()
	sess.SetState(StateActive)
	answeredAt := sess.AnsweredAt()
	if answeredAt.IsZero() || answeredAt.Before(before) {
		t.Fatalf("AnsweredAt()=%v want >= %v", answeredAt, before)
	}

	sess.SetState(StateReconnecting)
	sess.SetState(StateActive)
	if got := sess.AnsweredAt(); !got.Equal(answeredAt) {
		t.Fatalf("AnsweredAt()=%v after reconnect, want first answer %v", got, answeredAt)
	}
}

func TestAnsweredAtSetForInboundCalls(t *testing.T) {
	sess := &Session{Direction: "inbound"}
	sess.SetState(StateIncoming)
	if !sess.AnsweredAt().IsZero() {
		t.Fatal("incoming call must not have AnsweredAt before answer")
	}
	sess.SetState(StateActive)
	if sess.AnsweredAt().IsZero() {
		t.Fatal("inbound call must record AnsweredAt when it becomes active")
	}
}
