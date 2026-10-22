package session

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// mode0SlicePacket is one OpenH264 single-NAL RTP packet: SPS, PPS, an IDR
// slice, or a P slice. Payload is the raw NAL, including its header byte.
func mode0SlicePacket(seq uint16, ts uint32, marker bool, payload []byte) []byte {
	pkt := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Marker:         marker,
			PayloadType:    102,
			SequenceNumber: seq,
			Timestamp:      ts,
			SSRC:           0x11223344,
		},
		Payload: payload,
	}
	raw, err := pkt.Marshal()
	if err != nil {
		panic(err)
	}
	return raw
}

func paddedNAL(header byte, body int) []byte {
	payload := make([]byte, 1+body)
	payload[0] = header
	// first_mb_in_slice = 0, so the slice is not rejected as a parameter set.
	if header&0x1f == 1 || header&0x1f == 5 {
		payload[1] = 0x80
	}
	for i := 2; i < len(payload); i++ {
		payload[i] = byte(i)
	}
	return payload
}

type sliceReader struct {
	packets [][]byte
	idx     int
}

func (r *sliceReader) Read(b []byte) (int, error) {
	if r.idx >= len(r.packets) {
		return 0, io.EOF
	}
	raw := r.packets[r.idx]
	r.idx++
	if len(raw) > len(b) {
		return 0, io.ErrShortBuffer
	}
	n := copy(b, raw)
	// TrackRemote.Read reuses one buffer. Leftover bytes from a longer
	// previous packet must not leak into a held slice.
	for i := n; i < len(b); i++ {
		b[i] = 0xa5
	}
	return n, nil
}

func openH264Mode0Picture(t *testing.T) [][]byte {
	t.Helper()
	const idrTS = 90000
	const slices = 37
	const sliceBody = 1069 // NAL header + body ≈ 1070
	sps := []byte{0x67, 0x42, 0x00, 0x1f, 0xab}
	pps := []byte{0x68, 0xce, 0x06, 0xe2}
	var packets [][]byte
	seq := uint16(1)
	packets = append(packets, mode0SlicePacket(seq, idrTS, false, sps))
	seq++
	packets = append(packets, mode0SlicePacket(seq, idrTS, false, pps))
	seq++
	for i := 0; i < slices; i++ {
		nal := paddedNAL(0x65, sliceBody)
		nal[2] = byte(i) // distinguish slices without changing the NAL type
		packets = append(packets, mode0SlicePacket(seq, idrTS, i == slices-1, nal))
		seq++
	}
	// Several P pictures, two single-NAL slices each, marker on the last.
	for pic := 0; pic < 4; pic++ {
		ts := uint32(idrTS + (pic+1)*3000)
		for slice := 0; slice < 2; slice++ {
			nal := paddedNAL(0x41, 400)
			nal[2] = byte(pic)
			nal[3] = byte(slice)
			packets = append(packets, mode0SlicePacket(seq, ts, slice == 1, nal))
			seq++
		}
	}
	return packets
}

