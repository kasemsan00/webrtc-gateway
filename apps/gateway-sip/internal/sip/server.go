package sip

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
)

// StateNotifier interface for notifying session state changes
type StateNotifier interface {
	NotifySessionState(sessionID string, state session.SessionState)
}

// IncomingCallNotifier interface for notifying about incoming calls
type IncomingCallNotifier interface {
	NotifyIncomingCall(sessionID, from, to string)
}

// MessageNotifier interface for notifying about incoming SIP messages
type MessageNotifier interface {
	NotifySIPMessage(to, from, body, contentType string)
}

// SessionCreator interface for creating sessions from incoming calls
type SessionCreator interface {
	CreateSessionForIncoming(turnConfig config.TURNConfig) (*session.Session, error)
}

// SessionManager interface for finding sessions
type SessionManager interface {
	GetSessionBySIPCallID(callID string) (*session.Session, bool)
	GetSessionByFromUsername(username string) (*session.Session, bool) // For @switch message handling
	DeleteSession(id string)
}

// DTMFNotifier interface for notifying about received DTMF from SIP side
type DTMFNotifier interface {
	NotifyDTMF(sessionID, digit string)
}

// Server represents a SIP server
type Server struct {
	sipServer        *sipgo.Server
	sipClient        *sipgo.Client
	sipUserAgent     *sipgo.UserAgent
	config           config.SIPConfig
	rtpConfig        config.RTPConfig
	turnConfig       config.TURNConfig // For creating sessions for incoming calls
	audioTrack       *webrtc.TrackLocalStaticRTP
	unicastAddress   string
	publicAddress    string // Public IP address for NAT traversal
	sipPort          int
	sessionMgr       SessionManager       // For finding sessions by Call-ID
	sessionCreator   SessionCreator       // For creating sessions for incoming calls
	stateNotifier    StateNotifier        // For notifying WebSocket clients
	incomingNotifier IncomingCallNotifier // For notifying incoming calls
	messageNotifier  MessageNotifier      // For notifying incoming SIP messages
	dtmfNotifier     DTMFNotifier         // For notifying received DTMF
	logStore         logstore.LogStore
	logFullSIP       bool
	// Registration fields
	mu sync.RWMutex
	// SIP Public & Trunk management
	publicRegistry *PublicAccountRegistry
	trunkManager   *TrunkManager
	// Static registration state (ensures single refresh goroutine)
	regState *registrationState
}

// NewServer creates a new SIP server
func NewServer(cfg config.SIPConfig, rtpCfg config.RTPConfig, audioTrack *webrtc.TrackLocalStaticRTP, unicastAddress string, sipPort int) (*Server, error) {
	// Create SIP user agent
	sipUserAgent, err := sipgo.NewUA()
	if err != nil {
		return nil, fmt.Errorf("failed to create SIP user agent: %w", err)
	}

	// Create SIP server
	sipServer, err := sipgo.NewServer(sipUserAgent)
	if err != nil {
		return nil, fmt.Errorf("failed to create SIP server: %w", err)
	}

	// Determine public address (use configured public IP or fall back to unicast address)
	publicAddr := unicastAddress
	if cfg.PublicIP != "" {
		publicAddr = cfg.PublicIP
		fmt.Printf("Using configured Public IP: %s for SIP/SDP\n", publicAddr)
	} else {
		fmt.Printf("No Public IP configured, using local address: %s\n", publicAddr)
	}

	server := &Server{
		sipServer:      sipServer,
		sipUserAgent:   sipUserAgent,
		config:         cfg,
		rtpConfig:      rtpCfg,
		audioTrack:     audioTrack,
		unicastAddress: unicastAddress,
		publicAddress:  publicAddr,
		sipPort:        sipPort,
		regState:       &registrationState{},
	}

	// Setup SIP handlers
	server.setupHandlers()

	return server, nil
}

// SetSessionManager sets the session manager for the server
func (s *Server) SetSessionManager(mgr SessionManager) {
	s.sessionMgr = mgr
}

// SetStateNotifier sets the state notifier for the server
func (s *Server) SetStateNotifier(notifier StateNotifier) {
	s.stateNotifier = notifier
}

