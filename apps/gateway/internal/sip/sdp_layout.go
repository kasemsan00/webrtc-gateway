package sip

import (
	"strings"

	"webrtc-sip-gateway/internal/session"
)

// The gateway builds its SIP SDP as audio followed by at most one video
// (createSDPOffer). A peer may offer more m-lines than that: a second video
// stream, or a media type the gateway does not handle. RFC 3264 requires an
// answer to carry one m-line per offered m-line, in the same order, with port
// 0 for the ones that are refused. The functions here derive that order from
// the peer's offer and lay the gateway's own SDP out to match it.

// sipMediaLayoutFromOffer returns one slot per m-line of the offer. The first
// m=audio and the first m=video are the call's primary streams; every other
// m-line is declined. An offer without m-lines yields nil.
func sipMediaLayoutFromOffer(offer []byte) []session.SIPMediaSlot {
	var tracker primarySDPSectionTracker
	var layout []session.SIPMediaSlot
	for _, raw := range strings.Split(string(offer), "\n") {
		line := strings.TrimSpace(raw)
		section, isMLine := tracker.observe(line)
		if !isMLine {
			continue
		}
		slot := session.SIPMediaSlot{Kind: session.SIPMediaSlotDeclined, DeclinedLine: declinedSDPMediaLine(line)}
		switch section {
		case "audio":
			slot.Kind = session.SIPMediaSlotAudio
		case "video":
			slot.Kind = session.SIPMediaSlotVideo
		}
		layout = append(layout, slot)
	}
	return layout
}

// declinedSDPMediaLine rewrites an m= line to refuse the stream: same media
// type, transport and formats, port 0 (RFC 3264 section 6).
func declinedSDPMediaLine(mLine string) string {
	fields := strings.Fields(mLine)
	if len(fields) == 0 {
		return "m=audio 0 RTP/AVP 0"
	}
	media := fields[0]
	transport := "RTP/AVP"
	if len(fields) >= 3 {
		transport = fields[2]
	}
	formats := []string{"0"}
	if len(fields) >= 4 {
		formats = fields[3:]
	}
	return media + " 0 " + transport + " " + strings.Join(formats, " ")
}

// gatewaySDPParts is a gateway-built SDP split into its session-level lines
// and its audio and video sections.
type gatewaySDPParts struct {
	session         []string
	audio           []string
	video           []string
	// aux is the auxiliary video section to place at the layout's auxiliary
	// slot. It is not part of the gateway's base SDP; the caller sets it.
	aux             []string
	trailingNewline bool
}

func splitGatewaySDP(sdp []byte) gatewaySDPParts {
	body := string(sdp)
	parts := gatewaySDPParts{trailingNewline: strings.HasSuffix(body, "\n")}
	body = strings.TrimSuffix(body, "\n")

	var tracker primarySDPSectionTracker
	for _, line := range strings.Split(body, "\n") {
		section, _ := tracker.observe(strings.TrimSpace(line))
		switch section {
		case "":
			parts.session = append(parts.session, line)
		case "audio":
			parts.audio = append(parts.audio, line)
		case "video":
			parts.video = append(parts.video, line)
		}
	}
	return parts
}

func (p gatewaySDPParts) join(lines []string) []byte {
	out := strings.Join(lines, "\n")
	if p.trailingNewline {
		out += "\n"
	}
	return []byte(out)
}

// auxSlotIndex returns the layout position of the auxiliary video stream: the
// first m=video after the primary one. It is -1 when the dialog has none.
// While the stream is closed that position is an ordinary refused m-line.
func auxSlotIndex(layout []session.SIPMediaSlot) int {
	for i, slot := range layout {
		if slot.Kind == session.SIPMediaSlotDeclined && strings.HasPrefix(slot.DeclinedLine, "m=video ") {
			return i
		}
	}
	return -1
}

