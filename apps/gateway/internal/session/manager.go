package session

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"k2-gateway/internal/config"
)

// Manager manages multiple concurrent sessions
type Manager struct {
	sessions map[string]*Session
	config   *config.Config
	mu       sync.RWMutex
}

// NewManager creates a new session manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		config:   cfg,
	}
}

// CreateSession creates a new session with a WebRTC peer connection
func (m *Manager) CreateSession(turnConfig config.TURNConfig) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate session ID (12 characters, base62)
	id := generateShortID()

	session, err := NewSession(id, m.config, turnConfig)
	if err != nil {
		return nil, err
	}

	m.sessions[id] = session
	fmt.Printf("[%s] Created new session\n", id)

	return session, nil
}

// CreateSessionForIncoming creates a new session for incoming SIP calls
// This is an alias for CreateSession used by the SIP server
func (m *Manager) CreateSessionForIncoming(turnConfig config.TURNConfig) (*Session, error) {
	return m.CreateSession(turnConfig)
}

// GetSession retrieves a session by ID
func (m *Manager) GetSession(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[id]
	return session, ok
}

// GetSessionBySIPCallID retrieves a session by SIP Call-ID
func (m *Manager) GetSessionBySIPCallID(callID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.sessions {
		_, _, _, sipCallID := session.GetCallInfo()

		if sipCallID == callID {
			return session, true
		}
	}
	return nil, false
}

// GetSessionByFromUsername retrieves active sessions by From username (agent SIP username)
// Used for @switch message handling to find the correct session
func (m *Manager) GetSessionByFromUsername(username string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.sessions {
		_, fromField, _, _ := session.GetCallInfo()
		state := session.GetState()

		// Skip ended sessions
		if state == StateEnded {
			continue
		}

		// Check if username matches (exact match or contained in From field)
		// From field may be full SIP URI like "sip:00025@domain" or just "00025"
		if fromField == username || strings.Contains(fromField, username+"@") || strings.HasSuffix(fromField, ":"+username) {
			return session, true
		}
	}
	return nil, false
}

// DeleteSession removes a session and cleans up resources
func (m *Manager) DeleteSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[id]
	if !ok {
		return
	}

	// Close peer connection
	if session.PeerConnection != nil {
		session.PeerConnection.Close()
	}

	// Close RTP connections (audio and video)
	if session.RTPConn != nil {
		session.RTPConn.Close()
	}
	if session.VideoRTPConn != nil {
		session.VideoRTPConn.Close()
	}

	// Close RTCP connections (audio and video)
	if session.AudioRTCPConn != nil {
		session.AudioRTCPConn.Close()
	}
	if session.VideoRTCPConn != nil {
		session.VideoRTCPConn.Close()
	}

	// Update state
	session.mu.Lock()
	session.State = StateEnded
	session.UpdatedAt = time.Now()
	session.mu.Unlock()

	delete(m.sessions, id)
	fmt.Printf("[%s] Deleted session\n", id)
}

// ListSessions returns all active sessions
func (m *Manager) ListSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}
