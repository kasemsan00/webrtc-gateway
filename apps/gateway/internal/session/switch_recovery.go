package session

import (
	"fmt"
	"time"
)

const (
	defaultSwitchRecoveryWindow = 5 * time.Second
	defaultSwitchStableWindow   = 750 * time.Millisecond

	defaultSwitchRTPMinPacketDelta         = 30
	defaultSwitchRTPMaxGapDelta            = 12
	defaultSwitchRTPMaxMissingDelta        = 20
	defaultSwitchRTPMaxOutOfOrderDelta     = 20
	defaultSwitchRTPMaxReorderDropDelta    = 0
	defaultSwitchRTPMaxReorderTimeoutDelta = 5
	switchRTPUnstableLogInterval           = 500 * time.Millisecond
)

// VideoRecoverySummary captures SIP->WebRTC RTP counters for the current
// @switch recovery window. It is intentionally plain data so hot-path updates
// stay cheap and do not perform I/O.
type VideoRecoverySummary struct {
	Packets         int
	Gaps            int
	Missing         int
	OutOfOrder      int
	Duplicates      int
	ReorderBuffered int64
	ReorderReleased int64
	ReorderDropped  int64
	ReorderTimedOut int64
	ReorderPending  int
	LastKeyframeAge time.Duration
}

type switchRTPStabilityPolicy struct {
	MinPacketDelta         int
	MaxGapDelta            int
	MaxMissingDelta        int
	MaxOutOfOrderDelta     int
	MaxReorderDropDelta    int64
	MaxReorderTimeoutDelta int64
}

type switchRTPStabilityDelta struct {
	Packets         int
	Gaps            int
	Missing         int
	OutOfOrder      int
	ReorderDropped  int64
	ReorderTimedOut int64
}

func normalizePositiveDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func normalizePositiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func normalizePositiveInt64(value, fallback int) int64 {
	if value <= 0 {
		return int64(fallback)
	}
	return int64(value)
}

func normalizeNonNegativeInt64(value, fallback int) int64 {
	if value < 0 {
		return int64(fallback)
	}
	return int64(value)
}

func nonNegativeDeltaInt(current, baseline int) int {
	if current <= baseline {
		return 0
	}
	return current - baseline
}

func nonNegativeDeltaInt64(current, baseline int64) int64 {
	if current <= baseline {
		return 0
	}
	return current - baseline
}

// StartSwitchVideoRecovery starts a bounded, stateful recovery window for a
// SIP @switch transition. It also activates the existing recovery-burst policy
// with a shorter switch-specific max window.
func (s *Session) StartSwitchVideoRecovery(maxWindow, stableWindow time.Duration) {
	s.startSwitchVideoRecovery(0, 0, false, maxWindow, stableWindow)
}

// StartSwitchVideoRecoveryIfAuthoritative starts recovery only if the switch
// token still belongs to the current call and latest accepted generation.
func (s *Session) StartSwitchVideoRecoveryIfAuthoritative(generation int, mediaEpoch uint64, maxWindow, stableWindow time.Duration) bool {
	return s.startSwitchVideoRecovery(generation, mediaEpoch, true, maxWindow, stableWindow)
}

