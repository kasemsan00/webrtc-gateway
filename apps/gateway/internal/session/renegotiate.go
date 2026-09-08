package session

import (
	"fmt"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

const RENEGOTIATE_ICE_GATHER_TIMEOUT = 3 * time.Second
const renegotiateVideoOnTrackWatchdog = 3 * time.Second

type renegotiateVideoOfferDiagnostics = OfferVideoDiagnostics

func analyzeRenegotiateVideoOffer(sdp string) OfferVideoDiagnostics {
	return AnalyzeOfferVideo(sdp)
}

func waitForGatheringComplete(gatherComplete <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-gatherComplete:
		return true
	case <-time.After(timeout):
		return false
	}
}

func waitForRenegotiateIceGatheringComplete(pc *webrtc.PeerConnection, timeout time.Duration) bool {
	if pc == nil {
		return false
	}
	gatherDone := webrtc.GatheringCompletePromise(pc)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-gatherDone:
			return true
		case <-ticker.C:
			// Early-exit only when candidate set is reasonably usable for cross-network handover.
			// A single host candidate can be a false-ready signal on Wi-Fi -> Cellular transitions.
			if ld := pc.LocalDescription(); ld != nil && hasUsableResumeCandidatesInSDP(ld.SDP) {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

func hasUsableResumeCandidatesInSDP(sdp string) bool {
	if sdp == "" {
		return false
	}
	candidateCount := strings.Count(sdp, "\na=candidate:")
	if strings.HasPrefix(sdp, "a=candidate:") {
		candidateCount++
	}
	if candidateCount >= 2 {
		return true
	}
	return strings.Contains(sdp, " typ srflx ") || strings.Contains(sdp, " typ relay ")
}

// RenegotiatePeerConnection recreates the PeerConnection with a new SDP offer
// Used for call resumption after network changes (Part 5)
// This preserves the SIP call and RTP connections while creating a fresh WebRTC connection
func (s *Session) RenegotiatePeerConnection(newOfferSDP string, turnConfig config.TURNConfig, debugTURN bool) error {
	// Best-effort: cache H.264 SPS/PPS from the new offer SDP (if present).
	// Do this outside the session lock to avoid holding locks during parsing/base64 decode.
	offerSPS, offerPPS, offerOK := ExtractH264SpropParameterSets(newOfferSDP)

	fmt.Printf("[%s] 🔄 Renegotiating PeerConnection...\n", s.ID)
	videoDiag := analyzeRenegotiateVideoOffer(newOfferSDP)
	fmt.Printf("[%s] 🔍 Renegotiate offer video diagnostics: hasVideoMLine=%v videoPort=%d videoDirection=%s expectVideoUplink=%v\n",
		s.ID,
		videoDiag.HasVideoMLine,
		videoDiag.VideoPort,
		videoDiag.VideoDirection,
		videoDiag.ExpectVideoUplink,
	)

	if offerOK {
		s.mu.Lock()
		s.CachedSPS = make([]byte, len(offerSPS))
		s.CachedPPS = make([]byte, len(offerPPS))
		copy(s.CachedSPS, offerSPS)
		copy(s.CachedPPS, offerPPS)
		s.mu.Unlock()
		fmt.Printf("[%s] 💾 Cached SPS/PPS from SDP (renegotiate-offer) (SPS=%d bytes, PPS=%d bytes)\n", s.ID, len(offerSPS), len(offerPPS))
	}

	newPC, err := s.createReplacementPeerConnection(turnConfig, debugTURN, videoDiag)
	if err != nil {
		return err
	}

	// 9. Set remote description (the new offer from client)
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  newOfferSDP,
	}
	if err := newPC.SetRemoteDescription(offer); err != nil {
		newPC.Close()
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	videoPacketizationMode := s.GetSIPVideoPacketizationMode()
	if err := PreferWebRTCH264PacketizationMode(newPC, newOfferSDP, videoPacketizationMode); err != nil {
		newPC.Close()
		return fmt.Errorf("failed to select H264 packetization mode: %w", err)
	}
	fmt.Printf("[%s] 🎬 Renegotiated WebRTC H264 answer restricted to packetization-mode=%d (SIP leg)\n", s.ID, videoPacketizationMode)

	// 10. Create answer
	answer, err := newPC.CreateAnswer(nil)
	if err != nil {
		newPC.Close()
		return fmt.Errorf("failed to create answer: %w", err)
	}

	// 11. Set local description
	if err := newPC.SetLocalDescription(answer); err != nil {
		newPC.Close()
		return fmt.Errorf("failed to set local description: %w", err)
	}

	// 11.5. Wait for ICE gathering to complete (CRITICAL for renegotiation!)
	// If gathering takes too long, continue with currently available candidates.
	// This avoids blocking resume for slow network transitions.
	if waitForRenegotiateIceGatheringComplete(newPC, RENEGOTIATE_ICE_GATHER_TIMEOUT) {
		fmt.Printf("[%s] 🧊 ICE gathering complete for renegotiated connection\n", s.ID)
	} else {
		fmt.Printf("[%s] ⚠️ ICE gathering timeout after %s during renegotiation - proceeding with partial candidates\n", s.ID, RENEGOTIATE_ICE_GATHER_TIMEOUT)
	}

	// 12. Store new PeerConnection under lock
	s.mu.Lock()
	s.PeerConnection = newPC
	s.UpdatedAt = time.Now()
	s.mu.Unlock()

	fmt.Printf("[%s] ✅ PeerConnection renegotiated successfully\n", s.ID)
	return nil
}
