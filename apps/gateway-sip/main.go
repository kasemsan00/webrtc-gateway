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
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, initiating graceful shutdown...", sig)
		cancel()
	}()

	// Initialize LogStore (DB logging)
	store, err := logstore.New(cfg.DB)
	if err != nil {
		log.Fatalf("Failed to initialize log store: %v", err)
	}
	if err := store.Start(ctx); err != nil {
		log.Fatalf("Failed to start log store: %v", err)
	}
	defer store.Stop()

	// HTTP/WebSocket API mode with multiple sessions
	runAPIMode(ctx, cfg, unicast, store)
}

// runAPIMode runs the gateway with HTTP/WebSocket API
func runAPIMode(ctx context.Context, cfg *config.Config, unicastAddress string, store logstore.LogStore) {
	fmt.Println("\n=== Running in API Mode (HTTP/WebSocket) ===")

	// Register gateway instance for redirect lookup
	if cfg.Gateway.InstanceID == "" || cfg.Gateway.PublicWSURL == "" {
		log.Printf("⚠️ Warning: GATEWAY_INSTANCE_ID or GATEWAY_PUBLIC_WS_URL not set; trunk redirects disabled")
	} else {
		ttlSeconds := 120
		renewInterval := 60 * time.Second
		go func() {
			ticker := time.NewTicker(renewInterval)
			defer ticker.Stop()

			upsert := func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := store.UpsertGatewayInstance(ctx, cfg.Gateway.InstanceID, cfg.Gateway.PublicWSURL, ttlSeconds); err != nil {
					log.Printf("⚠️ Warning: Failed to upsert gateway instance registry: %v", err)
				}
			}

			upsert()
			for {
				select {
				case <-ticker.C:
					upsert()
				case <-ctx.Done():
					return
				}
			}
		}()
	}

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

	// Initialize PublicAccountRegistry (for SIP public multi-user registration)
	publicRegistry := sip.NewPublicAccountRegistry(
		cfg.SIPPublic,
		sipServer.GetUserAgent(),
		sipServer.GetPublicAddress(),
		sipServer.GetSIPPort(),
	)
	publicRegistry.Start()
	defer publicRegistry.Stop()
	sipServer.SetPublicAccountRegistry(publicRegistry)

	// Initialize TrunkManager (for DB-based trunk registration with lease)
	var trunkManager *sip.TrunkManager
	if cfg.DB.Enable && cfg.SIPTrunk.Enable {
		dbInterface := store.GetDB()
		if dbInterface != nil {
			trunkManager = sip.NewTrunkManager(dbInterface, cfg, sipServer.GetUserAgent(), cfg.Gateway.InstanceID)
			if err := trunkManager.Start(); err != nil {
				log.Printf("⚠️ Warning: TrunkManager failed to start: %v", err)
			} else {
				defer trunkManager.Stop()
			}
			sipServer.SetTrunkManager(trunkManager)
		} else {
			log.Printf("⚠️ Warning: DB not available for TrunkManager")
		}
	}

	// Start SIP server and register (consolidated initialization)
	if err := sipServer.InitializeAndRegisterSIPServer(ctx); err != nil {
		log.Printf("Warning: %v", err)
	}

	// Create and start API server (pass nil for trunk manager if it's not set, interface will handle it)
	var trunkMgrInterface api.TrunkManager
	if trunkManager != nil {
		trunkMgrInterface = trunkManager
	}
	apiServer := api.NewServer(cfg.API, cfg.TURN, cfg.Gateway, sessionMgr, sipServer, publicRegistry, trunkMgrInterface, store)

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

	// Start HTTP server (this blocks until context is cancelled)
	if err := apiServer.Start(ctx); err != nil {
		log.Printf("API server shutdown: %v", err)
	}

	// Graceful cleanup: stop SIP registration
	sipServer.StopRegistration()

	log.Println("Gateway shutdown complete")
}