func (s *Session) startSwitchVideoRecovery(generation int, mediaEpoch uint64, requireAuthority bool, maxWindow, stableWindow time.Duration) bool {
	if !s.VideoRecoveryBurstEnabled {
		if requireAuthority {
			return s.IsSwitchVideoAuthority(generation, mediaEpoch)
		}
		return true
	}

	now := time.Now()
	maxWindow = normalizePositiveDuration(maxWindow, defaultSwitchRecoveryWindow)
	stableWindow = normalizePositiveDuration(stableWindow, defaultSwitchStableWindow)

	s.mu.Lock()
	if requireAuthority && (s.MediaEpoch != mediaEpoch || s.SwitchGeneration != generation) {
		s.mu.Unlock()
		return false
	}
	s.SwitchReceivedAt = now
	s.SwitchVideoRecoveryStartedAt = now
	s.SwitchVideoRecoveryUntil = now.Add(maxWindow)
	s.SwitchVideoRecoveryFirstKeyframeAt = time.Time{}
	s.SwitchVideoRecoveryStableWindow = stableWindow
	s.SwitchVideoRecoveryOneShotPLI = true
	s.SwitchVideoRecoverySummary = VideoRecoverySummary{}
	s.SwitchVideoRecoveryRTPBaseline = VideoRecoverySummary{}
	s.SwitchVideoRecoveryRTPBaselineAt = time.Time{}
	s.SwitchVideoRecoveryLastUnstableLog = time.Time{}
	s.SwitchVideoRecoveryUnstableCount = 0
	s.VideoRecoveryBurstStartedAt = now
	s.VideoRecoveryBurstUntil = now.Add(maxWindow)
	s.VideoRecoveryBurstLastReason = "switch"
	if s.VideoRecoveryBurstUntil.After(s.VideoRTCPFallbackUntil) {
		s.VideoRTCPFallbackUntil = s.VideoRecoveryBurstUntil
	}
	interval := s.VideoRecoveryBurstInterval
	stale := s.VideoRecoveryBurstStale
	firStale := s.VideoRecoveryBurstFIRStale
	if firStale < stale {
		firStale = stale
	}
	until := s.VideoRecoveryBurstUntil
	s.mu.Unlock()

	fmt.Printf("[%s] switch_recovery_start until=%s stableWindow=%s\n",
		s.ID, until.Format(time.RFC3339Nano), stableWindow)
	fmt.Printf("[%s] 📈 video_recovery_window_start reason=switch until=%s\n", s.ID, until.Format(time.RFC3339Nano))
	fmt.Printf("[%s] 📈 recovery_policy interval=%s stale=%s firStale=%s\n", s.ID, interval, stale, firStale)
	if requireAuthority && !s.IsSwitchVideoAuthority(generation, mediaEpoch) {
		return false
	}
	_ = s.FlushPendingBrowserKeyframeRequest("switch")
	return true
}

func (s *Session) isSwitchVideoRecoveryActiveLocked(now time.Time) bool {
	return !s.SwitchVideoRecoveryUntil.IsZero() && now.Before(s.SwitchVideoRecoveryUntil)
}

func (s *Session) finishSwitchVideoRecoveryLocked(now time.Time, reason string) {
	if s.SwitchVideoRecoveryStartedAt.IsZero() && s.SwitchVideoRecoveryUntil.IsZero() {
		return
	}

	startedAt := s.SwitchVideoRecoveryStartedAt
	firstKeyframeAt := s.SwitchVideoRecoveryFirstKeyframeAt
	summary := s.SwitchVideoRecoverySummary

	s.SwitchVideoRecoveryStartedAt = time.Time{}
	s.SwitchVideoRecoveryUntil = time.Time{}
	s.SwitchVideoRecoveryFirstKeyframeAt = time.Time{}
	s.SwitchVideoRecoveryOneShotPLI = false
	s.SwitchVideoRecoverySummary = VideoRecoverySummary{}
	s.SwitchVideoRecoveryRTPBaseline = VideoRecoverySummary{}
	s.SwitchVideoRecoveryRTPBaselineAt = time.Time{}
	s.SwitchVideoRecoveryLastUnstableLog = time.Time{}
	s.SwitchVideoRecoveryUnstableCount = 0

	recoveryMS := int64(-1)
	firstKeyframeMS := int64(-1)
	if !startedAt.IsZero() {
		recoveryMS = now.Sub(startedAt).Milliseconds()
		if !firstKeyframeAt.IsZero() {
			firstKeyframeMS = firstKeyframeAt.Sub(startedAt).Milliseconds()
		}
	}

	fmt.Printf("[%s] switch_recovery_end reason=%s recovery_ms=%d first_keyframe_ms=%d packets=%d gaps=%d missing=%d ooo=%d dup=%d reorder(buf=%d rel=%d drop=%d to=%d pend=%d) keyframeAge=%s\n",
		s.ID, reason, recoveryMS, firstKeyframeMS,
		summary.Packets, summary.Gaps, summary.Missing, summary.OutOfOrder, summary.Duplicates,
		summary.ReorderBuffered, summary.ReorderReleased, summary.ReorderDropped, summary.ReorderTimedOut,
		summary.ReorderPending, summary.LastKeyframeAge)
}

