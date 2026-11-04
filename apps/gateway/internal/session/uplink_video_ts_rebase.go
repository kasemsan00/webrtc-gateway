package session

import (
	"fmt"
	"time"
)

// SIP_VIDEO_TS_REBASE. Before the agent answers, Asterisk plays a waiting
// video with RTP timestamps 0..691020, then bridges the gateway's video on
// the same SSRC and sequence, passing our timestamp through. The browser's
// first timestamp is random, so about half the time it is "older" mod 2^32
// than the waiting video. Android Linphone's oRTP then drops the first agent
// packet (the new SPS), and the decoder runs the agent IDR on the stale
// 640x480 SPS and freezes. Starting every call's outbound video at a fixed
// base above the waiting video's range keeps the agent stream "newer".

const (
	// DefaultSIPVideoTSBase is 60s at 90 kHz.
	DefaultSIPVideoTSBase = uint32(5400000)
	// uplinkVideoTSReorderWindow is how far behind the newest browser
	// timestamp a packet may be and still map on the current offset (a late
	// slice of a recent frame). Older than this is a new timeline.
	uplinkVideoTSReorderWindow = uint32(90000)
)

// uplinkVideoTSRebaser maps browser RTP timestamps onto the outbound
// timeline toward SIP: out = base + (in - firstIn) in uint32 arithmetic, so
// deltas, wrap-around and same-timestamp slices are preserved exactly. An
// SSRC change, a backward step beyond the reorder window, or a forward jump
// beyond maxReasonableH264TimestampGap continues the timeline from the
// newest outbound timestamp by the wall time since the last packet. The
// newest outbound timestamp never goes backward.
type uplinkVideoTSRebaser struct {
	started bool
	base    uint32
	ssrc    uint32
	lastIn  uint32 // newest browser timestamp on the current offset
	lastOut uint32 // outbound timestamp for lastIn; never decreases
	lastAt  time.Time
	discs   uint64
}

type uplinkVideoTSDiscontinuity struct {
	reason           string
	prevIn, in       uint32
	prevOut, out     uint32
	prevSSRC, ssrc   uint32
	wall             time.Duration
	count            uint64
	startedThisCall  bool
	firstBrowserTS   uint32
	firstOutboundTS  uint32
	firstBrowserSSRC uint32
}

// mapTS returns the outbound timestamp for one forwarded browser packet.
// event is non-nil for the first packet of the call and for each
// discontinuity.
func (r *uplinkVideoTSRebaser) mapTS(in, ssrc uint32, now time.Time) (uint32, *uplinkVideoTSDiscontinuity) {
	if !r.started {
		r.started = true
		r.ssrc = ssrc
		r.lastIn = in
		r.lastOut = r.base
		r.lastAt = now
		return r.base, &uplinkVideoTSDiscontinuity{
			reason: "start", startedThisCall: true,
			firstBrowserTS: in, firstOutboundTS: r.base, firstBrowserSSRC: ssrc,
		}
	}
	delta := in - r.lastIn
	if ssrc == r.ssrc {
		switch {
		case int32(delta) >= 0 && delta <= maxReasonableH264TimestampGap:
			r.lastIn = in
			r.lastOut += delta
			r.lastAt = now
			return r.lastOut, nil
		case int32(delta) < 0 && r.lastIn-in <= uplinkVideoTSReorderWindow:
			// Late packet of an earlier frame: same offset, timeline untouched.
			return r.lastOut - (r.lastIn - in), nil
		}
	}
	reason := "ts-jump"
	switch {
	case ssrc != r.ssrc:
		reason = "ssrc-change"
	case int32(delta) < 0:
		reason = "ts-backward"
	}
	step, wall := wallClockH264TimestampStep(r.lastAt, now)
	r.discs++
	d := &uplinkVideoTSDiscontinuity{
		reason: reason, prevIn: r.lastIn, in: in, prevOut: r.lastOut, out: r.lastOut + step,
		prevSSRC: r.ssrc, ssrc: ssrc, wall: wall, count: r.discs,
	}
	r.ssrc = ssrc
	r.lastIn = in
	r.lastOut += step
	r.lastAt = now
	return r.lastOut, d
}

// uplinkVideoOutboundTSLocked is the outbound RTP timestamp toward SIP for a
// browser packet that is about to be forwarded. Every packet built from it
// (SPS/PPS injection, STAP-A NALs, the packet itself) uses this value. With
// SIP_VIDEO_TS_REBASE=off it is the browser timestamp. The caller holds s.mu.
func (s *Session) uplinkVideoOutboundTSLocked(browserTS, browserSSRC uint32, now time.Time) uint32 {
	if !s.sipVideoTSRebase {
		return browserTS
	}
	if !s.uplinkTSRebaser.started {
		s.uplinkTSRebaser.base = s.sipVideoTSBaseOrDefault()
	}
	out, d := s.uplinkTSRebaser.mapTS(browserTS, browserSSRC, now)
	if d == nil {
		return out
	}
	if d.startedThisCall {
		fmt.Printf("[%s] uplink_video_ts_rebase start browser_ts=%d base=%d browser_ssrc=%d\n",
			s.ID, d.firstBrowserTS, d.firstOutboundTS, d.firstBrowserSSRC)
		return out
	}
	if d.count <= 10 || d.count%100 == 0 {
		fmt.Printf("[%s] uplink_video_ts_rebase discontinuity reason=%s browser_ts_prev=%d browser_ts=%d out_prev=%d out=%d browser_ssrc_prev=%d browser_ssrc=%d wall_ms=%d count=%d\n",
			s.ID, d.reason, d.prevIn, d.in, d.prevOut, d.out, d.prevSSRC, d.ssrc, d.wall.Milliseconds(), d.count)
	}
	return out
}

func (s *Session) sipVideoTSBaseOrDefault() uint32 {
	if s.sipVideoTSBase != 0 {
		return s.sipVideoTSBase
	}
	return DefaultSIPVideoTSBase
}
