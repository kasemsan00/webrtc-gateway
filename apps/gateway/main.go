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
	"k2-gateway/internal/auth"
	"k2-gateway/internal/config"
	"k2-gateway/internal/logger"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/push"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
	"k2-gateway/internal/sipclientauth"
	"k2-gateway/internal/translator"
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

	if cfg.SIP.AudioInboundGainEnable {
		codec, err := translator.NewOpusCodec(24000)
		if err != nil {
			log.Printf("⚠️ Warning: SIP_AUDIO_INBOUND_GAIN_ENABLE=true but Opus codec unavailable (%v); disabling inbound gain", err)
			cfg.SIP.AudioInboundGainEnable = false
		} else {
			codec.Close()
			log.Printf("🔊 SIP inbound audio gain enabled: gain=%.2f max=%.2f", cfg.SIP.AudioInboundGain, cfg.SIP.AudioInboundGainMax)
		}
	}

	if cfg.SIP.Port != cfg.SIP.LocalPort {
		fmt.Printf("⚠️ SIP_PORT (%d) differs from SIP_LOCAL_PORT (%d). Listener binds SIP_LOCAL_PORT; ensure upstream INVITEs target that port.\n", cfg.SIP.Port, cfg.SIP.LocalPort)
	}

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
	sessionMgr.StartCleanup(ctx, 30*time.Second)

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
	apiServer := api.NewServer(cfg.API, cfg.TURN, cfg.Gateway, cfg.Translator, sessionMgr, sipServer, publicRegistry, trunkMgrInterface, store)
	apiServer.SetRuntimeConfig(cfg)
	if cfg.Auth.FrontendPassword != "" {
		apiServer.SetAdminPassword(cfg.Auth.FrontendPassword)
		log.Printf("Admin REST password auth enabled")
	}
	if cfg.API.MobileSIPAuthRegisterURL != "" {
		if trunkManager == nil {
			log.Printf("⚠️ Warning: Mobile SIP provisioning disabled — trunk manager is not available")
		} else {
			timeout := time.Duration(cfg.API.MobileSIPAuthTimeoutMS) * time.Millisecond
			apiServer.SetMobileSIPProvisioner(api.NewMobileSIPProvisioner(
				sipclientauth.NewClient(sipclientauth.Config{
					RegisterURL: cfg.API.MobileSIPAuthRegisterURL,
					Timeout:     timeout,
				}),
				trunkManager,
			))
			log.Printf("Mobile SIP provisioning enabled: registerURL=%s timeout=%dms", cfg.API.MobileSIPAuthRegisterURL, cfg.API.MobileSIPAuthTimeoutMS)
		}
	}
	if cfg.Auth.Enable {
		if cfg.Auth.User.JWKSURL == "" && cfg.Auth.Employee.JWKSURL == "" {
			log.Fatalf("AUTH_ENABLE=true requires at least one realm JWKS URL: AUTH_TTRS_USERS_JWKS_URL or AUTH_TTRS_EMPLOYEE_JWKS_URL")
		}

		var userVerifier *auth.Verifier
		if cfg.Auth.User.JWKSURL != "" {
			verifier, err := auth.NewVerifier(auth.Config{
				JWKSURL:   cfg.Auth.User.JWKSURL,
				Issuer:    cfg.Auth.User.JWTIssuer,
				Audience:  cfg.Auth.User.JWTAudience,
				TimeoutMS: cfg.Auth.TimeoutMS,
			})
			if err != nil {
				log.Fatalf("Failed to initialize user auth verifier: %v", err)
			}
			startupCtx, cancelStartup := context.WithTimeout(ctx, time.Duration(cfg.Auth.TimeoutMS)*time.Millisecond)
			if err := verifier.Prefetch(startupCtx); err != nil {
				cancelStartup()
				log.Fatalf("Failed to prefetch user JWKS on startup: %v", err)
			}
			cancelStartup()
			userVerifier = verifier
		}

		var employeeVerifier *auth.Verifier
		if cfg.Auth.Employee.JWKSURL != "" {
			verifier, err := auth.NewVerifier(auth.Config{
				JWKSURL:   cfg.Auth.Employee.JWKSURL,
				Issuer:    cfg.Auth.Employee.JWTIssuer,
				Audience:  cfg.Auth.Employee.JWTAudience,
				TimeoutMS: cfg.Auth.TimeoutMS,
			})
			if err != nil {
				log.Fatalf("Failed to initialize employee auth verifier: %v", err)
			}
			startupCtx, cancelStartup := context.WithTimeout(ctx, time.Duration(cfg.Auth.TimeoutMS)*time.Millisecond)
			if err := verifier.Prefetch(startupCtx); err != nil {
				cancelStartup()
				log.Fatalf("Failed to prefetch employee JWKS on startup: %v", err)
			}
			cancelStartup()
			employeeVerifier = verifier
		}

		realmVerifier, err := auth.NewRealmVerifier(userVerifier, employeeVerifier)
		if err != nil {
			log.Fatalf("Failed to initialize realm auth verifier: %v", err)
		}
		apiServer.SetTokenVerifier(realmVerifier)

		logRealmInfo := func(name string, realmCfg config.AuthRealmConfig) {
			issuerInfo := "(not enforced)"
			if realmCfg.JWTIssuer != "" {
				issuerInfo = realmCfg.JWTIssuer
			}
			audienceInfo := "(not enforced)"
			if realmCfg.JWTAudience != "" {
				audienceInfo = realmCfg.JWTAudience
			}
			log.Printf("JWT auth realm enabled: realm=%s issuer=%s audience=%s", name, issuerInfo, audienceInfo)
		}
		if userVerifier != nil {
			logRealmInfo("user", cfg.Auth.User)
		}
		if employeeVerifier != nil {
			logRealmInfo("employee", cfg.Auth.Employee)
		}
	}

	// Initialize push notification service (FCM fallback + APNs PushKit where configured)
	if cfg.PushNotification.Enable {
		var ttrsClient *push.TTRSClient
		var fcmSender *push.FCMSender
		if cfg.PushNotification.FirebaseCredentialsFile != "" && cfg.PushNotification.FirebaseProjectID != "" {
			ttrsClient = push.NewTTRSClient(
				cfg.PushNotification.TTRSAPIURL,
				cfg.PushNotification.TTRSKeycloakTokenURL,
				cfg.PushNotification.TTRSTokenGrantType,
				cfg.PushNotification.TTRSClientID,
				cfg.PushNotification.TTRSClientSecret,
				cfg.PushNotification.TTRSAPITimeoutMS,
			)
			var err error
			fcmSender, err = push.NewFCMSender(
				cfg.PushNotification.FirebaseCredentialsFile,
				cfg.PushNotification.FirebaseProjectID,
			)
			if err != nil {
				log.Printf("⚠️ Warning: FCM fallback disabled — init failed: %v", err)
				fcmSender = nil
				ttrsClient = nil
			}
		} else {
			log.Printf("⚠️ Warning: FCM fallback disabled — PUSH_FIREBASE_CREDENTIALS_FILE or PUSH_FIREBASE_PROJECT_ID missing")
		}

		var apnsSender *push.APNSSender
		if cfg.PushNotification.APNSEnable {
			var err error
			apnsSender, err = push.NewAPNSSender(push.APNSConfig{
				Environment: cfg.PushNotification.APNSEnvironment,
				AuthMode:    cfg.PushNotification.APNSAuthMode,
				KeyFile:     cfg.PushNotification.APNSKeyFile,
				KeyID:       cfg.PushNotification.APNSKeyID,
				TeamID:      cfg.PushNotification.APNSTeamID,
				CertFile:    cfg.PushNotification.APNSCertFile,
				CertKeyFile: cfg.PushNotification.APNSCertKeyFile,
				BundleID:    cfg.PushNotification.APNSBundleID,
				Topic:       cfg.PushNotification.APNSTopic,
			})
			if err != nil {
				log.Printf("⚠️ Warning: APNs PushKit disabled — init failed: %v", err)
			}
		}

		if fcmSender != nil || apnsSender != nil {
			pushService := push.NewService(ttrsClient, fcmSender, apnsSender)
			apiServer.SetPushService(pushService)
			log.Printf("🔔 Push notifications enabled (fcm=%v apns=%v)", fcmSender != nil, apnsSender != nil)
		} else {
			log.Printf("⚠️ Warning: PUSH_ENABLE=true but no push channel initialized")
		}
	}

	// Initialize translator client for S2S speech translation
	if cfg.Translator.Enable {
		tc := translator.NewClient(translator.Config{
			Addr:        cfg.Translator.Addr,
			SourceLang:  cfg.Translator.SourceLang,
			TargetLang:  cfg.Translator.TargetLang,
			TTSVoice:    cfg.Translator.TTSVoice,
			OpusBitrate: cfg.Translator.OpusBitrate,
		})
		connectCtx, connectCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := tc.Connect(connectCtx); err != nil {
			connectCancel()
			log.Printf("⚠️ Warning: Translator client failed to connect (%v); translation disabled", err)
		} else {
			connectCancel()
			apiServer.SetTranslatorClient(tc)
			log.Printf("🎤 Translator client connected to %s (%s → %s)", cfg.Translator.Addr, cfg.Translator.SourceLang, cfg.Translator.TargetLang)

			// Health check
			healthCtx, healthCancel := context.WithTimeout(ctx, 3*time.Second)
			if err := tc.CheckHealth(healthCtx); err != nil {
				healthCancel()
				log.Printf("⚠️ Warning: Translator health check failed (%v); translation may not work", err)
			} else {
				healthCancel()
				log.Printf("✅ Translator health check passed")
			}
		}
		defer tc.Close()
	}

	// Wire dependencies for BYE request handling
	sipServer.SetSessionManager(sessionMgr)
	sipServer.SetStateNotifier(apiServer)
	sipServer.SetMidCallRenegotiationNotifier(apiServer)
	sipServer.SetRemoteMediaNotifier(apiServer)
	sipServer.SetSwitchVideoRenegotiationStarter(apiServer)

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
