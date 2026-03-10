// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js

// sip demonstrates how to bridge SIP traffic and WebRTC
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"k2-gateway/internal/api"
	"k2-gateway/internal/config"
	"k2-gateway/internal/logger"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
	"k2-gateway/internal/webrtc"
)

var (
	unicastAddress = flag.String("unicast-address", "", "IP of SIP Server (your public IP)")
	sipPort        = flag.Int("sip-port", 5060, "Port to listen for SIP Traffic")
	legacyMode     = flag.Bool("legacy", false, "Run in legacy mode (stdin/stdout signaling)")
)

func main() {
	// Parse command line flags
	flag.Parse()

	// Initialize logger (captures all fmt.Printf and log.Printf output)
	cleanup, err := logger.InitDefault()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer cleanup()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Display configuration
	cfg.Display()

	// Override LocalPort if flag is set to non-default value
	if *sipPort != 5060 {
		fmt.Printf("Overriding SIP Local Port with flag: %d\n", *sipPort)
		cfg.SIP.LocalPort = *sipPort
	}

	// Determine unicast address
	unicast, err := webrtc.GetUnicastAddress(*unicastAddress)
	if err != nil {
		log.Fatalf("Failed to get unicast address: %v", err)
	}
	*unicastAddress = unicast

	// Create context for the application
	ctx := context.Background()

	// Initialize LogStore (DB logging)
	store, err := logstore.New(cfg.DB)
	if err != nil {
		log.Fatalf("Failed to initialize log store: %v", err)
	}
	if err := store.Start(ctx); err != nil {
		log.Fatalf("Failed to start log store: %v", err)
	}
	defer store.Stop()

	if *legacyMode {
		// Legacy mode: single session with stdin/stdout signaling
		runLegacyMode(ctx, cfg, unicast, store)
	} else {
		// New mode: HTTP/WebSocket API with multiple sessions
		runAPIMode(ctx, cfg, unicast, store)
	}
}

// runLegacyMode runs the gateway in legacy stdin/stdout mode
func runLegacyMode(ctx context.Context, cfg *config.Config, unicastAddress string, store logstore.LogStore) {
	fmt.Println("\n=== Running in Legacy Mode (stdin/stdout) ===")

	// Initialize WebRTC gateway (but don't negotiate yet)
	gateway, err := webrtc.NewGateway(cfg.TURN, unicastAddress)
	if err != nil {
		log.Fatalf("Failed to create WebRTC gateway: %v", err)
	}

	// Setup audio track
	if err := gateway.SetupAudioTrack(); err != nil {
		log.Fatalf("Failed to setup audio track: %v", err)
	}

	// Initialize SIP server
	sipServer, err := sip.NewServer(cfg.SIP, cfg.RTP, gateway.AudioTrack, unicastAddress, cfg.SIP.LocalPort)
	if err != nil {
		log.Fatalf("Failed to create SIP server: %v", err)
	}
	sipServer.SetLogStore(store)
	sipServer.SetLogFullSIP(cfg.DB.LogFullSIP)

	// Create SIP client if configured
	if err := sipServer.CreateClient(); err != nil {
		log.Printf("Warning: %v", err)
	}

	// Start SIP server and register (consolidated initialization)
	if err := sipServer.InitializeAndRegisterSIPServer(ctx); err != nil {
		log.Printf("Warning: %v", err)
	}

	// Handle WebRTC negotiation (waits for offer from stdin)
	if err := gateway.HandleNegotiation(); err != nil {
		log.Fatalf("Failed to handle WebRTC negotiation: %v", err)
	}

	// Keep main goroutine alive
	select {}
}

// runAPIMode runs the gateway with HTTP/WebSocket API
func runAPIMode(ctx context.Context, cfg *config.Config, unicastAddress string, store logstore.LogStore) {
	fmt.Println("\n=== Running in API Mode (HTTP/WebSocket) ===")

	// Create session manager
	sessionMgr := session.NewManager(cfg)

	// Initialize SIP server (with nil audio track - sessions will have their own)
	sipServer, err := sip.NewServer(cfg.SIP, cfg.RTP, nil, unicastAddress, cfg.SIP.LocalPort)
	if err != nil {
		log.Fatalf("Failed to create SIP server: %v", err)
	}
	sipServer.SetLogStore(store)
	sipServer.SetLogFullSIP(cfg.DB.LogFullSIP)

	// Create SIP client if configured
	if err := sipServer.CreateClient(); err != nil {
		log.Printf("Warning: %v", err)
	}

	// Start SIP server and register (consolidated initialization)
	if err := sipServer.InitializeAndRegisterSIPServer(ctx); err != nil {
		log.Printf("Warning: %v", err)
	}

	// Create and start API server
	apiServer := api.NewServer(cfg.API, cfg.TURN, sessionMgr, sipServer)
	apiServer.SetLogStore(store)

	// Wire dependencies for BYE request handling
	sipServer.SetSessionManager(sessionMgr)
	sipServer.SetStateNotifier(apiServer)

	// Wire dependencies for incoming call support
	sipServer.SetSessionCreator(sessionMgr)
	sipServer.SetIncomingCallNotifier(apiServer)
	sipServer.SetTURNConfig(cfg.TURN)

	// Wire dependencies for SIP MESSAGE support
	sipServer.SetMessageNotifier(apiServer)

	// Wire dependencies for DTMF reception from SIP side
	sipServer.SetDTMFNotifier(apiServer)

	fmt.Println("\n[รอรับ WebRTC connections ผ่าน HTTP/WebSocket API...]")

	// Start HTTP server (this blocks)
	if err := apiServer.Start(); err != nil {
		log.Fatalf("API server error: %v", err)
	}
}