func (s *Session) switchRTPStabilityPolicyLocked() switchRTPStabilityPolicy {
	return switchRTPStabilityPolicy{
		MinPacketDelta:         normalizePositiveInt(s.SwitchVideoRTPMinPacketDelta, defaultSwitchRTPMinPacketDelta),
		MaxGapDelta:            normalizePositiveInt(s.SwitchVideoRTPMaxGapDelta, defaultSwitchRTPMaxGapDelta),
		MaxMissingDelta:        normalizePositiveInt(s.SwitchVideoRTPMaxMissingDelta, defaultSwitchRTPMaxMissingDelta),
		MaxOutOfOrderDelta:     normalizePositiveInt(s.SwitchVideoRTPMaxOutOfOrderDelta, defaultSwitchRTPMaxOutOfOrderDelta),
		MaxReorderDropDelta:    normalizeNonNegativeInt64(s.SwitchVideoRTPMaxReorderDropDelta, defaultSwitchRTPMaxReorderDropDelta),
		MaxReorderTimeoutDelta: normalizePositiveInt64(s.SwitchVideoRTPMaxReorderTimeoutDelta, defaultSwitchRTPMaxReorderTimeoutDelta),
	}
}

func switchRTPDelta(current, baseline VideoRecoverySummary) switchRTPStabilityDelta {
	return switchRTPStabilityDelta{
		Packets:         nonNegativeDeltaInt(current.Packets, baseline.Packets),
		Gaps:            nonNegativeDeltaInt(current.Gaps, baseline.Gaps),
		Missing:         nonNegativeDeltaInt(current.Missing, baseline.Missing),
		OutOfOrder:      nonNegativeDeltaInt(current.OutOfOrder, baseline.OutOfOrder),
		ReorderDropped:  nonNegativeDeltaInt64(current.ReorderDropped, baseline.ReorderDropped),
		ReorderTimedOut: nonNegativeDeltaInt64(current.ReorderTimedOut, baseline.ReorderTimedOut),
	}
}

func switchRTPStabilityFailure(delta switchRTPStabilityDelta, policy switchRTPStabilityPolicy) string {
	if delta.Packets < policy.MinPacketDelta {
		return "insufficient-packet-progress"
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
	if delta.ReorderDropped > policy.MaxReorderDropDelta {
		return "reorder-drop-delta"
	}
	if delta.ReorderTimedOut > policy.MaxReorderTimeoutDelta {
		return "reorder-timeout-delta"
	}
	return ""
}

func (s *Session) resetSwitchRTPStableBaselineLocked(now time.Time, summary VideoRecoverySummary) {
	s.SwitchVideoRecoveryRTPBaseline = summary
	s.SwitchVideoRecoveryRTPBaselineAt = now
}

func (s *Session) logSwitchRTPUnstableLocked(now time.Time, reason string, delta switchRTPStabilityDelta, policy switchRTPStabilityPolicy, summary VideoRecoverySummary) {
	if !s.SwitchVideoRecoveryLastUnstableLog.IsZero() && now.Sub(s.SwitchVideoRecoveryLastUnstableLog) < switchRTPUnstableLogInterval {
		return
	}
	s.SwitchVideoRecoveryLastUnstableLog = now
	elapsedMS := int64(-1)
	if !s.SwitchVideoRecoveryStartedAt.IsZero() {
		elapsedMS = now.Sub(s.SwitchVideoRecoveryStartedAt).Milliseconds()
	}
	fmt.Printf("[%s] switch_recovery_unstable_rtp reason=%s elapsed_ms=%d window_ms=%d delta(packets=%d gaps=%d missing=%d ooo=%d drop=%d to=%d) threshold(minPackets=%d maxGap=%d maxMissing=%d maxOOO=%d maxDrop=%d maxTimeout=%d) total(packets=%d gaps=%d missing=%d ooo=%d drop=%d to=%d)\n",
		s.ID, reason, elapsedMS,
		now.Sub(s.SwitchVideoRecoveryRTPBaselineAt).Milliseconds(),
		delta.Packets, delta.Gaps, delta.Missing, delta.OutOfOrder, delta.ReorderDropped, delta.ReorderTimedOut,
		policy.MinPacketDelta, policy.MaxGapDelta, policy.MaxMissingDelta, policy.MaxOutOfOrderDelta, policy.MaxReorderDropDelta, policy.MaxReorderTimeoutDelta,
		summary.Packets, summary.Gaps, summary.Missing, summary.OutOfOrder, summary.ReorderDropped, summary.ReorderTimedOut)
}

// ConsumeSwitchPLIBypass permits one switch-triggered PLI to bypass the normal
// minimum interval after prerequisites are known.
func (s *Session) ConsumeSwitchPLIBypass() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if !s.isSwitchVideoRecoveryActiveLocked(now) || !s.SwitchVideoRecoveryOneShotPLI {
		return false
	}
	s.SwitchVideoRecoveryOneShotPLI = false
	return true
}