func forwardPictureToAsterisk(t *testing.T, uplinkOpen bool, mode uint8, packets [][]byte) []rtp.Packet {
	t.Helper()
	recv, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { recv.Close() })
	// Production RTP sockets are not connected; WriteToUDP on a dialed
	// socket fails and would hide the packets under test.
	send, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { send.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sess := &Session{
		ID:                      "idr-slices",
		RTPBufferSize:           2048,
		mediaForwardReady:       true,
		VideoRTPConn:            send,
		AsteriskVideoAddr:       recv.LocalAddr().(*net.UDPAddr),
		UplinkVideoPacerEnabled: true,
		CachedSPS:               []byte{0x67, 0x42, 0x00, 0x1f, 0xab},
		CachedPPS:               []byte{0x68, 0xce, 0x06, 0xe2},
		ctx:                     ctx,
		cancel:                  cancel,
	}
	sess.SetSIPVideoPacketizationMode(mode)
	if uplinkOpen {
		sess.LastUplinkKeyframe = time.Now()
	}
	done := make(chan struct{})
	arrived := make(chan rtp.Packet, 128)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 2048)
		for {
			_ = recv.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, _, err := recv.ReadFromUDP(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					select {
					case <-done:
						return
					default:
						continue
					}
				}
				return
			}
			raw := append([]byte(nil), buf[:n]...)
			var pkt rtp.Packet
			if err := pkt.Unmarshal(raw); err != nil {
				t.Errorf("unmarshal uplink packet: %v", err)
				return
			}
			arrived <- pkt
		}
	}()
	go func() {
		sess.forwardRTPToAsteriskFrom(&sliceReader{packets: packets}, "video")
		// Last slice is held 40ms and the pacer then drains the IDR.
		time.Sleep(500 * time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("uplink forwarder did not return")
	}
	<-readDone
	close(arrived)
	var got []rtp.Packet
	for pkt := range arrived {
		got = append(got, pkt)
	}
	return got
}

func TestMode0MultiSliceIDRReachesAsteriskAndEcho(t *testing.T) {
	for _, uplinkOpen := range []bool{false, true} {
		name := "cold"
		if uplinkOpen {
			name = "uplink-open"
		}
		t.Run(name, func(t *testing.T) {
			got := forwardPictureToAsterisk(t, uplinkOpen, 0, openH264Mode0Picture(t))
			assertDecodableMode0Picture(t, got)
		})
	}
}

func assertDecodableMode0Picture(t *testing.T, got []rtp.Packet) {
	t.Helper()
	if len(got) < 2+37+8 {
		nal := make([]int, 0, len(got))
		for _, pkt := range got {
			if len(pkt.Payload) > 0 {
				nal = append(nal, int(pkt.Payload[0]&0x1f))
			}
		}
		t.Fatalf("forwarded %d packets, want SPS+PPS+37 IDR slices+8 P slices; nals=%v", len(got), nal)
	}
	nalType := func(i int) byte {
		if len(got[i].Payload) == 0 {
			return 0
		}
		return got[i].Payload[0] & 0x1f
	}
	if nalType(0) != 7 || nalType(1) != 8 {
		var summary []string
		for i := 0; i < len(got) && i < 12; i++ {
			summary = append(summary, fmt.Sprintf("nal=%d seq=%d ts=%d marker=%v bytes=%d", nalType(i), got[i].SequenceNumber, got[i].Timestamp, got[i].Marker, len(got[i].Payload)))
		}
		t.Fatalf("picture starts with nal %d,%d, want SPS then PPS (%d packets)\n%s", nalType(0), nalType(1), len(got), strings.Join(summary, "\n"))
	}
	// Parameter sets once: no second SPS/PPS before the slices or between them.
	sps, pps := 0, 0
	var idrTS uint32
	var idr []rtp.Packet
	var after []rtp.Packet
	seenNonParam := false
	for _, pkt := range got {
		kind := byte(0)
		if len(pkt.Payload) > 0 {
			kind = pkt.Payload[0] & 0x1f
		}
		switch kind {
		case 7:
			if seenNonParam {
				t.Fatal("SPS was forwarded again after picture data")
			}
			sps++
		case 8:
			if seenNonParam {
				t.Fatal("PPS was forwarded again after picture data")
			}
			pps++
		case 5:
			seenNonParam = true
			if len(idr) == 0 {
				idrTS = pkt.Timestamp
			}
			idr = append(idr, pkt)
		default:
			seenNonParam = true
			after = append(after, pkt)
		}
	}
	if sps != 1 || pps != 1 {
		t.Fatalf("parameter sets sps=%d pps=%d, want one each", sps, pps)
	}
	if len(idr) != 37 {
		t.Fatalf("IDR slices=%d, want 37", len(idr))
	}
	for i, pkt := range idr {
		if pkt.Timestamp != idrTS {
			t.Fatalf("IDR slice %d ts=%d, want %d", i, pkt.Timestamp, idrTS)
		}
		if pkt.Marker != (i == len(idr)-1) {
			t.Fatalf("IDR slice %d marker=%v, want marker only on the last slice", i, pkt.Marker)
		}
		if i > 0 && pkt.SequenceNumber != idr[i-1].SequenceNumber+1 {
			t.Fatalf("IDR slice %d seq=%d, previous %d", i, pkt.SequenceNumber, idr[i-1].SequenceNumber)
		}
		want := paddedNAL(0x65, 1069)
		want[2] = byte(i)
		if !bytes.Equal(pkt.Payload, want) {
			t.Fatalf("IDR slice %d payload mutated (held packet aliased the read buffer)", i)
		}
	}
	if len(after) != 8 {
		t.Fatalf("P-frame packets=%d, want 8", len(after))
	}
	for i, pkt := range after {
		if len(pkt.Payload) == 0 || pkt.Payload[0]&0x1f != 1 {
			t.Fatalf("P packet %d nal=%v", i, pkt.Payload)
		}
	}

	var emitted []NormalizedH264AccessUnit
	normalizer := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})
	for i := range got {
		normalizer.Push(&got[i])
	}
	// The last P packet may still be waiting on its marker inside the
	// normalizer only if the uplink failed to set one. A complete IDR must
	// already have been emitted.
	var idrAU *NormalizedH264AccessUnit
	for i := range emitted {
		if emitted[i].IsIDR {
			idrAU = &emitted[i]
			break
		}
	}
	if idrAU == nil {
		t.Fatalf("echo path keyframeAge=none: normalizer emitted %d access units, none were IDR", len(emitted))
	}
	echo := &Session{ID: "echo"}
	echo.RecordKeyframe()
	last, _ := echo.GetKeyframeTimes()
	if last.IsZero() {
		t.Fatal("keyframeAge=none after the echoed IDR")
	}
	if len(idrAU.Packets) != 2+37 {
		t.Fatalf("echo IDR has %d packets, want SPS+PPS+37 slices", len(idrAU.Packets))
	}
}

