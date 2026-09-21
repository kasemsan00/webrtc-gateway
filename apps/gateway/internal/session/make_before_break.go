package session

import (
	"fmt"
	"time"

	"github.com/pion/webrtc/v4"
)

// armMakeBeforeBreakLocked parks the live PeerConnection so SIP audio can keep
// flowing on it while signaling and new video go to the replacement PC.
func (s *Session) armMakeBeforeBreakLocked(oldPC *webrtc.PeerConnection, oldAudio, oldVideo *webrtc.TrackLocalStaticRTP) {
	if oldPC == nil {
		return
	}
	s.legacyPeerConnection = oldPC
	s.legacyAudioTrack = oldAudio
	s.legacyVideoTrack = oldVideo
}

// commitMakeBeforeBreak closes the parked PeerConnection after the replacement
// ICE-connects. Signaling already points at newPC.
func (s *Session) commitMakeBeforeBreak(newPC *webrtc.PeerConnection) {
	s.mu.Lock()
	if s.legacyPeerConnection == nil {
		s.mu.Unlock()
		return
	}
	if newPC != nil && s.PeerConnection != newPC {
		s.mu.Unlock()
		return
	}
	legacy := s.legacyPeerConnection
	s.legacyPeerConnection = nil
	s.legacyVideoTrack = nil
	s.mu.Unlock()
	if legacy == nil || legacy == newPC {
		return
	}
	fmt.Printf("[%s] switch_renegotiate_legacy_pc_closing_after_handoff\n", s.ID)
	go func() {
		time.Sleep(300 * time.Millisecond)
		s.mu.Lock()
		s.legacyAudioTrack = nil
		s.mu.Unlock()
		_ = legacy.Close()
		fmt.Printf("[%s] switch_renegotiate_legacy_pc_closed\n", s.ID)
	}()
}

// AbortMakeBeforeBreak restores the parked PeerConnection when the replacement
// never becomes usable. Returns false when no handoff was in progress.
func (s *Session) AbortMakeBeforeBreak() bool {
	s.mu.Lock()
	legacy := s.legacyPeerConnection
	if legacy == nil {
		s.mu.Unlock()
		return false
	}
	newPC := s.PeerConnection
	s.PeerConnection = legacy
	if s.legacyAudioTrack != nil {
		s.AudioTrack = s.legacyAudioTrack
	}
	if s.legacyVideoTrack != nil {
		s.VideoTrack = s.legacyVideoTrack
	}
	s.legacyPeerConnection = nil
	s.legacyAudioTrack = nil
	s.legacyVideoTrack = nil
	s.pendingRemoteICE = nil
	s.switchReplacementPCReady = false
	s.mu.Unlock()
	if newPC != nil && newPC != legacy {
		go func() {
			_ = newPC.Close()
		}()
	}
	fmt.Printf("[%s] switch_renegotiate_make_before_break_aborted\n", s.ID)
	return true
}

func (s *Session) WriteAudioToWebRTC(data []byte) error {
	s.mu.RLock()
	primary := s.AudioTrack
	legacy := s.legacyAudioTrack
	s.mu.RUnlock()
	if primary == nil {
		return fmt.Errorf("audio track is nil")
	}
	if _, err := primary.Write(data); err != nil {
		return err
	}
	if legacy != nil && legacy != primary {
		_, _ = legacy.Write(data)
	}
	return nil
}
