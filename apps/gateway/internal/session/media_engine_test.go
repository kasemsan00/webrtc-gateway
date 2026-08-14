package session

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestPreferWebRTCH264PacketizationModeAnswersWithSIPModeOnly(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       uint8
		wantMode   string
		rejectMode string
		wantPT     string
		rejectPTs  []string
	}{
		{name: "sip_mode_0", mode: 0, wantMode: "packetization-mode=0", rejectMode: "packetization-mode=1", wantPT: "a=rtpmap:107 H264/90000", rejectPTs: []string{"a=rtpmap:98 H264/90000", "a=rtpmap:103 H264/90000"}},
		{name: "sip_mode_1", mode: 1, wantMode: "packetization-mode=1", rejectMode: "packetization-mode=0", wantPT: "a=rtpmap:103 H264/90000", rejectPTs: []string{"a=rtpmap:96 H264/90000", "a=rtpmap:107 H264/90000"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offerer := newH264DualModeOfferer(t)
			defer offerer.Close()

			offer, err := offerer.CreateOffer(nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := offerer.SetLocalDescription(offer); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(offer.SDP, "packetization-mode=1") || !strings.Contains(offer.SDP, "packetization-mode=0") {
				t.Fatalf("test offer must advertise both H264 modes:\n%s", offer.SDP)
			}

			answerer := newGatewayH264Answerer(t)
			defer answerer.Close()
			if err := answerer.SetRemoteDescription(offer); err != nil {
				t.Fatal(err)
			}
			if err := PreferWebRTCH264PacketizationMode(answerer, offer.SDP, tc.mode); err != nil {
				t.Fatal(err)
			}

			answer, err := answerer.CreateAnswer(nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(answer.SDP, tc.wantMode) {
				t.Fatalf("answer does not contain %s:\n%s", tc.wantMode, answer.SDP)
			}
			if strings.Contains(answer.SDP, tc.rejectMode) {
				t.Fatalf("answer unexpectedly contains %s:\n%s", tc.rejectMode, answer.SDP)
			}
			if !strings.Contains(answer.SDP, tc.wantPT) {
				t.Fatalf("answer does not preserve offered payload mapping %s:\n%s", tc.wantPT, answer.SDP)
			}
			for _, rejectPT := range tc.rejectPTs {
				if strings.Contains(answer.SDP, rejectPT) {
					t.Fatalf("answer unexpectedly contains payload mapping %s:\n%s", rejectPT, answer.SDP)
				}
			}
			if err := answerer.SetLocalDescription(answer); err != nil {
				t.Fatal(err)
			}
			if err := offerer.SetRemoteDescription(answer); err != nil {
				t.Fatalf("browser must accept the restricted answer: %v\n%s", err, answer.SDP)
			}

			videoSenderChecked := false
			for _, sender := range offerer.GetSenders() {
				if sender.Track() == nil || sender.Track().Kind() != webrtc.RTPCodecTypeVideo {
					continue
				}
				videoSenderChecked = true
				codecs := sender.GetParameters().Codecs
				if len(codecs) == 0 || !strings.Contains(codecs[0].SDPFmtpLine, tc.wantMode) {
					t.Fatalf("browser video sender did not select %s: %+v", tc.wantMode, codecs)
				}
			}
			if !videoSenderChecked {
				t.Fatal("test offerer has no video sender")
			}
		})
	}
}

func newH264DualModeOfferer(t *testing.T) *webrtc.PeerConnection {
	t.Helper()
	mediaEngine := &webrtc.MediaEngine{}
	for _, codec := range []webrtc.RTPCodecParameters{
		h264CodecParameters(103, h264ConstrainedBaselineProfile, 1, nil),
		h264CodecParameters(107, h264ConstrainedBaselineProfile, 0, nil),
	} {
		if err := mediaEngine.RegisterCodec(codec, webrtc.RTPCodecTypeVideo); err != nil {
			t.Fatal(err)
		}
	}

	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
		"video",
		"browser-video",
	)
	if err != nil {
		pc.Close()
		t.Fatal(err)
	}
	if _, err := pc.AddTrack(videoTrack); err != nil {
		pc.Close()
		t.Fatal(err)
	}
	return pc
}

