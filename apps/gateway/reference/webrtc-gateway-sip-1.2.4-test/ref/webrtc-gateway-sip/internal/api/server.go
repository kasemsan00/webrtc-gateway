package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 180 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 16384
)

// Server represents the HTTP/WebSocket API server
type Server struct {
	sessionMgr *session.Manager
	sipMaker   SIPCallMaker
	logStore   logstore.LogStore
	config     config.APIConfig
	turnConfig config.TURNConfig
	upgrader   websocket.Upgrader
	wsClients  map[string]*WSClient
	mu         sync.RWMutex
}

// SIPCallMaker interface for making SIP calls (implemented by SIP server)
type SIPCallMaker interface {
	MakeCall(destination, from string, sess *session.Session) error
	Hangup(sess *session.Session) error
	SendDTMF(sess *session.Session, digits string) error
	AcceptCall(sess *session.Session) error
	RejectCall(sess *session.Session, reason string) error
	// Dynamic registration
	DynamicRegister(ctx context.Context, domain, username, password string, port int) error
	DynamicUnregister(ctx context.Context) error
	IsRegistered() bool
	GetRegisteredDomain() string
	// SIP Messaging
	SendMessage(destination, from, body, contentType string) error
	SendMessageToSession(sess *session.Session, body, contentType string) error
}

// WSClient represents a WebSocket client connection
type WSClient struct {
	conn      *websocket.Conn
	sessionID string
	send      chan []byte
}

// WSMessage represents a WebSocket message
type WSMessage struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"sessionId,omitempty"`
	SDP         string          `json:"sdp,omitempty"`
	Candidate   json.RawMessage `json:"candidate,omitempty"`
	Destination string          `json:"destination,omitempty"`
	From        string          `json:"from,omitempty"`
	To          string          `json:"to,omitempty"`
	Digits      string          `json:"digits,omitempty"`
	State       string          `json:"state,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	Error       string          `json:"error,omitempty"`
	// SIP Registration fields
	SIPDomain   string `json:"sipDomain,omitempty"`
	SIPUsername string `json:"sipUsername,omitempty"`
	SIPPassword string `json:"sipPassword,omitempty"`
	SIPPort     int    `json:"sipPort,omitempty"`
	Registered  bool   `json:"registered,omitempty"`
	// SIP Message fields
	Body        string `json:"body,omitempty"`
	ContentType string `json:"contentType,omitempty"`
}

// NewServer creates a new API server
func NewServer(cfg config.APIConfig, turnCfg config.TURNConfig, sessionMgr *session.Manager, sipMaker SIPCallMaker) *Server {
	return &Server{
		sessionMgr: sessionMgr,
		sipMaker:   sipMaker,
		config:     cfg,
		turnConfig: turnCfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins for now (controlled by CORS config)
				return true
			},
		},
		wsClients: make(map[string]*WSClient),
	}
}

// SetLogStore sets the log store for database logging.
func (s *Server) SetLogStore(store logstore.LogStore) {
	s.logStore = store
}

// Start starts the HTTP server
func (s *Server) Start() error {
	router := mux.NewRouter()

	// Enable CORS
	router.Use(s.corsMiddleware)

	// WebSocket endpoint
	if s.config.EnableWS {
		router.HandleFunc("/ws", s.handleWebSocket)
		fmt.Printf("WebSocket endpoint enabled: /ws\n")
	}

	// REST API endpoints
	if s.config.EnableREST {
		api := router.PathPrefix("/api").Subrouter()
		api.HandleFunc("/offer", s.handleOffer).Methods("POST", "OPTIONS")
		api.HandleFunc("/call", s.handleCall).Methods("POST", "OPTIONS")
		api.HandleFunc("/hangup/{sessionId}", s.handleHangup).Methods("POST", "OPTIONS")
		api.HandleFunc("/sessions", s.handleListSessions).Methods("GET", "OPTIONS")
		api.HandleFunc("/session/{sessionId}", s.handleGetSession).Methods("GET", "OPTIONS")
		api.HandleFunc("/dtmf/{sessionId}", s.handleDTMF).Methods("POST", "OPTIONS")
		fmt.Printf("REST API endpoints enabled: /api/*\n")
	}

	// Serve static files for test client
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./web")))

	addr := fmt.Sprintf(":%d", s.config.Port)
	fmt.Printf("\n=== API Server ===\n")
	fmt.Printf("Listening on: http://0.0.0.0%s\n", addr)
	fmt.Printf("==================\n\n")

	return http.ListenAndServe(addr, router)
}

