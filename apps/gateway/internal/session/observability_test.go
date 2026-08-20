package session

import (
	"testing"
	"time"
)

func TestObservabilitySnapshotReadsCountersSafely(t *testing.T) {
	now := time.Now().UTC()
	sess := &Session{ID: "session-1", PLISent: 3, PLIResponse: 2, LastPLISent: now, LastKeyframe: now, mediaForwardReady: true}
	sess.NoteInboundRTCP("audio", "rr")
	sess.NoteInboundRTCP("audio", "sr")
	sess.NoteInboundRTCP("video", "rr")
	sess.NoteInboundRTCP("video", "sr")

	snapshot := sess.Observability()
	if snapshot.SessionID != sess.ID || snapshot.PLISent != 3 || snapshot.PLIResponse != 2 {
		t.Fatalf("unexpected base snapshot: %#v", snapshot)
	}
	if snapshot.AudioRTCPRR != 1 || snapshot.AudioRTCPSR != 1 || snapshot.VideoRTCPRR != 1 || snapshot.VideoRTCPSR != 1 {
		t.Fatalf("unexpected RTCP snapshot: %#v", snapshot)
	}
	if snapshot.LastPLISentAt == nil || snapshot.LastKeyframeAt == nil || !snapshot.MediaForwardOK {
		t.Fatalf("expected timing/readiness data: %#v", snapshot)
	}
}
