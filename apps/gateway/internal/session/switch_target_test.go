package session

import (
	"testing"
	"time"
)

func TestPrepareSwitchVideoTargetIgnoresDuplicateInsideDebounce(t *testing.T) {
	sess := newBurstTestSession("switch-target-dup")
	now := time.Now()
	sess.RemoteVideoSSRC = 1111
	sess.SIPVideoRTPSource = "203.0.113.10:4000"

	first := sess.PrepareSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	if first.Ignore {
		t.Fatalf("expected first switch to be honored")
	}
	if first.Generation != 1 {
		t.Fatalf("expected first generation 1, got %d", first.Generation)
	}

	dup := sess.PrepareSwitchVideoTarget("14131", "00025", now.Add(30*time.Second), time.Minute, true)
	if !dup.Ignore {
		t.Fatalf("expected duplicate switch to be ignored")
	}
	if dup.Generation != first.Generation {
		t.Fatalf("expected ignored duplicate to keep generation %d, got %d", first.Generation, dup.Generation)
	}
	if dup.DuplicateCount != 1 {
		t.Fatalf("expected duplicate count 1, got %d", dup.DuplicateCount)
	}
}

func TestPrepareSwitchVideoTargetHonorsAfterDebounce(t *testing.T) {
	sess := newBurstTestSession("switch-target-expired")
	now := time.Now()
	sess.RemoteVideoSSRC = 1111
	sess.SIPVideoRTPSource = "203.0.113.10:4000"

	first := sess.PrepareSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	next := sess.PrepareSwitchVideoTarget("14131", "00025", now.Add(61*time.Second), time.Minute, true)

	if next.Ignore {
		t.Fatalf("expected switch after debounce window to be honored")
	}
	if next.Generation != first.Generation+1 {
		t.Fatalf("expected generation to advance, got first=%d next=%d", first.Generation, next.Generation)
	}
	if next.Reason != "debounce-window-expired" {
		t.Fatalf("expected debounce-window-expired reason, got %q", next.Reason)
	}
}

func TestPrepareSwitchVideoTargetHonorsMediaGenerationChange(t *testing.T) {
	sess := newBurstTestSession("switch-target-media-generation")
	now := time.Now()
	sess.RemoteVideoSSRC = 1111
	sess.SIPVideoRTPSource = "203.0.113.10:4000"

	first := sess.PrepareSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	sess.RemoteVideoSSRC = 2222
	sess.SIPVideoRTPSource = "203.0.113.10:4010"
	next := sess.PrepareSwitchVideoTarget("14131", "00025", now.Add(30*time.Second), time.Minute, true)

	if next.Ignore {
		t.Fatalf("expected same target with changed media generation to be honored")
	}
	if next.Generation != first.Generation+1 {
		t.Fatalf("expected generation to advance, got first=%d next=%d", first.Generation, next.Generation)
	}
	if next.Reason != "media-generation-changed" {
		t.Fatalf("expected media-generation-changed reason, got %q", next.Reason)
	}
}