// corsMiddleware adds CORS headers
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.config.CORSOrigins)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	client := &WSClient{
		conn: conn,
		send: make(chan []byte, 256),
	}

	// Start write pump
	go s.wsWritePump(client)

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		if s.config.DebugWebSocket {
			fmt.Printf("[WebSocket] 🏓 Received native pong from client (sessionID=%s)\n", client.sessionID)
		}
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Read messages
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		s.handleWSMessage(client, message)
	}

	// Cleanup - only delete if this client is still the registered one
	if client.sessionID != "" {
		s.mu.Lock()
		if s.wsClients[client.sessionID] == client {
			delete(s.wsClients, client.sessionID)
		}
		s.mu.Unlock()
	}
}

// wsWritePump pumps messages from the send channel to the WebSocket connection
// wsWritePump pumps messages from the send channel to the WebSocket connection
func (s *Server) wsWritePump(client *WSClient) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := client.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

			// Send any queued messages as SEPARATE WebSocket frames
			// (Don't concatenate into single frame - causes JSON parse errors)
			n := len(client.send)
			for i := 0; i < n; i++ {
				queuedMsg := <-client.send
				client.conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := client.conn.WriteMessage(websocket.TextMessage, queuedMsg); err != nil {
					return
				}
			}
		case <-ticker.C:
			client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if s.config.DebugWebSocket {
				fmt.Printf("[WebSocket] 🏓 Sending native ping to client (sessionID=%s)\n", client.sessionID)
			}
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleWSMessage processes WebSocket messages
func (s *Server) handleWSMessage(client *WSClient, message []byte) {
	var msg WSMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		s.sendWSError(client, "", "Invalid message format")
		return
	}

	switch msg.Type {
	case "offer":
		s.handleWSoffer(client, msg)
	case "call":
		s.handleWSCall(client, msg)
	case "hangup":
		s.handleWSHangup(client, msg)
	case "dtmf":
		s.handleWSDTMF(client, msg)
	case "accept":
		s.handleWSAccept(client, msg)
	case "reject":
		s.handleWSReject(client, msg)
	case "register":
		s.handleWSRegister(client, msg)
	case "unregister":
		s.handleWSUnregister(client, msg)
	case "ping":
		s.handleWSPing(client, msg)
	case "send_message":
		s.handleWSSendMessage(client, msg)
	case "resume":
		s.handleWSResume(client, msg)
	default:
		s.sendWSError(client, msg.SessionID, "Unknown message type")
	}
}

