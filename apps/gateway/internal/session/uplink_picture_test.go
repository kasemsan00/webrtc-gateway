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
	return rtpHasMarker(p)
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

func TestUplinkPictureHoldDoesNotInventAMarkerUntilFlushOrNewTimestamp(t *testing.T) {
	var hold uplinkPictureHold
	for i := 0; i < 5; i++ {
		prev, ok := hold.push(heldUplinkVideo{data: testRTP(90000, false, []byte{0x65, 0x80, byte(i)}), rtpTS: 90000})
		if i == 0 {
			if ok {
				t.Fatal("first slice was emitted before the picture ended")
			}
			continue
		}
		if !ok {
			t.Fatalf("slice %d was not emitted", i-1)
		}
		if prev.marker || rtpHasMarker(prev.data) {
			t.Fatalf("slice %d was marked before the picture ended", i-1)
		}
	}
	if !hold.set || hold.pkt.rtpTS != 90000 {
		t.Fatal("picture was flushed without a timestamp change")
	}
}

func TestSendUplinkVideoCopiesHeldPayload(t *testing.T) {
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
	sess.sendUplinkVideo(send, recv.LocalAddr().(*net.UDPAddr), first, true, 90000, 1)
	for i := range first {
		first[i] = 0xff
	}
	second := testRTP(180000, true, []byte{0x41, 0x80})
	sess.sendUplinkVideo(send, recv.LocalAddr().(*net.UDPAddr), second, false, 180000, 2)

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

func uplinkHoldUDP(t *testing.T) (*Session, *net.UDPConn, *net.UDPAddr) {
	t.Helper()
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
	dest := recv.LocalAddr().(*net.UDPAddr)
	sess := &Session{
		ID:                t.Name(),
		VideoRTPConn:      send,
		AsteriskVideoAddr: dest,
		ctx:               ctx,
		cancel:            cancel,
	}
	return sess, recv, dest
}

func readUplinkHoldPackets(t *testing.T, recv *net.UDPConn, deadline time.Duration) [][]byte {
	t.Helper()
	_ = recv.SetReadDeadline(time.Now().Add(deadline))
	var got [][]byte
	buf := make([]byte, 256)
	for {
		n, _, err := recv.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return got
			}
			t.Fatal(err)
		}
		got = append(got, append([]byte(nil), buf[:n]...))
		_ = recv.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
	}
}

func TestSendUplinkVideoKeepsPictureAcross40msGap(t *testing.T) {
	sess, recv, dest := uplinkHoldUDP(t)
	send := sess.VideoRTPConn
	const ts = uint32(90000)
	for i := 0; i < 5; i++ {
		sess.sendUplinkVideo(send, dest, testRTP(ts, false, []byte{0x65, 0x80, byte(i)}), true, ts, i+1)
	}
	time.Sleep(80 * time.Millisecond)
	first := readUplinkHoldPackets(t, recv, 20*time.Millisecond)
	if len(first) != 4 {
		t.Fatalf("after 80ms gap forwarded %d packets, want 4 held-one (timer must not flush)", len(first))
	}
	for i, pkt := range first {
		if rtpHasMarker(pkt) {
			t.Fatalf("packet %d had marker after a 80ms gap inside the picture", i)
		}
	}
	for i := 5; i < 8; i++ {
		sess.sendUplinkVideo(send, dest, testRTP(ts, false, []byte{0x65, 0x80, byte(i)}), true, ts, i+1)
	}
	mid := readUplinkHoldPackets(t, recv, 20*time.Millisecond)
	if len(mid) != 3 {
		t.Fatalf("second burst forwarded %d, want 3", len(mid))
	}
	for i, pkt := range mid {
		if rtpHasMarker(pkt) {
			t.Fatalf("second-burst packet %d had marker before the browser marker", i)
		}
	}
	sess.sendUplinkVideo(send, dest, testRTP(ts, true, []byte{0x65, 0x80, 8}), true, ts, 9)
	last := readUplinkHoldPackets(t, recv, 20*time.Millisecond)
	if len(last) != 2 {
		t.Fatalf("browser marker released %d packets, want held slice + last slice", len(last))
	}
	if rtpHasMarker(last[0]) || !rtpHasMarker(last[1]) {
		t.Fatalf("marker bits %v %v, want only the last slice", rtpHasMarker(last[0]), rtpHasMarker(last[1]))
	}
}

func TestSendUplinkVideoReleasesOnBrowserMarker(t *testing.T) {
	sess, recv, dest := uplinkHoldUDP(t)
	sess.sendUplinkVideo(sess.VideoRTPConn, dest, testRTP(90000, true, []byte{0x65, 0x80}), true, 90000, 1)
	got := readUplinkHoldPackets(t, recv, 20*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("forwarded %d packets, want the marked slice without a 40ms wait", len(got))
	}
	if !rtpHasMarker(got[0]) {
		t.Fatal("browser marker slice was forwarded without the marker bit")
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
