package api

import (
	"context"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

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

func TestStatsCollectionRunsForTelemetryWithoutDatabase(t *testing.T) {
	server := &Server{runtimeConfig: &config.Config{Observability: config.ObservabilityConfig{Enable: true, MetricsIntervalMS: 2500}}}
	if !server.statsCollectionEnabled() {
		t.Fatal("telemetry-enabled collector must run independently of database logging")
	}
	if got := server.statsCollectionInterval(); got != 2500*time.Millisecond {
		t.Fatalf("interval = %v", got)
	}
}

func TestCollectMediaQualityUsesPeriodicAggregateStats(t *testing.T) {
	report := webrtc.StatsReport{
		"inbound":  webrtc.InboundRTPStreamStats{Kind: "audio", PacketsReceived: 90, PacketsLost: 10, Jitter: 0.02, PLICount: 2, FIRCount: 3, NACKCount: 4},
		"outbound": webrtc.RemoteInboundRTPStreamStats{Kind: "video", FractionLost: 0.25, Jitter: 0.03, RoundTripTime: 0.04, PLICount: 5, FIRCount: 6, NACKCount: 7},
	}
	samples := collectMediaQuality(report)
	if len(samples) != 2 {
		t.Fatalf("samples = %#v", samples)
	}
	byDirection := map[string]mediaQualitySample{}
	for _, sample := range samples {
		byDirection[sample.direction] = sample
	}
	inbound := byDirection["inbound"]
	if inbound.kind != "audio" || inbound.packetLoss != 0.1 || inbound.jitter != 0.02 || inbound.pli != 2 || inbound.fir != 3 || inbound.nack != 4 {
		t.Fatalf("unexpected inbound aggregate: %#v", inbound)
	}
	outbound := byDirection["outbound"]
	if outbound.kind != "video" || outbound.packetLoss != 0.25 || outbound.jitter != 0.03 || outbound.rtt != 0.04 || outbound.pli != 5 || outbound.fir != 6 || outbound.nack != 7 {
		t.Fatalf("unexpected outbound aggregate: %#v", outbound)
	}
}
