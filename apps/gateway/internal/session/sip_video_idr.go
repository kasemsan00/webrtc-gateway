package session

import (
	"fmt"
	"time"

	"github.com/pion/rtp"
)

const (
	sipVideoIDRReplayMinInterval = 400 * time.Millisecond
	sipVideoIDRMaxReplays        = 8
	sipVideoIDRMaxPackets        = 512
	sipVideoIDRMaxBytes          = 512 * 1024
	// Do not rewrite a delivered queue still-IDR into a live timeline once
	// the cached keyframe is stale. Near @switch that poisons the decoder
	// just before the agent camera IDR arrives.
	sipVideoIDRStaleReplayMaxAge = time.Second
)

type sipVideoIDRCache struct {
	generation int
	sourceTS   uint32
	packets    []*rtp.Packet
	delivered  bool
	replays    int
	lastReplay time.Time
}

// RememberSIPVideoIDR stores the last complete SIP→WebRTC IDR so it can be
// written again if the client decoder missed it. Queue wait video often emits
// a single still-image IDR that Asterisk playback will not regenerate on PLI.
func (s *Session) RememberSIPVideoIDR(au NormalizedH264AccessUnit, delivered bool) {
	if !au.IsIDR || len(au.Packets) == 0 {
		return
	}
	cloned, bytes := cloneRTPPacketsCapped(au.Packets)
	if len(cloned) == 0 {
		return
	}

	s.mu.Lock()
	if s.State == StateEnded {
		s.mu.Unlock()
		return
	}
	s.sipVideoIDR = &sipVideoIDRCache{
		generation: au.Generation,
		sourceTS:   au.SourceTimestamp,
		packets:    cloned,
		delivered:  delivered,
	}
	if !delivered {
		s.sipVideoIDRReplayPending = true
		s.wakeSIPVideoIDRReplayLocked()
	}
	s.mu.Unlock()

	if !delivered {
		fmt.Printf("[%s] sip_video_idr_cached generation=%d packets=%d bytes=%d delivered=false\n",
			s.ID, au.Generation, len(cloned), bytes)
	}
}

// RequestSIPVideoIDRReplay queues a decoder-facing replay of the cached IDR.
// Returns true when a write is pending. This does not send RTCP to Asterisk.
func (s *Session) RequestSIPVideoIDRReplay(reason string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.canQueueSIPVideoIDRReplayLocked(time.Now()) {
		return false
	}
	alreadyQueued := s.sipVideoIDRReplayPending
	s.sipVideoIDRReplayPending = true
	s.wakeSIPVideoIDRReplayLocked()
	if !alreadyQueued {
		fmt.Printf("[%s] sip_video_idr_replay_queued reason=%s generation=%d delivered=%v\n",
			s.ID, reason, s.sipVideoIDR.generation, s.sipVideoIDR.delivered)
	}
	return true
}

// HasPendingSIPVideoIDRWrite reports whether the RTP loop should flush a cached IDR.
func (s *Session) HasPendingSIPVideoIDRWrite() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hasPendingSIPVideoIDRWriteLocked()
}

// SIPVideoIDRReplayNotify wakes the SIP video RTP loop when a cached IDR
// should be written without waiting for the next Asterisk packet.
func (s *Session) SIPVideoIDRReplayNotify() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSIPVideoIDRReplayNotifyLocked()
	return s.sipVideoIDRReplayNotify
}

// BindH264AUReplayRewriter registers the live SIP→WebRTC normalizer so cached
// IDR replays continue the outbound sequence/timestamp timeline.
func (s *Session) BindH264AUReplayRewriter(rewriter func([]*rtp.Packet) []*rtp.Packet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sipVideoIDRReplayRewriter = rewriter
}

// BindH264AUNumberer registers outbound RTP numbering so sequence/timestamp
// are assigned only after the switch video gate accepts an access unit.
func (s *Session) BindH264AUNumberer(numberer func(*NormalizedH264AccessUnit)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sipVideoAUNumberer = numberer
}

// NumberSIPVideoAccessUnit assigns outbound RTP sequence and timestamp for an
// access unit that will actually be written. No-op when no normalizer is bound.
func (s *Session) NumberSIPVideoAccessUnit(au *NormalizedH264AccessUnit) {
	if au == nil {
		return
	}
	s.mu.RLock()
	numberer := s.sipVideoAUNumberer
	s.mu.RUnlock()
	if numberer != nil {
		numberer(au)
	}
}

