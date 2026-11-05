package api

import (
	"context"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

type recordingStatsStore struct {
	logstore.LogStore
	records []*logstore.StatsRecord
	health  logstore.Health
}

func (s *recordingStatsStore) RecordStats(record *logstore.StatsRecord) {
	s.records = append(s.records, record)
}
func (s *recordingStatsStore) LogStoreHealth() logstore.Health { return s.health }

func TestCollectSessionStatsRecordsReviewedCounters(t *testing.T) {
	store := &recordingStatsStore{}
	server := &Server{logStore: store}
	now := time.Now().UTC().Truncate(time.Millisecond)
	active := &session.Session{ID: "session-1", PLISent: 4, PLIResponse: 3, LastPLISent: now, LastKeyframe: now}
	active.NoteInboundRTCP("audio", "rr")
	active.NoteInboundRTCP("video", "sr")
	server.collectSessionStats(now, []*session.Session{active})
	if len(store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(store.records))
	}
	record := store.records[0]
	if record.SessionID != "session-1" || record.PLISent != 4 || record.PLIResponse != 3 || record.AudioRTCPRR != 1 || record.VideoRTCPSR != 1 {
		t.Fatalf("unexpected record: %#v", record)
	}
	if record.Timestamp != now || record.LastPLISentAt == nil || record.LastKeyframeAt == nil {
		t.Fatalf("missing timestamps: %#v", record)
	}
}

func TestStatsCollectionDisabledSkipsPersistence(t *testing.T) {
	store := &recordingStatsStore{health: logstore.Health{Enabled: false}}
	server := &Server{logStore: store, runtimeConfig: &config.Config{DB: config.DBConfig{Enable: false}}}
	if server.statsCollectionEnabled() {
		t.Fatal("disabled database must disable stats collection")
	}
	if len(store.records) != 0 {
		t.Fatalf("disabled collector recorded %d samples", len(store.records))
	}
}

func TestRunStatsCollectorStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	collected := make(chan struct{}, 1)
	go runStatsCollector(ctx, time.Millisecond, func(time.Time) {
		select {
		case collected <- struct{}{}:
		default:
		}
	}, stopped)
	select {
	case <-collected:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("collector did not tick")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("collector did not stop after cancellation")
	}
}
