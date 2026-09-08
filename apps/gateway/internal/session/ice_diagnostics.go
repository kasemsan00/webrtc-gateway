package session

import (
	"fmt"
	"time"

	"github.com/pion/webrtc/v4"
)

const maxPendingRemoteICE = 64

// iceCallbackIsForCurrentPeerConnection reports whether an ICE state callback
// should mutate session state. Closing a replaced PeerConnection emits ICE
// closed; that must not enter reconnecting during @switch/resume replace.
func iceCallbackIsForCurrentPeerConnection(current, eventPC *webrtc.PeerConnection) bool {
	return current != nil && current == eventPC
}

// AddRemoteICECandidate applies a trickled client candidate, or queues it
// until SetRemoteDescription has run. Android recreates its PC and can trickle
// ICE before renegotiate_answer arrives.
func (s *Session) AddRemoteICECandidate(candidate webrtc.ICECandidateInit) (queued bool, err error) {
	s.mu.RLock()
	pc := s.PeerConnection
	s.mu.RUnlock()
	if pc == nil {
		return false, fmt.Errorf("peer connection not available")
	}
	if pc.RemoteDescription() != nil {
		return false, pc.AddICECandidate(candidate)
	}

	s.mu.Lock()
	if s.PeerConnection != pc {
		s.mu.Unlock()
		return false, fmt.Errorf("peer connection replaced")
	}
	if pc.RemoteDescription() != nil {
		s.mu.Unlock()
		return false, pc.AddICECandidate(candidate)
	}
	if len(s.pendingRemoteICE) >= maxPendingRemoteICE {
		s.mu.Unlock()
		return true, fmt.Errorf("pending ICE candidate queue full")
	}
	s.pendingRemoteICE = append(s.pendingRemoteICE, candidate)
	queuedCount := len(s.pendingRemoteICE)
	s.mu.Unlock()
	fmt.Printf("[%s] 🧊 Remote ICE candidate queued until remote description (%d queued)\n", s.ID, queuedCount)
	return true, nil
}

func (s *Session) flushPendingRemoteICE(pc *webrtc.PeerConnection) int {
	s.mu.Lock()
	queued := s.pendingRemoteICE
	s.pendingRemoteICE = nil
	s.mu.Unlock()
	if pc == nil || len(queued) == 0 {
		return 0
	}
	applied := 0
	for _, candidate := range queued {
		if err := pc.AddICECandidate(candidate); err != nil {
			fmt.Printf("[%s] ⚠️ queued ICE candidate rejected: %v\n", s.ID, err)
			continue
		}
		applied++
	}
	if applied > 0 {
		fmt.Printf("[%s] 🧊 Applied %d queued remote ICE candidates\n", s.ID, applied)
	}
	return len(queued)
}

// RecordLocalICECandidate records timing without exposing candidate addresses.
func (s *Session) RecordLocalICECandidate() (int, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.FirstLocalICECandidateAt.IsZero() {
		s.FirstLocalICECandidateAt = now
	}
	s.LocalICECandidateCount++
	return s.LocalICECandidateCount, now.Sub(s.CreatedAt)
}

// RecordRemoteICECandidate records the arrival order of trickled client candidates.
func (s *Session) RecordRemoteICECandidate() (int, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.FirstRemoteICECandidateAt.IsZero() {
		s.FirstRemoteICECandidateAt = now
	}
	s.RemoteICECandidateCount++
	return s.RemoteICECandidateCount, now.Sub(s.CreatedAt)
}

// ICECandidateCounts returns a race-safe diagnostic snapshot.
func (s *Session) ICECandidateCounts() (local, remote int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LocalICECandidateCount, s.RemoteICECandidateCount
}

func (s *Session) SetTerminalReason(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TerminalReason = reason
	s.UpdatedAt = time.Now()
}

func (s *Session) GetTerminalReason() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TerminalReason
}

func (s *Session) MarkOutboundInviteStarted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.OutboundInviteStarted = true
	s.UpdatedAt = time.Now()
}

func (s *Session) HasOutboundInviteStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.OutboundInviteStarted
}

func logICECandidatePairDiagnostics(sessionID string, pc *webrtc.PeerConnection, trigger string) {
	stats := pc.GetStats()
	var selectedPair *webrtc.ICECandidatePairStats
	candidates := make(map[string]webrtc.ICECandidateStats)
	for _, stat := range stats {
		switch typed := stat.(type) {
		case webrtc.ICECandidatePairStats:
			if typed.Nominated {
				copy := typed
				selectedPair = &copy
			}
		case webrtc.ICECandidateStats:
			candidates[typed.ID] = typed
		}
	}
	if selectedPair == nil {
		fmt.Printf("[%s] 🧊 ICE diagnostics trigger=%s selectedPair=none gathering=%s\n",
			sessionID, trigger, pc.ICEGatheringState().String())
		return
	}
	local, localOK := candidates[selectedPair.LocalCandidateID]
	remote, remoteOK := candidates[selectedPair.RemoteCandidateID]
	if !localOK || !remoteOK {
		fmt.Printf("[%s] 🧊 ICE diagnostics trigger=%s pair=%s nominated=%t state=%s candidateStats=missing gathering=%s\n",
			sessionID, trigger, selectedPair.ID, selectedPair.Nominated, selectedPair.State, pc.ICEGatheringState().String())
		return
	}
	usingRelay := local.CandidateType == webrtc.ICECandidateTypeRelay || remote.CandidateType == webrtc.ICECandidateTypeRelay
	fmt.Printf("[%s] 🧊 ICE diagnostics trigger=%s pair=%s state=%s relay=%t gathering=%s local=%s/%s/%s:%d remote=%s/%s/%s:%d\n",
		sessionID, trigger, selectedPair.ID, selectedPair.State, usingRelay, pc.ICEGatheringState().String(),
		local.CandidateType, local.Protocol, local.IP, local.Port,
		remote.CandidateType, remote.Protocol, remote.IP, remote.Port)
}
