package session

import (
	"strings"
	"testing"
	"time"

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
	if sess.legacyPeerConnection != old {
		t.Fatal("expected @switch to park the previous PeerConnection")
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

func TestAnswerClientOfferWaitsForSwitchReplacementPC(t *testing.T) {
	old := newGatewayH264Answerer(t)
	sess := &Session{ID: "switch-wait-pc", PeerConnection: old, State: StateActive}
	sess.SetSIPVideoPacketizationMode(1)
	if !sess.TryClaimSwitchVideoRenegotiation(1) {
		t.Fatal("expected claim")
	}
	if _, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		Source: MidCallRenegotiationSourceSwitchMessage,
		Method: "WS",
	}); !ok {
		t.Fatal("expected pending switch renegotiation")
	}

	client := newSwitchClientOfferPC(t)
	defer client.Close()

	type result struct {
		sdp string
		err error
	}
	done := make(chan result, 1)
	go func() {
		sdp, err := sess.AnswerClientOffer(client.LocalDescription().SDP)
		done <- result{sdp: sdp, err: err}
	}()

	time.Sleep(40 * time.Millisecond)
	select {
	case got := <-done:
		t.Fatalf("client offer applied before replacement PC was ready: sdpBytes=%d err=%v", len(got.sdp), got.err)
	default:
	}
	if sess.switchReplacementPCReady {
		t.Fatal("replacement PC became ready before PrepareSwitchPeerConnection")
	}

	if err := sess.PrepareSwitchPeerConnection(config.TURNConfig{}, false); err != nil {
		t.Fatalf("prepare @switch PC failed: %v", err)
	}
	if sess.PeerConnection == old {
		t.Fatal("expected replacement PeerConnection")
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("gateway rejected delayed @switch offer: %v", got.err)
		}
		if got.sdp == "" {
			t.Fatal("expected answer SDP after replacement PC was ready")
		}
		if sess.PeerConnection == nil || sess.PeerConnection.RemoteDescription() == nil {
			t.Fatal("expected client offer on the replacement PeerConnection")
		}
		if old.RemoteDescription() != nil {
			t.Fatal("client offer was applied to the parked PeerConnection")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for AnswerClientOffer after PrepareSwitchPeerConnection")
	}
}

func TestAnswerClientOfferAbortsWhenSwitchClaimReleased(t *testing.T) {
	old := newGatewayH264Answerer(t)
	sess := &Session{ID: "switch-wait-abort", PeerConnection: old, State: StateActive}
	if !sess.TryClaimSwitchVideoRenegotiation(2) {
		t.Fatal("expected claim")
	}
	started, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		Source: MidCallRenegotiationSourceSwitchMessage,
		Method: "WS",
	})
	if !ok {
		t.Fatal("expected pending switch renegotiation")
	}

	client := newSwitchClientOfferPC(t)
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := sess.AnswerClientOffer(client.LocalDescription().SDP)
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	if !sess.FailMidCallRenegotiation(started.ID, 500, "prepare failed") {
		t.Fatal("expected fail")
	}
	sess.ReleaseSwitchVideoRenegotiationClaim(2)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected AnswerClientOffer to fail after claim abort")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for aborted AnswerClientOffer")
	}
}

func newSwitchClientOfferPC(t *testing.T) *webrtc.PeerConnection {
	t.Helper()
	clientMedia, err := createCustomMediaEngine()
	if err != nil {
		t.Fatal(err)
	}
	client, err := webrtc.NewAPI(webrtc.WithMediaEngine(clientMedia)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
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
	return client
}
