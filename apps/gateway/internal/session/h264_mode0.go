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

type h264Mode0Reassembler struct {
	active      bool
	ssrc        uint32
	timestamp   uint32
	nextSeq     uint16
	payloadType uint8
	nal         []byte
}

func (r *h264Mode0Reassembler) reset() {
	r.active = false
	r.nal = nil
}

// push converts an FU-A sequence to one Single NAL Unit RTP packet. Single NAL
// and aggregation packets pass through; STAP-A is de-aggregated by the existing
// forwarding path. Malformed, discontinuous, or oversized FU-A sequences are
// dropped instead of sending mode-1 packets to a mode-0 SIP peer.
func (r *h264Mode0Reassembler) push(packet *rtp.Packet) (*rtp.Packet, h264Mode0Result) {
	if packet == nil || len(packet.Payload) == 0 {
		r.reset()
		return nil, h264Mode0Dropped
	}

	nalType := packet.Payload[0] & 0x1f
	if nalType != 28 {
		r.reset()
		// STAP-A is intentionally passed to the existing de-aggregation path.
		// All other aggregation and fragmentation packet types violate mode 0.
		if nalType >= 24 && nalType != 24 {
			return nil, h264Mode0Dropped
		}
		return packet, h264Mode0Passthrough
	}
	if len(packet.Payload) < 3 {
		r.reset()
		return nil, h264Mode0Dropped
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
			return nil, h264Mode0Dropped
		}
		r.nextSeq++
		r.nal = append(r.nal, packet.Payload[2:]...)
	}

	if len(r.nal) > maxH264Mode0NALPayload {
		r.reset()
		return nil, h264Mode0Dropped
	}
	if !end {
		return nil, h264Mode0Buffered
	}

	payload := append([]byte(nil), r.nal...)
	out := &rtp.Packet{
		Header:  packet.Header,
		Payload: payload,
	}
	out.PayloadType = r.payloadType
	r.reset()
	return out, h264Mode0Reassembled
}
