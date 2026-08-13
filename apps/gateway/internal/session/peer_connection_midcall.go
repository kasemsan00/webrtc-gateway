package session

import (
	"fmt"

	"github.com/pion/webrtc/v4"
)

// CreatePeerConnectionOffer builds an in-place WebRTC offer on the active
// PeerConnection for @switch. Existing clients answer this offer; the gateway
// restores H.264 preferences first so a 1.3.7 remote-PT lock does not carry
// into CreateOffer.
func (s *Session) CreatePeerConnectionOffer() (string, error) {
	s.mu.RLock()
	pc := s.PeerConnection
	s.mu.RUnlock()
	if pc == nil {
		return "", fmt.Errorf("peer connection not available")
	}

	if err := restoreSwitchOfferH264Preferences(pc, s.GetSIPVideoPacketizationMode()); err != nil {
		fmt.Printf("[%s] switch_renegotiate restore_h264 warning=%v\n", s.ID, err)
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return "", fmt.Errorf("create offer: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return "", fmt.Errorf("set local description: %w", err)
	}

	if waitForRenegotiateIceGatheringComplete(pc, RENEGOTIATE_ICE_GATHER_TIMEOUT) {
		fmt.Printf("[%s] switch_renegotiate ice_gathering=complete\n", s.ID)
	} else {
		fmt.Printf("[%s] switch_renegotiate ice_gathering=timeout after=%s\n", s.ID, RENEGOTIATE_ICE_GATHER_TIMEOUT)
	}

	ld := pc.LocalDescription()
	if ld == nil || ld.SDP == "" {
		return "", fmt.Errorf("local description missing after offer")
	}
	return ld.SDP, nil
}

// ApplyPeerConnectionAnswer applies a client answer SDP to the active
// PeerConnection during in-place mid-call renegotiation.
func (s *Session) ApplyPeerConnectionAnswer(answerSDP string) error {
	if answerSDP == "" {
		return fmt.Errorf("answer sdp missing")
	}

	s.mu.RLock()
	pc := s.PeerConnection
	s.mu.RUnlock()
	if pc == nil {
		return fmt.Errorf("peer connection not available")
	}

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}
	if err := pc.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("set remote description: %w", err)
	}
	return nil
}
