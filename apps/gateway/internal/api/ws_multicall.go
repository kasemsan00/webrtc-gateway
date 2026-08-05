package api

import (
	"strings"

	"k2-gateway/internal/session"
)

// bindClientSession records per-call ownership while retaining sessionID for
// older clients that assume a single current session.
func (s *Server) bindClientSession(client *WSClient, sessionID string) {
	if client == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if client.ownedSessionIDs == nil {
		client.ownedSessionIDs = make(map[string]struct{})
	}
	client.ownedSessionIDs[sessionID] = struct{}{}
	delete(client.pendingIncoming, sessionID)
	client.sessionID = sessionID
	s.wsClients[sessionID] = client
}

func (s *Server) unbindClientSession(client *WSClient, sessionID string) {
	if client == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(client.ownedSessionIDs, sessionID)
	delete(client.pendingIncoming, sessionID)
	if s.wsClients[sessionID] == client {
		delete(s.wsClients, sessionID)
	}
	if client.sessionID == sessionID {
		client.sessionID = ""
		for owned := range client.ownedSessionIDs {
			client.sessionID = owned
			break
		}
	}
}

func (s *Server) markPendingIncoming(client *WSClient, sessionID string) {
	if client == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if client.pendingIncoming == nil {
		client.pendingIncoming = make(map[string]struct{})
	}
	client.pendingIncoming[sessionID] = struct{}{}
}

func (s *Server) clearPendingIncoming(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for client := range s.wsConnections {
		delete(client.pendingIncoming, sessionID)
	}
}

func (s *Server) clientOwnsSession(client *WSClient, sessionID string) bool {
	if client == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := client.ownedSessionIDs[sessionID]; ok {
		return true
	}
	// Compatibility for clients/tests created before ownedSessionIDs existed.
	return client.sessionID == sessionID && s.wsClients[sessionID] == client
}

func (s *Server) clientHasPendingIncoming(client *WSClient, sessionID string) bool {
	if client == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := client.pendingIncoming[sessionID]
	return ok
}

func (s *Server) ownedClientSessionIDs(client *WSClient) []string {
	if client == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(client.ownedSessionIDs)+1)
	for id := range client.ownedSessionIDs {
		ids = append(ids, id)
	}
	if len(ids) == 0 && client.sessionID != "" {
		ids = append(ids, client.sessionID)
	}
	return ids
}

func (s *Server) activeOwnedSessionCount(client *WSClient) int {
	count := 0
	for _, id := range s.ownedClientSessionIDs(client) {
		if sess, ok := s.sessionMgr.GetSession(id); ok && sess != nil && sess.GetState() != session.StateEnded {
			count++
		}
	}
	return count
}

func (s *Server) agentClientCanAccessSession(client *WSClient, msg WSMessage) (bool, string) {
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return false, "Session ID required"
	}
	if s.clientOwnsSession(client, sessionID) {
		return true, ""
	}
	if (msg.Type == "accept" || msg.Type == "reject" || msg.Type == "offer") && s.clientHasPendingIncoming(client, sessionID) {
		return true, ""
	}
	return false, "Agent WebSocket can only access its own or presented incoming sessions"
}
