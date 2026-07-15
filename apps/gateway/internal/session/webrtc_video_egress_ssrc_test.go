package session

import (
	"testing"

	"github.com/pion/rtp"
)

func TestEnsureWebRTCVideoEgressSSRCUsesPacketThenSticky(t *testing.T) {
	sess := &Session{ID: "egress-init"}
	got := sess.EnsureWebRTCVideoEgressSSRC(4242)
	if got != 4242 {
		t.Fatalf("expected packet SSRC 4242, got %d", got)
	}
	if sess.EnsureWebRTCVideoEgressSSRC(9999) != 4242 {
		t.Fatalf("expected sticky egress SSRC")
	}
}

func TestEnsureWebRTCVideoEgressSSRCGeneratesWhenPacketZero(t *testing.T) {
	sess := &Session{ID: "egress-gen"}
	got := sess.EnsureWebRTCVideoEgressSSRC(0)
	if got == 0 {
		t.Fatal("expected generated non-zero SSRC")
	}
}

func TestRemapWebRTCVideoEgressSSRCChangesValue(t *testing.T) {
	sess := &Session{ID: "egress-remap"}
	first := sess.EnsureWebRTCVideoEgressSSRC(1111)
	second := sess.RemapWebRTCVideoEgressSSRC("accepted-switch")
	if second == 0 || second == first {
		t.Fatalf("expected new SSRC, first=%d second=%d", first, second)
	}
	if sess.GetWebRTCVideoEgressSSRC() != second {
		t.Fatalf("getter mismatch")
	}
}

func TestRemapWebRTCVideoEgressSSRCClearsRTPHistory(t *testing.T) {
	sess := &Session{ID: "egress-remap-cache"}
	sess.initVideoRTPHistory()
	sess.CacheVideoRTPPacket(42, []byte{1, 2, 3})
	if got := sess.getCachedVideoRTPPacket(42); got == nil {
		t.Fatal("expected packet to be cached before remap")
	}

	sess.RemapWebRTCVideoEgressSSRC("accepted-switch")

	if got := sess.getCachedVideoRTPPacket(42); got != nil {
		t.Fatalf("expected remap to clear cached packet, got %v", got)
	}
}

func TestApplyWebRTCVideoEgressSSRCRewritesPacket(t *testing.T) {
	sess := &Session{ID: "egress-apply"}
	sess.EnsureWebRTCVideoEgressSSRC(5555)
	pkt := &rtp.Packet{Header: rtp.Header{Version: 2, SSRC: 1111, SequenceNumber: 1}}
	sess.ApplyWebRTCVideoEgressSSRC(pkt)
	if pkt.SSRC != 5555 {
		t.Fatalf("expected rewrite to 5555, got %d", pkt.SSRC)
	}
}
