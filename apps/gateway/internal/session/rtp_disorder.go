package session

import (
	"fmt"
	"time"
)

const (
	defaultRTPDisorderMinPacketDelta     = 300
	defaultRTPDisorderMaxGapDelta        = 45
	defaultRTPDisorderMaxMissingDelta    = 80
	defaultRTPDisorderMaxOutOfOrderDelta = 80
	defaultRTPDisorderMaxReorderTimeout  = 20
	defaultRTPDisorderConsecutiveWindows = 3
	defaultRTPDisorderLogInterval        = 5 * time.Second
	defaultRTPDisorderContainment        = 10 * time.Second
)

type RTPDisorderObservation struct {
	BadWindow          bool
	Sustained          bool
	Reason             string
	Delta              switchRTPStabilityDelta
	ConsecutiveBad     int
	ContainmentStarted bool
	ContainmentEnded   bool
	ContainmentActive  bool
	ContainmentReason  string
	SwitchGeneration   int
}

type rtpDisorderPolicy struct {
	MinPacketDelta      int
	MaxGapDelta         int
	MaxMissingDelta     int
	MaxOutOfOrderDelta  int
	MaxReorderTimeout   int64
	ConsecutiveWindows  int
	LogInterval         time.Duration
	ContainmentEnabled  bool
	ContainmentDuration time.Duration
}

func (s *Session) rtpDisorderPolicyLocked() rtpDisorderPolicy {
	return rtpDisorderPolicy{
		MinPacketDelta:      normalizePositiveInt(s.VideoRTPDisorderMinPacketDelta, defaultRTPDisorderMinPacketDelta),
		MaxGapDelta:         normalizePositiveInt(s.VideoRTPDisorderMaxGapDelta, defaultRTPDisorderMaxGapDelta),
		MaxMissingDelta:     normalizePositiveInt(s.VideoRTPDisorderMaxMissingDelta, defaultRTPDisorderMaxMissingDelta),
		MaxOutOfOrderDelta:  normalizePositiveInt(s.VideoRTPDisorderMaxOutOfOrderDelta, defaultRTPDisorderMaxOutOfOrderDelta),
		MaxReorderTimeout:   normalizePositiveInt64(s.VideoRTPDisorderMaxReorderTimeout, defaultRTPDisorderMaxReorderTimeout),
		ConsecutiveWindows:  normalizePositiveInt(s.VideoRTPDisorderConsecutiveWindows, defaultRTPDisorderConsecutiveWindows),
		LogInterval:         normalizePositiveDuration(s.VideoRTPDisorderLogInterval, defaultRTPDisorderLogInterval),
		ContainmentEnabled:  s.VideoRTPDisorderContainmentEnabled,
		ContainmentDuration: normalizePositiveDuration(s.VideoRTPDisorderContainmentDuration, defaultRTPDisorderContainment),
	}
}

func rtpDisorderFailure(delta switchRTPStabilityDelta, policy rtpDisorderPolicy) string {
	if delta.Packets < policy.MinPacketDelta {
		return ""
	}
	if delta.Gaps > policy.MaxGapDelta {
		return "gap-delta"
	}
	if delta.Missing > policy.MaxMissingDelta {
		return "missing-delta"
	}
	if delta.OutOfOrder > policy.MaxOutOfOrderDelta {
		return "ooo-delta"
	}
	if delta.ReorderTimedOut > policy.MaxReorderTimeout {
		return "reorder-timeout-delta"
	}
	return ""
}

func (s *Session) startRTPDisorderContainmentLocked(now time.Time, reason string, summary VideoRecoverySummary, delta switchRTPStabilityDelta, policy rtpDisorderPolicy) {
	s.VideoRTPDisorderContainmentStartedAt = now
	s.VideoRTPDisorderContainmentUntil = now.Add(policy.ContainmentDuration)
	s.VideoRTPDisorderContainmentReason = reason
	s.VideoRTPDisorderContainmentSummary = summary
	fmt.Printf("[%s] sip_video_rtp_disorder_containment_start reason=%s switchGeneration=%d ssrc=%d source=%s until=%s windowPackets=%d delta(gaps=%d missing=%d ooo=%d to=%d) total(packets=%d gaps=%d missing=%d ooo=%d to=%d)\n",
		s.ID, reason, s.SwitchGeneration, s.RemoteVideoSSRC, s.SIPVideoRTPSource,
		s.VideoRTPDisorderContainmentUntil.Format(time.RFC3339Nano), delta.Packets, delta.Gaps, delta.Missing, delta.OutOfOrder, delta.ReorderTimedOut,
		summary.Packets, summary.Gaps, summary.Missing, summary.OutOfOrder, summary.ReorderTimedOut)
}

