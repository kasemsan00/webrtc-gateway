package session

import (
	"context"
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

// DeleteSession removes a session and cleans up resources.
// The map lock is released before closing heavy resources (PeerConnection, sockets)
// to avoid blocking other session operations.
func (m *Manager) DeleteSession(id string) {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.sessions, id)
	m.mu.Unlock()

	// Signal all session goroutines to stop (calls cancel() internally)
	session.SetState(StateEnded)

	// Close media transports outside map lock (reuses existing safe close pattern)
	session.CloseMediaTransports()

	// Close peer connection outside map lock (can block during ICE cleanup)
	if session.PeerConnection != nil {
		session.PeerConnection.Close()
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

const staleSessionThreshold = 60 * time.Second

// StartCleanup starts a background goroutine that periodically removes
// sessions stuck in StateEnded from the session map (prevents memory leak).
func (m *Manager) StartCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.cleanupEndedSessions()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (m *Manager) cleanupEndedSessions() {
	now := time.Now()

	m.mu.Lock()
	var toCleanup []*Session
	var toDeleteIDs []string
	for id, sess := range m.sessions {
		if sess.GetState() == StateEnded {
			sess.mu.RLock()
			updatedAt := sess.UpdatedAt
			sess.mu.RUnlock()
			if now.Sub(updatedAt) > staleSessionThreshold {
				toDeleteIDs = append(toDeleteIDs, id)
				toCleanup = append(toCleanup, sess)
			}
		}
	}
	for _, id := range toDeleteIDs {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	// Safety net: ensure resources are released outside lock
	for _, sess := range toCleanup {
		sess.CloseMediaTransports()
		if sess.PeerConnection != nil {
			sess.PeerConnection.Close()
		}
	}

	if len(toDeleteIDs) > 0 {
		fmt.Printf("[SessionManager] Cleaned up %d stale ended sessions\n", len(toDeleteIDs))
	}
}