// handleWSoffer handles WebSocket offer messages
func (s *Server) handleWSoffer(client *WSClient, msg WSMessage) {
	// Create or get session
	var sess *session.Session
	var err error

	if msg.SessionID != "" {
		var ok bool
		sess, ok = s.sessionMgr.GetSession(msg.SessionID)
		if !ok {
			s.sendWSError(client, msg.SessionID, "Session not found")
			return
		}
	} else {
		sess, err = s.sessionMgr.CreateSession(s.turnConfig)
		if err != nil {
			s.sendWSError(client, "", fmt.Sprintf("Failed to create session: %v", err))
			return
		}
	}

	ctx := context.Background()
	payloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sess.ID,
		Timestamp:   time.Now(),
		Kind:        "webrtc_sdp_offer",
		ContentType: "application/sdp",
		BodyText:    msg.SDP,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_offer_received",
		PayloadID: payloadID,
	})

	// Associate client with session
	client.sessionID = sess.ID
	s.mu.Lock()
	s.wsClients[sess.ID] = client
	s.mu.Unlock()

	// Best-effort: cache H.264 SPS/PPS from Offer SDP (if present) so SIP SDP can include sprop-parameter-sets.
	if sps, pps, ok := session.ExtractH264SpropParameterSets(msg.SDP); ok {
		sess.SetCachedSPSPPS(sps, pps, "ws-offer")
	}

	// Parse and set offer
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  msg.SDP,
	}

	if err := sess.PeerConnection.SetRemoteDescription(offer); err != nil {
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "ws",
			Name:      "webrtc_set_remote_description_err",
			Data:      map[string]interface{}{"error": err.Error()},
		})
		s.sendWSError(client, sess.ID, fmt.Sprintf("Failed to set offer: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "webrtc_set_remote_description_ok",
	})

	// Create answer
	answer, err := sess.PeerConnection.CreateAnswer(nil)
	if err != nil {
		s.sendWSError(client, sess.ID, fmt.Sprintf("Failed to create answer: %v", err))
		return
	}

	// Set local description
	if err := sess.PeerConnection.SetLocalDescription(answer); err != nil {
		s.sendWSError(client, sess.ID, fmt.Sprintf("Failed to set local description: %v", err))
		return
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(sess.PeerConnection)
	<-gatherComplete

	// Send answer with video configuration
	response := WSMessage{
		Type:      "answer",
		SessionID: sess.ID,
		SDP:       sess.PeerConnection.LocalDescription().SDP,
	}
	s.sendWSMessage(client, response)

	answerPayloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sess.ID,
		Timestamp:   time.Now(),
		Kind:        "webrtc_sdp_answer",
		ContentType: "application/sdp",
		BodyText:    response.SDP,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_answer_sent",
		PayloadID: answerPayloadID,
	})

	sess.UpdateState(session.StateConnecting)
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "session_state_changed",
		State:     string(session.StateConnecting),
	})
	s.logSessionSnapshot(ctx, sess, "")
}

// handleWSCall handles WebSocket call messages
func (s *Server) handleWSCall(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}

	if msg.Destination == "" {
		s.sendWSError(client, msg.SessionID, "Destination required")
		return
	}

	// Set call info
	sess.SetCallInfo("outbound", msg.From, msg.Destination, "")

	ctx := context.Background()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_call_request",
		Data:      map[string]interface{}{"destination": msg.Destination, "from": msg.From},
	})
	s.logSessionSnapshot(ctx, sess, "")

	// Make SIP call
	if s.sipMaker != nil {
		if err := s.sipMaker.MakeCall(msg.Destination, msg.From, sess); err != nil {
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "ws",
				Name:      "ws_call_failed",
				Data:      map[string]interface{}{"error": err.Error()},
			})
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to make call: %v", err))
			return
		}
	}

	// Send state update
	response := WSMessage{
		Type:      "state",
		SessionID: sess.ID,
		State:     string(sess.GetState()),
	}
	s.sendWSMessage(client, response)
}

// handleWSHangup handles WebSocket hangup messages
func (s *Server) handleWSHangup(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}

	// Update session state first
	sess.UpdateState(session.StateEnded)

	ctx := context.Background()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_hangup_request",
	})
	s.logSessionSnapshot(ctx, sess, "ws_hangup")

	// Send SIP BYE (this will wait for completion)
	if s.sipMaker != nil {
		if err := s.sipMaker.Hangup(sess); err != nil {
			log.Printf("[Session %s] Hangup error: %v", msg.SessionID, err)
		}
	}

	// Delete session after BYE is sent
	s.sessionMgr.DeleteSession(msg.SessionID)

	// Send state update
	response := WSMessage{
		Type:      "state",
		SessionID: msg.SessionID,
		State:     string(session.StateEnded),
	}
	s.sendWSMessage(client, response)
}

// handleWSDTMF handles WebSocket DTMF messages
func (s *Server) handleWSDTMF(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_dtmf_request",
		Data:      map[string]interface{}{"digits": msg.Digits},
	})

	if s.sipMaker != nil {
		if err := s.sipMaker.SendDTMF(sess, msg.Digits); err != nil {
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to send DTMF: %v", err))
			return
		}
	}
}

