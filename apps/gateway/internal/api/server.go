package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/auth"
	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/push"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
	"k2-gateway/internal/translator"
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

	// Log slow resume requests to help diagnose renegotiation/network bottlenecks.
	resumeSlowLogThreshold = 3 * time.Second

	// Timeout for loading trunk notify_user_id from DB before dispatching push.
	incomingPushTrunkLookupTimeout = 5 * time.Second
)

// Server represents the HTTP/WebSocket API server
type Server struct {
	sessionMgr       *session.Manager
	sipMaker         SIPCallMaker
	tokenVerifier    TokenVerifier
	publicRegistry   PublicAccountRegistry
	trunkManager     TrunkManager
	logStore         logstore.LogStore
	pushService      *push.Service
	config           config.APIConfig
	turnConfig       config.TURNConfig
	gatewayConfig    config.GatewayConfig
	translatorCfg    config.TranslatorConfig
	translatorClient *translator.Client
	upgrader         websocket.Upgrader
	wsClients        map[string]*WSClient
	wsConnections    map[*WSClient]struct{}
	trunkStreams     map[int]chan []byte
	trunkStreamSeq   int
	sessionStreams   map[int]chan []byte
	sessionStreamSeq int
	startTime        time.Time
	mu               sync.RWMutex
}

type trunkOutboundValidation struct {
	trunk    *sip.Trunk
	notFound bool
	reason   string
}

// TokenVerifier verifies bearer JWT tokens.
type TokenVerifier interface {
	VerifyToken(ctx context.Context, rawToken string, hint auth.TokenRealm) (*auth.VerifiedClaims, error)
}

// PublicAccountRegistry interface for managing SIP public accounts
type PublicAccountRegistry interface {
	AcquireAndRegister(ctx context.Context, domain, username, password string, port int) (accountKey string, err error)
	IncrementRefCount(accountKey string)
	DecrementRefCount(accountKey string)
	ListAccounts() []*sip.PublicAccount
}

// TrunkManager interface for managing SIP trunks
type TrunkManager interface {
	GetTrunkByID(id int64) (trunk interface{}, found bool)
	GetTrunkByPublicID(publicID string) (trunk interface{}, found bool)
	GetTrunkIDByPublicID(publicID string) (trunkID int64, found bool)
	GetDefaultTrunk() (trunk interface{}, found bool)
	RefreshTrunks() error
	CreateTrunk(ctx context.Context, payload sip.CreateTrunkPayload) (*sip.Trunk, error)
	UpdateTrunk(ctx context.Context, trunkID int64, patch sip.TrunkUpdatePatch) (*sip.Trunk, error)
	RegisterTrunk(trunkID int64, force bool) error
	UnregisterTrunk(trunkID int64, force bool) error
	ListTrunks(ctx context.Context, params sip.TrunkListParams) (*sip.TrunkListResult, error)
	GetTrunkByIDFromDB(ctx context.Context, trunkID int64) (*sip.Trunk, error)
	ListOwnedTrunks() []*sip.Trunk
	SetTrunkInUseBy(ctx context.Context, trunkID int64, username *string) error
	FindTrunkByInUseBy(ctx context.Context, inUseBy string) (*sip.Trunk, error)
	SetTrunkNotifyUserID(ctx context.Context, trunkID int64, userID *string) error
}

// SIPCallMaker interface for making SIP calls (implemented by SIP server)
type SIPCallMaker interface {
	MakeCall(destination, from string, sess *session.Session) error
	Hangup(sess *session.Session) error
	SendDTMF(sess *session.Session, digits string) error
	AcceptCall(sess *session.Session) error
	RejectCall(sess *session.Session, reason string) error
	// SIP Messaging
	SendMessage(destination, from, body, contentType string) error
	SendMessageToSession(sess *session.Session, body, contentType string) error
	TriggerSwitchMessage(body, callerURI string) error
}

