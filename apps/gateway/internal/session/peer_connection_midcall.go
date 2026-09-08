package session

import (
	"fmt"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

// PrepareSwitchPeerConnection replaces the live WebRTC PeerConnection without
// creating a gateway offer. Android cannot reliably answer a mid-call remote
// offer on a fresh PC; @switch therefore uses the resume contract: client
// creates the offer, gateway answers.
func (s *Session) PrepareSwitchPeerConnection(turnConfig config.TURNConfig, debugTURN bool) error {
	newPC, err := s.createReplacementPeerConnection(turnConfig, debugTURN, renegotiateVideoOfferDiagnostics{})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.PeerConnection = newPC
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
	fmt.Printf("[%s] switch_renegotiate_pc_replaced\n", s.ID)
	return nil
}

// AnswerClientOffer applies a client offer (resume/@switch) onto the current
// PeerConnection and returns the gateway answer SDP.
func (s *Session) AnswerClientOffer(offerSDP string) (string, error) {
	if offerSDP == "" {
		return "", fmt.Errorf("offer sdp missing")
	}

	s.mu.RLock()
	pc := s.PeerConnection
	s.mu.RUnlock()
	if pc == nil {
		return "", fmt.Errorf("peer connection not available")
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return "", fmt.Errorf("set remote description: %w", err)
	}
	queued := s.flushPendingRemoteICE(pc)

	videoPacketizationMode := s.GetSIPVideoPacketizationMode()
	if err := PreferWebRTCH264PacketizationMode(pc, offerSDP, videoPacketizationMode); err != nil {
		return "", fmt.Errorf("select H264 packetization mode: %w", err)
	}
	fmt.Printf("[%s] 🎬 Switch WebRTC H264 answer restricted to packetization-mode=%d queued_ice=%d\n",
		s.ID, videoPacketizationMode, queued)

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("create answer: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("set local description: %w", err)
	}
	if waitForRenegotiateIceGatheringComplete(pc, RENEGOTIATE_ICE_GATHER_TIMEOUT) {
		fmt.Printf("[%s] switch_renegotiate ice_gathering=complete\n", s.ID)
	} else {
		fmt.Printf("[%s] switch_renegotiate ice_gathering=timeout after=%s\n", s.ID, RENEGOTIATE_ICE_GATHER_TIMEOUT)
	}
	ld := pc.LocalDescription()
	if ld == nil || ld.SDP == "" {
		return "", fmt.Errorf("local description missing after answer")
	}
	fmt.Printf("[%s] switch_renegotiate_client_offer_applied\n", s.ID)
	return ld.SDP, nil
}

// ApplyPeerConnectionAnswer applies a client answer SDP to the active
// PeerConnection during mid-call renegotiation that the gateway offered.
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
	queued := s.flushPendingRemoteICE(pc)
	fmt.Printf("[%s] switch_renegotiate_answer_applied queued_ice=%d\n", s.ID, queued)
	return nil
}