// openH264FUAPicture is one IDR carried as FU-A. Only the start fragment is a
// keyframe; 93b36d1 dropped the rest while the start sat in the picture hold
// and then marked that single fragment, so the echo normalizer saw no IDR.
func openH264FUAPicture(t *testing.T) [][]byte {
	t.Helper()
	const idrTS = 90000
	const frags = 8
	const fragBody = 1068
	sps := []byte{0x67, 0x42, 0x00, 0x1f, 0xab}
	pps := []byte{0x68, 0xce, 0x06, 0xe2}
	var packets [][]byte
	seq := uint16(1)
	packets = append(packets, mode0SlicePacket(seq, idrTS, false, sps))
	seq++
	packets = append(packets, mode0SlicePacket(seq, idrTS, false, pps))
	seq++
	for i := 0; i < frags; i++ {
		fuHeader := byte(0x05)
		if i == 0 {
			fuHeader |= 0x80
		}
		if i == frags-1 {
			fuHeader |= 0x40
		}
		body := make([]byte, 2+fragBody)
		body[0] = 0x7c
		body[1] = fuHeader
		body[2] = byte(i)
		packets = append(packets, mode0SlicePacket(seq, idrTS, i == frags-1, body))
		seq++
	}
	for pic := 0; pic < 2; pic++ {
		ts := uint32(idrTS + (pic+1)*3000)
		nal := paddedNAL(0x41, 400)
		packets = append(packets, mode0SlicePacket(seq, ts, true, nal))
		seq++
	}
	return packets
}

