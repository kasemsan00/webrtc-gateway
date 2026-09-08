package api

import (
	"context"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/telemetry"
)

const minimumStatsInterval = time.Second

func (s *Server) statsCollectionEnabled() bool {
	if s.runtimeConfig == nil {
		return false
	}
	if s.runtimeConfig.Observability.Enable {
		return true
	}
	if !s.runtimeConfig.DB.Enable || s.logStore == nil {
		return false
	}
	if reporter, ok := s.logStore.(logstore.HealthReporter); ok {
		return reporter.LogStoreHealth().Enabled
	}
	return true
}

func (s *Server) statsCollectionInterval() time.Duration {
	interval := time.Duration(s.runtimeConfig.DB.StatsIntervalMS) * time.Millisecond
	if !s.runtimeConfig.DB.Enable {
		interval = time.Duration(s.runtimeConfig.Observability.MetricsIntervalMS) * time.Millisecond
	} else if s.runtimeConfig.Observability.Enable {
		telemetryInterval := time.Duration(s.runtimeConfig.Observability.MetricsIntervalMS) * time.Millisecond
		if telemetryInterval > 0 && telemetryInterval < interval {
			interval = telemetryInterval
		}
	}
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
	activeIDs := make(map[string]struct{}, len(sessions))
	for _, active := range sessions {
		snapshot := active.Observability()
		activeIDs[snapshot.SessionID] = struct{}{}
		if s.runtimeConfig != nil && s.runtimeConfig.Observability.Enable {
			s.collectSessionTelemetry(active, snapshot)
		}
		if s.logStore == nil || (s.runtimeConfig != nil && !s.runtimeConfig.DB.Enable) {
			continue
		}
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
	s.mu.Lock()
	for sessionID := range s.mediaTelemetry {
		if _, ok := activeIDs[sessionID]; !ok {
			delete(s.mediaTelemetry, sessionID)
		}
	}
	s.mu.Unlock()
}

type mediaRecoveryCounters struct{ pli, fir, nack, keyframe uint64 }

type mediaQualitySample struct {
	direction  string
	kind       string
	packetLoss float64
	jitter     float64
	rtt        float64
	pli        uint64
	fir        uint64
	nack       uint64
}

func collectMediaQuality(report webrtc.StatsReport) []mediaQualitySample {
	samples := make([]mediaQualitySample, 0, 4)
	for _, value := range report {
		switch stats := value.(type) {
		case webrtc.InboundRTPStreamStats:
			total := int64(stats.PacketsReceived) + int64(stats.PacketsLost)
			loss := 0.0
			if total > 0 && stats.PacketsLost > 0 {
				loss = float64(stats.PacketsLost) / float64(total)
			}
			samples = append(samples, mediaQualitySample{direction: "inbound", kind: stats.Kind, packetLoss: loss, jitter: stats.Jitter, pli: uint64(stats.PLICount), fir: uint64(stats.FIRCount), nack: uint64(stats.NACKCount)})
		case webrtc.RemoteInboundRTPStreamStats:
			samples = append(samples, mediaQualitySample{direction: "outbound", kind: stats.Kind, packetLoss: stats.FractionLost, jitter: stats.Jitter, rtt: stats.RoundTripTime, pli: uint64(stats.PLICount), fir: uint64(stats.FIRCount), nack: uint64(stats.NACKCount)})
		}
	}
	return samples
}

func (s *Server) collectSessionTelemetry(active *session.Session, snapshot session.ObservabilitySnapshot) {
	if active == nil {
		return
	}
	var totals mediaRecoveryCounters
	if active.PeerConnection != nil {
		for _, sample := range collectMediaQuality(active.PeerConnection.GetStats()) {
			telemetry.RecordMediaHealth(context.Background(), sample.direction, sample.kind, sample.packetLoss, sample.jitter, sample.rtt)
			totals.pli += sample.pli
			totals.fir += sample.fir
			totals.nack += sample.nack
		}
	}
	if snapshot.PLISent > 0 {
		totals.pli += uint64(snapshot.PLISent)
	}
	if snapshot.PLIResponse > 0 {
		totals.keyframe += uint64(snapshot.PLIResponse)
	}
	s.mu.Lock()
	if s.mediaTelemetry == nil {
		s.mediaTelemetry = make(map[string]mediaRecoveryCounters)
	}
	previous := s.mediaTelemetry[snapshot.SessionID]
	s.mediaTelemetry[snapshot.SessionID] = totals
	s.mu.Unlock()
	recordDelta := func(kind string, current, before uint64) {
		if current > before {
			telemetry.RecordMediaRecoveryCount(context.Background(), "inbound", kind, "success", int64(current-before))
		}
	}
	recordDelta("pli", totals.pli, previous.pli)
	recordDelta("fir", totals.fir, previous.fir)
	recordDelta("nack", totals.nack, previous.nack)
	recordDelta("keyframe", totals.keyframe, previous.keyframe)
}
