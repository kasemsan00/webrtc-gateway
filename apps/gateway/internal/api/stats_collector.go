package api

import (
	"context"
	"time"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

const minimumStatsInterval = time.Second

func (s *Server) statsCollectionEnabled() bool {
	if s.runtimeConfig == nil || !s.runtimeConfig.DB.Enable || s.logStore == nil {
		return false
	}
	if reporter, ok := s.logStore.(logstore.HealthReporter); ok {
		return reporter.LogStoreHealth().Enabled
	}
	return true
}

func (s *Server) statsCollectionInterval() time.Duration {
	interval := time.Duration(s.runtimeConfig.DB.StatsIntervalMS) * time.Millisecond
	if interval < minimumStatsInterval {
		return minimumStatsInterval
	}
	return interval
}

// startStatsCollector owns one bounded goroutine for every gateway server. It
// snapshots sessions under their own short locks and queues records only after
// the locks are released.
func (s *Server) startStatsCollector(ctx context.Context) {
	if !s.statsCollectionEnabled() || s.sessionMgr == nil {
		return
	}
	interval := s.statsCollectionInterval()
	go runStatsCollector(ctx, interval, func(timestamp time.Time) {
		s.collectSessionStats(timestamp, s.sessionMgr.ListSessions())
	}, nil)
}

func runStatsCollector(ctx context.Context, interval time.Duration, collect func(time.Time), stopped chan<- struct{}) {
	if stopped != nil {
		defer close(stopped)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case timestamp := <-ticker.C:
			collect(timestamp)
		}
	}
}

func (s *Server) collectSessionStats(timestamp time.Time, sessions []*session.Session) {
	for _, active := range sessions {
		snapshot := active.Observability()
		s.logStore.RecordStats(&logstore.StatsRecord{
			Timestamp: timestamp.UTC(), SessionID: snapshot.SessionID,
			PLISent: snapshot.PLISent, PLIResponse: snapshot.PLIResponse,
			AudioRTCPRR: snapshot.AudioRTCPRR, AudioRTCPSR: snapshot.AudioRTCPSR,
			VideoRTCPRR: snapshot.VideoRTCPRR, VideoRTCPSR: snapshot.VideoRTCPSR,
			LastPLISentAt: snapshot.LastPLISentAt, LastKeyframeAt: snapshot.LastKeyframeAt,
			Data: map[string]interface{}{
				"mediaForwardReady": snapshot.MediaForwardOK, "videoRecoveryActive": snapshot.VideoRecoveryOn,
			},
		})
	}
}
