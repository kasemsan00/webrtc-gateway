package session

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestCreatePeerConnectionOfferAfterPreferStaysClientAnswerable(t *testing.T) {
	offerer := newH264DualModeOfferer(t)
	defer offerer.Close()

	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}

	answerer := newGatewayH264Answerer(t)
	defer answerer.Close()
	if err := answerer.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	if err := PreferWebRTCH264PacketizationMode(answerer, offer.SDP, 1); err != nil {
		t.Fatal(err)
	}
	answer, err := answerer.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := answerer.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	if err := offerer.SetRemoteDescription(answer); err != nil {
		t.Fatal(err)
	}

	sess := &Session{ID: "switch-offer", PeerConnection: answerer}
	sess.SetSIPVideoPacketizationMode(1)

	switchSDP, err := sess.CreatePeerConnectionOffer()
	if err != nil {
		t.Fatalf("gateway @switch CreateOffer failed: %v", err)
	}
	if strings.Contains(switchSDP, "packetization-mode=0") {
		t.Fatalf("@switch offer advertised H264 packetization-mode=0:\n%s", switchSDP)
	}
	if !strings.Contains(switchSDP, "packetization-mode=1") {
		t.Fatalf("@switch offer missing H264 packetization-mode=1:\n%s", switchSDP)
	}

	if err := offerer.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  switchSDP,
	}); err != nil {
		t.Fatalf("n1669-like peer rejected gateway @switch offer: %v\n%s", err, switchSDP)
	}
	switchAnswer, err := offerer.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("n1669-like peer failed to answer @switch offer: %v\n%s", err, switchSDP)
	}
	if err := offerer.SetLocalDescription(switchAnswer); err != nil {
		t.Fatal(err)
	}
	if err := sess.ApplyPeerConnectionAnswer(switchAnswer.SDP); err != nil {
		t.Fatalf("gateway rejected n1669-like @switch answer: %v\n%s", err, switchAnswer.SDP)
	}
}