// sendWSMessage sends a message to a WebSocket client
func (s *Server) sendWSMessage(client *WSClient, msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message: %v", err)
		return
	}
	client.send <- data
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

	_ = s.logStore.UpsertSession(ctx, &logstore.SessionRecord{
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
		Meta:          map[string]interface{}{"source": "api"},
	})
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

// NotifySessionState notifies WebSocket clients about session state changes
func (s *Server) NotifySessionState(sessionID string, state session.SessionState) {
	s.mu.RLock()
	client, ok := s.wsClients[sessionID]
	s.mu.RUnlock()

	if ok {
		msg := WSMessage{
			Type:      "state",
			SessionID: sessionID,
			State:     string(state),
		}
		s.sendWSMessage(client, msg)
	}
}

// NotifyIncomingCall notifies all connected WebSocket clients about an incoming call
func (s *Server) NotifyIncomingCall(sessionID, from, to string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Broadcast to all connected clients (or first one if single-user mode)
	msg := WSMessage{
		Type:      "incoming",
		SessionID: sessionID,
		From:      from,
		To:        to,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal incoming call notification: %v", err)
		return
	}

	// Send to all connected clients
	for _, client := range s.wsClients {
		select {
		case client.send <- data:
			log.Printf("📲 Sent incoming call notification to client (sessionID=%s)", sessionID)
		default:
			log.Printf("Failed to send incoming call notification - client channel full")
		}
	}

	// If no clients connected yet, store as pending
	if len(s.wsClients) == 0 {
		log.Printf("⚠️ No WebSocket clients connected for incoming call notification")
	}
}

// handleWSAccept handles WebSocket accept messages for incoming calls
func (s *Server) handleWSAccept(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	log.Printf("📞 [Accept] Incoming sessionID: %s, Client sessionID: %s", msg.SessionID, client.sessionID)

	// Get the incoming call session (this has the SIP transaction but no WebRTC)
	incomingSess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}

	ctx := context.Background()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: incomingSess.ID,
		Category:  "ws",
		Name:      "ws_accept_request",
	})

	// Find the client's existing WebRTC session (this has WebRTC but no SIP)
	var webrtcSess *session.Session
	if client.sessionID != "" && client.sessionID != msg.SessionID {
		if sess, ok := s.sessionMgr.GetSession(client.sessionID); ok {
			if sess.PeerConnection != nil {
				webrtcSess = sess
				log.Printf("📞 Found client's WebRTC session: %s", webrtcSess.ID)
			} else {
				log.Printf("⚠️ Client session %s has no PeerConnection", client.sessionID)
			}
		} else {
			log.Printf("⚠️ Client session %s not found in sessionMgr", client.sessionID)
		}
	} else {
		log.Printf("⚠️ No valid client.sessionID (empty=%v, same=%v)", client.sessionID == "", client.sessionID == msg.SessionID)
	}

	// If we have a WebRTC session, transfer SIP data to it
	if webrtcSess != nil {
		log.Printf("📞 Transferring SIP data from session %s to WebRTC session %s", incomingSess.ID, webrtcSess.ID)

		// Transfer SIP transaction and request to WebRTC session (thread-safe)
		webrtcSess.CopyIncomingInviteFrom(incomingSess)
		_, from, to, sipCallID := incomingSess.GetCallInfo()
		webrtcSess.SetCallInfo("inbound", from, to, sipCallID)

		// DON'T delete yet - only delete after AcceptCall succeeds
	}

	// Determine which session to use for the call
	callSession := incomingSess
	incomingSessionID := msg.SessionID // Remember for later deletion
	if webrtcSess != nil {
		callSession = webrtcSess
	}

	// Associate client with the call session
	if client.sessionID == "" || client.sessionID != callSession.ID {
		client.sessionID = callSession.ID
		s.mu.Lock()
		s.wsClients[callSession.ID] = client
		s.mu.Unlock()
	}

	// Accept the incoming call
	acceptError := false
	if s.sipMaker != nil {
		if err := s.sipMaker.AcceptCall(callSession); err != nil {
			log.Printf("⚠️ AcceptCall error (call may still work via retransmission): %v", err)
			// Don't return - the call might still work
			// The 200 OK might have been sent despite the error (e.g., "transaction terminated")
			// We'll still send the state update so browser knows the correct session ID
			acceptError = true
		}
	}

	s.logSessionSnapshot(ctx, callSession, "")

	// Delete the old incoming session (even if AcceptCall reported error, call may work)
	if webrtcSess != nil && incomingSessionID != webrtcSess.ID {
		log.Printf("🗑️ Deleting old incoming session: %s", incomingSessionID)
		s.sessionMgr.DeleteSession(incomingSessionID)
	}

	// ALWAYS send state update with the CORRECT session ID
	// This ensures browser uses the right session for hangup
	response := WSMessage{
		Type:      "state",
		SessionID: callSession.ID,
		State:     "active", // Assume active - dialog state will be set from ACK
	}
	s.sendWSMessage(client, response)

	if acceptError {
		log.Printf("⚠️ Call accepted with warning, using session: %s (dialog state will be set from ACK)", callSession.ID)
	} else {
		log.Printf("✅ Call accepted, using session: %s", callSession.ID)
	}
}

