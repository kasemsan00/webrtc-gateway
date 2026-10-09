package session

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

func offerSDP(t *testing.T, twccEnabled bool) string {
	t.Helper()
	api, err := newGatewayWebRTCAPI(twccEnabled)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	for _, track := range []struct {
		codec  webrtc.RTPCodecCapability
		id     string
		stream string
	}{
		{codec: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, id: "audio", stream: "gateway-audio"},
		{codec: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, id: "video", stream: "gateway-video"},
	} {
		local, err := webrtc.NewTrackLocalStaticRTP(track.codec, track.id, track.stream)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pc.AddTrack(local); err != nil {
			t.Fatal(err)
		}
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	return offer.SDP
}

func TestGatewayWebRTCAPIAdvertisesTWCC(t *testing.T) {
	sdp := offerSDP(t, true)
	for _, want := range []string{
		"a=rtcp-fb:96 nack",
		"a=rtcp-fb:96 nack pli",
		"a=rtcp-fb:96 ccm fir",
		"a=rtcp-fb:96 transport-cc",
		transportCCURI,
	} {
		if !strings.Contains(sdp, want) {
			t.Fatalf("TWCC offer missing %q\nSDP:\n%s", want, sdp)
		}
	}
	if strings.Contains(sdp, "goog-remb") {
		t.Fatalf("offer must not advertise goog-remb\nSDP:\n%s", sdp)
	}
	if strings.Count(sdp, "a=rtcp-fb:96 transport-cc") != 1 {
		t.Fatalf("expected one transport-cc feedback line for PT 96\nSDP:\n%s", sdp)
	}
}

func TestGatewayWebRTCAPIOmitsTWCCWhenDisabled(t *testing.T) {
	sdp := offerSDP(t, false)
	if strings.Contains(sdp, "transport-cc") || strings.Contains(sdp, transportCCURI) || strings.Contains(sdp, "goog-remb") {
		t.Fatalf("disabled TWCC offer still advertises bandwidth feedback\nSDP:\n%s", sdp)
	}
	for _, want := range []string{
		"a=rtcp-fb:96 nack",
		"a=rtcp-fb:96 nack pli",
		"a=rtcp-fb:96 ccm fir",
	} {
		if !strings.Contains(sdp, want) {
			t.Fatalf("disabled TWCC offer missing %q\nSDP:\n%s", want, sdp)
		}
	}
}
