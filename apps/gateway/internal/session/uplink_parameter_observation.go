package session

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Counts successful socket writes, so these diagnostics cover both injected
// and encoder-provided sets throughout the call. Guarded by Session.mu.
type uplinkParameterObservation struct {
	set       bool
	timestamp uint32
	ended     bool
	sps       int
	pps       int
	spsSeq    uint16
	ppsSeq    uint16
	seenIDR   bool
	idrs      uint64
	missing   uint64
	lastLog   time.Time
}

func (s *Session) observeUplinkParameterSets(data, payload []byte, timestamp uint32, marker, keyframe bool, now time.Time) {
	if len(data) < 12 || len(payload) == 0 {
		return
	}
	s.mu.Lock()
	o := &s.uplinkParameterObservation
	if !o.set || o.timestamp != timestamp || o.ended {
		o.set, o.timestamp = true, timestamp
		o.sps, o.pps, o.spsSeq, o.ppsSeq, o.seenIDR = 0, 0, 0, 0, false
	}
	o.ended = marker
	seq := binary.BigEndian.Uint16(data[2:4])
	switch payload[0] & 0x1f {
	case 7:
		o.sps++
		o.spsSeq = seq
	case 8:
		o.pps++
		o.ppsSeq = seq
	}
	logIDR := keyframe && !o.seenIDR
	if logIDR {
		o.seenIDR = true
		o.idrs++
		if o.sps == 0 || o.pps == 0 {
			o.missing++
		}
	}
	logIDR = logIDR && (o.lastLog.IsZero() || now.Sub(o.lastLog) >= time.Second)
	observation := *o
	mode := s.SIPVideoPacketizationMode
	if logIDR {
		o.lastLog = now
	}
	s.mu.Unlock()
	if logIDR {
		fmt.Printf("[%s] uplink_idr_parameter_sets direction=browser-to-sip mode=%d ts=%d sps=%d pps=%d sps_seq=%d pps_seq=%d idr_seq=%d idrs=%d missing_parameter_sets=%d\n",
			s.ID, mode, timestamp, observation.sps, observation.pps, observation.spsSeq, observation.ppsSeq, seq, observation.idrs, observation.missing)
	}
}