// handleWSReject handles WebSocket reject messages for incoming calls
func (s *Server) handleWSReject(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}

	ctx := context.Background()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_reject_request",
		Data:      map[string]interface{}{"reason": msg.Reason},
	})

	reason := msg.Reason
	if reason == "" {
		reason = "busy" // Default to busy
	}

	// Reject the incoming call
	if s.sipMaker != nil {
		if err := s.sipMaker.RejectCall(sess, reason); err != nil {
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to reject call: %v", err))
			return
		}
	}

	// Delete session
	s.sessionMgr.DeleteSession(msg.SessionID)
	s.logSessionSnapshot(ctx, sess, "ws_reject")

	// Send state update
	response := WSMessage{
		Type:      "state",
		SessionID: msg.SessionID,
		State:     string(session.StateEnded),
	}
	s.sendWSMessage(client, response)
}

// handleWSRegister handles WebSocket SIP registration messages
func (s *Server) handleWSRegister(client *WSClient, msg WSMessage) {
	log.Printf("📞 Received SIP register request: domain=%s, username=%s, port=%d",
		msg.SIPDomain, msg.SIPUsername, msg.SIPPort)

	// Validate required fields
	if msg.SIPDomain == "" {
		s.sendWSError(client, "", "SIP domain is required")
		return
	}
	if msg.SIPUsername == "" {
		s.sendWSError(client, "", "SIP username is required")
		return
	}
	if msg.SIPPassword == "" {
		s.sendWSError(client, "", "SIP password is required")
		return
	}

	// Use default port if not specified
	port := msg.SIPPort
	if port == 0 {
		port = 5060
	}

	// Perform SIP registration
	if s.sipMaker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.sipMaker.DynamicRegister(ctx, msg.SIPDomain, msg.SIPUsername, msg.SIPPassword, port); err != nil {
			log.Printf("❌ SIP registration failed: %v", err)
			s.sendWSError(client, "", fmt.Sprintf("Registration failed: %v", err))
			return
		}

		log.Printf("✅ SIP registration successful: %s@%s", msg.SIPUsername, msg.SIPDomain)
	}

	// Send success response
	response := WSMessage{
		Type:       "registerStatus",
		Registered: true,
		SIPDomain:  msg.SIPDomain,
	}
	s.sendWSMessage(client, response)
}

// handleWSUnregister handles WebSocket SIP unregistration messages
func (s *Server) handleWSUnregister(client *WSClient, msg WSMessage) {
	log.Printf("📞 Received SIP unregister request")

	if s.sipMaker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.sipMaker.DynamicUnregister(ctx); err != nil {
			log.Printf("⚠️ SIP unregistration warning: %v", err)
			// Don't return error - unregister should always report success to UI
		}

		log.Printf("✅ SIP unregistered")
	}

	// Send success response
	response := WSMessage{
		Type:       "registerStatus",
		Registered: false,
	}
	s.sendWSMessage(client, response)
}

