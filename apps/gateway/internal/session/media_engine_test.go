package session

import (
	"testing"

	"github.com/pion/webrtc/v4"
)

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
