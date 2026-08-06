package session

import (
	"bytes"
	"testing"

	"github.com/pion/rtp"
)

func mode0FUAPacket(seq uint16, timestamp uint32, start, end bool, nalType byte, fragment []byte) *rtp.Packet {
	fuHeader := nalType & 0x1f
	if start {
		fuHeader |= 0x80
	}
	if end {
		fuHeader |= 0x40
	}
	return &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    103,
			SequenceNumber: seq,
			Timestamp:      timestamp,
			SSRC:           99,
			Marker:         end,
		},
		Payload: append([]byte{0x7c, fuHeader}, fragment...),
	}
}

func TestH264Mode0ReassemblerReassemblesFUA(t *testing.T) {
	r := h264Mode0Reassembler{}

	if packet, result := r.push(mode0FUAPacket(10, 9000, true, false, 5, []byte{0x01, 0x02})); packet != nil || result != h264Mode0Buffered {
		t.Fatalf("start result = (%v, %v), want buffered", packet, result)
	}
	if packet, result := r.push(mode0FUAPacket(11, 9000, false, false, 5, []byte{0x03})); packet != nil || result != h264Mode0Buffered {
		t.Fatalf("middle result = (%v, %v), want buffered", packet, result)
	}
	packet, result := r.push(mode0FUAPacket(12, 9000, false, true, 5, []byte{0x04, 0x05}))
	if result != h264Mode0Reassembled || packet == nil {
		t.Fatalf("end result = (%v, %v), want reassembled", packet, result)
	}
	if want := []byte{0x65, 0x01, 0x02, 0x03, 0x04, 0x05}; !bytes.Equal(packet.Payload, want) {
		t.Fatalf("payload = %x, want %x", packet.Payload, want)
	}
	if !packet.Marker || packet.PayloadType != 103 {
		t.Fatalf("header marker=%v pt=%d, want marker=true pt=103", packet.Marker, packet.PayloadType)
	}
}

func TestH264Mode0ReassemblerDropsSequenceGap(t *testing.T) {
	r := h264Mode0Reassembler{}
	r.push(mode0FUAPacket(10, 9000, true, false, 1, []byte{0x01}))

	if packet, result := r.push(mode0FUAPacket(12, 9000, false, true, 1, []byte{0x02})); packet != nil || result != h264Mode0Dropped {
		t.Fatalf("gap result = (%v, %v), want dropped", packet, result)
	}
}

func TestH264Mode0ReassemblerPassesSingleNAL(t *testing.T) {
	r := h264Mode0Reassembler{}
	want := &rtp.Packet{Payload: []byte{0x65, 0x01}}
	got, result := r.push(want)
	if result != h264Mode0Passthrough || got != want {
		t.Fatalf("single NAL result = (%v, %v), want original passthrough", got, result)
	}
}

func TestH264Mode0ReassemblerDropsOversizedFUA(t *testing.T) {
	r := h264Mode0Reassembler{}
	fragment := make([]byte, maxH264Mode0NALPayload)
	if packet, result := r.push(mode0FUAPacket(10, 9000, true, true, 5, fragment)); packet != nil || result != h264Mode0Dropped {
		t.Fatalf("oversized result = (%v, %v), want dropped", packet, result)
	}
}

func TestH264Mode0ReassemblerDropsUnsupportedAggregationPacket(t *testing.T) {
	r := h264Mode0Reassembler{}
	packet := &rtp.Packet{Payload: []byte{0x79, 0x01}} // STAP-B (NAL type 25)
	if got, result := r.push(packet); got != nil || result != h264Mode0Dropped {
		t.Fatalf("STAP-B result = (%v, %v), want dropped", got, result)
	}
}