// handleWSPing handles WebSocket ping messages
func (s *Server) handleWSPing(client *WSClient, msg WSMessage) {
	if s.config.DebugWebSocket {
		fmt.Printf("[WebSocket] 💓 Received ping from client (sessionID=%s)\n", client.sessionID)
	}
	response := WSMessage{
		Type: "pong",
	}
	s.sendWSMessage(client, response)
	if s.config.DebugWebSocket {
		fmt.Printf("[WebSocket] 💚 Sent pong to client (sessionID=%s)\n", client.sessionID)
	}
}

// handleWSSendMessage handles WebSocket send_message requests
func (s *Server) handleWSSendMessage(client *WSClient, msg WSMessage) {
	if msg.Body == "" {
		s.sendWSError(client, "", "Message body required")
		return
	}

	// Default content type
	contentType := msg.ContentType
	if contentType == "" {
		contentType = "text/plain;charset=UTF-8"
	}

	// Send SIP MESSAGE
	if s.sipMaker != nil {
		// Try to find an active session for this client to send in-dialog message
		var sess *session.Session
		if client.sessionID != "" {
			sess, _ = s.sessionMgr.GetSession(client.sessionID)
		}

		// If we have a session with remote contact, use in-dialog messaging
		if sess != nil {
			_, _, remoteContact, _, _, _, _ := sess.GetSIPDialogState()
			if remoteContact != "" {
				log.Printf("💬 Sending in-dialog message via session %s to %s", sess.ID, remoteContact)
				if err := s.sipMaker.SendMessageToSession(sess, msg.Body, contentType); err != nil {
					s.sendWSError(client, "", fmt.Sprintf("Failed to send in-dialog message: %v", err))
					return
				}
				return
			}
		}

		// Fallback to out-of-dialog message - requires destination
		if msg.Destination == "" {
			s.sendWSError(client, "", "No active session and no destination specified")
			return
		}
		if err := s.sipMaker.SendMessage(msg.Destination, msg.From, msg.Body, contentType); err != nil {
			s.sendWSError(client, "", fmt.Sprintf("Failed to send message: %v", err))
			return
		}
	}

	// Send confirmation to client
	response := WSMessage{
		Type:        "messageSent",
		Destination: msg.Destination,
		Body:        msg.Body,
	}
	s.sendWSMessage(client, response)
	log.Printf("💬 Message sent successfully")
}

