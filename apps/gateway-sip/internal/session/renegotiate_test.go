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

func TestRenegotiateIceGatherTimeoutIsHandoverSafe(t *testing.T) {
	if RENEGOTIATE_ICE_GATHER_TIMEOUT < 3*time.Second {
		t.Fatalf("expected renegotiate ICE gather timeout >= 3s, got %s", RENEGOTIATE_ICE_GATHER_TIMEOUT)
	}
}

func TestHasUsableResumeCandidatesInSDP(t *testing.T) {
	t.Run("rejects empty SDP", func(t *testing.T) {
		if hasUsableResumeCandidatesInSDP("") {
			t.Fatalf("expected empty SDP to be unusable")
		}
	})

	t.Run("rejects single host candidate", func(t *testing.T) {
		sdp := "v=0\r\na=candidate:1 1 udp 2122260223 10.0.0.2 59784 typ host\r\n"
		if hasUsableResumeCandidatesInSDP(sdp) {
			t.Fatalf("expected single host candidate to be unusable for resume early-exit")
		}
	})

	t.Run("accepts relay candidate", func(t *testing.T) {
		sdp := "v=0\r\na=candidate:1 1 udp 1686052607 1.2.3.4 50000 typ relay raddr 0.0.0.0 rport 0\r\n"
		if !hasUsableResumeCandidatesInSDP(sdp) {
			t.Fatalf("expected relay candidate to be usable")
		}
	})

	t.Run("accepts multiple candidates", func(t *testing.T) {
		sdp := "v=0\r\na=candidate:1 1 udp 2122260223 10.0.0.2 59784 typ host\r\na=candidate:2 1 udp 2122194687 10.0.0.2 59785 typ host\r\n"
		if !hasUsableResumeCandidatesInSDP(sdp) {
			t.Fatalf("expected multiple candidates to be usable")
		}
	})
}