func newGatewayH264Answerer(t *testing.T) *webrtc.PeerConnection {
	t.Helper()
	mediaEngine, err := createCustomMediaEngine()
	if err != nil {
		t.Fatal(err)
	}
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
		"video",
		"gateway-video",
	)
	if err != nil {
		pc.Close()
		t.Fatal(err)
	}
	if _, err := pc.AddTrack(videoTrack); err != nil {
		pc.Close()
		t.Fatal(err)
	}
	return pc
}

func TestCustomMediaEngineAnswersBrowserLikeOffer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		audio bool
		video bool
	}{
		{name: "audio", audio: true},
		{name: "video", video: true},
		{name: "audio_and_video", audio: true, video: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testCustomMediaEngineAnswersBrowserLikeOffer(t, tc.audio, tc.video)
		})
	}
}

func testCustomMediaEngineAnswersBrowserLikeOffer(t *testing.T, addAudio, addVideo bool) {
	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer offerer.Close()

	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"browser-audio",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := offerer.AddTrack(audioTrack); err != nil {
		t.Fatal(err)
	}
	if _, err := offerer.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := offerer.CreateDataChannel("data", nil); err != nil {
		t.Fatal(err)
	}

	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}

	mediaEngine, err := createCustomMediaEngine()
	if err != nil {
		t.Fatal(err)
	}
	answerer, err := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer answerer.Close()

	if addAudio {
		answerAudio, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
			"audio",
			"gateway-audio",
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := answerer.AddTrack(answerAudio); err != nil {
			t.Fatal(err)
		}
	}
	if addVideo {
		answerVideo, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
			"video",
			"gateway-video",
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := answerer.AddTrack(answerVideo); err != nil {
			t.Fatal(err)
		}
	}

	if err := answerer.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := answerer.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := answerer.SetLocalDescription(answer); err != nil {
		t.Fatalf("browser-like offer must be answerable: %v", err)
	}
}

func TestPreferWebRTCH264PacketizationModeSkipsAudioOnlyOffer(t *testing.T) {
	sdp := "v=0\r\n" +
		"o=- 0 0 IN IP4 127.0.0.1\r\n" +
		"s=-\r\n" +
		"t=0 0\r\n" +
		"m=audio 9 UDP/TLS/RTP/SAVPF 111\r\n" +
		"a=rtpmap:111 opus/48000/2\r\n" +
		"a=sendrecv\r\n" +
		"m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n" +
		"a=sctp-port:5000\r\n"
	if err := PreferWebRTCH264PacketizationMode(nil, sdp, 1); err != nil {
		t.Fatalf("audio-only offer must not require H264 packetization selection: %v", err)
	}
}

func TestPreferWebRTCH264PacketizationModeAnswersAudioOnlyBrowserOffer(t *testing.T) {
	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer offerer.Close()

	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"browser-audio",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := offerer.AddTrack(audioTrack); err != nil {
		t.Fatal(err)
	}
	if _, err := offerer.CreateDataChannel("data", nil); err != nil {
		t.Fatal(err)
	}

	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(offer.SDP, "m=video") {
		t.Fatalf("audio-only offer must omit m=video:\n%s", offer.SDP)
	}

	answerer := newGatewayH264Answerer(t)
	defer answerer.Close()
	if err := answerer.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	if err := PreferWebRTCH264PacketizationMode(answerer, offer.SDP, 1); err != nil {
		t.Fatalf("audio-only offer must skip H264 preference: %v", err)
	}

	answer, err := answerer.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(answer.SDP, "m=video") {
		t.Fatalf("audio-only answer must omit m=video:\n%s", answer.SDP)
	}
	if !strings.Contains(answer.SDP, "m=audio") {
		t.Fatalf("audio-only answer must keep m=audio:\n%s", answer.SDP)
	}
	if err := answerer.SetLocalDescription(answer); err != nil {
		t.Fatalf("audio-only offer must be answerable: %v", err)
	}
	if err := offerer.SetRemoteDescription(answer); err != nil {
		t.Fatalf("browser must accept the audio-only answer: %v\n%s", err, answer.SDP)
	}
}