// handleWSResume handles WebSocket resume messages for reconnecting after network change
// This allows a client to resume an existing call session after a WebSocket disconnection
// If SDP is provided, it renegotiates the PeerConnection to establish a fresh WebRTC connection
func (s *Server) handleWSResume(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required for resume")
		return
	}

	log.Printf("🔄 Resume request for session: %s (has SDP: %v)", msg.SessionID, msg.SDP != "")

	// Try to find the existing session
	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		log.Printf("❌ Resume failed: session %s not found", msg.SessionID)
		response := WSMessage{
			Type:      "resume_failed",
			SessionID: msg.SessionID,
			Reason:    "Session not found or expired",
		}
		s.sendWSMessage(client, response)
		return
	}

	// Check if the session is still in an active call state (including reconnecting state)
	state := sess.GetState()
	if state != session.StateActive &&
		state != session.StateConnecting &&
		state != session.StateRinging &&
		state != session.StateReconnecting {
		log.Printf("❌ Resume failed: session %s is in state %s (not resumable)", msg.SessionID, state)
		response := WSMessage{
			Type:      "resume_failed",
			SessionID: msg.SessionID,
			Reason:    fmt.Sprintf("Session is in state '%s', cannot resume", state),
		}
		s.sendWSMessage(client, response)
		return
	}

	// If session was in reconnecting state, transition back to active
	wasReconnecting := state == session.StateReconnecting
	if wasReconnecting {
		sess.SetState(session.StateActive)
		log.Printf("✅ Session %s transitioned from reconnecting to active", msg.SessionID)
	}

	// Remove old client mapping if exists (from previous WebSocket connection)
	s.mu.Lock()
	oldClient, hadOldClient := s.wsClients[msg.SessionID]
	if hadOldClient && oldClient != client {
		// Clear old client's sessionID to prevent its cleanup from deleting the new client
		oldClient.sessionID = ""
		log.Printf("🔄 Replacing old WebSocket client for session %s", msg.SessionID)
	}
	// Associate the new client with the session
	s.wsClients[msg.SessionID] = client
	client.sessionID = msg.SessionID
	s.mu.Unlock()

	// If client provided SDP, renegotiate the PeerConnection
	var answerSDP string
	if msg.SDP != "" {
		log.Printf("🔄 Renegotiating PeerConnection for session %s", msg.SessionID)

		// Renegotiate with the new SDP offer
		if err := sess.RenegotiatePeerConnection(msg.SDP, s.turnConfig, s.config.DebugTURN); err != nil {
			log.Printf("❌ Resume renegotiation failed for session %s: %v", msg.SessionID, err)
			response := WSMessage{
				Type:      "resume_failed",
				SessionID: msg.SessionID,
				Reason:    fmt.Sprintf("Failed to renegotiate: %v", err),
			}
			s.sendWSMessage(client, response)
			return
		}

		// Get the answer SDP to send back to client
		if sess.PeerConnection != nil && sess.PeerConnection.LocalDescription() != nil {
			answerSDP = sess.PeerConnection.LocalDescription().SDP
		}

		log.Printf("✅ Session %s PeerConnection renegotiated successfully", msg.SessionID)
	}

	direction, _, _, _ := sess.GetCallInfo()
	log.Printf("✅ Session %s resumed successfully (state: %s, direction: %s, wasReconnecting: %v, hasSDP: %v)", msg.SessionID, state, direction, wasReconnecting, answerSDP != "")

	// Send success response with session details (and answer SDP if renegotiated)
	_, from, to, _ := sess.GetCallInfo()
	response := WSMessage{
		Type:      "resumed",
		SessionID: msg.SessionID,
		State:     string(state),
		From:      from,
		To:        to,
		SDP:       answerSDP,
	}
	s.sendWSMessage(client, response)
}

// NotifySIPMessage notifies all WebSocket clients about an incoming SIP message
func (s *Server) NotifySIPMessage(to, from, body, contentType string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	log.Printf("💬 Incoming SIP message from: %s to: %s", from, to)

	msg := WSMessage{
		Type:        "message",
		From:        from,
		To:          to,
		Body:        body,
		ContentType: contentType,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message notification: %v", err)
		return
	}

	// Broadcast to all connected clients
	for _, client := range s.wsClients {
		select {
		case client.send <- data:
			log.Printf("💬 Sent message notification to client (sessionID=%s)", client.sessionID)
		default:
			log.Printf("Failed to send message notification - client channel full")
		}
	}

	if len(s.wsClients) == 0 {
		log.Printf("⚠️ No WebSocket clients connected for message notification")
	}
}

// NotifyDTMF notifies the WebSocket client about a received DTMF digit from SIP side
func (s *Server) NotifyDTMF(sessionID, digit string) {
	s.mu.RLock()
	client, ok := s.wsClients[sessionID]
	s.mu.RUnlock()

	if !ok {
		log.Printf("📞 DTMF received for session %s but no WebSocket client found", sessionID)
		return
	}

	log.Printf("📞 Forwarding DTMF '%s' to WebSocket client (sessionID=%s)", digit, sessionID)

	msg := WSMessage{
		Type:      "dtmf",
		SessionID: sessionID,
		Digits:    digit,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal DTMF notification: %v", err)
		return
	}

	select {
	case client.send <- data:
		log.Printf("📞 DTMF '%s' sent to client (sessionID=%s)", digit, sessionID)
	default:
		log.Printf("Failed to send DTMF notification - client channel full")
	}
}
