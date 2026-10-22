package session

import "github.com/pion/rtp"

// RFC 6184 packetization-mode=0 permits only Single NAL Unit packets. The
// largest RTP packet that fits in an IPv4 UDP datagram has a 65,507-byte UDP
// payload; reserve the fixed 12-byte RTP header for the reconstructed packet.
const maxH264Mode0NALPayload = 65507 - 12

type h264Mode0Result uint8

const (
	h264Mode0Passthrough h264Mode0Result = iota
	h264Mode0Buffered
	h264Mode0Reassembled
	h264Mode0Dropped
)

type h264Mode0Decision struct {
	packet   *rtp.Packet
	result   h264Mode0Result
	reason   string
	nalBytes int
}

type h264Mode0Reassembler struct {
	active      bool
	ssrc        uint32
	timestamp   uint32
	nextSeq     uint16
	payloadType uint8
	nal         []byte
}

// pushShortFUA handles an FU-A that has no fragment byte. A start+end packet
// is a complete one-byte NAL, which is what a tiny low-bitrate slice looks
// like. Anything shorter that does not finish an open frame is ignored.
func (r *h264Mode0Reassembler) pushShortFUA(packet *rtp.Packet) (h264Mode0Decision, bool) {
	if len(packet.Payload) >= 3 {
		return h264Mode0Decision{}, false
	}
	active := r.active
	if len(packet.Payload) == 2 && packet.Payload[1]&0x80 != 0 && packet.Payload[1]&0x40 != 0 {
		nalType := packet.Payload[1] & 0x1f
		out := &rtp.Packet{
			Header:  packet.Header,
			Payload: []byte{packet.Payload[0]&0xe0 | nalType},
		}
		out.PayloadType = packet.PayloadType
		r.reset()
		return h264Mode0Decision{packet: out, result: h264Mode0Reassembled}, true
	}
	r.reset()
	if active {
		return h264Mode0Decision{result: h264Mode0Dropped, reason: "discontinuous-fua"}, true
	}
	return h264Mode0Decision{result: h264Mode0Dropped, reason: "short-nal"}, true
}

func (r *h264Mode0Reassembler) reset() {
	r.active = false
	r.nal = nil
}

// mode0DropArmsRecovery is true only when a broken FU-A frame should hold
// forwarding until the next IDR. A small single NAL, a one-packet FU-A, or a
// stray short packet must not.
func mode0DropArmsRecovery(reason string) bool {
	return reason == "discontinuous-fua" || reason == "oversized-fua"
}

// push converts an FU-A sequence to one Single NAL Unit RTP packet. Single NAL
// and aggregation packets pass through; STAP-A is de-aggregated by the existing
// forwarding path. A discontinuous or oversized FU-A drops that frame and
// resets, so the next start bit or single NAL is forwarded again.
func (r *h264Mode0Reassembler) push(packet *rtp.Packet) h264Mode0Decision {
	if packet == nil || len(packet.Payload) == 0 {
		return h264Mode0Decision{result: h264Mode0Dropped, reason: "empty"}
	}

	nalType := packet.Payload[0] & 0x1f
	if nalType != 28 {
		active := r.active
		r.reset()
		// STAP-A is intentionally passed to the existing de-aggregation path.
		// Other aggregation types are not a mode-0 frame. Drop the packet
		// without asking for an IDR unless it interrupted an open FU-A.
		if nalType >= 24 && nalType != 24 {
			if active {
				return h264Mode0Decision{result: h264Mode0Dropped, reason: "discontinuous-fua"}
			}
			return h264Mode0Decision{result: h264Mode0Dropped, reason: "unsupported-nal"}
		}
		if len(packet.Payload) > maxH264Mode0NALPayload {
			return h264Mode0Decision{result: h264Mode0Dropped, reason: "oversized-fua", nalBytes: len(packet.Payload)}
		}
		return h264Mode0Decision{packet: packet, result: h264Mode0Passthrough}
	}
	if decision, done := r.pushShortFUA(packet); done {
		return decision
	}

	fuIndicator := packet.Payload[0]
	fuHeader := packet.Payload[1]
	start := fuHeader&0x80 != 0
	end := fuHeader&0x40 != 0
	nalType = fuHeader & 0x1f

	if start {
		r.reset()
		r.active = true
		r.ssrc = packet.SSRC
		r.timestamp = packet.Timestamp
		r.nextSeq = packet.SequenceNumber + 1
		r.payloadType = packet.PayloadType
		r.nal = make([]byte, 1, len(packet.Payload)-1)
		r.nal[0] = fuIndicator&0xe0 | nalType
		r.nal = append(r.nal, packet.Payload[2:]...)
	} else {
		if !r.active || packet.SSRC != r.ssrc || packet.Timestamp != r.timestamp || packet.SequenceNumber != r.nextSeq {
			r.reset()
			return h264Mode0Decision{result: h264Mode0Dropped, reason: "discontinuous-fua"}
		}
		r.nextSeq++
		r.nal = append(r.nal, packet.Payload[2:]...)
	}

	if len(r.nal) > maxH264Mode0NALPayload {
		nalBytes := len(r.nal)
		r.reset()
		return h264Mode0Decision{result: h264Mode0Dropped, reason: "oversized-fua", nalBytes: nalBytes}
	}
	if !end {
		return h264Mode0Decision{result: h264Mode0Buffered}
	}

	payload := append([]byte(nil), r.nal...)
	out := &rtp.Packet{
		Header:  packet.Header,
		Payload: payload,
	}
	out.PayloadType = r.payloadType
	r.reset()
	return h264Mode0Decision{packet: out, result: h264Mode0Reassembled}
}
