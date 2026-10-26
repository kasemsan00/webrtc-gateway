package session

import (
	"bytes"
	"testing"
	"time"

	"github.com/pion/rtp"

	"webrtc-sip-gateway/internal/config"
)

// pictureSizeStream builds a mode-0 uplink: a 720p IDR and P picture, then a
// Chrome balanced-mode downscale to 640x360. withIDR=false sends the new SPS
// with P slices only, and the 360p IDR arrives two pictures later.
type pictureSizeStream struct {
	packets            [][]byte
	sps720, sps360     []byte
	pps720, pps360     []byte
	changeTS, idr360TS uint32
}

func newPictureSizeStream(withIDR bool) pictureSizeStream {
	st := pictureSizeStream{
		sps720: buildBaselineSPS(66, 0xe0, 31, 1280, 720, 0),
		sps360: buildBaselineSPS(66, 0xe0, 31, 640, 368, 8),
		pps720: []byte{0x68, 0xce, 0x06, 0xe2},
		pps360: []byte{0x68, 0xce, 0x3c, 0x80},
	}
	seq := uint16(100)
	add := func(ts uint32, marker bool, nal []byte) {
		st.packets = append(st.packets, mode0SlicePacket(seq, ts, marker, nal))
		seq++
	}
	idr := func(ts uint32, slices int) {
		for i := 0; i < slices; i++ {
			nal := paddedNAL(0x65, 300)
			nal[2] = byte(i)
			add(ts, i == slices-1, nal)
		}
	}
	p := func(ts uint32) {
		add(ts, false, paddedNAL(0x41, 200))
		add(ts, true, paddedNAL(0x41, 200))
	}
	ts := uint32(90000)
	add(ts, false, st.sps720)
	add(ts, false, st.pps720)
	idr(ts, 3)
	ts += 3000
	p(ts)
	ts += 3000
	st.changeTS = ts
	add(ts, false, st.sps360)
	add(ts, false, st.pps360)
	if withIDR {
		st.idr360TS = ts
		idr(ts, 2)
	} else {
		p(ts)
		ts += 3000
		p(ts)
		ts += 3000
		st.idr360TS = ts
		add(ts, false, st.sps360)
		add(ts, false, st.pps360)
		idr(ts, 2)
	}
	ts += 3000
	p(ts)
	return st
}

func forwardPictureSizeStream(t *testing.T, st pictureSizeStream, mode string) ([]rtp.Packet, *Session) {
	t.Helper()
	return forwardUplinkVideoForTest(t, st.packets, nil, func(sess *Session) {
		sess.SetSIPVideoPacketizationMode(0)
		sess.uplinkPictureSizeChange = mode
		sess.CachedSPS = nil
		sess.CachedPPS = nil
	})
}

func assertUplinkRTPContinuity(t *testing.T, got []rtp.Packet) {
	t.Helper()
	if len(got) == 0 {
		t.Fatal("nothing forwarded")
	}
	for i := 1; i < len(got); i++ {
		if got[i].SSRC != got[0].SSRC {
			t.Fatalf("packet %d SSRC %d, want %d", i, got[i].SSRC, got[0].SSRC)
		}
		if got[i].SequenceNumber != got[i-1].SequenceNumber+1 {
			t.Fatalf("packet %d seq %d after %d", i, got[i].SequenceNumber, got[i-1].SequenceNumber)
		}
		if int32(got[i].Timestamp-got[i-1].Timestamp) < 0 {
			t.Fatalf("packet %d timestamp %d went back from %d", i, got[i].Timestamp, got[i-1].Timestamp)
		}
	}
}

