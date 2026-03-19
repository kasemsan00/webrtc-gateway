package session

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"k2-gateway/internal/config"
	"k2-gateway/internal/observability"
)

// Manager manages multiple concurrent sessions
type Manager struct {
	sessions         map[string]*Session
	callIDToSession  map[string]string
	sessionToCallID  map[string]string
	config           *config.Config
	metrics          *observability.Registry
	mu               sync.RWMutex
}

// NewManager creates a new session manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		sessions:        make(map[string]*Session),
		callIDToSession: make(map[string]string),
		sessionToCallID: make(map[string]string),
		config:          cfg,
	}
}

func (m *Manager) SetMetrics(metrics *observability.Registry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = metrics
}

func (m *Manager) UpdateSessionCallID(sessionID, callID string) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if prevCallID, ok := m.sessionToCallID[sessionID]; ok && prevCallID != "" && prevCallID != callID {
		delete(m.callIDToSession, prevCallID)
	}
	if callID == "" {
		delete(m.sessionToCallID, sessionID)
		return
	}
	if prevSessionID, ok := m.callIDToSession[callID]; ok && prevSessionID != "" && prevSessionID != sessionID {
		delete(m.sessionToCallID, prevSessionID)
	}
	m.callIDToSession[callID] = sessionID
	m.sessionToCallID[sessionID] = callID
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
	if sessionID, ok := m.callIDToSession[callID]; ok {
		if session, found := m.sessions[sessionID]; found {
			_, _, _, sipCallID := session.GetCallInfo()
			if sipCallID == callID {
				m.mu.RUnlock()
				return session, true
			}
		}
	}
	m.mu.RUnlock()
	m.mu.Lock()
	delete(m.callIDToSession, callID)
	m.mu.Unlock()
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
	session, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.sessions, id)
	if callID, ok := m.sessionToCallID[id]; ok {
		if currentOwner, exists := m.callIDToSession[callID]; exists && currentOwner == id {
			delete(m.callIDToSession, callID)
		}
		delete(m.sessionToCallID, id)
	}
	m.mu.Unlock()

	// Close peer connection
	if session.PeerConnection != nil {
		if err := session.PeerConnection.Close(); err != nil && m.metrics != nil {
			m.metrics.Inc(observability.MetricSessionCleanupErrorTotal, 1)
		}
	}

	// Close RTP connections (audio and video)
	if session.RTPConn != nil {
		if err := session.RTPConn.Close(); err != nil && m.metrics != nil {
			m.metrics.Inc(observability.MetricSessionCleanupErrorTotal, 1)
		}
	}
	if session.VideoRTPConn != nil {
		if err := session.VideoRTPConn.Close(); err != nil && m.metrics != nil {
			m.metrics.Inc(observability.MetricSessionCleanupErrorTotal, 1)
		}
	}

	// Close RTCP connections (audio and video)
	if session.AudioRTCPConn != nil {
		if err := session.AudioRTCPConn.Close(); err != nil && m.metrics != nil {
			m.metrics.Inc(observability.MetricSessionCleanupErrorTotal, 1)
		}
	}
	if session.VideoRTCPConn != nil {
		if err := session.VideoRTCPConn.Close(); err != nil && m.metrics != nil {
			m.metrics.Inc(observability.MetricSessionCleanupErrorTotal, 1)
		}
	}

	// Update state
	session.mu.Lock()
	session.State = StateEnded
	session.UpdatedAt = time.Now()
	session.mu.Unlock()

	if err := session.stopWorkers(2 * time.Second); err != nil && m.metrics != nil {
		m.metrics.Inc(observability.MetricSessionCleanupErrorTotal, 1)
	}
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
