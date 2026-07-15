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

	"k2-gateway/internal/auth"
	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/push"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
	"k2-gateway/internal/translator"
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
	runtimeConfig     *config.Config
	upgrader          websocket.Upgrader
	wsClients         map[string]*WSClient
	wsConnections     map[*WSClient]struct{}
	trunkStreams      map[int]chan []byte
	trunkStreamSeq    int
	sessionStreams    map[int]chan []byte
	sessionStreamSeq  int
	wsClientStreams   map[int]chan []byte
	wsClientStreamSeq int
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
	clientID        string // UUID assigned at connect, stable across session assignment
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
		wsClientStreams:  make(map[int]chan []byte),
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

// SetRuntimeConfig exposes the loaded gateway configuration for read-only inspection.
func (s *Server) SetRuntimeConfig(cfg *config.Config) {
	s.runtimeConfig = cfg
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
		router.HandleFunc("/api/client-diagnostics", s.handleListClientDiagnostics).Methods("GET", "OPTIONS")
		router.HandleFunc("/api/client-diagnostics/sessions/{sessionId}/events", s.handleListClientDiagnosticSessionEvents).Methods("GET", "OPTIONS")
		router.HandleFunc("/api/client-diagnostics/sessions/{sessionId}/payloads", s.handleListClientDiagnosticSessionPayloads).Methods("GET", "OPTIONS")
		router.HandleFunc("/api/client-diagnostics/payloads/{payloadId}", s.handleGetClientDiagnosticPayload).Methods("GET", "OPTIONS")

		api := router.PathPrefix("/api").Subrouter()
		if s.tokenVerifier != nil {
			api.Use(s.authMiddleware)
		}
		api.HandleFunc("/logs", s.handleListLogFiles).Methods("GET", "OPTIONS")
		api.HandleFunc("/logs/current", s.handleGetCurrentLog).Methods("GET", "OPTIONS")
		api.HandleFunc("/logs/{name}", s.handleGetLogFile).Methods("GET", "OPTIONS")
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
		api.HandleFunc("/ws-clients/stream", s.handleWSClientsStream).Methods("GET", "OPTIONS")
		api.HandleFunc("/dashboard", s.handleDashboard).Methods("GET", "OPTIONS")
		api.HandleFunc("/dashboard/summary", s.handleDashboardSummary).Methods("GET", "OPTIONS")
		api.HandleFunc("/config", s.handleGetConfig).Methods("GET", "OPTIONS")
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
