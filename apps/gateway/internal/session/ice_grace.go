package session

import (
	"fmt"
	"time"
)

// iceReconnectGracePeriod is how long a call stays in StateReconnecting after
// ICE disconnects before it is ended, giving the client time to resume.
const iceReconnectGracePeriod = 30 * time.Second

// enterICEReconnectGraceLocked moves an answered call to StateReconnecting
// and arms the grace timer for this disconnect episode. Caller must hold s.mu.
// It reports whether the state changed.
//
// Each disconnect gets its own episode. A timer from an earlier episode that
// already recovered must not end the call one second into a later grace period
// (disconnect t=0, reconnect t=25s, disconnect t=29s: the first timer fires at
// t=30s while the call is reconnecting again).
func (s *Session) enterICEReconnectGraceLocked(grace time.Duration, cause string) bool {
	if s.State == StateReconnecting {
		return false
	}
	s.State = StateReconnecting
	s.iceGraceEpisode++
	episode := s.iceGraceEpisode
	fmt.Printf("[%s] 📡 ICE %s - entering reconnection grace period (%s, episode=%d)\n", s.ID, cause, grace, episode)

	var done <-chan struct{}
	if s.ctx != nil {
		done = s.ctx.Done()
	}
	go func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-done:
			return
		}
		s.mu.Lock()
		_, after := s.expireICEReconnectGraceLocked(episode)
		s.mu.Unlock()
		runICEEndedHook(after)
	}()
	return true
}

// expireICEReconnectGraceLocked ends the call when the grace period of the
// given episode runs out and the call is still reconnecting in that episode.
// after must be run once s.mu is released. Caller must hold s.mu.
func (s *Session) expireICEReconnectGraceLocked(episode uint64) (ended bool, after func()) {
	if s.State != StateReconnecting || s.iceGraceEpisode != episode {
		return false, nil
	}
	fmt.Printf("[%s] ⏰ Grace period expired - ending session (episode=%d)\n", s.ID, episode)
	return true, s.endForICELocked("ice_grace_expired")
}

// ICEEndedHandler finishes a call that ended because WebRTC ICE gave up
// (failed, or the reconnect grace ran out): BYE toward the SIP side and the
// client's ended state. The session package cannot reach SIP itself.
type ICEEndedHandler func(sess *Session, reason string)

// SetICEEndedHandler installs the handler for this session.
func (s *Session) SetICEEndedHandler(fn ICEEndedHandler) {
	s.mu.Lock()
	s.iceEndedHandler = fn
	s.mu.Unlock()
}

// endForICELocked marks the call ended because ICE gave up and closes the SIP
// media sockets. Without the handler the SIP leg stayed up (no BYE) until
// Asterisk's RTP timeout and the client was never told. It returns the
// handler call to run after s.mu is released. Caller must hold s.mu.
func (s *Session) endForICELocked(reason string) func() {
	s.State = StateEnded
	if s.cancel != nil {
		s.cancel()
	}
	s.closeMediaConnsLocked()
	handler := s.iceEndedHandler
	if handler == nil {
		return nil
	}
	return func() { handler(s, reason) }
}

// runICEEndedHook runs the handler off the caller's goroutine: it does SIP
// I/O and closes the PeerConnection, which must not happen inside a pion ICE
// callback.
func runICEEndedHook(after func()) {
	if after != nil {
		go after()
	}
}

// closeMediaConnsLocked closes the SIP RTP/RTCP sockets. Caller must hold s.mu.
func (s *Session) closeMediaConnsLocked() {
	if s.RTPConn != nil {
		s.RTPConn.Close()
		s.RTPConn = nil
	}
	if s.VideoRTPConn != nil {
		s.VideoRTPConn.Close()
		s.VideoRTPConn = nil
	}
	if s.AudioRTCPConn != nil {
		s.AudioRTCPConn.Close()
		s.AudioRTCPConn = nil
	}
	if s.VideoRTCPConn != nil {
		s.VideoRTCPConn.Close()
		s.VideoRTCPConn = nil
	}
}
