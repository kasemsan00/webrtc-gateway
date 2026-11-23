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
	if s.videoEgressClosed {
		return 0, errVideoEgressStopped
	}
	// VideoTrack is swapped under s.mu (resume, @switch, make-before-break).
	// Lock order is videoEgressMu, then s.mu.
	track := s.currentVideoTrack()
	if track == nil {
		return 0, errVideoEgressStopped
	}
	return track.Write(data)
}

func (s *Session) currentVideoTrack() *webrtc.TrackLocalStaticRTP {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VideoTrack
}

// HasVideoTrack reports whether a WebRTC video track is attached.
func (s *Session) HasVideoTrack() bool {
	return s.currentVideoTrack() != nil
}

// StopVideoEgress waits for any in-flight WebRTC video write, then rejects
// further writes. Call this immediately before PeerConnection.Close().
func (s *Session) StopVideoEgress() {
	s.videoEgressMu.Lock()
	s.videoEgressClosed = true
	s.videoEgressMu.Unlock()
}

// DetachPeerConnection stops video egress and takes the PeerConnection so the
// caller can close it without racing later cleanup.
func (s *Session) DetachPeerConnection() *webrtc.PeerConnection {
	if s == nil {
		return nil
	}
	s.StopVideoEgress()
	// No audio write may be in flight on these tracks when the caller
	// closes the PeerConnection (same pion Write/Close overlap as video).
	s.audioEgressMu.Lock()
	s.mu.Lock()
	pc := s.PeerConnection
	legacy := s.legacyPeerConnection
	s.PeerConnection = nil
	s.AudioTrack = nil
	s.VideoTrack = nil
	s.legacyPeerConnection = nil
	s.legacyAudioTrack = nil
	s.legacyVideoTrack = nil
	s.closed = true
	s.mu.Unlock()
	s.audioEgressMu.Unlock()
	if legacy != nil && legacy != pc {
		ClosePeerConnectionAsync(legacy, s.ID)
	}
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
