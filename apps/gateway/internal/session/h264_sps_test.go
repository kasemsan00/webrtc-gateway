package session

import (
	"testing"
	"time"
)

func TestH264SPSPictureSizeBaseline720p(t *testing.T) {
	nal := buildBaselineSPS(66, 0xe0, 31, 1280, 720, 0)
	w, h, ok := h264SPSPictureSize(nal)
	if !ok || w != 1280 || h != 720 {
		t.Fatalf("got %dx%d ok=%v nal=%x", w, h, ok, nal)
	}
}

func TestH264SPSPictureSizeCrops1088To1080(t *testing.T) {
	nal := buildBaselineSPS(66, 0xe0, 40, 1920, 1088, 8)
	w, h, ok := h264SPSPictureSize(nal)
	if !ok || w != 1920 || h != 1080 {
		t.Fatalf("got %dx%d ok=%v", w, h, ok)
	}
}

func TestUplinkDropsVideoAfterSPSPictureSizeChange(t *testing.T) {
	sess := &Session{ID: "sps-size"}
	first := buildBaselineSPS(66, 0xe0, 31, 1280, 720, 0)
	if sess.rejectUplinkPictureSizeChangeLocked(first) {
		t.Fatal("first SPS was rejected")
	}
	if sess.uplinkSPSWidth != 1280 || sess.uplinkSPSHeight != 720 {
		t.Fatalf("locked size %dx%d", sess.uplinkSPSWidth, sess.uplinkSPSHeight)
	}
	same := buildBaselineSPS(66, 0xc0, 31, 1280, 720, 0)
	if sess.rejectUplinkPictureSizeChangeLocked(same) {
		t.Fatal("same picture size was rejected")
	}
	changed := buildBaselineSPS(66, 0xe0, 31, 640, 480, 0)
	if !sess.rejectUplinkPictureSizeChangeLocked(changed) {
		t.Fatal("picture size change was forwarded")
	}
	if !sess.rejectUplinkPictureSizeChangeLocked(first) {
		t.Fatal("later original-size video was forwarded after the change")
	}
	if sess.uplinkSPSWidth != 1280 || sess.uplinkSPSHeight != 720 {
		t.Fatalf("lock moved to %dx%d", sess.uplinkSPSWidth, sess.uplinkSPSHeight)
	}

	sess.mu.Lock()
	sess.armSwitchKeyframeRequestLocked(time.Now())
	sess.mu.Unlock()
	if sess.rejectUplinkPictureSizeChangeLocked(changed) {
		t.Fatal("armed switch dropped the post-switch picture size")
	}
	if sess.uplinkSPSWidth != 640 || sess.uplinkSPSHeight != 480 {
		t.Fatalf("switch lock %dx%d", sess.uplinkSPSWidth, sess.uplinkSPSHeight)
	}
	other := buildBaselineSPS(66, 0xe0, 31, 320, 240, 0)
	if !sess.rejectUplinkPictureSizeChangeLocked(other) {
		t.Fatal("a second picture-size change after the switch was forwarded")
	}

	sess.ResetMediaState()
	if sess.rejectUplinkPictureSizeChangeLocked(changed) {
		t.Fatal("new call rejected its first SPS")
	}
	if sess.uplinkSPSWidth != 640 || sess.uplinkSPSHeight != 480 {
		t.Fatalf("reset lock %dx%d", sess.uplinkSPSWidth, sess.uplinkSPSHeight)
	}
}

func buildBaselineSPS(profile, constraints, level byte, width, height, cropBottomPx int) []byte {
	w := bitWriter{}
	w.u(uint(profile), 8)
	w.u(uint(constraints), 8)
	w.u(uint(level), 8)
	w.ue(0) // seq_parameter_set_id
	w.ue(0) // log2_max_frame_num_minus4
	w.ue(0) // pic_order_cnt_type
	w.ue(0) // log2_max_pic_order_cnt_lsb_minus4
	w.ue(1) // max_num_ref_frames
	w.u(0, 1)
	w.ue(uint(width/16 - 1))
	w.ue(uint(height/16 - 1))
	w.u(1, 1) // frame_mbs_only_flag
	w.u(1, 1) // direct_8x8_inference_flag
	if cropBottomPx == 0 {
		w.u(0, 1)
	} else {
		w.u(1, 1)
		w.ue(0)
		w.ue(0)
		w.ue(0)
		w.ue(uint(cropBottomPx / 2))
	}
	w.u(0, 1) // vui_parameters_present_flag
	return append([]byte{0x67}, w.bytes()...)
}

type bitWriter struct {
	buf []byte
	n   int
	cur byte
}

func (w *bitWriter) u(value uint, n int) {
	for i := n - 1; i >= 0; i-- {
		bit := byte((value >> i) & 1)
		w.cur = (w.cur << 1) | bit
		w.n++
		if w.n == 8 {
			w.buf = append(w.buf, w.cur)
			w.n = 0
			w.cur = 0
		}
	}
}

func (w *bitWriter) ue(value uint) {
	x := value + 1
	bits := 0
	for t := x; t > 0; t >>= 1 {
		bits++
	}
	if bits > 1 {
		w.u(0, bits-1)
	}
	w.u(x, bits)
}

func (w *bitWriter) bytes() []byte {
	if w.n == 0 {
		return append([]byte(nil), w.buf...)
	}
	return append(append([]byte(nil), w.buf...), w.cur<<(8-w.n))
}
