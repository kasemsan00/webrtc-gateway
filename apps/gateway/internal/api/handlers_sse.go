package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"k2-gateway/internal/sip"
)

type TrunkStreamEvent struct {
	Type    string `json:"type"`
	TrunkID *int64 `json:"trunkId,omitempty"`
	At      string `json:"at"`
}

type SessionStreamEvent struct {
	Type      string  `json:"type"`
	SessionID *string `json:"sessionId,omitempty"`
	At        string  `json:"at"`
}

type WSClientStreamEvent struct {
	Type     string            `json:"type"`
	ClientID string            `json:"clientId"`
	At       string            `json:"at"`
	Client   *WSClientResponse `json:"client,omitempty"`
}

func (s *Server) notifyTrunkListChanged(eventType string, trunkID *int64) {
	payload, err := json.Marshal(TrunkStreamEvent{
		Type:    eventType,
		TrunkID: trunkID,
		At:      time.Now().Format(time.RFC3339Nano),
	})
	if err != nil {
		return
	}
	s.broadcastTrunkStream(payload)
}

func (s *Server) notifySessionListChanged(eventType string, sessionID *string) {
	payload, err := json.Marshal(SessionStreamEvent{
		Type:      eventType,
		SessionID: sessionID,
		At:        time.Now().Format(time.RFC3339Nano),
	})
	if err != nil {
		return
	}
	s.broadcastSessionStream(payload)
}

func (s *Server) notifyWSClientChanged(eventType string, client *WSClient) {
	if client == nil {
		return
	}
	s.mu.RLock()
	resp := s.buildWSClientResponse(client)
	s.mu.RUnlock()
	payload, err := json.Marshal(WSClientStreamEvent{
		Type:     eventType,
		ClientID: client.clientID,
		At:       time.Now().Format(time.RFC3339Nano),
		Client:   &resp,
	})
	if err != nil {
		return
	}
	s.broadcastWSClientStream(payload)
}

func (s *Server) buildWSClientResponse(client *WSClient) WSClientResponse {
	resp := WSClientResponse{
		ClientID:        client.clientID,
		ConnectedAt:     client.ConnectedAt.Format(time.RFC3339),
		TrunkResolved:   client.trunkResolved,
		ResolvedTrunkID: client.resolvedTrunkID,
		Availability:    client.availability,
		CallState:       client.callState,
		PublicOnly:      client.publicOnly,
		AgentOnly:       client.agentOnly,
	}
	if client.agentOnly {
		resp.PresenceMode = "ephemeral"
		if client.resolvedTrunkID > 0 {
			resp.AgentTrunkRefCount = s.agentTrunkRefCountLocked(client.resolvedTrunkID)
		}
	} else if client.publicOnly {
		resp.PresenceMode = "public"
	} else {
		resp.PresenceMode = "sticky"
	}
	if client.sessionID != "" {
		resp.SessionID = client.sessionID
	}
	if client.authClaims != nil {
		resp.AuthSubject = client.authClaims.Subject
	}
	if s.trunkManager != nil && client.resolvedTrunkID > 0 {
		if trunkRaw, ok := s.trunkManager.GetTrunkByID(client.resolvedTrunkID); ok {
			if trunk, ok := trunkRaw.(*sip.Trunk); ok && trunk.PublicID != "" {
				resp.ResolvedTrunkPublicID = trunk.PublicID
			}
		}
	}
	return resp
}

// agentTrunkRefCountLocked returns refcount; caller must hold s.mu (RLock or Lock)
// when used from buildWSClientResponse which already holds RLock via notify path.
// For notifyWSClientChanged it RLocks then calls build — so we need a version that
// does not take the lock again. Use unlocked helper when lock already held.
func (s *Server) agentTrunkRefCountLocked(trunkID int64) int {
	if s.agentTrunkBindings == nil {
		return 0
	}
	return len(s.agentTrunkBindings[trunkID])
}

func (s *Server) writeSSE(w http.ResponseWriter, eventName string, payload []byte) error {
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func (s *Server) handleTrunkStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	id, ch := s.subscribeTrunkStream()
	defer s.unsubscribeTrunkStream(id)

	connectedPayload, _ := json.Marshal(TrunkStreamEvent{
		Type: "connected",
		At:   time.Now().Format(time.RFC3339Nano),
	})
	if err := s.writeSSE(w, "connected", connectedPayload); err != nil {
		return
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			if err := s.writeSSE(w, "trunk", payload); err != nil {
				return
			}
		case <-heartbeat.C:
			heartbeatPayload, _ := json.Marshal(TrunkStreamEvent{
				Type: "heartbeat",
				At:   time.Now().Format(time.RFC3339Nano),
			})
			if err := s.writeSSE(w, "heartbeat", heartbeatPayload); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleSessionStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	id, ch := s.subscribeSessionStream()
	defer s.unsubscribeSessionStream(id)

	connectedPayload, _ := json.Marshal(SessionStreamEvent{
		Type: "connected",
		At:   time.Now().Format(time.RFC3339Nano),
	})
	if err := s.writeSSE(w, "connected", connectedPayload); err != nil {
		return
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			if err := s.writeSSE(w, "session", payload); err != nil {
				return
			}
		case <-heartbeat.C:
			heartbeatPayload, _ := json.Marshal(SessionStreamEvent{
				Type: "heartbeat",
				At:   time.Now().Format(time.RFC3339Nano),
			})
			if err := s.writeSSE(w, "heartbeat", heartbeatPayload); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleWSClientsStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	id, ch := s.subscribeWSClientStream()
	defer s.unsubscribeWSClientStream(id)

	connectedPayload, _ := json.Marshal(WSClientStreamEvent{
		Type: "connected",
		At:   time.Now().Format(time.RFC3339Nano),
	})
	if err := s.writeSSE(w, "connected", connectedPayload); err != nil {
		return
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			if err := s.writeSSE(w, "ws-client", payload); err != nil {
				return
			}
		case <-heartbeat.C:
			heartbeatPayload, _ := json.Marshal(WSClientStreamEvent{
				Type: "heartbeat",
				At:   time.Now().Format(time.RFC3339Nano),
			})
			if err := s.writeSSE(w, "heartbeat", heartbeatPayload); err != nil {
				return
			}
		}
	}
}