func TestFUAIDRContinuationsReachAsteriskAndEcho(t *testing.T) {
	for _, uplinkOpen := range []bool{false, true} {
		name := "cold"
		if uplinkOpen {
			name = "uplink-open"
		}
		t.Run(name, func(t *testing.T) {
			// Mode 1 leaves FU-A intact. Mode 0 would reassemble it first and
			// hide a gate that drops non-keyframe fragments.
			got := forwardPictureToAsterisk(t, uplinkOpen, 1, openH264FUAPicture(t))
			assertDecodableFUAPicture(t, got)
		})
	}
}

func assertDecodableFUAPicture(t *testing.T, got []rtp.Packet) {
	t.Helper()
	const frags = 8
	if len(got) < 2+frags+2 {
		t.Fatalf("forwarded %d packets, want SPS+PPS+%d FU-A fragments+2 P slices", len(got), frags)
	}
	nalType := func(i int) byte {
		if len(got[i].Payload) == 0 {
			return 0
		}
		return got[i].Payload[0] & 0x1f
	}
	if nalType(0) != 7 || nalType(1) != 8 {
		t.Fatalf("picture starts with nal %d,%d, want SPS then PPS", nalType(0), nalType(1))
	}
	var fua []rtp.Packet
	var pframes int
	sps, pps := 0, 0
	for _, pkt := range got {
		if len(pkt.Payload) == 0 {
			t.Fatal("empty payload")
		}
		switch pkt.Payload[0] & 0x1f {
		case 7:
			sps++
		case 8:
			pps++
		case 28:
			fua = append(fua, pkt)
		case 1:
			pframes++
		default:
			t.Fatalf("unexpected nal %d", pkt.Payload[0]&0x1f)
		}
	}
	if sps != 1 || pps != 1 {
		t.Fatalf("parameter sets sps=%d pps=%d, want one each", sps, pps)
	}
	if len(fua) != frags {
		t.Fatalf("FU-A fragments=%d, want %d (a dropped tail is the packets=1 failure)", len(fua), frags)
	}
	ts := fua[0].Timestamp
	for i, pkt := range fua {
		if pkt.Timestamp != ts {
			t.Fatalf("fragment %d ts=%d, want %d", i, pkt.Timestamp, ts)
		}
		if pkt.Marker != (i == len(fua)-1) {
			t.Fatalf("fragment %d marker=%v, want marker only on the last fragment", i, pkt.Marker)
		}
		if i > 0 && pkt.SequenceNumber != fua[i-1].SequenceNumber+1 {
			t.Fatalf("fragment %d seq=%d, previous %d", i, pkt.SequenceNumber, fua[i-1].SequenceNumber)
		}
		if len(pkt.Payload) < 3 {
			t.Fatalf("fragment %d payload %d bytes", i, len(pkt.Payload))
		}
		start := pkt.Payload[1]&0x80 != 0
		end := pkt.Payload[1]&0x40 != 0
		if start != (i == 0) || end != (i == len(fua)-1) {
			t.Fatalf("fragment %d start=%v end=%v", i, start, end)
		}
		if pkt.Payload[1]&0x1f != 5 {
			t.Fatalf("fragment %d inner nal %d, want IDR", i, pkt.Payload[1]&0x1f)
		}
		if pkt.Payload[2] != byte(i) {
			t.Fatalf("fragment %d payload mutated (held packet aliased the read buffer)", i)
		}
	}
	if pframes != 2 {
		t.Fatalf("P-frames=%d, want 2", pframes)
	}
	var emitted []NormalizedH264AccessUnit
	normalizer := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})
	for i := range got {
		normalizer.Push(&got[i])
	}
	for _, au := range emitted {
		if au.IsIDR {
			echo := &Session{ID: "echo"}
			echo.RecordKeyframe()
			last, _ := echo.GetKeyframeTimes()
			if last.IsZero() {
				t.Fatal("keyframeAge=none after the echoed IDR")
			}
			return
		}
	}
	t.Fatalf("echo path keyframeAge=none: normalizer emitted %d access units, none were IDR", len(emitted))
}
