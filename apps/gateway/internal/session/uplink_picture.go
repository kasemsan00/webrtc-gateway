package session

import (
	"fmt"
	"net"
	"strconv"
	"time"
)

// uplinkPictureMarkerHold is how long the last packet of a picture waits
// before it is sent with the marker bit. The next packet usually arrives
// first and decides the marker; this only flushes a picture that has ended.
const uplinkPictureMarkerHold = 40 * time.Millisecond

// heldUplinkVideo is one WebRTC→SIP packet waiting to learn whether it is
// the last slice of its RTP timestamp.
type heldUplinkVideo struct {
	conn        *net.UDPConn
	addr        net.UDPAddr
	data        []byte
	keyframe    bool
	rtpTS       uint32
	packetCount int
	marker      bool
}

// uplinkPictureHold keeps one packet. A later packet with the same timestamp
// emits the held one with the marker cleared. A later timestamp, or a flush,
// emits it with the marker set. Release order is the enqueue order.
// next.data must be an owned copy: the WebRTC read buffer is reused.
type uplinkPictureHold struct {
	set bool
	pkt heldUplinkVideo
}

func (h *uplinkPictureHold) push(next heldUplinkVideo) (prev heldUplinkVideo, ok bool) {
	if !h.set {
		h.pkt = next
		h.set = true
		return heldUplinkVideo{}, false
	}
	prev = h.pkt
	prev.marker = h.pkt.rtpTS != next.rtpTS
	setRTPMarker(prev.data, prev.marker)
	h.pkt = next
	return prev, true
}

func (h *uplinkPictureHold) flush() (prev heldUplinkVideo, ok bool) {
	if !h.set {
		return heldUplinkVideo{}, false
	}
	prev = h.pkt
	prev.marker = true
	setRTPMarker(prev.data, true)
	h.set = false
	h.pkt = heldUplinkVideo{}
	return prev, true
}

func setRTPMarker(p []byte, marker bool) {
	if len(p) < 2 {
		return
	}
	if marker {
		p[1] |= 0x80
	} else {
		p[1] &^= 0x80
	}
}

func rtpHeaderLen(p []byte) int {
	if len(p) < 12 || p[0]>>6 != 2 {
		return -1
	}
	off := 12 + int(p[0]&0x0f)*4
	if p[0]&0x10 != 0 {
		if len(p) < off+4 {
			return -1
		}
		extWords := int(p[off+2])<<8 | int(p[off+3])
		off += 4 + extWords*4
	}
	if off > len(p) {
		return -1
	}
	return off
}

func rtpPayload(p []byte) []byte {
	off := rtpHeaderLen(p)
	if off < 0 || off > len(p) {
		return nil
	}
	return p[off:]
}

// uplinkHeldKeyframe is true for an IDR NAL or FU-A IDR fragment. SPS/PPS
// ride in front of that IDR and must not start a new pacer account.
func uplinkHeldKeyframe(payload []byte, flagged bool) bool {
	if len(payload) == 0 {
		return flagged
	}
	switch payload[0] & 0x1f {
	case 5:
		return true
	case 7, 8:
		return false
	case 28:
		if len(payload) > 1 && payload[1]&0x1f == 5 {
			return true
		}
	}
	return flagged
}

// h264FirstMBInSlice reads first_mb_in_slice from a single-NAL or starting
// FU-A payload. It is the first unsigned Exp-Golomb value in the slice header.
func h264FirstMBInSlice(payload []byte) (uint, bool) {
	if len(payload) < 2 {
		return 0, false
	}
	var body []byte
	switch payload[0] & 0x1f {
	case 1, 5:
		body = payload[1:]
	case 28:
		if len(payload) < 3 || payload[1]&0x80 == 0 {
			return 0, false
		}
		inner := payload[1] & 0x1f
		if inner != 1 && inner != 5 {
			return 0, false
		}
		body = payload[2:]
	default:
		return 0, false
	}
	r := bitReader{data: stripEmulationPrevention(body)}
	return r.ue()
}

// maybeLogFIRIDRSlice prints one line per slice of the first IDR picture
// forwarded after a SIP picture_fast_update. Slices that still belong to the
// picture that provoked the FIR are skipped.
func (s *Session) maybeLogFIRIDRSlice(payload []byte, ts uint32, marker bool) {
	if s == nil || len(payload) == 0 || payload[0]&0x1f != 5 {
		return
	}
	s.mu.Lock()
	if !s.firIDRSliceLog {
		s.mu.Unlock()
		return
	}
	if s.uplinkVideoIDRTimestampSeen && !s.firIDRSliceTSSet && ts == s.uplinkVideoLastIDRTimestamp {
		s.mu.Unlock()
		return
	}
	if !s.firIDRSliceTSSet {
		s.firIDRSliceTS = ts
		s.firIDRSliceTSSet = true
	}
	if ts != s.firIDRSliceTS {
		s.firIDRSliceLog = false
		s.mu.Unlock()
		return
	}
	if marker {
		s.firIDRSliceLog = false
	}
	id := s.ID
	s.mu.Unlock()
	mbText := "-1"
	if mb, ok := h264FirstMBInSlice(payload); ok {
		mbText = strconv.FormatUint(uint64(mb), 10)
	}
	markerBit := 0
	if marker {
		markerBit = 1
	}
	fmt.Printf("[%s] uplink_idr_slice ts=%d first_mb=%s nal_bytes=%d marker=%d\n",
		id, ts, mbText, len(payload), markerBit)
}

// armUplinkPictureFlush sends the held packet if nothing newer arrives.
// gen is the picture generation captured when that packet was held.
func (s *Session) armUplinkPictureFlush(gen uint64) {
	time.AfterFunc(uplinkPictureMarkerHold, func() {
		if s == nil {
			return
		}
		s.uplinkPictureMu.Lock()
		if s.uplinkPictureGen != gen || !s.uplinkPicture.set {
			s.uplinkPictureMu.Unlock()
			return
		}
		pkt, ok := s.uplinkPicture.flush()
		s.uplinkPictureGen++
		s.uplinkPictureMu.Unlock()
		if ok {
			s.deliverUplinkVideo(pkt)
		}
	})
}
