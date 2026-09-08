package api

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/telemetry"
)

func (s *Server) sendWSMessage(client *WSClient, msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message: %v", err)
		return
	}
	select {
	case client.send <- data:
	default:
		log.Printf("Dropping WebSocket message: client send buffer full (sessionID=%s, type=%s)", client.sessionID, msg.Type)
		telemetry.RecordWebSocketMessage(context.Background(), msg.Type, "dropped")
		_ = telemetry.Log(context.Background(), telemetry.LogEvent{Severity: telemetry.SeverityWarn, Component: "websocket", Name: "websocket.message.dropped", Outcome: "dropped", Reason: "queue_full", Correlation: telemetry.Correlation{SessionID: client.sessionID}})
	}
}

// sendWSError sends an error message to a WebSocket client
func (s *Server) sendWSError(client *WSClient, sessionID, errMsg string) {
	msg := WSMessage{
		Type:      "error",
		SessionID: sessionID,
		Error:     errMsg,
	}
	s.sendWSMessage(client, msg)
}

func (s *Server) logTerminalAction(sess *session.Session, action string, sipStatus int, reason, source string) {
	if sess == nil {
		return
	}
	_, _, _, sipCallID := sess.GetCallInfo()
	data := map[string]interface{}{
		"sessionId": sess.ID,
		"sipCallId": sipCallID,
		"action":    action,
		"reason":    reason,
		"source":    source,
	}
	if sipStatus > 0 {
		data["sipStatus"] = sipStatus
	}
	s.logEvent(&logstore.Event{
		Timestamp:     time.Now(),
		SessionID:     sess.ID,
		Category:      "sip",
		Name:          "sip_terminal_action",
		SIPCallID:     sipCallID,
		SIPStatusCode: sipStatus,
		Data:          data,
	})
}

func (s *Server) logSessionSnapshot(ctx context.Context, sess *session.Session, endReason string) {
	if s.logStore == nil || sess == nil {
		return
	}

	snap := sess.Snapshot()
	var endedAt *time.Time
	if snap.State == session.StateEnded {
		ended := time.Now()
		endedAt = &ended
	}

	if endReason == "" && snap.State == session.StateEnded {
		endReason = "ended"
	}

	if snap.Direction != "inbound" && snap.Direction != "outbound" {
		if s.config.DebugWebSocket {
			log.Printf("Skipping session snapshot for %s: direction not set", snap.ID)
		}
		return
	}

	// Extract auth context
	authMode, _, trunkID, _, sipUsername, _, _ := sess.GetSIPAuthContext()
	var trunkIDPtr *int64
	if trunkID > 0 {
		trunkIDPtr = &trunkID
	}
	var trunkName string
	if authMode == "trunk" && trunkID > 0 && s.trunkManager != nil {
		if trunk, err := s.trunkManager.GetTrunkByIDFromDB(ctx, trunkID); err == nil {
			trunkName = trunk.Name
		}
	}

	if err := s.logStore.UpsertSession(ctx, &logstore.SessionRecord{
		SessionID:     snap.ID,
		CreatedAt:     snap.CreatedAt,
		UpdatedAt:     time.Now(),
		EndedAt:       endedAt,
		Direction:     snap.Direction,
		FromURI:       snap.From,
		ToURI:         snap.To,
		SIPCallID:     snap.SIPCallID,
		FinalState:    string(snap.State),
		EndReason:     endReason,
		RTPAudioPort:  snap.RTPPort,
		RTPVideoPort:  snap.VideoRTPPort,
		RTCPAudioPort: snap.AudioRTCPPort,
		RTCPVideoPort: snap.VideoRTCPPort,
		SIPOpusPT:     int(snap.SIPOpusPT),
		AuthMode:      authMode,
		TrunkID:       trunkIDPtr,
		TrunkName:     trunkName,
		SIPUsername:   sipUsername,
		Meta:          s.buildSessionMeta(sess, "api"),
	}); err != nil {
		log.Printf("⚠️ Failed to upsert session snapshot for %s: %v", snap.ID, err)
	}
}

func (s *Server) buildSessionMeta(sess *session.Session, source string) map[string]interface{} {
	meta := map[string]interface{}{"source": source}
	if sess != nil {
		authMode, accountKey, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
		if authMode != "" {
			meta["authMode"] = authMode
		}
		if accountKey != "" {
			meta["accountKey"] = accountKey
		}
		if trunkID > 0 {
			meta["trunkId"] = trunkID
		}
	}
	return meta
}

func (s *Server) logEvent(event *logstore.Event) {
	if s.logStore == nil || event == nil {
		return
	}
	s.logStore.LogEvent(event)
}

func (s *Server) storePayload(ctx context.Context, payload *logstore.PayloadRecord) *int64 {
	if s.logStore == nil || payload == nil {
		return nil
	}

	payloadID, err := s.logStore.StorePayload(ctx, payload)
	if err != nil {
		return nil
	}
	return &payloadID
}

func (s *Server) subscribeTrunkStream() (int, chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.trunkStreamSeq++
	id := s.trunkStreamSeq
	ch := make(chan []byte, 32)
	s.trunkStreams[id] = ch
	return id, ch
}

func (s *Server) unsubscribeTrunkStream(id int) {
	s.mu.Lock()
	delete(s.trunkStreams, id)
	s.mu.Unlock()
}

func (s *Server) broadcastTrunkStream(payload []byte) {
	s.mu.RLock()
	streams := make([]chan []byte, 0, len(s.trunkStreams))
	for _, ch := range s.trunkStreams {
		streams = append(streams, ch)
	}
	s.mu.RUnlock()

	for _, ch := range streams {
		select {
		case ch <- payload:
		default:
		}
	}
}

func (s *Server) subscribeSessionStream() (int, chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessionStreamSeq++
	id := s.sessionStreamSeq
	ch := make(chan []byte, 32)
	s.sessionStreams[id] = ch
	return id, ch
}

func (s *Server) unsubscribeSessionStream(id int) {
	s.mu.Lock()
	delete(s.sessionStreams, id)
	s.mu.Unlock()
}

func (s *Server) broadcastSessionStream(payload []byte) {
	s.mu.RLock()
	streams := make([]chan []byte, 0, len(s.sessionStreams))
	for _, ch := range s.sessionStreams {
		streams = append(streams, ch)
	}
	s.mu.RUnlock()

	for _, ch := range streams {
		select {
		case ch <- payload:
		default:
		}
	}
}

func (s *Server) subscribeWSClientStream() (int, chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.wsClientStreamSeq++
	id := s.wsClientStreamSeq
	ch := make(chan []byte, 32)
	s.wsClientStreams[id] = ch
	return id, ch
}

func (s *Server) unsubscribeWSClientStream(id int) {
	s.mu.Lock()
	delete(s.wsClientStreams, id)
	s.mu.Unlock()
}

func (s *Server) broadcastWSClientStream(payload []byte) {
	s.mu.RLock()
	streams := make([]chan []byte, 0, len(s.wsClientStreams))
	for _, ch := range s.wsClientStreams {
		streams = append(streams, ch)
	}
	s.mu.RUnlock()

	for _, ch := range streams {
		select {
		case ch <- payload:
		default:
		}
	}
}
