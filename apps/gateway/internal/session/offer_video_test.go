package session

import "testing"

func TestAnalyzeOfferVideo(t *testing.T) {
	t.Run("sendrecv expects uplink", func(t *testing.T) {
		diag := AnalyzeOfferVideo("v=0\r\nm=video 9 RTP/AVP 96\r\na=sendrecv\r\n")
		if !diag.HasVideoMLine {
			t.Fatalf("expected video m-line")
		}
		if diag.VideoPort != 9 {
			t.Fatalf("video port = %d, want 9", diag.VideoPort)
		}
		if diag.VideoDirection != "sendrecv" {
			t.Fatalf("direction = %s, want sendrecv", diag.VideoDirection)
		}
		if !diag.ExpectVideoUplink {
			t.Fatalf("expected video uplink for sendrecv")
		}
		if !diag.HasActiveVideo() {
			t.Fatalf("expected active video for sendrecv")
		}
	})

	t.Run("sendonly expects uplink", func(t *testing.T) {
		diag := AnalyzeOfferVideo("v=0\r\nm=video 9 RTP/AVP 96\r\na=sendonly\r\n")
		if !diag.ExpectVideoUplink {
			t.Fatalf("expected video uplink for sendonly")
		}
	})

	t.Run("recvonly does not expect uplink", func(t *testing.T) {
		diag := AnalyzeOfferVideo("v=0\r\nm=video 9 RTP/AVP 96\r\na=recvonly\r\n")
		if diag.ExpectVideoUplink {
			t.Fatalf("did not expect video uplink for recvonly")
		}
		if !diag.HasVideoMLine {
			t.Fatalf("recvonly still has a video m-line")
		}
		if !diag.HasActiveVideo() {
			t.Fatalf("recvonly with a non-zero port is still active media")
		}
	})

	t.Run("inactive does not expect uplink", func(t *testing.T) {
		diag := AnalyzeOfferVideo("v=0\r\nm=video 9 RTP/AVP 96\r\na=inactive\r\n")
		if diag.ExpectVideoUplink {
			t.Fatalf("did not expect video uplink for inactive")
		}
		if diag.HasActiveVideo() {
			t.Fatalf("inactive video is not active media")
		}
	})

	t.Run("no video m-line", func(t *testing.T) {
		diag := AnalyzeOfferVideo("v=0\r\nm=audio 9 RTP/AVP 111\r\na=sendrecv\r\n")
		if diag.HasVideoMLine {
			t.Fatalf("did not expect a video m-line")
		}
		if diag.ExpectVideoUplink {
			t.Fatalf("did not expect video uplink without m=video")
		}
		if diag.HasActiveVideo() {
			t.Fatalf("audio-only SDP is not active video")
		}
	})

	t.Run("empty sdp", func(t *testing.T) {
		diag := AnalyzeOfferVideo("")
		if diag.ExpectVideoUplink || diag.HasVideoMLine {
			t.Fatalf("empty SDP must not advertise video: %+v", diag)
		}
	})
}

func TestShouldPrimeVideoForSIPOffer(t *testing.T) {
	if ShouldPrimeVideoForSIPOffer("v=0\r\nm=video 9 RTP/AVP 96\r\na=recvonly\r\n") {
		t.Fatalf("recvonly must skip video priming")
	}
	if ShouldPrimeVideoForSIPOffer("v=0\r\nm=audio 9 RTP/AVP 111\r\n") {
		t.Fatalf("audio-only must skip video priming")
	}
	if ShouldPrimeVideoForSIPOffer("v=0\r\nm=video 9 RTP/AVP 96\r\na=inactive\r\n") {
		t.Fatalf("inactive video must skip video priming")
	}
	if !ShouldPrimeVideoForSIPOffer("v=0\r\nm=video 9 RTP/AVP 96\r\na=sendrecv\r\n") {
		t.Fatalf("sendrecv must prime video")
	}
}

func TestSIPOfferIncludeVideoDefaultsTrue(t *testing.T) {
	sess := &Session{ID: "default-video"}
	if !sess.SIPOfferIncludeVideo() {
		t.Fatalf("unset session must include video for backward compatibility")
	}

	sess.SetSIPOfferIncludeVideo(false)
	if sess.SIPOfferIncludeVideo() {
		t.Fatalf("explicit false must omit video")
	}

	sess.SetSIPOfferIncludeVideo(true)
	if !sess.SIPOfferIncludeVideo() {
		t.Fatalf("explicit true must include video")
	}
}
