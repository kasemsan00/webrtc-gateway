package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"k2-gateway/internal/auth"
	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/push"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
	"k2-gateway/internal/translator"
)

const (
	defaultMidCallRenegotiationTimeout = 10 * time.Second
)

// Server represents the HTTP/WebSocket API server
type Server struct {
	sessionMgr        *session.Manager
	sipMaker          SIPCallMaker
	tokenVerifier     TokenVerifier
	publicRegistry    PublicAccountRegistry
	trunkManager      TrunkManager
	logStore          logstore.LogStore
	pushService       *push.Service
	mobileProvisioner MobileSIPProvisioner
	config            config.APIConfig
	turnConfig        config.TURNConfig
	gatewayConfig     config.GatewayConfig
	translatorCfg     config.TranslatorConfig
	translatorClient  *translator.Client
	upgrader          websocket.Upgrader
	wsClients         map[string]*WSClient
	wsConnections     map[*WSClient]struct{}
	trunkStreams      map[int]chan []byte
	trunkStreamSeq    int
	sessionStreams    map[int]chan []byte
	sessionStreamSeq  int
	incomingCounters  map[string]int64
	diagnosticLimits  map[string]*diagnosticRateState
	startTime         time.Time
	mu                sync.RWMutex
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
	SetTrunkNotifyUserIDAndPlatform(ctx context.Context, trunkID int64, userID *string, platform *string) error
	SetTrunkPushContact(ctx context.Context, trunkID int64, contact sip.TrunkPushContact) (bool, error)
}

// SIPCallMaker interface for making SIP calls (implemented by SIP server)
type SIPCallMaker interface {
	MakeCall(destination, from string, sess *session.Session) error
	CancelPendingCall(sess *session.Session) error
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
	availability    string
	callState       string
	send            chan []byte
	ConnectedAt     time.Time
	authClaims      *auth.VerifiedClaims // populated when tokenVerifier is set
	publicOnly      bool                 // true for unauthenticated /ws-public clients
}

