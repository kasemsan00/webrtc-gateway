package session

import (
	"fmt"
	"time"

	"github.com/pion/rtp"
)

// uplinkH264AU follows source access-unit boundaries, including encoders that
// reuse a timestamp after a marker. It belongs to one track reader. Session.mu
// protects the session fields touched by these methods.
type uplinkH264AU struct {
	set       bool
	ssrc      uint32
	timestamp uint32
	ended     bool
	blocked   bool
	lastPLI   time.Time
	heldIDRs  uint64
	// idrStarted is true once an IDR start (single NAL, FU-A start, or a
	// STAP-A carrying an IDR) of this AU was actually written to SIP, with
	// its SPS/PPS prefix. FU-A IDR continuations are forwarded only then.
	idrStarted bool
	orphaned   bool
	orphanAUs  uint64
}

// isFUAIDRContinuation is a middle or end FU-A fragment of an IDR slice.
func isFUAIDRContinuation(payload []byte) bool {
	return len(payload) > 1 && payload[0]&0x1f == 28 && payload[1]&0x80 == 0 && payload[1]&0x1f == 5
}

// dropOrphanIDRFragmentLocked handles an FU-A IDR fragment whose start never
// reached SIP (start lost, or written while the SIP video address was not
// set yet). That tail has no SPS/PPS and cannot be decoded, and writing it
// would record a "forwarded" uplink IDR that silences the startup PLI and
// the decoder-join kicks. Drop it and ask for a fresh IDR at most once a
// second (shared with the missing-parameter-set request).
func (a *uplinkH264AU) dropOrphanIDRFragmentLocked(s *Session, canWrite bool, now time.Time) (requestPLI bool) {
	if !a.orphaned {
		a.orphaned = true
		a.orphanAUs++
	}
	if !canWrite || (!a.lastPLI.IsZero() && now.Sub(a.lastPLI) < time.Second) {
		return false
	}
	a.lastPLI = now
	fmt.Printf("[%s] uplink_idr_fragment_dropped reason=no-start source_ts=%d dropped_aus=%d request_keyframe=true\n",
		s.ID, a.timestamp, a.orphanAUs)
	return true
}

// Cache replacement must invalidate the claim that the old bytes have already
// reached this AU. In particular a later STAP-A may carry newer parameter sets.
func (s *Session) uplinkParameterSetsChangedLocked(sps, pps bool) {
	if sps {
		s.uplinkInBandSPS = false
	}
	if pps {
		s.uplinkInBandPPS = false
	}
	s.uplinkSPSPPSTimestampSet = false
}

func (a *uplinkH264AU) beginLocked(s *Session, p *rtp.Packet) {
	if !a.set || a.ssrc != p.SSRC || a.timestamp != p.Timestamp || a.ended {
		a.set, a.ssrc, a.timestamp = true, p.SSRC, p.Timestamp
		a.blocked = false
		a.idrStarted = false
		a.orphaned = false
		s.uplinkSPSPPSTimestampSet = false
		s.uplinkInBandTimestampSet = false
	}
	a.ended = p.Marker
}

// Never open the startup/recovery gates on an undecodable IDR. Once its first
// slice was dropped, discard the rest of that AU even if SPS/PPS arrive late;
// forwarding its tail would produce a partial picture. Ask for a fresh IDR,
// at most once a second, until both browser parameter sets are available.
func (a *uplinkH264AU) holdLocked(s *Session, isIDR bool, now time.Time) (hold, requestPLI bool) {
	if isIDR && (!isSingleNALType(s.CachedSPS, 7) || !isSingleNALType(s.CachedPPS, 8)) {
		if !a.blocked {
			a.heldIDRs++
		}
		a.blocked = true
		if a.lastPLI.IsZero() || now.Sub(a.lastPLI) >= time.Second {
			a.lastPLI = now
			fmt.Printf("[%s] uplink_idr_wait_parameter_sets source_ts=%d sps=%v pps=%v held_idrs=%d request_keyframe=true\n",
				s.ID, a.timestamp, isSingleNALType(s.CachedSPS, 7), isSingleNALType(s.CachedPPS, 8), a.heldIDRs)
			requestPLI = true
		}
	}
	return a.blocked, requestPLI
}

// Missing sets become single-NAL packets on the same outbound clock as the
// IDR. The caller assigns the IDR's next sequence number after this prefix.
func (s *Session) uplinkIDRParameterSetPacketsLocked(pt uint8, sourceTS, outTS uint32) [][]byte {
	hasInBand := s.uplinkInBandTimestampSet && s.uplinkInBandTimestamp == sourceTS
	hasSPS := hasInBand && s.uplinkInBandSPS
	var out [][]byte
	for i, nal := range [][]byte{s.CachedSPS, s.CachedPPS} {
		// A lone PPS must be repeated after a missing SPS to restore ordering.
		if hasSPS && (i == 0 || (i == 1 && s.uplinkInBandPPS)) {
			continue
		}
		s.VideoSeq++
		p := &rtp.Packet{
			Header:  rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: s.VideoSeq, Timestamp: outTS, SSRC: s.VideoSSRC},
			Payload: forceNRI(nal),
		}
		if data, err := p.Marshal(); err == nil {
			out = append(out, data)
		}
	}
	s.LastSPSPPSInjectionTime = time.Now()
	s.noteUplinkInBandParameterSetLocked(sourceTS, true, true)
	return out
}

// Put the latest sets first even if an encoder put them after its IDR inside
// STAP-A. Already forwarded in-band sets in this AU need no second copy.
func (s *Session) prefixSTAPAIDRLocked(payload []byte, timestamp uint32) []byte {
	hasSPS := s.uplinkInBandTimestampSet && s.uplinkInBandTimestamp == timestamp && s.uplinkInBandSPS
	hasPPS := s.uplinkInBandTimestampSet && s.uplinkInBandTimestamp == timestamp && s.uplinkInBandPPS
	out := make([]byte, 0, len(payload)+len(s.CachedSPS)+len(s.CachedPPS)+4)
	appendNAL := func(nal []byte) {
		out = append(out, byte(len(nal)>>8), byte(len(nal)))
		out = append(out, nal...)
	}
	if !hasSPS {
		appendNAL(s.CachedSPS)
	}
	if !hasPPS || !hasSPS {
		appendNAL(s.CachedPPS)
	}
	for offset := 0; offset+2 <= len(payload); {
		size := int(payload[offset])<<8 | int(payload[offset+1])
		offset += 2
		if size == 0 || offset+size > len(payload) {
			break
		}
		nal := payload[offset : offset+size]
		offset += size
		if nal[0]&0x1f != 7 && nal[0]&0x1f != 8 {
			appendNAL(nal)
		}
	}
	s.noteUplinkInBandParameterSetLocked(timestamp, true, true)
	s.uplinkKeyframeParameterSetCopiesLocked(timestamp)
	return out
}