// assertNewSizeStartsClean checks that nothing of the new size goes out
// before one SPS/PPS pair, that the pair is the new one, and that it sits
// right in front of the first new-size IDR slice.
func assertNewSizeStartsClean(t *testing.T, got []rtp.Packet, st pictureSizeStream) {
	t.Helper()
	first := -1
	for i, pkt := range got {
		if pkt.Timestamp >= st.changeTS {
			first = i
			break
		}
	}
	if first < 0 || first+2 >= len(got) {
		t.Fatalf("new picture size never forwarded (first=%d of %d)", first, len(got))
	}
	if got[first].Timestamp != st.idr360TS {
		t.Fatalf("first new-size packet ts=%d, want the IDR ts %d", got[first].Timestamp, st.idr360TS)
	}
	if !bytes.Equal(got[first].Payload, st.sps360) {
		t.Fatalf("first new-size packet %x, want new SPS", got[first].Payload)
	}
	if !bytes.Equal(got[first+1].Payload, st.pps360) {
		t.Fatalf("second new-size packet %x, want new PPS", got[first+1].Payload)
	}
	if got[first+2].Payload[0]&0x1f != 5 {
		t.Fatalf("new SPS/PPS followed by NAL %d, want IDR", got[first+2].Payload[0]&0x1f)
	}
	sps, pps := 0, 0
	for _, pkt := range got[first:] {
		switch pkt.Payload[0] & 0x1f {
		case 7:
			sps++
		case 8:
			pps++
		}
	}
	if sps != 1 || pps != 1 {
		t.Fatalf("new size sent %d SPS and %d PPS, want one pair", sps, pps)
	}
	last := got[len(got)-1]
	if last.Payload[0]&0x1f != 1 || !last.Marker {
		t.Fatalf("trailing 360p P picture missing: nal=%d marker=%v", last.Payload[0]&0x1f, last.Marker)
	}
}

func TestUplinkPictureSizeChangeWithIDRIsForwarded(t *testing.T) {
	st := newPictureSizeStream(true)
	got, sess := forwardPictureSizeStream(t, st, "")
	assertUplinkRTPContinuity(t, got)
	assertNewSizeStartsClean(t, got, st)
	if sess.uplinkSPSWidth != 640 || sess.uplinkSPSHeight != 360 {
		t.Fatalf("tracked size %dx%d, want 640x360", sess.uplinkSPSWidth, sess.uplinkSPSHeight)
	}
	if sess.uplinkPictureSizeIDRPending || sess.uplinkSPSDimensionBlocked {
		t.Fatalf("pending=%v blocked=%v after the new IDR", sess.uplinkPictureSizeIDRPending, sess.uplinkSPSDimensionBlocked)
	}
	if sess.uplinkPictureSizePLIs != 0 {
		t.Fatal("browser PLI requested although the new SPS came with an IDR")
	}
	if !bytes.Equal(sess.CachedSPS, st.sps360) {
		t.Fatal("new SPS not cached for later IDR injection")
	}
}

func TestUplinkPictureSizeChangeWithoutIDRHoldsAndAsksBrowser(t *testing.T) {
	st := newPictureSizeStream(false)
	got, sess := forwardPictureSizeStream(t, st, config.SIPUplinkPictureSizeAccept)
	assertUplinkRTPContinuity(t, got)
	assertNewSizeStartsClean(t, got, st)
	for _, pkt := range got {
		if pkt.Timestamp >= st.changeTS && pkt.Timestamp < st.idr360TS {
			t.Fatalf("new-size P slice ts=%d forwarded before its IDR", pkt.Timestamp)
		}
	}
	// Four held P slices arrive within the retry window: exactly one PLI.
	if sess.uplinkPictureSizePLIs != 1 {
		t.Fatalf("browser PLIs for the new size = %d, want 1", sess.uplinkPictureSizePLIs)
	}
	if sess.uplinkPictureSizeIDRPending {
		t.Fatal("hold still armed after the new IDR")
	}
}

