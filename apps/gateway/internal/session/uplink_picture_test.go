package session

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func testRTP(ts uint32, marker bool, payload []byte) []byte {
	p := make([]byte, 12+len(payload))
	p[0] = 0x80
	p[1] = 98
	if marker {
		p[1] |= 0x80
	}
	p[4] = byte(ts >> 24)
	p[5] = byte(ts >> 16)
	p[6] = byte(ts >> 8)
	p[7] = byte(ts)
	copy(p[12:], payload)
	return p
}

func rtpMarker(p []byte) bool {
	return len(p) > 1 && p[1]&0x80 != 0
}

func rtpTimestamp(p []byte) uint32 {
	return uint32(p[4])<<24 | uint32(p[5])<<16 | uint32(p[6])<<8 | uint32(p[7])
}

func TestUplinkPictureHoldKeepsOneTimestampAndMarksTheLastSlice(t *testing.T) {
	var hold uplinkPictureHold
	slices := [][]byte{
		testRTP(90000, true, []byte{0x67, 0x42}),
		testRTP(90000, true, []byte{0x68, 0xce}),
		testRTP(90000, true, []byte{0x65, 0x80}),
		testRTP(90000, true, []byte{0x65, 0x40}),
		testRTP(90000, true, []byte{0x65, 0x80, 0x11}),
	}
	var emitted []heldUplinkVideo
	for i, raw := range slices {
		prev, ok := hold.push(heldUplinkVideo{data: append([]byte(nil), raw...), rtpTS: 90000})
		if i == 0 {
			if ok {
				t.Fatal("first packet was emitted before the next slice arrived")
			}
			continue
		}
		if !ok {
			t.Fatalf("slice %d was not emitted", i-1)
		}
		emitted = append(emitted, prev)
	}
	prev, ok := hold.push(heldUplinkVideo{data: testRTP(180000, true, []byte{0x61, 0x80}), rtpTS: 180000})
	if !ok {
		t.Fatal("last slice of the IDR was not emitted when the next picture arrived")
	}
	emitted = append(emitted, prev)
	if len(emitted) != 5 {
		t.Fatalf("emitted %d packets, want 5", len(emitted))
	}
	for i, pkt := range emitted {
		if rtpTimestamp(pkt.data) != 90000 {
			t.Fatalf("packet %d timestamp %d, want 90000", i, rtpTimestamp(pkt.data))
		}
		wantMarker := i == len(emitted)-1
		if pkt.marker != wantMarker || rtpMarker(pkt.data) != wantMarker {
			t.Fatalf("packet %d marker=%v header=%v, want %v", i, pkt.marker, rtpMarker(pkt.data), wantMarker)
		}
	}
	last, ok := hold.flush()
	if !ok || !last.marker || rtpTimestamp(last.data) != 180000 || !rtpMarker(last.data) {
		t.Fatalf("following picture flush marker=%v ts=%d ok=%v", last.marker, rtpTimestamp(last.data), ok)
	}
}

func TestEnqueueUplinkVideoCopiesHeldPayload(t *testing.T) {
	recv, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { recv.Close() })
	send, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { send.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sess := &Session{
		ID:                "hold-copy",
		VideoRTPConn:      send,
		AsteriskVideoAddr: recv.LocalAddr().(*net.UDPAddr),
		ctx:               ctx,
		cancel:            cancel,
	}
	first := testRTP(90000, false, []byte{0x65, 0x80, 0x11, 0x22, 0x33})
	want := append([]byte(nil), first...)
	sess.enqueueUplinkVideo(send, recv.LocalAddr().(*net.UDPAddr), first, true, 90000, 1)
	for i := range first {
		first[i] = 0xff
	}
	second := testRTP(180000, true, []byte{0x41, 0x80})
	sess.enqueueUplinkVideo(send, recv.LocalAddr().(*net.UDPAddr), second, false, 180000, 2)

	_ = recv.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	n, _, err := recv.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	got := buf[:n]
	if !rtpMarker(got) {
		t.Fatal("first picture was emitted without the marker")
	}
	payload := rtpPayload(got)
	wantPayload := rtpPayload(want)
	if !bytes.Equal(payload, wantPayload) {
		t.Fatalf("held IDR payload corrupted: got %x want %x", payload, wantPayload)
	}
}

func TestUplinkHeldKeyframe(t *testing.T) {
	if !uplinkHeldKeyframe([]byte{0x65, 0x80}, false) {
		t.Fatal("NAL 5 was not a keyframe")
	}
	if uplinkHeldKeyframe([]byte{0x67, 0x42}, true) {
		t.Fatal("SPS counted as a keyframe")
	}
	if !uplinkHeldKeyframe([]byte{0x7c, 0x85, 0x01}, false) {
		t.Fatal("FU-A IDR start was not a keyframe")
	}
	if !uplinkHeldKeyframe([]byte{0x7c, 0x05, 0x02}, false) {
		t.Fatal("FU-A IDR tail was not a keyframe")
	}
}

func TestH264FirstMBInSlice(t *testing.T) {
	mb, ok := h264FirstMBInSlice([]byte{0x65, 0x80})
	if !ok || mb != 0 {
		t.Fatalf("first slice mb=%d ok=%v, want 0", mb, ok)
	}
	mb, ok = h264FirstMBInSlice([]byte{0x65, 0x40})
	if !ok || mb != 1 {
		t.Fatalf("second slice mb=%d ok=%v, want 1", mb, ok)
	}
	if _, ok = h264FirstMBInSlice([]byte{0x67, 0x42}); ok {
		t.Fatal("SPS was parsed as a slice")
	}
}

func TestFIRSliceLogCoversOneNewPicture(t *testing.T) {
	sess := &Session{
		ID:                          "slice",
		firIDRSliceLog:              true,
		uplinkVideoIDRTimestampSeen: true,
		uplinkVideoLastIDRTimestamp: 1,
	}
	sess.maybeLogFIRIDRSlice([]byte{0x65, 0x80}, 1, false)
	if !sess.firIDRSliceLog {
		t.Fatal("slices of the picture that provoked the FIR cleared the log")
	}
	sess.maybeLogFIRIDRSlice([]byte{0x65, 0x80}, 90000, false)
	sess.maybeLogFIRIDRSlice([]byte{0x65, 0x40}, 90000, false)
	if !sess.firIDRSliceLog || !sess.firIDRSliceTSSet || sess.firIDRSliceTS != 90000 {
		t.Fatalf("log=%v tsSet=%v ts=%d", sess.firIDRSliceLog, sess.firIDRSliceTSSet, sess.firIDRSliceTS)
	}
	sess.maybeLogFIRIDRSlice([]byte{0x65, 0x80, 0x11}, 90000, true)
	if sess.firIDRSliceLog {
		t.Fatal("marker slice left the FIR slice log armed")
	}
}