func (s *Session) endRTPDisorderContainmentLocked(now time.Time, reason string, summary VideoRecoverySummary) {
	if s.VideoRTPDisorderContainmentUntil.IsZero() {
		return
	}
	duration := now.Sub(s.VideoRTPDisorderContainmentStartedAt)
	if s.VideoRTPDisorderContainmentStartedAt.IsZero() {
		duration = 0
	}
	fmt.Printf("[%s] sip_video_rtp_disorder_containment_end reason=%s startReason=%s switchGeneration=%d ssrc=%d source=%s duration=%s total(packets=%d gaps=%d missing=%d ooo=%d to=%d)\n",
		s.ID, reason, s.VideoRTPDisorderContainmentReason, s.SwitchGeneration, s.RemoteVideoSSRC, s.SIPVideoRTPSource,
		duration, summary.Packets, summary.Gaps, summary.Missing, summary.OutOfOrder, summary.ReorderTimedOut)
	s.VideoRTPDisorderContainmentUntil = time.Time{}
	s.VideoRTPDisorderContainmentStartedAt = time.Time{}
	s.VideoRTPDisorderContainmentReason = ""
	s.VideoRTPDisorderContainmentSummary = VideoRecoverySummary{}
}

// ObserveSIPVideoRTPDisorder observes a sampled SIP->WebRTC RTP summary.
// It is intended to run at the existing RTP stats cadence, not per packet.
func (s *Session) ObserveSIPVideoRTPDisorder(summary VideoRecoverySummary, now time.Time) RTPDisorderObservation {
	s.mu.Lock()
	defer s.mu.Unlock()

	obs := RTPDisorderObservation{SwitchGeneration: s.SwitchGeneration}
	if !s.VideoRTPDisorderMonitorEnabled {
		return obs
	}

	if s.VideoRTPDisorderLastSummaryAt.IsZero() {
		s.VideoRTPDisorderLastSummary = summary
		s.VideoRTPDisorderLastSummaryAt = now
		return obs
	}

	policy := s.rtpDisorderPolicyLocked()
	delta := switchRTPDelta(summary, s.VideoRTPDisorderLastSummary)
	obs.Delta = delta

	reason := rtpDisorderFailure(delta, policy)
	if reason == "" {
		s.VideoRTPDisorderConsecutiveBad = 0
		if !s.VideoRTPDisorderContainmentUntil.IsZero() {
			endReason := "metrics-recovered"
			if now.After(s.VideoRTPDisorderContainmentUntil) {
				endReason = "expired"
			}
			s.endRTPDisorderContainmentLocked(now, endReason, summary)
			obs.ContainmentEnded = true
		}
		s.VideoRTPDisorderLastSummary = summary
		s.VideoRTPDisorderLastSummaryAt = now
		return obs
	}

	if !s.VideoRTPDisorderContainmentUntil.IsZero() && now.After(s.VideoRTPDisorderContainmentUntil) {
		s.endRTPDisorderContainmentLocked(now, "expired", summary)
		obs.ContainmentEnded = true
	}

	s.VideoRTPDisorderConsecutiveBad++
	obs.BadWindow = true
	obs.Reason = reason
	obs.ConsecutiveBad = s.VideoRTPDisorderConsecutiveBad
	obs.ContainmentActive = !s.VideoRTPDisorderContainmentUntil.IsZero()
	obs.ContainmentReason = s.VideoRTPDisorderContainmentReason

	if s.VideoRTPDisorderConsecutiveBad >= policy.ConsecutiveWindows {
		obs.Sustained = true
		shouldLog := s.VideoRTPDisorderLastLogAt.IsZero() || now.Sub(s.VideoRTPDisorderLastLogAt) >= policy.LogInterval
		if shouldLog {
			s.VideoRTPDisorderLastLogAt = now
			fmt.Printf("[%s] sip_video_rtp_disorder_sustained reason=%s switchGeneration=%d ssrc=%d source=%s consecutive=%d windowPackets=%d delta(gaps=%d missing=%d ooo=%d to=%d) total(packets=%d gaps=%d missing=%d ooo=%d to=%d) keyframeAge=%s threshold(minPackets=%d maxGap=%d maxMissing=%d maxOOO=%d maxTimeout=%d)\n",
				s.ID, reason, s.SwitchGeneration, s.RemoteVideoSSRC, s.SIPVideoRTPSource,
				s.VideoRTPDisorderConsecutiveBad, delta.Packets, delta.Gaps, delta.Missing, delta.OutOfOrder, delta.ReorderTimedOut,
				summary.Packets, summary.Gaps, summary.Missing, summary.OutOfOrder, summary.ReorderTimedOut, summary.LastKeyframeAge,
				policy.MinPacketDelta, policy.MaxGapDelta, policy.MaxMissingDelta, policy.MaxOutOfOrderDelta, policy.MaxReorderTimeout)
		}

		if policy.ContainmentEnabled && (s.VideoRTPDisorderContainmentUntil.IsZero() || now.After(s.VideoRTPDisorderContainmentUntil)) {
			s.startRTPDisorderContainmentLocked(now, reason, summary, delta, policy)
			obs.ContainmentStarted = true
			obs.ContainmentActive = true
			obs.ContainmentReason = reason
		}
	}

	s.VideoRTPDisorderLastSummary = summary
	s.VideoRTPDisorderLastSummaryAt = now
	return obs
}
