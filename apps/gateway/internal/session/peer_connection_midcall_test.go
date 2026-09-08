package session

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

func TestPrepareSwitchPeerConnectionAnswersClientOffer(t *testing.T) {
	old := newGatewayH264Answerer(t)
	sess := &Session{ID: "switch-offer", PeerConnection: old, State: StateActive}
	sess.SetSIPVideoPacketizationMode(1)

	if err := sess.PrepareSwitchPeerConnection(config.TURNConfig{}, false); err != nil {
		t.Fatalf("prepare @switch PC failed: %v", err)
	}
	if sess.PeerConnection == nil || sess.PeerConnection == old {
		t.Fatal("expected @switch to replace the PeerConnection")
	}

	clientMedia, err := createCustomMediaEngine()
	if err != nil {
		t.Fatal(err)
	}
	client, err := webrtc.NewAPI(webrtc.WithMediaEngine(clientMedia)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"client-audio",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddTrack(audioTrack); err != nil {
		t.Fatal(err)
	}
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
		"video",
		"client-video",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddTrack(videoTrack); err != nil {
		t.Fatal(err)
	}
	clientOffer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(clientOffer); err != nil {
		t.Fatal(err)
	}

	mid := "0"
	queued, err := sess.AddRemoteICECandidate(webrtc.ICECandidateInit{
		Candidate: "candidate:1 1 UDP 2122260223 192.0.2.1 9 typ host",
		SDPMid:    &mid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Fatal("expected trickle ICE before client offer to be queued")
	}

	answerSDP, err := sess.AnswerClientOffer(client.LocalDescription().SDP)
	if err != nil {
		t.Fatalf("gateway rejected resume-style @switch offer: %v\n%s", err, client.LocalDescription().SDP)
	}
	if strings.Contains(answerSDP, "packetization-mode=0") {
		t.Fatalf("@switch answer advertised H264 packetization-mode=0:\n%s", answerSDP)
	}
	if !strings.Contains(answerSDP, "packetization-mode=1") {
		t.Fatalf("@switch answer missing H264 packetization-mode=1:\n%s", answerSDP)
	}
	if got := len(sess.pendingRemoteICE); got != 0 {
		t.Fatalf("queued ICE leftover after offer: %d", got)
	}
	if err := client.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		t.Fatalf("client rejected gateway @switch answer: %v\n%s", err, answerSDP)
	}
}