// ensureAuxSlot returns the layout with a position for the auxiliary video
// stream and that position's index. An earlier refused m=video is reused, so
// the dialog never grows a third video m-line; otherwise a slot is appended.
func ensureAuxSlot(layout []session.SIPMediaSlot, declinedLine string) ([]session.SIPMediaSlot, int) {
	if index := auxSlotIndex(layout); index >= 0 {
		return layout, index
	}
	out := append(append([]session.SIPMediaSlot(nil), layout...),
		session.SIPMediaSlot{Kind: session.SIPMediaSlotDeclined, DeclinedLine: declinedLine})
	return out, len(out) - 1
}

// plainSIPMediaLayout is the layout of a gateway-built SDP on a dialog whose
// peer never sent an offer: audio, then video when the SDP has it.
func plainSIPMediaLayout(parts gatewaySDPParts) []session.SIPMediaSlot {
	var layout []session.SIPMediaSlot
	if len(parts.audio) > 0 {
		layout = append(layout, session.SIPMediaSlot{Kind: session.SIPMediaSlotAudio, DeclinedLine: declinedSDPMediaLine(parts.audio[0])})
	}
	if len(parts.video) > 0 {
		layout = append(layout, session.SIPMediaSlot{Kind: session.SIPMediaSlotVideo, DeclinedLine: declinedSDPMediaLine(parts.video[0])})
	}
	return layout
}

// layoutSDPLines places the gateway's sections at the layout's positions. It
// reports which of the gateway's sections were placed.
func layoutSDPLines(parts gatewaySDPParts, layout []session.SIPMediaSlot) (lines []string, audioPlaced, videoPlaced bool) {
	lines = append(lines, parts.session...)
	auxIndex := -1
	if len(parts.aux) > 0 {
		auxIndex = auxSlotIndex(layout)
	}
	for i, slot := range layout {
		switch {
		case slot.Kind == session.SIPMediaSlotAudio && len(parts.audio) > 0:
			lines = append(lines, parts.audio...)
			audioPlaced = true
		case slot.Kind == session.SIPMediaSlotVideo && len(parts.video) > 0:
			lines = append(lines, parts.video...)
			videoPlaced = true
		case i == auxIndex:
			lines = append(lines, parts.aux...)
		default:
			lines = append(lines, slot.DeclinedLine)
		}
	}
	return lines, audioPlaced, videoPlaced
}

// renderSDPAnswerForLayout lays a gateway-built answer out to mirror the
// offer it answers: one m-line per offered m-line, in the offered order. The
// gateway's audio and video sections go to the first audio and first video
// positions, every other position is refused with port 0, and a gateway
// section the offer has no position for is left out. With the plain
// audio[+video] order the result is byte-identical to base. A non-empty aux
// section is written at the layout's auxiliary slot instead of a refusal.
func renderSDPAnswerForLayout(base []byte, layout []session.SIPMediaSlot, aux []string) []byte {
	if len(layout) == 0 {
		return base
	}
	parts := splitGatewaySDP(base)
	parts.aux = aux
	lines, _, _ := layoutSDPLines(parts, layout)
	return parts.join(lines)
}

// renderSDPOfferForLayout lays a gateway-originated mid-call offer (hold,
// resume) out on the dialog's current m-line order, so positions refused in
// an earlier exchange stay in the SDP with port 0. Unlike an answer it never
// drops the gateway's own audio or video: a section the layout has no
// position for is appended. A non-empty aux section is written at the
// layout's auxiliary slot; the caller makes sure the layout has one
// (ensureAuxSlot).
func renderSDPOfferForLayout(base []byte, layout []session.SIPMediaSlot, aux []string) []byte {
	if len(layout) == 0 {
		return base
	}
	parts := splitGatewaySDP(base)
	parts.aux = aux
	lines, audioPlaced, videoPlaced := layoutSDPLines(parts, layout)
	if !audioPlaced {
		lines = append(lines, parts.audio...)
	}
	if !videoPlaced {
		lines = append(lines, parts.video...)
	}
	return parts.join(lines)
}
