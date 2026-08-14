package session

import (
	"strconv"
	"strings"
)

// OfferVideoDiagnostics describes the video m-line in a WebRTC or SIP SDP blob.
type OfferVideoDiagnostics struct {
	HasVideoMLine     bool
	VideoPort         int
	VideoDirection    string
	ExpectVideoUplink bool
}

// HasActiveVideo reports whether the SDP advertises usable video media.
// Port 0 or a=inactive is treated as no active video. An omitted direction
// attribute still counts as active when a video m-line with a non-zero port exists.
func (d OfferVideoDiagnostics) HasActiveVideo() bool {
	return d.HasVideoMLine && d.VideoPort > 0 && d.VideoDirection != "inactive"
}

// AnalyzeOfferVideo inspects an SDP offer or answer for video uplink intent.
// ExpectVideoUplink is true only for a non-zero video port with sendrecv or sendonly.
func AnalyzeOfferVideo(sdp string) OfferVideoDiagnostics {
	diag := OfferVideoDiagnostics{
		HasVideoMLine:     false,
		VideoPort:         -1,
		VideoDirection:    "unspecified",
		ExpectVideoUplink: false,
	}
	if sdp == "" {
		return diag
	}

	inVideoSection := false
	lines := strings.Split(sdp, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "m=") {
			inVideoSection = strings.HasPrefix(line, "m=video ")
			if inVideoSection {
				diag.HasVideoMLine = true
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if port, err := strconv.Atoi(fields[1]); err == nil {
						diag.VideoPort = port
					}
				}
			}
			continue
		}

		if !inVideoSection {
			continue
		}

		switch line {
		case "a=sendrecv":
			diag.VideoDirection = "sendrecv"
		case "a=sendonly":
			diag.VideoDirection = "sendonly"
		case "a=recvonly":
			diag.VideoDirection = "recvonly"
		case "a=inactive":
			diag.VideoDirection = "inactive"
		}
	}

	diag.ExpectVideoUplink = diag.HasVideoMLine && diag.VideoPort != 0 &&
		(diag.VideoDirection == "sendrecv" || diag.VideoDirection == "sendonly")

	return diag
}

// ShouldPrimeVideoForSIPOffer reports whether outbound SIP INVITE construction
// should wait for WebRTC H.264 SPS/PPS. Audio-only and recvonly offers must not wait.
func ShouldPrimeVideoForSIPOffer(sdp string) bool {
	return AnalyzeOfferVideo(sdp).ExpectVideoUplink
}
