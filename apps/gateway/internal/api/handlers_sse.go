package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
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