func (s *Server) notifySessionStateChange(sess *session.Session, state session.SessionState) {
	if sess == nil || s.stateNotifier == nil {
		return
	}
	if state != session.StateActive && state != session.StateEnded {
		return
	}
	s.stateNotifier.NotifySessionState(sess.ID, state)
}

// SetSessionCreator sets the session creator for incoming calls
func (s *Server) SetSessionCreator(creator SessionCreator) {
	s.sessionCreator = creator
}

// SetIncomingCallNotifier sets the notifier for incoming calls
func (s *Server) SetIncomingCallNotifier(notifier IncomingCallNotifier) {
	s.incomingNotifier = notifier
}

// SetMessageNotifier sets the notifier for incoming SIP messages
func (s *Server) SetMessageNotifier(notifier MessageNotifier) {
	s.messageNotifier = notifier
}

// SetDTMFNotifier sets the notifier for received DTMF from SIP side
func (s *Server) SetDTMFNotifier(notifier DTMFNotifier) {
	s.dtmfNotifier = notifier
}

// SetLogStore sets the log store for database logging.
func (s *Server) SetLogStore(store logstore.LogStore) {
	s.logStore = store
}

// SetLogFullSIP controls whether to store full SIP messages in payload logs.
func (s *Server) SetLogFullSIP(enabled bool) {
	s.logFullSIP = enabled
}

// SetTURNConfig sets the TURN configuration for creating sessions
func (s *Server) SetTURNConfig(cfg config.TURNConfig) {
	s.turnConfig = cfg
}

// SetPublicAccountRegistry sets the public account registry
func (s *Server) SetPublicAccountRegistry(registry *PublicAccountRegistry) {
	s.publicRegistry = registry
}

// SetTrunkManager sets the trunk manager
func (s *Server) SetTrunkManager(mgr *TrunkManager) {
	s.trunkManager = mgr
}

// GetUserAgent returns the SIP user agent (for PublicAccountRegistry and TrunkManager)
func (s *Server) GetUserAgent() *sipgo.UserAgent {
	return s.sipUserAgent
}

// GetPublicAddress returns the public address
func (s *Server) GetPublicAddress() string {
	return s.publicAddress
}

// GetSIPPort returns the SIP port
func (s *Server) GetSIPPort() int {
	return s.sipPort
}

// InitializeAndRegisterSIPServer creates, starts, and registers SIP server
// This consolidates initialization logic from runLegacyMode and runAPIMode
func (s *Server) InitializeAndRegisterSIPServer(ctx context.Context) error {
	// Start SIP server listener in background (required for receiving responses)
	go func() {
		if err := s.Start(ctx); err != nil {
			fmt.Printf("SIP server error: %v\n", err)
		}
	}()

	// Wait a moment for the listener to start
	time.Sleep(500 * time.Millisecond)

	// Perform SIP registration automatically (after listener is started)
	if err := s.Register(ctx); err != nil {
		return fmt.Errorf("SIP registration failed: %w", err)
	}

	return nil
}

// Start starts SIP listeners based on configured transports.
// TCP is enabled by default; UDP can be enabled for providers/clients that route INVITE over UDP.
func (s *Server) Start(ctx context.Context) error {
	fmt.Println("Starting SIP Listener")

	// Use LocalIP from config to bind listeners
	// If set to specific IPv4 address, prevents IPv6 Via headers
	listenAddr := fmt.Sprintf("%s:%d", s.config.LocalIP, s.sipPort)
	listenTCP := s.config.ListenTCP
	listenUDP := s.config.ListenUDP

	if !listenTCP && !listenUDP {
		return fmt.Errorf("SIP server error: both TCP and UDP listeners are disabled")
	}

	errCh := make(chan error, 2)

	if listenUDP {
		go func() {
			fmt.Printf("Starting UDP listener on %s\n", listenAddr)
			if err := s.sipServer.ListenAndServe(ctx, "udp", listenAddr); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("SIP UDP server error: %w", err)
			}
		}()
	}

	if listenTCP {
		go func() {
			fmt.Printf("Starting TCP listener on %s\n", listenAddr)
			if err := s.sipServer.ListenAndServe(ctx, "tcp", listenAddr); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("SIP TCP server error: %w", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}