// WSMessage represents a WebSocket message
type WSMessage struct {
	Type         string          `json:"type"`
	SessionID    string          `json:"sessionId,omitempty"`
	SDP          string          `json:"sdp,omitempty"`
	Candidate    json.RawMessage `json:"candidate,omitempty"`
	Destination  string          `json:"destination,omitempty"`
	From         string          `json:"from,omitempty"`
	To           string          `json:"to,omitempty"`
	Digits       string          `json:"digits,omitempty"`
	State        string          `json:"state,omitempty"`
	Availability string          `json:"availability,omitempty"`
	CallState    string          `json:"callState,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	ReasonSource string          `json:"reasonSource,omitempty"`
	Error        string          `json:"error,omitempty"`
	HasVideo     string          `json:"hasVideo,omitempty"`
	// Mid-call renegotiation fields. Additive for clients that support
	// renegotiate, renegotiate_answer, and renegotiate_result messages.
	RenegotiationID string `json:"renegotiationId,omitempty"`
	MediaDirection  string `json:"mediaDirection,omitempty"`
	RequiresAnswer  bool   `json:"requiresAnswer,omitempty"`
	Status          string `json:"status,omitempty"`
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
	TrunkID        int64  `json:"trunkId,omitempty"`       // Use trunk from DB (0 = not specified)
	TrunkPublicID  string `json:"trunkPublicId,omitempty"` // Stable public trunk reference
	DevicePlatform string `json:"devicePlatform,omitempty"`
	PNAppID        string `json:"pnAppId,omitempty"` // SIP Contact push app-id
	PNType         string `json:"pnType,omitempty"`  // SIP Contact push pn-type
	PNToken        string `json:"pnToken,omitempty"` // SIP Contact push pn-tok
	// S2S Translation fields
	SourceLang     string `json:"sourceLang,omitempty"`
	TargetLang     string `json:"targetLang,omitempty"`
	TTSVoice       string `json:"ttsVoice,omitempty"`
	Direction      string `json:"direction,omitempty"`
	RecognizedText string `json:"recognizedText,omitempty"`
	TranslatedText string `json:"translatedText,omitempty"`
	IsFinal        *bool  `json:"isFinal,omitempty"`
	// Session resume redirect
	RedirectURL string `json:"redirectUrl,omitempty"` // Server response: resume_redirect with new WS URL
}

// NewServer creates a new API server
func NewServer(cfg config.APIConfig, turnCfg config.TURNConfig, gatewayCfg config.GatewayConfig, translatorCfg config.TranslatorConfig, sessionMgr *session.Manager, sipMaker SIPCallMaker, publicRegistry PublicAccountRegistry, trunkMgr TrunkManager, logStore logstore.LogStore) *Server {
	cfg.TrunkPNAppID = normalizeTrunkPNAppID(cfg.TrunkPNAppID)
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
		wsClients:        make(map[string]*WSClient),
		wsConnections:    make(map[*WSClient]struct{}),
		trunkStreams:     make(map[int]chan []byte),
		sessionStreams:   make(map[int]chan []byte),
		incomingCounters: make(map[string]int64),
		diagnosticLimits: make(map[string]*diagnosticRateState),
		startTime:        time.Now(),
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

// SetMobileSIPProvisioner enables mobile SIP trunk provisioning during WebSocket auth.
func (s *Server) SetMobileSIPProvisioner(provisioner MobileSIPProvisioner) {
	s.mobileProvisioner = provisioner
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
	if s.config.EnablePublicWS {
		router.HandleFunc("/ws-public", s.handlePublicWebSocket)
		fmt.Printf("Public WebSocket endpoint enabled: /ws-public\n")
	}

	// REST API endpoints
	if s.config.EnableREST {
		router.HandleFunc("/api/logs", s.handleListLogFiles).Methods("GET", "OPTIONS")
		router.HandleFunc("/api/logs/current", s.handleGetCurrentLog).Methods("GET", "OPTIONS")
		router.HandleFunc("/api/logs/{name}", s.handleGetLogFile).Methods("GET", "OPTIONS")
		router.HandleFunc("/api/client-diagnostics", s.handleListClientDiagnostics).Methods("GET", "OPTIONS")
		router.HandleFunc("/api/client-diagnostics/sessions/{sessionId}/events", s.handleListClientDiagnosticSessionEvents).Methods("GET", "OPTIONS")
		router.HandleFunc("/api/client-diagnostics/sessions/{sessionId}/payloads", s.handleListClientDiagnosticSessionPayloads).Methods("GET", "OPTIONS")
		router.HandleFunc("/api/client-diagnostics/payloads/{payloadId}", s.handleGetClientDiagnosticPayload).Methods("GET", "OPTIONS")

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
		api.HandleFunc("/client-diagnostics", s.handleClientDiagnostics).Methods("POST", "OPTIONS")
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

func (s *Server) handleWSRenegotiateAnswer(client *WSClient, msg WSMessage) {
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = client.sessionID
	}
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required for renegotiation answer")
		return
	}
	if msg.RenegotiationID == "" {
		s.sendWSError(client, sessionID, "Renegotiation ID required")
		return
	}
	if s.sessionMgr == nil {
		s.sendWSError(client, sessionID, "Session manager not available")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok || sess == nil {
		s.sendWSError(client, sessionID, "Session not found")
		return
	}

	status := strings.ToLower(strings.TrimSpace(msg.Status))
	if status == "" {
		status = "ok"
	}

	var completed bool
	switch status {
	case "ok":
		completed = sess.CompleteMidCallRenegotiation(msg.RenegotiationID, msg.SDP)
	case "failed":
		completed = sess.FailMidCallRenegotiation(msg.RenegotiationID, 488, msg.Reason)
	default:
		s.sendWSError(client, sessionID, "Unsupported renegotiation answer status")
		return
	}
	if !completed {
		s.sendWSError(client, sessionID, "Renegotiation not pending or mismatched")
		return
	}

	resultStatus := "ok"
	if status == "failed" {
		resultStatus = "failed"
	}
	s.sendWSMessage(client, WSMessage{
		Type:            "renegotiate_result",
		SessionID:       sessionID,
		RenegotiationID: msg.RenegotiationID,
		Status:          resultStatus,
		Reason:          msg.Reason,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Category:  "ws",
		Name:      "ws_midcall_renegotiation_answer",
		Data: map[string]interface{}{
			"renegotiationId": msg.RenegotiationID,
			"status":          resultStatus,
			"reason":          msg.Reason,
		},
	})
}

// sendWSMessage sends a message to a WebSocket client
func (s *Server) handleTranslationCaption(sessionID string, event session.TranslationCaptionEvent) {
	if sessionID == "" || event.Direction != "sip_to_webrtc" {
		return
	}

	s.mu.RLock()
	client := s.wsClients[sessionID]
	s.mu.RUnlock()
	if client == nil {
		log.Printf("[%s] Translation caption dropped: no WebSocket client", sessionID)
		return
	}

	isFinal := event.IsFinal
	s.sendWSMessage(client, WSMessage{
		Type:           "translation_caption",
		SessionID:      sessionID,
		Direction:      event.Direction,
		SourceLang:     event.SourceLang,
		TargetLang:     event.TargetLang,
		RecognizedText: event.RecognizedText,
		TranslatedText: event.TranslatedText,
		IsFinal:        &isFinal,
	})
}

func (s *Server) scheduleMidCallRenegotiationTimeout(sessionID, renegotiationID string, timeout time.Duration) {
	if timeout <= 0 {
		return
	}

	time.AfterFunc(timeout, func() {
		if s.sessionMgr == nil {
			return
		}
		sess, ok := s.sessionMgr.GetSession(sessionID)
		if !ok || sess == nil {
			return
		}
		if !sess.FailMidCallRenegotiation(renegotiationID, 408, "client_timeout") {
			return
		}

		s.mu.RLock()
		client := s.wsClients[sessionID]
		s.mu.RUnlock()
		if client == nil {
			return
		}
		s.sendWSMessage(client, WSMessage{
			Type:            "renegotiate_result",
			SessionID:       sessionID,
			RenegotiationID: renegotiationID,
			Status:          "timeout",
			Reason:          "client_timeout",
		})
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sessionID,
			Category:  "ws",
			Name:      "ws_midcall_renegotiation_timeout",
			Data: map[string]interface{}{
				"renegotiationId": renegotiationID,
				"status":          "timeout",
				"reason":          "client_timeout",
			},
		})
	})
}

func (s *Server) handleWSClientState(client *WSClient, msg WSMessage) {
	availability := normalizeClientAvailability(msg.Availability)
	callState := normalizeClientCallState(msg.CallState)

	s.mu.Lock()
	client.availability = availability
	client.callState = callState
	s.mu.Unlock()

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: msg.SessionID,
		Category:  "ws",
		Name:      "ws_client_state",
		Data: map[string]interface{}{
			"availability": availability,
			"callState":    callState,
			"sessionId":    msg.SessionID,
		},
	})
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

const (
	trunkPNType = "apple"
)

func normalizeTrunkPNAppID(appID string) string {
	value := strings.TrimSpace(appID)
	if value == "" {
		return config.DefaultTrunkPNAppID
	}
	return value
}

func hasTrunkPushContact(msg WSMessage) bool {
	return strings.TrimSpace(msg.PNAppID) != "" ||
		strings.TrimSpace(msg.PNType) != "" ||
		strings.TrimSpace(msg.PNToken) != ""
}

func validateTrunkPushContact(msg WSMessage, expectedAppID string) (sip.TrunkPushContact, error) {
	contact := sip.TrunkPushContact{
		PNAppID: strings.TrimSpace(msg.PNAppID),
		PNType:  strings.TrimSpace(msg.PNType),
		PNToken: strings.TrimSpace(msg.PNToken),
	}
	expectedAppID = normalizeTrunkPNAppID(expectedAppID)
	if contact.PNAppID != expectedAppID {
		return contact, fmt.Errorf("pnAppId must be %s", expectedAppID)
	}
	if contact.PNType != trunkPNType {
		return contact, fmt.Errorf("pnType must be %s", trunkPNType)
	}
	if len(contact.PNToken) < 32 || len(contact.PNToken) > 256 {
		return contact, fmt.Errorf("pnToken length is invalid")
	}
	for _, r := range contact.PNToken {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return contact, fmt.Errorf("pnToken must be hex")
		}
	}
	return contact, nil
}

func (s *Server) updateTrunkPushContact(ctx context.Context, client *WSClient, trunkID int64, msg WSMessage) error {
	if s.trunkManager == nil {
		return fmt.Errorf("trunk manager not available")
	}
	if client == nil || client.authClaims == nil || strings.TrimSpace(client.authClaims.Subject) == "" {
		return fmt.Errorf("authenticated client required for trunk push token update")
	}

	contact, err := validateTrunkPushContact(msg, s.config.TrunkPNAppID)
	if err != nil {
		return err
	}
	changed, err := s.trunkManager.SetTrunkPushContact(ctx, trunkID, contact)
	if err != nil {
		return err
	}
	if !changed {
		log.Printf("📲 Trunk push contact unchanged: trunkID=%d appID=%s pnType=%s action=unchanged_skip_reregister", trunkID, contact.PNAppID, contact.PNType)
		return nil
	}
	if err := s.trunkManager.RegisterTrunk(trunkID, true); err != nil {
		return fmt.Errorf("trunk push token persisted but re-register failed: %w", err)
	}
	log.Printf("📲 Trunk push contact updated: trunkID=%d appID=%s pnType=%s action=updated_and_reregistered", trunkID, contact.PNAppID, contact.PNType)
	return nil
}

func (s *Server) handleWSTrunkPushToken(client *WSClient, msg WSMessage) {
	ctx := context.Background()
	if _, ok := normalizeDevicePlatform(msg.DevicePlatform); !ok {
		s.sendWSError(client, msg.SessionID, "Invalid devicePlatform")
		return
	}
	if s.trunkManager == nil {
		s.sendWSError(client, msg.SessionID, "Trunk manager not available")
		return
	}
	if !client.trunkResolved || client.resolvedTrunkID <= 0 {
		s.sendWSError(client, msg.SessionID, "Trunk must be resolved before updating push token")
		return
	}

	targetTrunkID := msg.TrunkID
	if targetTrunkID == 0 && strings.TrimSpace(msg.TrunkPublicID) != "" {
		publicID, ok := sip.NormalizeTrunkPublicID(msg.TrunkPublicID)
		if !ok {
			s.sendWSError(client, msg.SessionID, "Invalid trunkPublicId")
			return
		}
		resolvedID, ok := s.trunkManager.GetTrunkIDByPublicID(publicID)
		if !ok {
			s.sendWSError(client, msg.SessionID, "Trunk not found")
			return
		}
		targetTrunkID = resolvedID
	}
	if targetTrunkID == 0 {
		targetTrunkID = client.resolvedTrunkID
	}
	if targetTrunkID != client.resolvedTrunkID {
		s.sendWSError(client, msg.SessionID, "Push token trunk mismatch")
		return
	}

	if err := s.updateTrunkPushContact(ctx, client, targetTrunkID, msg); err != nil {
		s.sendWSError(client, msg.SessionID, err.Error())
		return
	}
}

// handleWSTrunkResolve resolves trunk ownership/route from either credentials or trunk ID/public ID.
func (s *Server) handleWSTrunkResolve(client *WSClient, msg WSMessage) {
	ctx := context.Background()
	devicePlatform, ok := normalizeDevicePlatform(msg.DevicePlatform)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Invalid devicePlatform")
		return
	}
	trunkID := int64(0)
	trunkPublicID := ""
	sipUsername := strings.TrimSpace(msg.SIPUsername)
	resolveBy := "credentials"
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
			resolveBy = "trunkId"
		} else {
			resolveBy = "trunkPublicId"
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
		sipUsername = strings.TrimSpace(trunk.Username)
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
				sipUsername = strings.TrimSpace(trunk.Username)
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
				preferredUsername := strings.TrimSpace(client.authClaims.PreferredUsername)
				var platformPtr *string
				if devicePlatform != "" {
					platformPtr = &devicePlatform
				}
				if err := s.trunkManager.SetTrunkNotifyUserIDAndPlatform(ctx, trunkID, &sub, platformPtr); err != nil {
					log.Printf(
						"⚠️ [Trunk NotifyUserID] update_failed sessionID=%s trunkID=%d trunkPublicID=%s preferredUsername=%s sipUsername=%s authSub=%s error=%v",
						msg.SessionID,
						trunkID,
						trunkPublicID,
						preferredUsername,
						sipUsername,
						sub,
						err,
					)
				} else {
					log.Printf(
						"📝 [Trunk NotifyUserID] updated sessionID=%s trunkID=%d trunkPublicID=%s preferredUsername=%s sipUsername=%s authSub=%s",
						msg.SessionID,
						trunkID,
						trunkPublicID,
						preferredUsername,
						sipUsername,
						sub,
					)
				}
			}
		}
		if hasTrunkPushContact(msg) {
			if err := s.updateTrunkPushContact(ctx, client, trunkID, msg); err != nil {
				log.Printf("⚠️ Failed to update push contact for trunk %d: %v", trunkID, err)
				s.sendWSError(client, msg.SessionID, err.Error())
			}
		}
		if client.authClaims != nil &&
			client.authClaims.Realm == auth.TokenRealmUser &&
			strings.TrimSpace(client.authClaims.Subject) != "" {
			log.Printf(
				"📱 [WS Trunk Resolve] mobile_resolved sessionID=%s trunkID=%d trunkPublicID=%s authSub=%s resolveBy=%s",
				msg.SessionID,
				trunkID,
				trunkPublicID,
				client.authClaims.Subject,
				resolveBy,
			)
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
