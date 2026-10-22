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

	if decision := r.push(mode0FUAPacket(10, 9000, true, false, 5, []byte{0x01, 0x02})); decision.packet != nil || decision.result != h264Mode0Buffered {
		t.Fatalf("start result = (%v, %v), want buffered", decision.packet, decision.result)
	}
	if decision := r.push(mode0FUAPacket(11, 9000, false, false, 5, []byte{0x03})); decision.packet != nil || decision.result != h264Mode0Buffered {
		t.Fatalf("middle result = (%v, %v), want buffered", decision.packet, decision.result)
	}
	decision := r.push(mode0FUAPacket(12, 9000, false, true, 5, []byte{0x04, 0x05}))
	packet := decision.packet
	if decision.result != h264Mode0Reassembled || packet == nil {
		t.Fatalf("end result = (%v, %v), want reassembled", packet, decision.result)
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

	decision := r.push(mode0FUAPacket(12, 9000, false, true, 1, []byte{0x02}))
	if decision.packet != nil || decision.result != h264Mode0Dropped || decision.reason != "discontinuous-fua" {
		t.Fatalf("gap result = (%v, %v, %s), want dropped discontinuous-fua", decision.packet, decision.result, decision.reason)
	}
}

func TestH264Mode0ReassemblerResumesAfterDiscontinuousFrame(t *testing.T) {
	r := h264Mode0Reassembler{}
	r.push(mode0FUAPacket(10, 9000, true, false, 1, []byte{0x01}))
	if decision := r.push(mode0FUAPacket(12, 9000, false, true, 1, []byte{0x02})); decision.reason != "discontinuous-fua" {
		t.Fatalf("gap reason = %s", decision.reason)
	}

	if decision := r.push(mode0FUAPacket(13, 18000, true, false, 5, []byte{0x09})); decision.result != h264Mode0Buffered {
		t.Fatalf("next start result = %v, reassembler stayed latched", decision.result)
	}
	decision := r.push(mode0FUAPacket(14, 18000, false, true, 5, []byte{0x08}))
	if decision.result != h264Mode0Reassembled || decision.packet == nil {
		t.Fatalf("next frame result = %v, want reassembled", decision.result)
	}
	if want := []byte{0x65, 0x09, 0x08}; !bytes.Equal(decision.packet.Payload, want) {
		t.Fatalf("payload = %x, want %x", decision.packet.Payload, want)
	}
}

func TestH264Mode0ReassemblerPassesSingleNAL(t *testing.T) {
	r := h264Mode0Reassembler{}
	want := &rtp.Packet{Payload: []byte{0x65, 0x01}}
	decision := r.push(want)
	if decision.result != h264Mode0Passthrough || decision.packet != want {
		t.Fatalf("single NAL result = (%v, %v), want original passthrough", decision.packet, decision.result)
	}
}

func TestH264Mode0ReassemblerDropsOversizedFUA(t *testing.T) {
	r := h264Mode0Reassembler{}
	fragment := make([]byte, maxH264Mode0NALPayload)
	decision := r.push(mode0FUAPacket(10, 9000, true, true, 5, fragment))
	if decision.packet != nil || decision.result != h264Mode0Dropped || decision.reason != "oversized-fua" || decision.nalBytes <= maxH264Mode0NALPayload {
		t.Fatalf("oversized result = (%v, %v, %s, %d), want dropped oversized", decision.packet, decision.result, decision.reason, decision.nalBytes)
	}
	if next := r.push(&rtp.Packet{Payload: []byte{0x65, 0x01}}); next.result != h264Mode0Passthrough {
		t.Fatalf("single NAL after oversized = %v, reassembler stayed latched", next.result)
	}
}

func TestH264Mode0ReassemblerDropsUnsupportedAggregationPacket(t *testing.T) {
	r := h264Mode0Reassembler{}
	packet := &rtp.Packet{Payload: []byte{0x79, 0x01}} // STAP-B (NAL type 25)
	decision := r.push(packet)
	if decision.packet != nil || decision.result != h264Mode0Dropped || decision.reason != "unsupported-nal" || mode0DropArmsRecovery(decision.reason) {
		t.Fatalf("STAP-B result = (%v, %v, %s), want an ignored unsupported NAL", decision.packet, decision.result, decision.reason)
	}
}

func TestH264Mode0ReassemblerKeepsSmallFrames(t *testing.T) {
	r := h264Mode0Reassembler{}
	small := &rtp.Packet{Payload: []byte{0x65}}
	if decision := r.push(small); decision.result != h264Mode0Passthrough || decision.packet != small {
		t.Fatalf("1-byte IDR = %v %s", decision.result, decision.reason)
	}

	onePacket := r.push(mode0FUAPacket(7, 1000, true, true, 1, []byte{0x88}))
	if onePacket.result != h264Mode0Reassembled || onePacket.reason != "" {
		t.Fatalf("single-packet FU-A = %v %s", onePacket.result, onePacket.reason)
	}
	if want := []byte{0x61, 0x88}; !bytes.Equal(onePacket.packet.Payload, want) {
		t.Fatalf("single-packet payload = %x, want %x", onePacket.packet.Payload, want)
	}

	headerOnly := r.push(&rtp.Packet{Payload: []byte{0x7c, 0xc5}})
	if headerOnly.result != h264Mode0Reassembled || mode0DropArmsRecovery(headerOnly.reason) {
		t.Fatalf("empty start+end FU-A = %v %s", headerOnly.result, headerOnly.reason)
	}
	if want := []byte{0x65}; !bytes.Equal(headerOnly.packet.Payload, want) {
		t.Fatalf("header-only payload = %x", headerOnly.packet.Payload)
	}
}
