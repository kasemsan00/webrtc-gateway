package session

import (
	"fmt"
	"strings"

	"github.com/pion/webrtc/v4"
)

// AuxVideoMidFromOffer returns the a=mid of the WebRTC media section that
// carries the auxiliary video in a client offer: the second m=video. The
// first m=video is always the camera. It reports false unless the offer has
// exactly two video sections that are not rejected (port 0) and the second
// one names a mid.
func AuxVideoMidFromOffer(sdp string) (string, bool) {
	videoSections := 0
	mid := ""
	inAux := false
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "m=") {
			inAux = false
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[0] != "m=video" || fields[1] == "0" {
				continue
			}
			videoSections++
			inAux = videoSections == 2
			continue
		}
		if inAux && mid == "" && strings.HasPrefix(line, "a=mid:") {
			mid = strings.TrimSpace(strings.TrimPrefix(line, "a=mid:"))
		}
	}
	if videoSections != 2 || mid == "" {
		return "", false
	}
	return mid, true
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