// WSClient represents a WebSocket client connection
type WSClient struct {
	conn            *websocket.Conn
	sessionID       string
	trunkResolved   bool
	resolvedTrunkID int64
	send            chan []byte
	ConnectedAt     time.Time
	authClaims      *auth.VerifiedClaims // populated when tokenVerifier is set
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
	// Trunk resolve fields
	SIPDomain   string `json:"sipDomain,omitempty"`
	SIPUsername string `json:"sipUsername,omitempty"`
	SIPPassword string `json:"sipPassword,omitempty"`
	SIPPort     int    `json:"sipPort,omitempty"`
	// SIP Message fields
	Body        string `json:"body,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	// SIP Public/Trunk fields (new for multi-user registration)
	// For SIP Public mode outbound call: include sipDomain, sipUsername, sipPassword, sipPort
	// For SIP Trunk mode outbound call: include trunkId or trunkPublicId
	TrunkID       int64  `json:"trunkId,omitempty"`       // Use trunk from DB (0 = not specified)
	TrunkPublicID string `json:"trunkPublicId,omitempty"` // Stable public trunk reference
	// S2S Translation fields
	SourceLang string `json:"sourceLang,omitempty"`
	TargetLang string `json:"targetLang,omitempty"`
	TTSVoice   string `json:"ttsVoice,omitempty"`
	// Session resume redirect
	RedirectURL string `json:"redirectUrl,omitempty"` // Server response: resume_redirect with new WS URL
}

type resumeVideoOfferDiagnostics struct {
	HasVideoMLine  bool
	VideoPort      int
	VideoDirection string
}

func analyzeResumeOfferVideoSDP(sdp string) resumeVideoOfferDiagnostics {
	diag := resumeVideoOfferDiagnostics{
		HasVideoMLine:  false,
		VideoPort:      -1,
		VideoDirection: "unspecified",
	}
	if sdp == "" {
		return diag
	}

	inVideoSection := false
	lines := strings.Split(sdp, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "m=") {
			inVideoSection = strings.HasPrefix(line, "m=video ")
			if inVideoSection {
				diag.HasVideoMLine = true
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if port, err := strconv.Atoi(fields[1]); err == nil {
						diag.VideoPort = port
					}
				}
			}
			continue
		}

		if !inVideoSection {
			continue
		}

		switch line {
		case "a=sendrecv":
			diag.VideoDirection = "sendrecv"
		case "a=sendonly":
			diag.VideoDirection = "sendonly"
		case "a=recvonly":
			diag.VideoDirection = "recvonly"
		case "a=inactive":
			diag.VideoDirection = "inactive"
		}
	}

	return diag
}

// NewServer creates a new API server
func NewServer(cfg config.APIConfig, turnCfg config.TURNConfig, gatewayCfg config.GatewayConfig, translatorCfg config.TranslatorConfig, sessionMgr *session.Manager, sipMaker SIPCallMaker, publicRegistry PublicAccountRegistry, trunkMgr TrunkManager, logStore logstore.LogStore) *Server {
	return &Server{
		sessionMgr:     sessionMgr,
		sipMaker:       sipMaker,
		publicRegistry: publicRegistry,
		trunkManager:   trunkMgr,
		logStore:       logStore,
		config:         cfg,
		turnConfig:     turnCfg,
		gatewayConfig:  gatewayCfg,
		translatorCfg:  translatorCfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins for now (controlled by CORS config)
				return true
			},
		},
		wsClients:      make(map[string]*WSClient),
		wsConnections:  make(map[*WSClient]struct{}),
		trunkStreams:   make(map[int]chan []byte),
		sessionStreams: make(map[int]chan []byte),
		startTime:      time.Now(),
	}
}

// SetTranslatorClient sets the translator client for S2S translation.
func (s *Server) SetTranslatorClient(client *translator.Client) {
	s.translatorClient = client
}

// SetLogStore sets the log store for database logging.
func (s *Server) SetLogStore(store logstore.LogStore) {
	s.logStore = store
}

// SetTokenVerifier enables JWT auth enforcement for /api/* and /ws.
func (s *Server) SetTokenVerifier(verifier TokenVerifier) {
	s.tokenVerifier = verifier
}

// SetPushService enables push notifications on incoming calls.
func (s *Server) SetPushService(svc *push.Service) {
	s.pushService = svc
}

// Start starts the HTTP server with graceful shutdown support
func (s *Server) Start(ctx context.Context) error {
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
		if s.tokenVerifier != nil {
			api.Use(s.authMiddleware)
		}
		api.HandleFunc("/offer", s.handleOffer).Methods("POST", "OPTIONS")
		api.HandleFunc("/call", s.handleCall).Methods("POST", "OPTIONS")
		api.HandleFunc("/hangup/{sessionId}", s.handleHangup).Methods("POST", "OPTIONS")
		api.HandleFunc("/sessions", s.handleListSessions).Methods("GET", "OPTIONS")
		api.HandleFunc("/sessions/stream", s.handleSessionStream).Methods("GET", "OPTIONS")
		api.HandleFunc("/session/{sessionId}", s.handleGetSession).Methods("GET", "OPTIONS")
		api.HandleFunc("/dtmf/{sessionId}", s.handleDTMF).Methods("POST", "OPTIONS")
		api.HandleFunc("/switch", s.handleSwitch).Methods("POST", "OPTIONS")
		api.HandleFunc("/sessions/history", s.handleListSessionHistory).Methods("GET", "OPTIONS")
		api.HandleFunc("/sessions/{sessionId}/events", s.handleListSessionEvents).Methods("GET", "OPTIONS")
		api.HandleFunc("/sessions/{sessionId}/payloads", s.handleListSessionPayloads).Methods("GET", "OPTIONS")
		api.HandleFunc("/sessions/{sessionId}/dialogs", s.handleListSessionDialogs).Methods("GET", "OPTIONS")
		api.HandleFunc("/sessions/{sessionId}/stats", s.handleListSessionStats).Methods("GET", "OPTIONS")
		api.HandleFunc("/payloads/{payloadId}", s.handleGetPayload).Methods("GET", "OPTIONS")
		api.HandleFunc("/gateway/instances", s.handleListGatewayInstances).Methods("GET", "OPTIONS")
		api.HandleFunc("/session-directory", s.handleListSessionDirectory).Methods("GET", "OPTIONS")
		api.HandleFunc("/public-accounts", s.handleListPublicAccounts).Methods("GET", "OPTIONS")
		api.HandleFunc("/ws-clients", s.handleListWSClients).Methods("GET", "OPTIONS")
		api.HandleFunc("/dashboard", s.handleDashboard).Methods("GET", "OPTIONS")
		api.HandleFunc("/dashboard/summary", s.handleDashboardSummary).Methods("GET", "OPTIONS")
		api.HandleFunc("/trunks", s.handleListTrunks).Methods("GET", "OPTIONS")
		api.HandleFunc("/trunks/stream", s.handleTrunkStream).Methods("GET", "OPTIONS")
		api.HandleFunc("/trunks", s.handleCreateTrunk).Methods("POST", "OPTIONS")
		api.HandleFunc("/trunks/refresh", s.handleRefreshTrunks).Methods("POST", "OPTIONS")
		api.HandleFunc("/trunk/{id}", s.handleGetTrunk).Methods("GET", "OPTIONS")
		api.HandleFunc("/trunk/{id}", s.handleUpdateTrunk).Methods("PUT", "OPTIONS")
		api.HandleFunc("/trunk/{id}/register", s.handleTrunkRegister).Methods("POST", "OPTIONS")
		api.HandleFunc("/trunk/{id}/unregister", s.handleTrunkUnregister).Methods("POST", "OPTIONS")
		api.HandleFunc("/user/trunk", s.handleUserTrunkHeartbeat).Methods("PUT", "OPTIONS")
		fmt.Printf("REST API endpoints enabled: /api/*\n")
	}

	// Serve static files for test client
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./web")))

	addr := fmt.Sprintf(":%d", s.config.Port)
	fmt.Printf("\n=== API Server ===\n")
	fmt.Printf("Listening on: http://0.0.0.0%s\n", addr)
	fmt.Printf("==================\n\n")

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server error: %v", err)
		}
	}()

	// Wait for context cancellation for graceful shutdown
	<-ctx.Done()

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
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
	req := r
	if s.tokenVerifier != nil {
		rawToken := strings.TrimSpace(r.URL.Query().Get("access_token"))
		if rawToken == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		realmHint := extractAuthRealmHint(r)
		claims, err := s.tokenVerifier.VerifyToken(r.Context(), rawToken, realmHint)
		if err != nil {
			log.Printf("WebSocket auth rejected: hint=%s err=%v", realmHint, err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		log.Printf("WebSocket auth accepted: hint=%s realm=%s sub=%s", realmHint, claims.Realm, claims.Subject)
		req = withAuthClaims(r, claims)
	}

	conn, err := s.upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	client := &WSClient{
		conn:        conn,
		send:        make(chan []byte, 256),
		ConnectedAt: time.Now(),
	}
	if claims, ok := AuthClaimsFromContext(req.Context()); ok {
		client.authClaims = claims
	}
	s.mu.Lock()
	s.wsConnections[client] = struct{}{}
	s.mu.Unlock()

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
	s.mu.Lock()
	delete(s.wsConnections, client)
	if client.sessionID != "" {
		if s.wsClients[client.sessionID] == client {
			delete(s.wsClients, client.sessionID)
		}
	}
	s.mu.Unlock()
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
	case "ping":
		s.handleWSPing(client, msg)
	case "send_message":
		s.handleWSSendMessage(client, msg)
	case "resume":
		s.handleWSResume(client, msg)
	case "request_keyframe":
		s.handleWSRequestKeyframe(client, msg)
	case "trunk_resolve":
		s.handleWSTrunkResolve(client, msg)
	case "translate":
		s.handleWSTranslate(client, msg)
	case "translate_stop":
		s.handleWSTranslateStop(client, msg)
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

func (s *Server) validateTrunkReadyForOutboundCall(ctx context.Context, trunkID int64) trunkOutboundValidation {
	if s.trunkManager == nil {
		return trunkOutboundValidation{reason: "Trunk manager not available"}
	}

	trunk, err := s.trunkManager.GetTrunkByIDFromDB(ctx, trunkID)
	if err != nil || trunk == nil {
		return trunkOutboundValidation{
			notFound: true,
			reason:   fmt.Sprintf("Trunk %d not found", trunkID),
		}
	}

	if trunk.LeaseOwner == nil || strings.TrimSpace(*trunk.LeaseOwner) == "" || trunk.LeaseUntil == nil || trunk.LeaseUntil.Before(time.Now()) {
		return trunkOutboundValidation{reason: "Trunk lease not active"}
	}

	leaseOwner := strings.TrimSpace(*trunk.LeaseOwner)
	if leaseOwner != s.gatewayConfig.InstanceID {
		return trunkOutboundValidation{
			reason: fmt.Sprintf("Trunk is owned by another gateway instance (%s)", leaseOwner),
		}
	}

	return trunkOutboundValidation{trunk: trunk}
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

	ctx := context.Background()

	// Determine SIP auth mode: public vs trunk
	var authMode string
	var accountKey string
	var trunkID int64
	var trunkPublicID string

	if msg.TrunkID > 0 || msg.TrunkPublicID != "" {
		// Trunk mode: use trunk from DB
		authMode = "trunk"
		if s.trunkManager == nil {
			s.sendWSError(client, msg.SessionID, "Trunk manager not available")
			return
		}
		if msg.TrunkID > 0 {
			trunkID = msg.TrunkID
		} else {
			normalized, ok := sip.NormalizeTrunkPublicID(msg.TrunkPublicID)
			if !ok {
				s.sendWSError(client, msg.SessionID, "Invalid trunkPublicId")
				return
			}
			trunkPublicID = normalized
			resolvedID, found := s.trunkManager.GetTrunkIDByPublicID(trunkPublicID)
			if !found {
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk %s not found", trunkPublicID))
				return
			}
			trunkID = resolvedID
		}

		if !client.trunkResolved || client.resolvedTrunkID != trunkID {
			s.sendWSError(client, msg.SessionID, "Trunk must be resolved before placing call")
			return
		}

		validation := s.validateTrunkReadyForOutboundCall(ctx, trunkID)
		if validation.notFound {
			s.sendWSError(client, msg.SessionID, validation.reason)
			return
		}
		if validation.reason != "" {
			client.trunkResolved = false
			client.resolvedTrunkID = 0
			s.sendWSMessage(client, WSMessage{Type: "trunk_not_ready", Reason: validation.reason})
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not ready: %s", validation.reason))
			return
		}
		if validation.trunk != nil {
			trunkPublicID = validation.trunk.PublicID
		}

		// Set trunk auth context in session
		sess.SetSIPAuthContext("trunk", "", trunkID, "", "", "", 0)

		// Require authenticated user to use a trunk.
		if s.tokenVerifier != nil && client.authClaims == nil {
			s.sendWSError(client, msg.SessionID, "Token authentication required to use trunk")
			return
		}

		// Record who is using this trunk.
		if client.authClaims != nil && s.trunkManager != nil {
			username := client.authClaims.PreferredUsername
			if err := s.trunkManager.SetTrunkInUseBy(ctx, trunkID, &username); err != nil {
				log.Printf("⚠️ [WS Call] Failed to set in_use_by for trunk %d: %v", trunkID, err)
			}
		}

		log.Printf(
			"📞 [WS Call] Session %s using trunk mode (trunkId=%d, trunkPublicId=%s)",
			sess.ID, trunkID, trunkPublicID,
		)

	} else if msg.SIPDomain != "" && msg.SIPUsername != "" && msg.SIPPassword != "" {
		// Public mode: register and bind credentials to session
		authMode = "public"

		// Use port as-is from client (0 means "not specified" for hostname domains)
		// This allows DNS SRV resolution for hostnames without explicit port
		port := msg.SIPPort

		// Guard against identity switches on an existing public session.
		// Public username/domain changes require a new offer/session.
		existingMode, _, _, existingDomain, existingUsername, _, _ := sess.GetSIPAuthContext()
		if existingMode == "public" && (existingUsername != "" || existingDomain != "") {
			if existingUsername != msg.SIPUsername || !strings.EqualFold(existingDomain, msg.SIPDomain) {
				const errMsg = "Public SIP identity changed (username/domain). Send a new offer to create a new session."
				s.logEvent(&logstore.Event{
					Timestamp: time.Now(),
					SessionID: sess.ID,
					Category:  "ws",
					Name:      "ws_call_rejected_identity_change",
					Data: map[string]interface{}{
						"existingSipUsername": existingUsername,
						"existingSipDomain":   existingDomain,
						"newSipUsername":      msg.SIPUsername,
						"newSipDomain":        msg.SIPDomain,
					},
				})
				s.sendWSError(client, msg.SessionID, errMsg)
				return
			}
		}

		// Acquire and register account
		if s.publicRegistry != nil {
			var err error
			accountKey, err = s.publicRegistry.AcquireAndRegister(ctx, msg.SIPDomain, msg.SIPUsername, msg.SIPPassword, port)
			if err != nil {
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to register SIP account: %v", err))
				return
			}

			// Increment ref count for active call
			s.publicRegistry.IncrementRefCount(accountKey)
		}

		// Set public auth context in session
		sess.SetSIPAuthContext("public", accountKey, 0, msg.SIPDomain, msg.SIPUsername, msg.SIPPassword, port)

		log.Printf("📞 [WS Call] Session %s using public mode (account=%s, port=%d)", sess.ID, accountKey, port)

	} else {
		// Legacy mode: use global/dynamic SIP credentials (backward compatibility)
		authMode = ""
		log.Printf("📞 [WS Call] Session %s using legacy mode (global credentials)", sess.ID)
	}

	// Set call info
	sess.SetCallInfo("outbound", msg.From, msg.Destination, "")

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_call_request",
		Data: map[string]interface{}{
			"destination":   msg.Destination,
			"from":          msg.From,
			"authMode":      authMode,
			"accountKey":    accountKey,
			"trunkId":       trunkID,
			"trunkPublicId": trunkPublicID,
		},
	})
	s.logSessionSnapshot(ctx, sess, "")

	// Upsert session directory entry
	if s.logStore != nil && s.gatewayConfig.InstanceID != "" && s.gatewayConfig.PublicWSURL != "" {
		ttl := 7200 // 2 hours default
		if err := s.logStore.UpsertSessionDirectory(ctx, sess.ID, s.gatewayConfig.InstanceID, s.gatewayConfig.PublicWSURL, ttl); err != nil {
			log.Printf("⚠️ Failed to upsert session directory for %s: %v", sess.ID, err)
		}
	}

	// Make SIP call
	if s.sipMaker != nil {
		if err := s.sipMaker.MakeCall(msg.Destination, msg.From, sess); err != nil {
			// Cleanup: decrement ref count if public mode
			if authMode == "public" && accountKey != "" && s.publicRegistry != nil {
				s.publicRegistry.DecrementRefCount(accountKey)
			}

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

	// Send state=ended to browser IMMEDIATELY so the frontend knows the
	// call is ending. This must happen BEFORE Hangup() because Hangup()
	// blocks waiting for the SIP BYE response (up to 10 s timeout).
	response := WSMessage{
		Type:      "state",
		SessionID: msg.SessionID,
		State:     string(session.StateEnded),
	}
	s.sendWSMessage(client, response)

	// Send SIP BYE (this will wait for completion)
	if s.sipMaker != nil {
		if err := s.sipMaker.Hangup(sess); err != nil {
			log.Printf("[%s] Hangup error: %v", msg.SessionID, err)
		}
	}

	// Decrement public account refcount if applicable (before deleting session)
	authMode, accountKey, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
	if authMode == "public" && accountKey != "" && s.publicRegistry != nil {
		s.publicRegistry.DecrementRefCount(accountKey)
	}

	// Clear in_use_by for trunk sessions.
	if authMode == "trunk" && trunkID > 0 && s.trunkManager != nil {
		if err := s.trunkManager.SetTrunkInUseBy(context.Background(), trunkID, nil); err != nil {
			log.Printf("⚠️ [WS Hangup] Failed to clear in_use_by for trunk %d: %v", trunkID, err)
		}
	}

	// Delete session after BYE is sent
	s.sessionMgr.DeleteSession(msg.SessionID)
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
	select {
	case client.send <- data:
	default:
		log.Printf("Dropping WebSocket message: client send buffer full (sessionID=%s, type=%s)", client.sessionID, msg.Type)
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
		AuthMode:      authMode,
		TrunkID:       trunkIDPtr,
		TrunkName:     trunkName,
		SIPUsername:   sipUsername,
		Meta:          s.buildSessionMeta(sess, "api"),
	})
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

// NotifySessionState notifies WebSocket clients about session state changes
func (s *Server) NotifySessionState(sessionID string, state session.SessionState) {
	// Notify session stream listeners about state changes
	var eventType string
	switch state {
	case session.StateConnecting:
		eventType = "session_created"
	case session.StateActive:
		eventType = "session_active"
	case session.StateEnded:
		eventType = "session_ended"
	default:
		eventType = "session_state_changed"
	}
	sid := sessionID
	s.notifySessionListChanged(eventType, &sid)

	// Notify trunk stream listeners if trunk mode
	if s.sessionMgr != nil && (state == session.StateActive || state == session.StateEnded) {
		if sess, ok := s.sessionMgr.GetSession(sessionID); ok {
			authMode, _, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
			if authMode == "trunk" && trunkID > 0 {
				tid := trunkID
				s.notifyTrunkListChanged("session_updated", &tid)
			}
		}
	}

	// Notify WebSocket client
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

// NotifyIncomingCall notifies connected WebSocket clients about an incoming call for a specific trunk.
func (s *Server) NotifyIncomingCall(sessionID, from, to string, trunkID int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if trunkID <= 0 {
		log.Printf("📲 Skipping incoming call notification for session %s: missing trunkID", sessionID)
		return
	}

	totalConnections := len(s.wsConnections)
	recipients := 0
	recipientSessionIDs := make([]string, 0)

	// Broadcast only to clients resolved on the same trunk.
	for client := range s.wsConnections {
		if client == nil || !client.trunkResolved || client.resolvedTrunkID != trunkID {
			continue
		}
		recipients++
		recipientSessionIDs = append(recipientSessionIDs, client.sessionID)
		s.sendWSMessage(client, WSMessage{
			Type:      "incoming",
			SessionID: sessionID,
			From:      from,
			To:        to,
		})
		log.Printf("📲 Sent incoming call notification to resolved client (sessionID=%s trunkID=%d)", sessionID, trunkID)
	}

	if totalConnections == 0 {
		log.Printf("⚠️ No WebSocket clients connected for incoming call notification")
	} else {
		log.Printf("📲 Incoming fanout summary: sessionID=%s trunkID=%d recipients=%d recipientSessionIDs=%v filtered=%d total=%d", sessionID, trunkID, recipients, recipientSessionIDs, totalConnections-recipients, totalConnections)
	}

	// Send push notification in a separate goroutine (fire-and-forget).
	// Uses notify_user_id (Keycloak sub UUID) which persists across sessions,
	// so push works even when the user is offline / app is closed.
	if s.pushService == nil {
		log.Printf("🔔 [Push] Skip incoming call push: push service is not configured (sessionID=%s trunkID=%d)", sessionID, trunkID)
		return
	}
	if s.trunkManager == nil {
		log.Printf("🔔 [Push] Skip incoming call push: trunk manager is not available (sessionID=%s trunkID=%d)", sessionID, trunkID)
		return
	}

	go func() {
		lookupCtx, cancel := context.WithTimeout(context.Background(), incomingPushTrunkLookupTimeout)
		defer cancel()

		trunk, err := s.trunkManager.GetTrunkByIDFromDB(lookupCtx, trunkID)
		if err != nil {
			log.Printf("🔔 [Push] Skip incoming call push: failed to load trunk from DB (sessionID=%s trunkID=%d err=%v)", sessionID, trunkID, err)
			return
		}
		if trunk == nil {
			log.Printf("🔔 [Push] Skip incoming call push: trunk not found in DB (sessionID=%s trunkID=%d)", sessionID, trunkID)
			return
		}

		if trunk.NotifyUserID == nil || *trunk.NotifyUserID == "" {
			log.Printf("🔔 [Push] Skip incoming call push: notify_user_id is empty (sessionID=%s trunkID=%d)", sessionID, trunkID)
			return
		}

		log.Printf("🔔 [Push] Dispatch incoming call push: userID=%s sessionID=%s trunkID=%d", *trunk.NotifyUserID, sessionID, trunkID)
		s.pushService.NotifyIncomingCall(*trunk.NotifyUserID, sessionID, from, to)
	}()
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

	// First-accept-wins: Try to claim the incoming call
	clientID := fmt.Sprintf("%p", client) // Use client pointer as unique ID
	if !incomingSess.TryClaimIncoming(clientID) {
		// Already claimed by another client
		log.Printf("⚠️ [Accept] Session %s already claimed by another client", msg.SessionID)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: incomingSess.ID,
			Category:  "ws",
			Name:      "ws_incoming_action_result",
			Data: map[string]interface{}{
				"incomingAction": "sending_accept",
				"result":         "already_claimed",
				"sessionId":      msg.SessionID,
			},
		})
		s.sendWSError(client, msg.SessionID, "Call already accepted by another client")
		return
	}

	log.Printf("✅ [Accept] Session %s claimed by client %s", msg.SessionID, clientID)

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
	} else {
		log.Printf("⚠️ [Accept] No dedicated WebRTC session ready for incoming session %s (client.sessionID=%s). Proceeding with incoming session may result in one-way media until browser offer/answer completes.", msg.SessionID, client.sessionID)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: incomingSess.ID,
			Category:  "ws",
			Name:      "ws_accept_without_webrtc_session",
			Data: map[string]interface{}{
				"incomingSessionId": msg.SessionID,
				"clientSessionId":   client.sessionID,
			},
		})
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
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: callSession.ID,
			Category:  "ws",
			Name:      "ws_incoming_action_result",
			Data: map[string]interface{}{
				"incomingAction": "sending_accept",
				"result":         "accepted_with_warning",
				"sessionId":      callSession.ID,
			},
		})
	} else {
		log.Printf("✅ Call accepted, using session: %s", callSession.ID)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: callSession.ID,
			Category:  "ws",
			Name:      "ws_incoming_action_result",
			Data: map[string]interface{}{
				"incomingAction": "sending_accept",
				"result":         "accepted",
				"sessionId":      callSession.ID,
			},
		})
	}
}

func isBenignIncomingRejectError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "transaction terminated")
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
		Data: map[string]interface{}{
			"incomingAction": "sending_reject",
			"reason":         msg.Reason,
		},
	})

	reason := msg.Reason
	if reason == "" {
		reason = "busy" // Default to busy
	}

	// Decrement public account refcount if applicable (before deleting session)
	authMode, accountKey, _, _, _, _, _ := sess.GetSIPAuthContext()
	if authMode == "public" && accountKey != "" && s.publicRegistry != nil {
		s.publicRegistry.DecrementRefCount(accountKey)
	}

	// Reject the incoming call
	if s.sipMaker != nil {
		if err := s.sipMaker.RejectCall(sess, reason); err != nil {
			if isBenignIncomingRejectError(err) {
				log.Printf("⚠️ [Reject] Incoming reject reached terminated transaction, treating as ended (session=%s): %v", msg.SessionID, err)
				s.logEvent(&logstore.Event{
					Timestamp: time.Now(),
					SessionID: sess.ID,
					Category:  "ws",
					Name:      "ws_incoming_action_result",
					Data: map[string]interface{}{
						"incomingAction": "sending_reject",
						"reason":         reason,
						"result":         "already_terminated",
						"sessionId":      msg.SessionID,
					},
				})
			} else {
				s.logEvent(&logstore.Event{
					Timestamp: time.Now(),
					SessionID: sess.ID,
					Category:  "ws",
					Name:      "ws_incoming_action_result",
					Data: map[string]interface{}{
						"incomingAction": "sending_reject",
						"reason":         reason,
						"result":         "failed",
						"sessionId":      msg.SessionID,
						"error":          err.Error(),
					},
				})
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to reject call: %v", err))
				return
			}
		} else {
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "ws",
				Name:      "ws_incoming_action_result",
				Data: map[string]interface{}{
					"incomingAction": "sending_reject",
					"reason":         reason,
					"result":         "rejected",
					"sessionId":      msg.SessionID,
				},
			})
		}
	}

	// Ensure terminal state is reflected for clients even if SIP transaction already ended.
	sess.UpdateState(session.StateEnded)

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

// handleWSRequestKeyframe handles explicit keyframe requests from clients.
// Used by resume/video-recovery flows to trigger fast FIR/PLI toward SIP side.
func (s *Server) handleWSRequestKeyframe(client *WSClient, msg WSMessage) {
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = client.sessionID
	}
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required for keyframe request")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.sendWSError(client, sessionID, "Session not found")
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_request_keyframe",
	})

	if s.config.DebugWebSocket {
		log.Printf("📸 Keyframe request received: session=%s", sessionID)
	}

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	log.Printf("📈 request_keyframe_handled session=%s action=%s", sessionID, action)
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
	resumeStartedAt := time.Now()
	defer func() {
		elapsed := time.Since(resumeStartedAt)
		if elapsed > resumeSlowLogThreshold {
			log.Printf("⚠️ Slow resume request: session=%s elapsed=%s hasSDP=%v", msg.SessionID, elapsed.Round(10*time.Millisecond), msg.SDP != "")
		}
	}()

	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required for resume")
		return
	}

	log.Printf("🔄 Resume request for session: %s (has SDP: %v)", msg.SessionID, msg.SDP != "")
	log.Printf("📊 Resume timing start: session=%s", msg.SessionID)

	localLookupStartedAt := time.Now()
	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	localLookupElapsed := time.Since(localLookupStartedAt).Round(10 * time.Millisecond)
	log.Printf("📊 Resume local session lookup: session=%s found=%v elapsed=%s", msg.SessionID, ok, localLookupElapsed)

	// Local-first: only hit directory when local session is missing.
	if !ok && s.logStore != nil && s.gatewayConfig.InstanceID != "" {
		dirLookupStartedAt := time.Now()
		lookupCtx, cancelLookup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelLookup()

		ownerInstanceID, wsURL, found, err := s.logStore.LookupSessionDirectory(lookupCtx, msg.SessionID)
		dirLookupElapsed := time.Since(dirLookupStartedAt).Round(10 * time.Millisecond)
		if err != nil {
			log.Printf("⚠️ Resume directory lookup failed for %s: %v (elapsed=%s)", msg.SessionID, err, dirLookupElapsed)
		} else if found && ownerInstanceID != s.gatewayConfig.InstanceID {
			log.Printf("🔀 Session %s is owned by instance %s, redirecting to %s (lookup_elapsed=%s)", msg.SessionID, ownerInstanceID, wsURL, dirLookupElapsed)
			response := WSMessage{
				Type:        "resume_redirect",
				SessionID:   msg.SessionID,
				RedirectURL: wsURL,
			}
			s.sendWSMessage(client, response)
			return
		} else {
			log.Printf("📊 Resume directory lookup: session=%s found=%v owner=%s elapsed=%s", msg.SessionID, found, ownerInstanceID, dirLookupElapsed)
		}

		// Re-check local session once after directory lookup in case of race with in-memory restore.
		sess, ok = s.sessionMgr.GetSession(msg.SessionID)
		log.Printf("📊 Resume local session recheck: session=%s found=%v", msg.SessionID, ok)
	}

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

	mediaStatus := sess.GetMediaEndpointStatus()
	log.Printf("🔍 Resume media status for %s: audioRTP=%v:%d videoRTP=%v:%d audioRTCP=%v:%d videoRTCP=%v:%d hasAsteriskAudio=%v hasAsteriskVideo=%v",
		msg.SessionID,
		mediaStatus.AudioRTPReady,
		mediaStatus.AudioRTPPort,
		mediaStatus.VideoRTPReady,
		mediaStatus.VideoRTPPort,
		mediaStatus.AudioRTCPReady,
		mediaStatus.AudioRTCPPort,
		mediaStatus.VideoRTCPReady,
		mediaStatus.VideoRTCPPort,
		mediaStatus.HasAsteriskAudio,
		mediaStatus.HasAsteriskVideo,
	)

	if (mediaStatus.HasAsteriskAudio && !mediaStatus.AudioRTPReady) ||
		(mediaStatus.HasAsteriskVideo && !mediaStatus.VideoRTPReady) {
		reason := "Session media endpoints expired - cannot resume"
		log.Printf(
			"❌ Resume failed: session %s media endpoints unavailable (reason=%s audioRTP=%v:%d videoRTP=%v:%d audioRTCP=%v:%d videoRTCP=%v:%d hasAsteriskAudio=%v hasAsteriskVideo=%v)",
			msg.SessionID,
			reason,
			mediaStatus.AudioRTPReady,
			mediaStatus.AudioRTPPort,
			mediaStatus.VideoRTPReady,
			mediaStatus.VideoRTPPort,
			mediaStatus.AudioRTCPReady,
			mediaStatus.AudioRTCPPort,
			mediaStatus.VideoRTCPReady,
			mediaStatus.VideoRTCPPort,
			mediaStatus.HasAsteriskAudio,
			mediaStatus.HasAsteriskVideo,
		)
		response := WSMessage{
			Type:      "resume_failed",
			SessionID: msg.SessionID,
			Reason:    reason,
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

	// If session was in reconnecting state, transition back to active only after resume succeeds.
	wasReconnecting := state == session.StateReconnecting

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
		renegotiateStartedAt := time.Now()
		log.Printf("🔄 Renegotiating PeerConnection for session %s", msg.SessionID)
		videoDiag := analyzeResumeOfferVideoSDP(msg.SDP)
		log.Printf("🔍 Resume SDP video diagnostics: session=%s hasVideoMLine=%v videoPort=%d videoDirection=%s",
			msg.SessionID,
			videoDiag.HasVideoMLine,
			videoDiag.VideoPort,
			videoDiag.VideoDirection,
		)
		if videoDiag.HasVideoMLine && (videoDiag.VideoPort == 0 || videoDiag.VideoDirection == "recvonly" || videoDiag.VideoDirection == "inactive") {
			log.Printf("⚠️ Resume SDP indicates no client video uplink: session=%s videoPort=%d videoDirection=%s",
				msg.SessionID,
				videoDiag.VideoPort,
				videoDiag.VideoDirection,
			)
		}

		// Renegotiate with the new SDP offer
		if err := sess.RenegotiatePeerConnection(msg.SDP, s.turnConfig, s.config.DebugTURN); err != nil {
			log.Printf("❌ Resume renegotiation failed for session %s: %v (elapsed=%s)", msg.SessionID, err, time.Since(renegotiateStartedAt).Round(10*time.Millisecond))
			response := WSMessage{
				Type:      "resume_failed",
				SessionID: msg.SessionID,
				Reason:    fmt.Sprintf("Failed to renegotiate: %v", err),
			}
			s.sendWSMessage(client, response)
			return
		}
		log.Printf("📊 Resume renegotiation elapsed: session=%s elapsed=%s", msg.SessionID, time.Since(renegotiateStartedAt).Round(10*time.Millisecond))

		// Get the answer SDP to send back to client
		if sess.PeerConnection != nil && sess.PeerConnection.LocalDescription() != nil {
			answerSDP = sess.PeerConnection.LocalDescription().SDP
		}

		log.Printf("✅ Session %s PeerConnection renegotiated successfully", msg.SessionID)
	}

	finalState := sess.GetState()
	if wasReconnecting {
		sess.SetState(session.StateActive)
		finalState = session.StateActive
		log.Printf("✅ Session %s transitioned from reconnecting to active", msg.SessionID)
	}
	sess.StartVideoRecoveryBurst("ws-resume-success")
	direction, _, _, _ := sess.GetCallInfo()
	log.Printf("📊 Resume total elapsed: session=%s elapsed=%s", msg.SessionID, time.Since(resumeStartedAt).Round(10*time.Millisecond))
	log.Printf("✅ Session %s resumed successfully (state: %s, direction: %s, wasReconnecting: %v, hasSDP: %v)", msg.SessionID, finalState, direction, wasReconnecting, answerSDP != "")

	// Send success response with session details (and answer SDP if renegotiated)
	_, from, to, _ := sess.GetCallInfo()
	response := WSMessage{
		Type:      "resumed",
		SessionID: msg.SessionID,
		State:     string(finalState),
		From:      from,
		To:        to,
		SDP:       answerSDP,
	}
	resumeSendStartedAt := time.Now()
	s.sendWSMessage(client, response)
	log.Printf("📊 Resume send elapsed: session=%s elapsed=%s", msg.SessionID, time.Since(resumeSendStartedAt).Round(10*time.Millisecond))
}

// handleWSTrunkResolve resolves trunk ownership/route from either credentials or trunk ID/public ID.
func (s *Server) handleWSTrunkResolve(client *WSClient, msg WSMessage) {
	ctx := context.Background()
	trunkID := int64(0)
	trunkPublicID := ""
	var leaseOwner *string
	var leaseUntil *time.Time
	found := false

	if msg.TrunkID > 0 || strings.TrimSpace(msg.TrunkPublicID) != "" {
		if s.trunkManager == nil {
			s.sendWSError(client, msg.SessionID, "Trunk manager not available")
			return
		}

		if msg.TrunkID > 0 {
			trunkID = msg.TrunkID
		} else {
			publicID, ok := sip.NormalizeTrunkPublicID(msg.TrunkPublicID)
			if !ok {
				reason := "Invalid trunkPublicId"
				s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
				return
			}
			resolvedID, ok := s.trunkManager.GetTrunkIDByPublicID(publicID)
			if !ok {
				reason := "No matching trunk ID/public ID"
				s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
				return
			}
			trunkID = resolvedID
		}

		trunk, err := s.trunkManager.GetTrunkByIDFromDB(ctx, trunkID)
		if err != nil || trunk == nil {
			reason := "No matching trunk ID/public ID"
			s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
			return
		}

		found = true
		leaseOwner = trunk.LeaseOwner
		leaseUntil = trunk.LeaseUntil
		trunkPublicID = trunk.PublicID
	} else {
		if msg.SIPDomain == "" || msg.SIPUsername == "" || msg.SIPPassword == "" {
			s.sendWSError(client, msg.SessionID, "sipDomain, sipUsername, and sipPassword are required")
			return
		}
		// Use port as-is from client (0 means "not specified" for hostname domains)
		// This allows DNS SRV resolution for hostnames without explicit port
		port := msg.SIPPort

		if s.logStore == nil {
			s.sendWSError(client, msg.SessionID, "LogStore not available")
			return
		}

		var err error
		trunkID, leaseOwner, leaseUntil, found, err = s.logStore.ResolveTrunkByCredentials(ctx, msg.SIPDomain, port, msg.SIPUsername, msg.SIPPassword)
		if err != nil {
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to resolve trunk: %v", err))
			return
		}
		if !found {
			reason := "No matching trunk credentials"
			s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
			return
		}

		if s.trunkManager != nil {
			if trunk, getErr := s.trunkManager.GetTrunkByIDFromDB(ctx, trunkID); getErr == nil && trunk != nil {
				trunkPublicID = trunk.PublicID
			}
		}
	}

	if !found {
		reason := "No matching trunk"
		s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
		s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
		return
	}

	if leaseOwner == nil || *leaseOwner == "" || leaseUntil == nil || leaseUntil.Before(time.Now()) {
		s.sendWSMessage(client, WSMessage{Type: "trunk_not_ready", Reason: "Trunk lease not active"})
		return
	}

	if *leaseOwner == s.gatewayConfig.InstanceID {
		client.trunkResolved = true
		client.resolvedTrunkID = trunkID

		// Persist Keycloak sub (UUID) for offline push notifications.
		if client.authClaims != nil && s.trunkManager != nil {
			if sub := client.authClaims.Subject; sub != "" {
				if err := s.trunkManager.SetTrunkNotifyUserID(ctx, trunkID, &sub); err != nil {
					log.Printf("⚠️ Failed to set notify_user_id for trunk %d: %v", trunkID, err)
				}
			}
		}

		s.sendWSMessage(client, WSMessage{
			Type:          "trunk_resolved",
			TrunkID:       trunkID,
			TrunkPublicID: trunkPublicID,
		})
		s.notifyPendingIncomingForClient(client, trunkID)
		return
	}

	if s.logStore == nil {
		s.sendWSError(client, msg.SessionID, "LogStore not available")
		return
	}

	// Not owned by this instance - redirect to owner
	wsURL, found, err := s.logStore.LookupGatewayInstance(ctx, *leaseOwner)
	if err != nil {
		s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to lookup gateway instance: %v", err))
		return
	}
	if !found || wsURL == "" {
		s.sendWSMessage(client, WSMessage{Type: "trunk_not_ready", Reason: "Owner instance not discoverable"})
		return
	}

	s.sendWSMessage(client, WSMessage{Type: "trunk_redirect", RedirectURL: wsURL})
}

// notifyPendingIncomingForClient replays queued incoming calls to a specific client.
// This is used after trunk_resolve so UI can pick up incoming calls that arrived before resolve.
func (s *Server) notifyPendingIncomingForClient(client *WSClient, trunkID int64) {
	if s.sessionMgr == nil || client == nil {
		return
	}

	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil || sess.GetState() != session.StateIncoming {
			continue
		}
		authMode, _, sessTrunkID, _, _, _, _ := sess.GetSIPAuthContext()
		if authMode != "trunk" || sessTrunkID != trunkID {
			continue
		}
		_, from, to, _ := sess.GetCallInfo()
		s.sendWSMessage(client, WSMessage{
			Type:      "incoming",
			SessionID: sess.ID,
			From:      from,
			To:        to,
		})
	}
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
