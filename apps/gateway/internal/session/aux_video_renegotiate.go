package session

import (
	"fmt"
	"strings"

	"github.com/pion/webrtc/v4"
)

type auxOfferVideoSection struct {
	mid       string
	direction string
}

func parseAuxOfferVideoSections(sdp string) (activeCount int, auxSections []auxOfferVideoSection) {
	var current *auxOfferVideoSection
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "m=") {
			current = nil
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[0] != "m=video" || fields[1] == "0" {
				continue
			}
			activeCount++
			if activeCount >= 2 {
				auxSections = append(auxSections, auxOfferVideoSection{})
				current = &auxSections[len(auxSections)-1]
			}
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case current.mid == "" && strings.HasPrefix(line, "a=mid:"):
			current.mid = strings.TrimSpace(strings.TrimPrefix(line, "a=mid:"))
		case line == "a=sendonly", line == "a=recvonly", line == "a=sendrecv", line == "a=inactive":
			current.direction = strings.TrimPrefix(line, "a=")
		}
	}
	return activeCount, auxSections
}

// AuxVideoMidFromOffer returns the a=mid of the WebRTC media section that
// carries the auxiliary video in a client offer. The first active m=video is
// always the camera. Offers with two active video sections return the second
// section's mid. Offers with three active video sections (dual screen share:
// camera + remote screen recvonly + local screen sendonly) return the unique
// non-recvonly auxiliary section's mid.
func AuxVideoMidFromOffer(sdp string) (string, bool) {
	return AuxUplinkMidFromOffer(sdp, "")
}

// AuxUplinkMidFromOffer returns the a=mid of the client's auxiliary uplink
// video section in an offer carrying 2 or 3 active m=video sections (camera
// plus local screen, and optionally an already-negotiated remote screen
// section named by excludeMid).
func AuxUplinkMidFromOffer(sdp, excludeMid string) (string, bool) {
	activeCount, auxSections := parseAuxOfferVideoSections(sdp)
	if activeCount < 2 || activeCount > 3 {
		return "", false
	}
	if activeCount == 2 {
		sec := auxSections[0]
		if sec.mid == "" || (excludeMid != "" && sec.mid == excludeMid) {
			return "", false
		}
		return sec.mid, true
	}

	// Three active video sections: camera + downlink (recvonly) + uplink (sendonly/sendrecv).
	var candidates []auxOfferVideoSection
	for _, sec := range auxSections {
		if sec.mid == "" {
			return "", false
		}
		if excludeMid != "" && sec.mid == excludeMid {
			continue
		}
		if sec.direction == "recvonly" || sec.direction == "inactive" {
			continue
		}
		candidates = append(candidates, sec)
	}
	if len(candidates) != 1 {
		return "", false
	}
	return candidates[0].mid, true
}

// auxOfferVideoMids resolves auxiliary uplink and downlink mids from a resume
// offer that carries 2 or 3 active video sections.
func auxOfferVideoMids(sdp string, wantUplink, wantDownlink bool) (uplinkMid, downlinkMid string, uplinkBeforeDownlink bool) {
	activeCount, auxSections := parseAuxOfferVideoSections(sdp)
	if activeCount < 2 || activeCount > 3 {
		return "", "", false
	}
	if activeCount == 2 {
		sec := auxSections[0]
		if sec.mid == "" {
			return "", "", false
		}
		if wantUplink && !wantDownlink && sec.direction != "recvonly" && sec.direction != "inactive" {
			return sec.mid, "", false
		}
		if wantDownlink && !wantUplink && sec.direction != "sendonly" && sec.direction != "inactive" {
			return "", sec.mid, false
		}
		if wantUplink && wantDownlink {
			switch sec.direction {
			case "recvonly":
				return "", sec.mid, false
			case "sendonly":
				return sec.mid, "", false
			}
		}
		return "", "", false
	}

	uplinkIdx, downlinkIdx := -1, -1
	for i, sec := range auxSections {
		if sec.mid == "" {
			continue
		}
		switch sec.direction {
		case "recvonly":
			if downlinkIdx == -1 {
				downlinkIdx = i
				downlinkMid = sec.mid
			}
		case "sendonly":
			if uplinkIdx == -1 {
				uplinkIdx = i
				uplinkMid = sec.mid
			}
		case "sendrecv", "":
			if wantUplink && uplinkIdx == -1 {
				uplinkIdx = i
				uplinkMid = sec.mid
			} else if wantDownlink && downlinkIdx == -1 {
				downlinkIdx = i
				downlinkMid = sec.mid
			}
		}
	}
	if !wantUplink {
		uplinkMid = ""
		uplinkIdx = -1
	}
	if !wantDownlink {
		downlinkMid = ""
		downlinkIdx = -1
	}
	return uplinkMid, downlinkMid, uplinkIdx >= 0 && downlinkIdx >= 0 && uplinkIdx < downlinkIdx
}

// AnswerClientOfferInPlace applies a client offer on the live PeerConnection
// and returns the gateway's answer. Unlike resume and @switch it keeps the
// PeerConnection, its ICE and DTLS transports and the existing tracks, so the
// call's audio and primary video are not interrupted. It is used to add the
// auxiliary video section mid-call.
func (s *Session) AnswerClientOfferInPlace(offerSDP string) (string, error) {
	if strings.TrimSpace(offerSDP) == "" {
		return "", fmt.Errorf("offer sdp missing")
	}
	s.mu.RLock()
	pc := s.PeerConnection
	s.mu.RUnlock()
	if pc == nil {
		return "", fmt.Errorf("peer connection not available")
	}
	if state := pc.SignalingState(); state != webrtc.SignalingStateStable {
		return "", fmt.Errorf("peer connection is negotiating (%s)", state)
	}

	// pion cannot roll back a remote offer, so everything that can be checked
	// is checked before the offer is applied. A failure after that point
	// leaves the PeerConnection mid-negotiation; a resume replaces it.
	videoMode := s.GetSIPVideoPacketizationMode()
	if AnalyzeOfferVideo(offerSDP).HasVideoMLine {
		if _, err := offeredH264CodecForPacketizationMode(offerSDP, videoMode); err != nil {
			return "", fmt.Errorf("select H264 packetization mode: %w", err)
		}
	}

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		return "", fmt.Errorf("set remote description: %w", err)
	}
	if err := PreferWebRTCH264PacketizationMode(pc, offerSDP, videoMode); err != nil {
		return "", fmt.Errorf("select H264 packetization mode: %w", err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("create answer: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("set local description: %w", err)
	}
	local := pc.LocalDescription()
	if local == nil || local.SDP == "" {
		return "", fmt.Errorf("local description missing after answer")
	}
	fmt.Printf("[%s] in-place renegotiation applied\n", s.ID)
	return local.SDP, nil
}