func (s *Session) IsSwitchVideoRecoveryActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isSwitchVideoRecoveryActiveLocked(time.Now())
}

func (s *Session) MarkSwitchVideoKeyframe(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isSwitchVideoRecoveryActiveLocked(now) {
		return
	}
	if s.SwitchVideoRecoveryFirstKeyframeAt.IsZero() {
		s.SwitchVideoRecoveryFirstKeyframeAt = now
		s.resetSwitchRTPStableBaselineLocked(now, s.SwitchVideoRecoverySummary)
		fmt.Printf("[%s] switch_recovery_first_keyframe elapsed_ms=%d\n",
			s.ID, now.Sub(s.SwitchVideoRecoveryStartedAt).Milliseconds())
	}
}

func (s *Session) MarkSwitchVideoProgress(now time.Time, isKeyframe bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isSwitchVideoRecoveryActiveLocked(now) {
		return
	}
	if isKeyframe && s.SwitchVideoRecoveryFirstKeyframeAt.IsZero() {
		s.SwitchVideoRecoveryFirstKeyframeAt = now
		s.resetSwitchRTPStableBaselineLocked(now, s.SwitchVideoRecoverySummary)
		fmt.Printf("[%s] switch_recovery_first_keyframe elapsed_ms=%d\n",
			s.ID, now.Sub(s.SwitchVideoRecoveryStartedAt).Milliseconds())
	}
	if s.SwitchVideoRecoveryFirstKeyframeAt.IsZero() {
		return
	}

	stableWindow := normalizePositiveDuration(s.SwitchVideoRecoveryStableWindow, defaultSwitchStableWindow)
	summary := s.SwitchVideoRecoverySummary
	if !s.SwitchVideoRecoveryRTPBaselineAt.IsZero() &&
		s.SwitchVideoRecoveryRTPBaseline.Packets == 0 &&
		summary.Packets > 0 &&
		now.Sub(s.SwitchVideoRecoveryFirstKeyframeAt) < stableWindow {
		s.resetSwitchRTPStableBaselineLocked(now, summary)
	}
	if now.Sub(s.SwitchVideoRecoveryFirstKeyframeAt) < stableWindow {
		return
	}

	if !s.SwitchVideoRTPStabilityEnabled {
		s.endVideoRecoveryBurst(now, "stable-media")
		return
	}

	if s.SwitchVideoRecoveryRTPBaselineAt.IsZero() {
		s.resetSwitchRTPStableBaselineLocked(now, summary)
		return
	}
	if now.Sub(s.SwitchVideoRecoveryRTPBaselineAt) < stableWindow {
		return
	}

	policy := s.switchRTPStabilityPolicyLocked()
	delta := switchRTPDelta(summary, s.SwitchVideoRecoveryRTPBaseline)
	if reason := switchRTPStabilityFailure(delta, policy); reason != "" {
		s.SwitchVideoRecoveryUnstableCount++
		s.logSwitchRTPUnstableLocked(now, reason, delta, policy, summary)
		s.resetSwitchRTPStableBaselineLocked(now, summary)
		return
	}

	s.endVideoRecoveryBurst(now, "stable-rtp")
}

func (s *Session) UpdateSwitchVideoRecoverySummary(summary VideoRecoverySummary) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isSwitchVideoRecoveryActiveLocked(time.Now()) {
		return
	}
	s.SwitchVideoRecoverySummary = summary
}

func (s *Session) StopSwitchVideoRecoveryIfActive(reason string) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.SwitchVideoRecoveryUntil.IsZero() || !s.SwitchVideoRecoveryStartedAt.IsZero() {
		s.finishSwitchVideoRecoveryLocked(now, reason)
	}
}
