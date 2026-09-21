package session

import (
	"fmt"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

// PrepareSwitchPeerConnection creates a replacement PeerConnection without
// creating a gateway offer and without closing the live PC. @switch uses the
// resume contract (client offer / gateway answer) with make-before-break:
// SIP audio keeps flowing on the old PC until the new ICE connects.
func (s *Session) PrepareSwitchPeerConnection(turnConfig config.TURNConfig, debugTURN bool) error {
	newPC, err := s.createReplacementPeerConnection(turnConfig, debugTURN, renegotiateVideoOfferDiagnostics{}, true)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.PeerConnection = newPC
	s.switchReplacementPCReady = true
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
	fmt.Printf("[%s] switch_renegotiate_pc_replaced make_before_break=true\n", s.ID)
	return nil
}

func (s *Session) waitForSwitchReplacementPeerConnectionIfNeeded(timeout time.Duration) error {
	s.mu.RLock()
	needed := s.SwitchVideoRenegotiateHold && !s.switchReplacementPCReady
	s.mu.RUnlock()
	if !needed {
		return nil
	}
	return s.WaitForSwitchReplacementPeerConnection(timeout)
}

// WaitForSwitchReplacementPeerConnection blocks until PrepareSwitchPeerConnection
// has installed the replacement PC, or until the claim is aborted / times out.
func (s *Session) WaitForSwitchReplacementPeerConnection(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.RLock()
		ready := s.switchReplacementPCReady
		aborted := !ready && !s.SwitchVideoRenegotiateHold && s.PendingMidCallRenegotiation == nil
		s.mu.RUnlock()
		if ready {
			return nil
		}
		if aborted {
			return fmt.Errorf("switch peer connection aborted")
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("switch peer connection not ready")
		}
		<-ticker.C
	}
}

// AnswerClientOffer applies a client offer (resume/@switch) onto the current
// PeerConnection and returns the gateway answer SDP.
func (s *Session) AnswerClientOffer(offerSDP string) (string, error) {
	if offerSDP == "" {
		return "", fmt.Errorf("offer sdp missing")
	}
	if err := s.waitForSwitchReplacementPeerConnectionIfNeeded(switchReplacementPCWait); err != nil {
		return "", err
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
	iceWaitStarted := time.Now()
	if waitForSwitchIceGatheringComplete(pc, SWITCH_ICE_GATHER_TIMEOUT) {
		fmt.Printf("[%s] switch_renegotiate ice_gathering=ready wait_ms=%d budget_ms=%d\n",
			s.ID, time.Since(iceWaitStarted).Milliseconds(), SWITCH_ICE_GATHER_TIMEOUT.Milliseconds())
	} else {
		fmt.Printf("[%s] switch_renegotiate ice_gathering=partial wait_ms=%d budget_ms=%d\n",
			s.ID, time.Since(iceWaitStarted).Milliseconds(), SWITCH_ICE_GATHER_TIMEOUT.Milliseconds())
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
