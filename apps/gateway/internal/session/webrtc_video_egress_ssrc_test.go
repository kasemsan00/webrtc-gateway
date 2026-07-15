package session

import (
	"testing"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
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
	packet := &rtp.Packet{Header: rtp.Header{Version: 2, SSRC: 1234, SequenceNumber: 42}}
	data, err := packet.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sess.EnsureWebRTCVideoEgressSSRC(packet.SSRC)
	sess.CacheVideoRTPPacket(packet.SequenceNumber, data)
	if got := sess.getCachedVideoRTPPacket(42); got == nil {
		t.Fatal("expected packet to be cached before remap")
	}

	sess.RemapWebRTCVideoEgressSSRC("accepted-switch")

	if got := sess.getCachedVideoRTPPacket(42); got != nil {
		t.Fatalf("expected remap to clear cached packet, got %v", got)
	}
}

func TestVideoRTPHistoryRejectsPacketFromStaleEgressSSRC(t *testing.T) {
	sess := &Session{ID: "egress-stale-cache"}
	sess.initVideoRTPHistory()
	sess.EnsureWebRTCVideoEgressSSRC(2222)

	packet := &rtp.Packet{Header: rtp.Header{Version: 2, SSRC: 1111, SequenceNumber: 42}}
	data, err := packet.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sess.CacheVideoRTPPacket(packet.SequenceNumber, data)

	if got := sess.getCachedVideoRTPPacket(packet.SequenceNumber); got != nil {
		t.Fatalf("expected stale-SSRC packet to be rejected, got %v", got)
	}
}

func TestGetCachedVideoRTPPacketRejectsHistoryFromStaleEgressSSRC(t *testing.T) {
	sess := &Session{ID: "egress-stale-retransmit"}
	sess.initVideoRTPHistory()
	sess.EnsureWebRTCVideoEgressSSRC(2222)
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		"test",
	)
	if err != nil {
		t.Fatalf("new video track: %v", err)
	}
	sess.VideoTrack = track

	packet := &rtp.Packet{Header: rtp.Header{Version: 2, SSRC: 1111, SequenceNumber: 42}}
	data, err := packet.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	index := int(packet.SequenceNumber % uint16(sess.VideoRTPHistorySize))
	sess.VideoRTPHistoryPackets[index] = data
	sess.VideoRTPHistorySeq[index] = packet.SequenceNumber

	if got := sess.getCachedVideoRTPPacket(packet.SequenceNumber); got != nil {
		t.Fatalf("expected stale-SSRC history to be refused, got %v", got)
	}
	sent, missing := sess.RetransmitVideoNACK([]rtcp.NackPair{{PacketID: packet.SequenceNumber}})
	if sent != 0 || missing != 1 {
		t.Fatalf("stale retransmit sent=%d missing=%d, want 0/1", sent, missing)
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