func TestUplinkPictureSizeChangeRejectModeDropsVideo(t *testing.T) {
	st := newPictureSizeStream(true)
	got, sess := forwardPictureSizeStream(t, st, config.SIPUplinkPictureSizeReject)
	assertUplinkRTPContinuity(t, got)
	for _, pkt := range got {
		if pkt.Timestamp >= st.changeTS {
			t.Fatalf("reject mode forwarded ts=%d nal=%d after the size change", pkt.Timestamp, pkt.Payload[0]&0x1f)
		}
	}
	if !sess.uplinkSPSDimensionBlocked || sess.uplinkSPSWidth != 1280 {
		t.Fatalf("blocked=%v width=%d, want the 720p lock", sess.uplinkSPSDimensionBlocked, sess.uplinkSPSWidth)
	}
}

// Chrome balanced often ramps resolution before the first IDR reaches SIP.
// The startup gate owns that window; the call must not lose video.
func TestUplinkPictureSizeChangeBeforeFirstIDRKeepsStartupPath(t *testing.T) {
	sps180 := buildBaselineSPS(66, 0xe0, 31, 320, 192, 12)
	sps360 := buildBaselineSPS(66, 0xe0, 31, 640, 368, 8)
	pps := []byte{0x68, 0xce, 0x06, 0xe2}
	packets := [][]byte{
		mode0SlicePacket(1, 3000, false, sps180),
		mode0SlicePacket(2, 3000, false, pps),
		mode0SlicePacket(3, 3000, true, paddedNAL(0x41, 100)),
		mode0SlicePacket(4, 6000, false, sps360),
		mode0SlicePacket(5, 6000, false, pps),
		mode0SlicePacket(6, 6000, true, paddedNAL(0x65, 300)),
		mode0SlicePacket(7, 9000, true, paddedNAL(0x41, 100)),
	}
	got, sess := forwardUplinkVideoForTest(t, packets, nil, func(sess *Session) {
		sess.SetSIPVideoPacketizationMode(0)
	})
	assertUplinkRTPContinuity(t, got)
	if sess.uplinkPictureSizeIDRPending {
		t.Fatal("startup size change armed the mid-call hold")
	}
	var idr, p bool
	for _, pkt := range got {
		switch pkt.Payload[0] & 0x1f {
		case 5:
			idr = true
		case 1:
			if pkt.Timestamp == 3000 {
				t.Fatal("P slice before the first IDR was forwarded")
			}
			p = true
		}
	}
	if !idr || !p {
		t.Fatalf("idr=%v p=%v, want the 360p IDR and the P picture after it", idr, p)
	}
}

func TestUplinkPictureSizePLIIsRateLimited(t *testing.T) {
	sess := &Session{ID: "size-pli", uplinkPictureSizeIDRPending: true}
	now := time.Now()
	if hold, pli := sess.holdUplinkForPictureSizeLocked(false, true, 7, now); !hold || pli {
		t.Fatalf("held SPS hold=%v pli=%v, want hold without PLI", hold, pli)
	}
	if hold, pli := sess.holdUplinkForPictureSizeLocked(false, false, 1, now); !hold || !pli {
		t.Fatalf("first P slice hold=%v pli=%v, want both", hold, pli)
	}
	if _, pli := sess.holdUplinkForPictureSizeLocked(false, false, 1, now.Add(UplinkPictureSizePLIRetry/2)); pli {
		t.Fatal("second PLI inside the retry window")
	}
	if _, pli := sess.holdUplinkForPictureSizeLocked(false, false, 1, now.Add(UplinkPictureSizePLIRetry)); !pli {
		t.Fatal("no PLI retry after the window")
	}
	if hold, pli := sess.holdUplinkForPictureSizeLocked(true, false, 5, now.Add(2*UplinkPictureSizePLIRetry)); hold || pli {
		t.Fatalf("IDR hold=%v pli=%v, want release", hold, pli)
	}
	if hold, _ := sess.holdUplinkForPictureSizeLocked(false, false, 1, now.Add(3*UplinkPictureSizePLIRetry)); hold {
		t.Fatal("P slice held after the IDR released the size change")
	}
}
