package session

import (
	"testing"
	"time"
)

func TestWaitForGatheringComplete_ReturnsTrueWhenGatheringCompletes(t *testing.T) {
	done := make(chan struct{})
	close(done)

	ok := waitForGatheringComplete(done, 100*time.Millisecond)
	if !ok {
		t.Fatalf("expected waitForGatheringComplete to return true when channel is closed")
	}
}

func TestWaitForGatheringComplete_ReturnsFalseOnTimeout(t *testing.T) {
	done := make(chan struct{})
	start := time.Now()

	ok := waitForGatheringComplete(done, 40*time.Millisecond)
	elapsed := time.Since(start)

	if ok {
		t.Fatalf("expected waitForGatheringComplete to return false when timeout is reached")
	}
	if elapsed < 35*time.Millisecond {
		t.Fatalf("expected waitForGatheringComplete to wait close to timeout, elapsed=%s", elapsed)
	}
}

func TestAnalyzeRenegotiateVideoOffer(t *testing.T) {
	t.Run("sendrecv expects uplink", func(t *testing.T) {
		sdp := "v=0\r\nm=video 9 RTP/AVP 96\r\na=sendrecv\r\n"
		diag := analyzeRenegotiateVideoOffer(sdp)
		if !diag.ExpectVideoUplink {
			t.Fatalf("expected video uplink for sendrecv")
		}
	})

	t.Run("sendonly expects uplink", func(t *testing.T) {
		sdp := "v=0\r\nm=video 9 RTP/AVP 96\r\na=sendonly\r\n"
		diag := analyzeRenegotiateVideoOffer(sdp)
		if !diag.ExpectVideoUplink {
			t.Fatalf("expected video uplink for sendonly")
		}
	})

	t.Run("recvonly does not expect uplink", func(t *testing.T) {
		sdp := "v=0\r\nm=video 9 RTP/AVP 96\r\na=recvonly\r\n"
		diag := analyzeRenegotiateVideoOffer(sdp)
		if diag.ExpectVideoUplink {
			t.Fatalf("did not expect video uplink for recvonly")
		}
	})

	t.Run("inactive does not expect uplink", func(t *testing.T) {
		sdp := "v=0\r\nm=video 9 RTP/AVP 96\r\na=inactive\r\n"
		diag := analyzeRenegotiateVideoOffer(sdp)
		if diag.ExpectVideoUplink {
			t.Fatalf("did not expect video uplink for inactive")
		}
	})

	t.Run("video port zero does not expect uplink", func(t *testing.T) {
		sdp := "v=0\r\nm=video 0 RTP/AVP 96\r\na=sendrecv\r\n"
		diag := analyzeRenegotiateVideoOffer(sdp)
		if diag.ExpectVideoUplink {
			t.Fatalf("did not expect video uplink for disabled video port")
		}
	})
}