// WritePendingSIPVideoIDR writes a queued or never-delivered cached IDR.
// rewrite=true assigns new sequence numbers through the bound normalizer.
func (s *Session) WritePendingSIPVideoIDR(write func([]byte) (int, error)) (bool, string) {
	if write == nil {
		return false, ""
	}

	s.mu.Lock()
	if s.State == StateEnded {
		s.sipVideoIDRReplayPending = false
		s.mu.Unlock()
		return false, ""
	}
	if !s.hasPendingSIPVideoIDRWriteLocked() {
		s.mu.Unlock()
		return false, ""
	}
	cache := s.sipVideoIDR
	rewriter := s.sipVideoIDRReplayRewriter
	skipReplay := s.shouldSkipSIPVideoIDRReplayLocked(time.Now())
	reason := "undelivered"
	if cache.delivered {
		reason = "decoder-replay"
	}
	packets := cloneRTPPackets(cache.packets)
	s.sipVideoIDRReplayPending = false
	s.mu.Unlock()

	if skipReplay {
		return false, ""
	}
	if rewriter != nil {
		packets = rewriter(packets)
		reason += "-rewritten"
	}
	if len(packets) == 0 {
		return false, ""
	}

	for _, packet := range packets {
		data, err := packet.Marshal()
		if err != nil {
			s.markSIPVideoIDRWriteResult(false)
			fmt.Printf("[%s] sip_video_idr_replay_error stage=marshal seq=%d error=%v\n",
				s.ID, packet.SequenceNumber, err)
			return false, ""
		}
		if _, err := write(data); err != nil {
			s.markSIPVideoIDRWriteResult(false)
			fmt.Printf("[%s] sip_video_idr_replay_error stage=track seq=%d error=%v\n",
				s.ID, packet.SequenceNumber, err)
			return false, ""
		}
		s.CacheVideoRTPPacket(packet.SequenceNumber, data)
	}
	s.markSIPVideoIDRWriteResult(true)
	fmt.Printf("[%s] sip_video_idr_replay reason=%s packets=%d generation=%d\n",
		s.ID, reason, len(packets), cache.generation)
	return true, reason
}

func (s *Session) markSIPVideoIDRWriteResult(ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sipVideoIDR == nil || s.State == StateEnded {
		return
	}
	if ok {
		s.sipVideoIDR.delivered = true
		s.sipVideoIDR.replays++
		s.sipVideoIDR.lastReplay = time.Now()
		s.sipVideoIDRReplayPending = false
		return
	}
	s.sipVideoIDR.delivered = false
	s.sipVideoIDRReplayPending = true
	s.wakeSIPVideoIDRReplayLocked()
}

func (s *Session) canQueueSIPVideoIDRReplayLocked(now time.Time) bool {
	if s.State == StateEnded || s.sipVideoIDR == nil || len(s.sipVideoIDR.packets) == 0 {
		return false
	}
	if s.shouldSkipSIPVideoIDRReplayLocked(now) {
		return false
	}
	if s.hasPendingSIPVideoIDRWriteLocked() && !s.sipVideoIDR.delivered {
		return true
	}
	if s.sipVideoIDR.replays >= sipVideoIDRMaxReplays {
		return false
	}
	if !s.sipVideoIDR.lastReplay.IsZero() && now.Sub(s.sipVideoIDR.lastReplay) < sipVideoIDRReplayMinInterval {
		return false
	}
	return true
}

func (s *Session) shouldSkipSIPVideoIDRReplayLocked(now time.Time) bool {
	if s.SwitchVideoGateActive || s.isSwitchVideoRecoveryActiveLocked(now) {
		return true
	}
	if s.sipVideoIDR == nil {
		return false
	}
	if s.SwitchGeneration > s.sipVideoIDR.generation {
		return true
	}
	if s.sipVideoIDR.delivered && !s.LastKeyframe.IsZero() && now.Sub(s.LastKeyframe) > sipVideoIDRStaleReplayMaxAge {
		return true
	}
	return false
}

func (s *Session) hasPendingSIPVideoIDRWriteLocked() bool {
	if s.State == StateEnded || s.sipVideoIDR == nil || len(s.sipVideoIDR.packets) == 0 {
		return false
	}
	if s.shouldSkipSIPVideoIDRReplayLocked(time.Now()) {
		return false
	}
	return s.sipVideoIDRReplayPending || !s.sipVideoIDR.delivered
}

func (s *Session) wakeSIPVideoIDRReplayLocked() {
	s.ensureSIPVideoIDRReplayNotifyLocked()
	select {
	case s.sipVideoIDRReplayNotify <- struct{}{}:
	default:
	}
}

func (s *Session) ensureSIPVideoIDRReplayNotifyLocked() {
	if s.sipVideoIDRReplayNotify == nil {
		s.sipVideoIDRReplayNotify = make(chan struct{}, 1)
	}
}

func (s *Session) clearSIPVideoIDRCacheLocked() {
	s.sipVideoIDR = nil
	s.sipVideoIDRReplayPending = false
}

func cloneRTPPackets(packets []*rtp.Packet) []*rtp.Packet {
	cloned, _ := cloneRTPPacketsCapped(packets)
	return cloned
}

func cloneRTPPacketsCapped(packets []*rtp.Packet) ([]*rtp.Packet, int) {
	out := make([]*rtp.Packet, 0, len(packets))
	bytes := 0
	for _, packet := range packets {
		if packet == nil {
			continue
		}
		if len(out) >= sipVideoIDRMaxPackets {
			return nil, 0
		}
		payload := append([]byte(nil), packet.Payload...)
		bytes += len(payload)
		if bytes > sipVideoIDRMaxBytes {
			return nil, 0
		}
		clone := packet.Clone()
		if clone == nil {
			header := packet.Header
			clone = &rtp.Packet{Header: header, Payload: payload}
		} else {
			clone.Payload = payload
		}
		out = append(out, clone)
	}
	return out, bytes
}
