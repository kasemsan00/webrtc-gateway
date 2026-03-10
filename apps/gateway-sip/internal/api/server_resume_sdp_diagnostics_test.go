package api

import "testing"

func TestAnalyzeResumeOfferVideoSDP(t *testing.T) {
	t.Run("sendrecv", func(t *testing.T) {
		sdp := "v=0\r\nm=video 9 RTP/AVP 96\r\na=sendrecv\r\n"
		diag := analyzeResumeOfferVideoSDP(sdp)
		if !diag.HasVideoMLine {
			t.Fatalf("expected video m-line")
		}
		if diag.VideoPort != 9 {
			t.Fatalf("expected video port 9, got %d", diag.VideoPort)
		}
		if diag.VideoDirection != "sendrecv" {
			t.Fatalf("expected sendrecv, got %s", diag.VideoDirection)
		}
	})

	t.Run("recvonly", func(t *testing.T) {
		sdp := "v=0\r\nm=video 9 RTP/AVP 96\r\na=recvonly\r\n"
		diag := analyzeResumeOfferVideoSDP(sdp)
		if diag.VideoDirection != "recvonly" {
			t.Fatalf("expected recvonly, got %s", diag.VideoDirection)
		}
	})

	t.Run("inactive", func(t *testing.T) {
		sdp := "v=0\r\nm=video 9 RTP/AVP 96\r\na=inactive\r\n"
		diag := analyzeResumeOfferVideoSDP(sdp)
		if diag.VideoDirection != "inactive" {
			t.Fatalf("expected inactive, got %s", diag.VideoDirection)
		}
	})

	t.Run("video port zero", func(t *testing.T) {
		sdp := "v=0\r\nm=video 0 RTP/AVP 96\r\na=sendrecv\r\n"
		diag := analyzeResumeOfferVideoSDP(sdp)
		if diag.VideoPort != 0 {
			t.Fatalf("expected video port 0, got %d", diag.VideoPort)
		}
	})
}
