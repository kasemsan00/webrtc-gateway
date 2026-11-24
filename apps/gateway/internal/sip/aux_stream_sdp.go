package sip

import (
	"fmt"
	"strconv"
	"strings"
)

// SDP of the auxiliary (screen) video m-section: reading the peer's sections
// by position and writing the gateway's own section for an offer or an answer.

// sdpMediaDescription is what the gateway reads from one m= section.
type sdpMediaDescription struct {
	kind              string
	port              int
	transport         string
	ip                string
	rtcpMux           bool
	rtcpPort          int
	h264PT            uint8
	packetizationMode uint8
	direction         string
}

// describeSDPMediaSections returns one description per m-line, in order. A
// section without its own c= line uses the session-level address.
func describeSDPMediaSections(body []byte) []sdpMediaDescription {
	var sections []sdpMediaDescription
	sessionIP := ""
	var current *sdpMediaDescription
	var fmtpMode1 map[uint8]bool
	finalizeCurrent := func() {
		if current != nil && current.h264PT != 0 && fmtpMode1[current.h264PT] {
			current.packetizationMode = 1
		}
	}
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "m=") {
			finalizeCurrent()
			fields := strings.Fields(line)
			desc := sdpMediaDescription{ip: sessionIP}
			if len(fields) > 0 {
				desc.kind = strings.TrimPrefix(strings.ToLower(fields[0]), "m=")
			}
			if len(fields) > 1 {
				portText, _, _ := strings.Cut(fields[1], "/")
				desc.port, _ = strconv.Atoi(portText)
			}
			if len(fields) > 2 {
				desc.transport = fields[2]
			}
			sections = append(sections, desc)
			current = &sections[len(sections)-1]
			fmtpMode1 = make(map[uint8]bool)
			continue
		}
		if strings.HasPrefix(line, "c=IN IP4 ") {
			ip := strings.TrimSpace(strings.TrimPrefix(line, "c=IN IP4 "))
			if current == nil {
				sessionIP = ip
			} else {
				current.ip = ip
			}
			continue
		}
		if current == nil {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case line == "a=rtcp-mux":
			current.rtcpMux = true
		case line == "a=sendrecv", line == "a=sendonly", line == "a=recvonly", line == "a=inactive":
			current.direction = strings.TrimPrefix(line, "a=")
		case strings.HasPrefix(lower, "a=rtcp:"):
			if fields := strings.Fields(line[len("a=rtcp:"):]); len(fields) > 0 {
				current.rtcpPort, _ = strconv.Atoi(fields[0])
			}
		case strings.HasPrefix(lower, "a=rtpmap:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.EqualFold(fields[1], "H264/90000") && current.h264PT == 0 {
				if pt, err := strconv.ParseUint(fields[0][len("a=rtpmap:"):], 10, 8); err == nil {
					current.h264PT = uint8(pt)
				}
			}
		case sdpFmtpHasParameter(line, "packetization-mode", "1"):
			if pt, ok := parseFmtpPayloadType(line); ok {
				fmtpMode1[pt] = true
			}
		}
	}
	finalizeCurrent()
	return sections
}

// buildAuxSDPSection derives the auxiliary video m-section from the
// gateway's primary video section, so both streams use the same transport
// profile, payload type and feedback. The camera's parameter sets are not
// carried over: the screen has its own.
func buildAuxSDPSection(videoSection []string, rtpPort, rtcpPort int, direction string) []string {
	if len(videoSection) == 0 {
		return nil
	}
	fields := strings.Fields(videoSection[0])
	if len(fields) < 4 {
		return nil
	}
	fields[1] = strconv.Itoa(rtpPort)
	section := []string{strings.Join(fields, " ")}
	for _, raw := range videoSection[1:] {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		switch {
		case line == "":
		case line == "a=sendrecv", line == "a=sendonly", line == "a=recvonly", line == "a=inactive":
		case strings.HasPrefix(lower, "a=content:"):
		case strings.HasPrefix(lower, "a=rtcp:"):
			section = append(section, "a=rtcp:"+strconv.Itoa(rtcpPort))
		case strings.HasPrefix(lower, "a=fmtp:"):
			section = append(section, stripSDPFmtpParameter(line, "sprop-parameter-sets"))
		default:
			section = append(section, line)
		}
	}
	return append(section, "a=content:slides", "a="+direction)
}

// buildAuxSDPAnswerSection answers the peer's auxiliary video m-section. An
// answer must use the offer's transport and payload type for that m-line,
// which need not be the camera's. It is nil when the offer has no H.264.
func buildAuxSDPAnswerSection(offer sdpMediaDescription, rtpPort, rtcpPort int, direction string) []string {
	if offer.h264PT == 0 || offer.kind != "video" {
		return nil
	}
	transport := offer.transport
	if transport == "" {
		transport = "RTP/AVP"
	}
	pt := strconv.Itoa(int(offer.h264PT))
	section := []string{
		fmt.Sprintf("m=video %d %s %s", rtpPort, transport, pt),
		"a=rtpmap:" + pt + " H264/90000",
		fmt.Sprintf("a=fmtp:%s profile-level-id=42E01F;packetization-mode=%d", pt, offer.packetizationMode),
	}
	if offer.rtcpMux {
		section = append(section, "a=rtcp-mux")
	} else {
		section = append(section, "a=rtcp:"+strconv.Itoa(rtcpPort))
	}
	if strings.HasSuffix(strings.ToUpper(transport), "AVPF") {
		section = append(section, strings.Split(strings.TrimSuffix(sipVideoRTCPFeedbackSDP, "\n"), "\n")...)
	}
	return append(section, "a=content:slides", "a="+direction)
}

func stripSDPFmtpParameter(line, key string) string {
	head, params, ok := strings.Cut(line, " ")
	if !ok {
		return line
	}
	var kept []string
	for _, param := range strings.Split(params, ";") {
		name, _, _ := strings.Cut(strings.TrimSpace(param), "=")
		if strings.EqualFold(strings.TrimSpace(name), key) || strings.TrimSpace(param) == "" {
			continue
		}
		kept = append(kept, strings.TrimSpace(param))
	}
	return head + " " + strings.Join(kept, ";")
}
