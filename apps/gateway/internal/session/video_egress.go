package session

import (
	"errors"
	"fmt"

	"github.com/pion/webrtc/v4"
)

var errVideoEgressStopped = errors.New("video egress stopped")

// WriteVideoToWebRTC writes one RTP packet onto the WebRTC video track.
// Hangup/BYE must call StopVideoEgress before PeerConnection.Close(); writing
// to TrackLocalStaticRTP concurrently with Close can deadlock pion and block
// the next outbound call.
func (s *Session) WriteVideoToWebRTC(data []byte) (int, error) {
	s.videoEgressMu.Lock()
	defer s.videoEgressMu.Unlock()
	if s.videoEgressClosed || s.VideoTrack == nil {
		return 0, errVideoEgressStopped
	}
	return s.VideoTrack.Write(data)
}

// StopVideoEgress waits for any in-flight WebRTC video write, then rejects
// further writes. Call this immediately before PeerConnection.Close().
func (s *Session) StopVideoEgress() {
	s.videoEgressMu.Lock()
	s.videoEgressClosed = true
	s.videoEgressMu.Unlock()
}

func (s *Session) videoEgressIsStopped() bool {
	s.videoEgressMu.Lock()
	defer s.videoEgressMu.Unlock()
	return s.videoEgressClosed
}

// DetachPeerConnection stops video egress and takes the PeerConnection so the
// caller can close it without racing later cleanup.
func (s *Session) DetachPeerConnection() *webrtc.PeerConnection {
	s.StopVideoEgress()
	s.mu.Lock()
	pc := s.PeerConnection
	s.PeerConnection = nil
	s.mu.Unlock()
	return pc
}

// ClosePeerConnectionAsync closes a detached PeerConnection off the signaling
// goroutine. pion Close can block for seconds if a track write raced it.
func ClosePeerConnectionAsync(pc *webrtc.PeerConnection, sessionID string) {
	if pc == nil {
		return
	}
	go func() {
		if err := pc.Close(); err != nil {
			fmt.Printf("[%s] PeerConnection.Close() error: %v\n", sessionID, err)
			return
		}
		fmt.Printf("[%s] ✅ PeerConnection closed\n", sessionID)
	}()
}
