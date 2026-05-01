package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all application configuration
type Config struct {
	TURN             TURNConfig
	SIP              SIPConfig
	API              APIConfig
	Auth             AuthConfig
	RTP              RTPConfig
	DB               DBConfig
	SIPPublic        SIPPublicConfig
	SIPTrunk         SIPTrunkConfig
	Gateway          GatewayConfig
	SessionDir       SessionDirectoryConfig
	PushNotification PushNotificationConfig
	Translator       TranslatorConfig
}

// TranslatorConfig holds S2S speech translation configuration
type TranslatorConfig struct {
	Enable     bool   // Enable translation client (default: false)
	Addr       string // gRPC server address (default: "localhost:5000")
	SourceLang string // Source language code (default: "en")
	TargetLang string // Target language code (default: "th")
	TTSVoice   string // TTS voice name (default: "th-TH-Sarawut")
	OpusBitrate int   // Opus encoding bitrate (default: 24000)
}

// RTPConfig holds RTP UDP port range configuration
type RTPConfig struct {
	PortMin    int // Minimum RTP port (default: 10500)
	PortMax    int // Maximum RTP port (default: 10600)
	BufferSize int // RTP packet buffer size in bytes (default: 4096)
}

// APIConfig holds HTTP API server configuration
type APIConfig struct {
	Port           int    // HTTP server port (default: 8080)
	EnableWS       bool   // Enable WebSocket endpoint
	EnableREST     bool   // Enable REST API
	CORSOrigins    string // CORS allowed origins (comma-separated)
	DebugWebSocket bool   // Enable WebSocket debug logging (ping/pong, messages)
	DebugTURN      bool   // Enable TURN/ICE debug logging (candidates, selected pair)
}

// AuthConfig holds JWT/JWKS authentication settings.
type AuthConfig struct {
	Enable    bool
	TimeoutMS int
	User      AuthRealmConfig
	Employee  AuthRealmConfig
}

// AuthRealmConfig holds JWT verification settings for one realm.
type AuthRealmConfig struct {
	JWKSURL     string
	JWTIssuer   string
	JWTAudience string
}

// TURNConfig holds TURN server configuration
type TURNConfig struct {
	Server   string
	Username string
	Password string
}

// SIPConfig holds SIP server configuration
type SIPConfig struct {
	Domain           string
	Username         string
	Password         string
	Port             int
	LocalPort        int
	LocalIP          string // Local IP to bind listeners (prevents IPv6, default: 0.0.0.0)
	PublicIP         string // Public IP address for NAT traversal (optional)
	ListenTCP        bool   // Enable SIP TCP listener (default: true)
	ListenUDP        bool   // Enable SIP UDP listener (default: false)
	DebugSIPMessage  bool   // Enable verbose SIP MESSAGE logging
	DebugSIPInvite   bool   // Enable verbose SIP INVITE logging (header dump)
	SwitchPLIDelayMS int    // Delay in milliseconds before sending PLI on @switch message (default: 0)
	// @switch video transition hold (SIP->WebRTC): temporarily drop remote video packets
	// to intentionally keep screen black before showing target video.
	SwitchVideoBlackoutEnabled   bool // Enable @switch blackout hold policy (default: true)
	SwitchVideoBlackoutMS        int  // Minimum blackout duration in ms (default: 700)
	SwitchVideoBlackoutMaxWaitMS int  // Max wait for keyframe after blackout in ms (default: 2000)
	AudioUseAVPF                 bool // Use RTP/AVPF profile for audio with RTCP feedback (default: false)
	VideoUseAVPF                 bool // Use RTP/AVPF profile for video with RTCP feedback (PLI/FIR/NACK) (default: true)
	// SIP-side transport target for outbound video feedback packets (PLI/FIR/NACK): auto|rtp|rtcp|dual
	// - auto: legacy learned-RTCP + fallback-window behavior
	// - rtp:  always send to SIP video RTP port (rtcp-mux style)
	// - rtcp: always send to learned/rtp+1 RTCP target only
	// - dual: always send to both RTP and RTCP targets (RTP first)
	VideoFeedbackTransport string
	VideoPreserveSTAPA     bool // Preserve STAP-A packets (don't de-aggregate) when they contain SPS+PPS+IDR (default: false)
	// Keyframe watchdog: request FIR/PLI when keyframes go stale (SIP → WebRTC)
	VideoKeyframeWatchdogEnabled    bool // Enable keyframe watchdog (default: true)
	VideoKeyframeWatchdogIntervalMS int  // Check interval in ms (default: 1000)
	VideoKeyframeStaleMS            int  // Stale threshold for PLI (default: 1500)
	VideoKeyframeFIRStaleMS         int  // Stale threshold for FIR (default: 3000)
	// Dynamic post-reconnect recovery burst policy (temporary aggressive window)
	VideoRecoveryBurstEnabled    bool // Enable dynamic burst recovery policy (default: true)
	VideoRecoveryBurstWindowMS   int  // Burst window duration in ms (default: 12000)
	VideoRecoveryBurstIntervalMS int  // Burst watchdog interval in ms (default: 800)
	VideoRecoveryBurstStaleMS    int  // Burst stale threshold for PLI in ms (default: 1200)
	VideoRecoveryBurstFIRStaleMS int  // Burst stale threshold for FIR in ms (default: 2500)
}

const (
	SIPVideoFeedbackTransportAuto = "auto"
	SIPVideoFeedbackTransportRTP  = "rtp"
	SIPVideoFeedbackTransportRTCP = "rtcp"
	SIPVideoFeedbackTransportDual = "dual"
)

// DBConfig holds PostgreSQL database configuration
type DBConfig struct {
	Enable                 bool   // Enable database logging
	DSN                    string // PostgreSQL connection string
	StatsIntervalMS        int    // Stats flush interval in milliseconds
	LogFullSIP             bool   // Log full SIP messages (req.String())
	BatchSize              int    // Number of events per batch insert
	BatchIntervalMS        int    // Batch flush interval in milliseconds
	PartitionLookaheadDays int    // Create partitions N days ahead
	RetentionPayloadsDays  int    // Retention for call_payloads (days)
	RetentionEventsDays    int    // Retention for call_events (days)
	RetentionStatsDays     int    // Retention for call_stats (days)
	RetentionSessionsDays  int    // Retention for call_sessions (days)
}

// SIPPublicConfig holds SIP public (temporary) registration configuration
type SIPPublicConfig struct {
	RegisterExpiresSeconds int // Expires header value for REGISTER (default: 3600)
	RegisterTimeoutSeconds int // Timeout for REGISTER request (default: 10)
	IdleTTLSeconds         int // Unregister after N seconds of no active calls (default: 600)
	CleanupIntervalSeconds int // Cleanup worker interval (default: 30)
	MaxAccounts            int // Maximum concurrent public accounts (default: 1000)
}

// SIPTrunkConfig holds SIP trunk configuration
type SIPTrunkConfig struct {
	Enable             bool // Enable trunk auto-register from DB (default: true if DB_ENABLE=true)
	LeaseTTLSeconds    int  // Lease duration in seconds (default: 60)
	LeaseRenewInterval int  // Renew lease every N seconds (default: 20)
	RegisterTimeout    int  // Timeout for trunk REGISTER (default: 10)
}

// GatewayConfig holds gateway instance configuration
type GatewayConfig struct {
	InstanceID  string // Unique instance ID (default: hostname or random)
	PublicWSURL string // Public WebSocket URL for this instance (e.g., wss://gw-1.../ws)
}

// SessionDirectoryConfig holds session directory configuration
type SessionDirectoryConfig struct {
	TTLSeconds         int // Session directory entry TTL (default: 7200 = 2 hours)
	CleanupIntervalSec int // Cleanup expired entries interval (default: 300 = 5 min)
}

// PushNotificationConfig holds push notification configuration for incoming calls
type PushNotificationConfig struct {
	Enable                  bool   // Enable push notifications on incoming trunk calls (default: false)
	TTRSAPIURL              string // TTRS Notification API base URL (e.g., https://api.ttrs.or.th)
	TTRSAPITimeoutMS        int    // TTRS API HTTP timeout in milliseconds (default: 5000)
	TTRSKeycloakTokenURL    string // Keycloak token endpoint for client credentials grant
	TTRSTokenGrantType      string // OAuth2 grant_type for token endpoint (default: client_credentials)
	TTRSClientID            string // Keycloak client ID for TTRS API auth
	TTRSClientSecret        string // Keycloak client secret for TTRS API auth
	FirebaseCredentialsFile string // Path to Firebase service account JSON file
	FirebaseProjectID       string // Firebase project ID for FCM v1 API
}

// Load loads configuration from .env file and environment variables
func Load() (*Config, error) {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Warning: Error loading .env file: %v\n", err)
	}

	sipPortNum := getEnvAsInt("SIP_PORT", 5060)
	sipLocalPortNum := getEnvAsInt("SIP_LOCAL_PORT", 5060)
	apiPort := getEnvAsInt("API_PORT", 8080)
	rtpPortMin := getEnvAsInt("RTP_PORT_MIN", 10500)
	rtpPortMax := getEnvAsInt("RTP_PORT_MAX", 10600)
	rtpBufferSize := getEnvAsInt("RTP_BUFFER_SIZE", 16384)

	return &Config{
		TURN: TURNConfig{
			Server:   os.Getenv("TURN_SERVER"),
			Username: os.Getenv("TURN_USERNAME"),
			Password: os.Getenv("TURN_PASSWORD"),
		},
		SIP: SIPConfig{
			Port:                            sipPortNum,
			LocalPort:                       sipLocalPortNum,
			LocalIP:                         getEnvWithDefault("SIP_LOCAL_IP", "0.0.0.0"),
			PublicIP:                        os.Getenv("SIP_PUBLIC_IP"),
			ListenTCP:                       getEnvAsBool("SIP_LISTEN_TCP", true),
			ListenUDP:                       getEnvAsBool("SIP_LISTEN_UDP", false),
			DebugSIPMessage:                 getEnvAsBool("DEBUG_SIP_MESSAGE", false),
			DebugSIPInvite:                  getEnvAsBool("DEBUG_SIP_INVITE", false),
			SwitchPLIDelayMS:                getEnvAsInt("SWITCH_PLI_DELAY_MS", 0),
			SwitchVideoBlackoutEnabled:      getEnvAsBool("SIP_SWITCH_VIDEO_BLACKOUT_ENABLED", true),
			SwitchVideoBlackoutMS:           getEnvAsInt("SIP_SWITCH_VIDEO_BLACKOUT_MS", 700),
			SwitchVideoBlackoutMaxWaitMS:    getEnvAsInt("SIP_SWITCH_VIDEO_BLACKOUT_MAX_WAIT_MS", 2000),
			AudioUseAVPF:                    getEnvAsBool("SIP_AUDIO_USE_AVPF", false),
			VideoUseAVPF:                    getEnvAsBool("SIP_VIDEO_USE_AVPF", true),
			VideoFeedbackTransport:          getSIPVideoFeedbackTransport(),
			VideoPreserveSTAPA:              getEnvAsBool("SIP_VIDEO_PRESERVE_STAPA", false),
			VideoKeyframeWatchdogEnabled:    getEnvAsBool("SIP_VIDEO_KEYFRAME_WATCHDOG", true),
			VideoKeyframeWatchdogIntervalMS: getEnvAsInt("SIP_VIDEO_KEYFRAME_WATCHDOG_INTERVAL_MS", 1500),
			VideoKeyframeStaleMS:            getEnvAsInt("SIP_VIDEO_KEYFRAME_STALE_MS", 2000),
			VideoKeyframeFIRStaleMS:         getEnvAsInt("SIP_VIDEO_KEYFRAME_FIR_STALE_MS", 5000),
			VideoRecoveryBurstEnabled:       getEnvAsBool("SIP_VIDEO_RECOVERY_BURST_ENABLED", true),
			VideoRecoveryBurstWindowMS:      getEnvAsInt("SIP_VIDEO_RECOVERY_BURST_WINDOW_MS", 12000),
			VideoRecoveryBurstIntervalMS:    getEnvAsInt("SIP_VIDEO_RECOVERY_BURST_INTERVAL_MS", 800),
			VideoRecoveryBurstStaleMS:       getEnvAsInt("SIP_VIDEO_RECOVERY_BURST_STALE_MS", 1200),
			VideoRecoveryBurstFIRStaleMS:    getEnvAsInt("SIP_VIDEO_RECOVERY_BURST_FIR_STALE_MS", 2500),
		},
		API: APIConfig{
			Port:           apiPort,
			EnableWS:       getEnvAsBool("API_ENABLE_WS", true),
			EnableREST:     getEnvAsBool("API_ENABLE_REST", true),
			CORSOrigins:    getEnvWithDefault("API_CORS_ORIGINS", "*"),
			DebugWebSocket: getEnvAsBool("DEBUG_WEBSOCKET", false),
			DebugTURN:      getEnvAsBool("DEBUG_TURN", false),
		},
		Auth: AuthConfig{
			Enable:    getEnvAsBool("AUTH_ENABLE", false),
			TimeoutMS: getEnvAsInt("AUTH_JWKS_TIMEOUT_MS", 5000),
			User: AuthRealmConfig{
				JWKSURL:     os.Getenv("AUTH_TTRS_USERS_JWKS_URL"),
				JWTIssuer:   os.Getenv("AUTH_TTRS_USERS_JWT_ISSUER"),
				JWTAudience: os.Getenv("AUTH_TTRS_USERS_JWT_AUDIENCE"),
			},
			Employee: AuthRealmConfig{
				JWKSURL:     os.Getenv("AUTH_TTRS_EMPLOYEE_JWKS_URL"),
				JWTIssuer:   os.Getenv("AUTH_TTRS_EMPLOYEE_JWT_ISSUER"),
				JWTAudience: os.Getenv("AUTH_TTRS_EMPLOYEE_JWT_AUDIENCE"),
			},
		},
		RTP: RTPConfig{
			PortMin:    rtpPortMin,
			PortMax:    rtpPortMax,
			BufferSize: rtpBufferSize,
		},
		DB: DBConfig{
			Enable:                 getEnvAsBool("DB_ENABLE", false),
			DSN:                    os.Getenv("DB_DSN"),
			StatsIntervalMS:        getEnvAsInt("DB_STATS_INTERVAL_MS", 5000),
			LogFullSIP:             getEnvAsBool("DB_LOG_FULL_SIP", false),
			BatchSize:              getEnvAsInt("DB_BATCH_SIZE", 100),
			BatchIntervalMS:        getEnvAsInt("DB_BATCH_INTERVAL_MS", 1000),
			PartitionLookaheadDays: getEnvAsInt("DB_PARTITION_LOOKAHEAD_DAYS", 7),
			RetentionPayloadsDays:  getEnvAsInt("DB_RETENTION_PAYLOADS_DAYS", 730),
			RetentionEventsDays:    getEnvAsInt("DB_RETENTION_EVENTS_DAYS", 730),
			RetentionStatsDays:     getEnvAsInt("DB_RETENTION_STATS_DAYS", 730),
			RetentionSessionsDays:  getEnvAsInt("DB_RETENTION_SESSIONS_DAYS", 730),
		},
		SIPPublic: SIPPublicConfig{
			RegisterExpiresSeconds: getEnvAsInt("SIP_PUBLIC_REGISTER_EXPIRES_SECONDS", 3600),
			RegisterTimeoutSeconds: getEnvAsInt("SIP_PUBLIC_REGISTER_TIMEOUT_SECONDS", 10),
			IdleTTLSeconds:         getEnvAsInt("SIP_PUBLIC_IDLE_TTL_SECONDS", 600),
			CleanupIntervalSeconds: getEnvAsInt("SIP_PUBLIC_CLEANUP_INTERVAL_SECONDS", 30),
			MaxAccounts:            getEnvAsInt("SIP_PUBLIC_MAX_ACCOUNTS", 1000),
		},
		SIPTrunk: SIPTrunkConfig{
			Enable:             getEnvAsBool("SIP_TRUNK_ENABLE", getEnvAsBool("DB_ENABLE", false)),
			LeaseTTLSeconds:    getEnvAsInt("SIP_TRUNK_LEASE_TTL_SECONDS", 60),
			LeaseRenewInterval: getEnvAsInt("SIP_TRUNK_LEASE_RENEW_INTERVAL_SECONDS", 20),
			RegisterTimeout:    getEnvAsInt("SIP_TRUNK_REGISTER_TIMEOUT_SECONDS", 10),
		},
		Gateway: GatewayConfig{
			InstanceID:  getEnvWithDefault("GATEWAY_INSTANCE_ID", generateInstanceID()),
			PublicWSURL: os.Getenv("GATEWAY_PUBLIC_WS_URL"),
		},
		SessionDir: SessionDirectoryConfig{
			TTLSeconds:         getEnvAsInt("SESSION_DIRECTORY_TTL_SECONDS", 7200),
			CleanupIntervalSec: getEnvAsInt("SESSION_DIRECTORY_CLEANUP_INTERVAL_SECONDS", 300),
		},
		PushNotification: PushNotificationConfig{
			Enable:                  getEnvAsBool("PUSH_ENABLE", false),
			TTRSAPIURL:              os.Getenv("PUSH_TTRS_API_URL"),
			TTRSAPITimeoutMS:        getEnvAsInt("PUSH_TTRS_API_TIMEOUT_MS", 5000),
			TTRSKeycloakTokenURL:    os.Getenv("AUTH_TTRS_EMPLOYEE_TOKEN_URL"),
			TTRSTokenGrantType:      getEnvWithDefault("AUTH_TTRS_EMPLOYEE_TOKEN_GRANT_TYPE", "client_credentials"),
			TTRSClientID:            os.Getenv("AUTH_TTRS_EMPLOYEE_CLIENT_ID"),
			TTRSClientSecret:        os.Getenv("AUTH_TTRS_EMPLOYEE_CLIENT_SECRET"),
			FirebaseCredentialsFile: os.Getenv("PUSH_FIREBASE_CREDENTIALS_FILE"),
			FirebaseProjectID:       os.Getenv("PUSH_FIREBASE_PROJECT_ID"),
		},
		Translator: TranslatorConfig{
			Enable:      getEnvAsBool("TRANSLATOR_ENABLE", false),
			Addr:        getEnvWithDefault("TRANSLATOR_ADDR", "localhost:5000"),
			SourceLang:  getEnvWithDefault("TRANSLATOR_SOURCE_LANG", "en"),
			TargetLang:  getEnvWithDefault("TRANSLATOR_TARGET_LANG", "th"),
			TTSVoice:    getEnvWithDefault("TRANSLATOR_TTS_VOICE", "th-TH-Sarawut"),
			OpusBitrate: getEnvAsInt("TRANSLATOR_OPUS_BITRATE", 24000),
		},
	}, nil
}

// Display prints the loaded configuration to stdout
func (c *Config) Display() {
	fmt.Println("=== Environment Configuration ===")

	// Display TURN Server Configuration
	fmt.Println("\nTURN Server:")
	if c.TURN.Server != "" {
		fmt.Printf("  Server: %s\n", c.TURN.Server)
		fmt.Printf("  Username: %s\n", c.TURN.Username)
		if c.TURN.Password != "" {
			fmt.Printf("  Password: %s\n", maskPassword(c.TURN.Password))
		}
	} else {
		fmt.Println("  Not configured")
	}

	// Display SIP Configuration
	fmt.Println("\nSIP Configuration:")
	if c.SIP.Domain != "" {
		fmt.Printf("  Domain: %s\n", c.SIP.Domain)
		fmt.Printf("  Username: %s\n", c.SIP.Username)
		if c.SIP.Password != "" {
			fmt.Printf("  Password: %s\n", maskPassword(c.SIP.Password))
		}
		fmt.Printf("  Port: %d\n", c.SIP.Port)
		fmt.Printf("  Local Port: %d\n", c.SIP.LocalPort)
		if c.SIP.PublicIP != "" {
			fmt.Printf("  Public IP: %s (for NAT traversal)\n", c.SIP.PublicIP)
		}
	} else {
		fmt.Println("  Not configured")
	}
	fmt.Printf("  Debug SIP MESSAGE: %v\n", c.SIP.DebugSIPMessage)
	fmt.Printf("  Debug SIP INVITE: %v\n", c.SIP.DebugSIPInvite)
	fmt.Printf("  SIP Listen TCP: %v\n", c.SIP.ListenTCP)
	fmt.Printf("  SIP Listen UDP: %v\n", c.SIP.ListenUDP)
	fmt.Printf("  Audio Use AVPF: %v\n", c.SIP.AudioUseAVPF)
	fmt.Printf("  Video Use AVPF: %v\n", c.SIP.VideoUseAVPF)
	fmt.Printf("  Video Feedback Transport: %s\n", c.SIP.VideoFeedbackTransport)
	fmt.Printf("  Video Preserve STAP-A: %v\n", c.SIP.VideoPreserveSTAPA)
	fmt.Printf("  Video Keyframe Watchdog: %v (interval=%dms, stale=%dms, firStale=%dms)\n",
		c.SIP.VideoKeyframeWatchdogEnabled,
		c.SIP.VideoKeyframeWatchdogIntervalMS,
		c.SIP.VideoKeyframeStaleMS,
		c.SIP.VideoKeyframeFIRStaleMS,
	)
	fmt.Printf("  Video Recovery Burst: %v (window=%dms, interval=%dms, stale=%dms, firStale=%dms)\n",
		c.SIP.VideoRecoveryBurstEnabled,
		c.SIP.VideoRecoveryBurstWindowMS,
		c.SIP.VideoRecoveryBurstIntervalMS,
		c.SIP.VideoRecoveryBurstStaleMS,
		c.SIP.VideoRecoveryBurstFIRStaleMS,
	)
	fmt.Printf("  @switch Video Blackout: %v (blackout=%dms, maxWait=%dms)\n",
		c.SIP.SwitchVideoBlackoutEnabled,
		c.SIP.SwitchVideoBlackoutMS,
		c.SIP.SwitchVideoBlackoutMaxWaitMS,
	)

	// Display API Configuration
	fmt.Println("\nAPI Configuration:")
	fmt.Printf("  Port: %d\n", c.API.Port)
	fmt.Printf("  WebSocket: %v\n", c.API.EnableWS)
	fmt.Printf("  REST API: %v\n", c.API.EnableREST)
	fmt.Printf("  CORS Origins: %s\n", c.API.CORSOrigins)
	fmt.Printf("  Debug WebSocket: %v\n", c.API.DebugWebSocket)
	fmt.Printf("  Debug TURN/ICE: %v\n", c.API.DebugTURN)

	// Display Auth Configuration
	fmt.Println("\nAuth Configuration:")
	fmt.Printf("  Enabled: %v\n", c.Auth.Enable)
	if c.Auth.Enable {
		fmt.Printf("  JWKS Timeout: %d ms\n", c.Auth.TimeoutMS)
		fmt.Printf("  User JWKS URL: %s\n", c.Auth.User.JWKSURL)
		fmt.Printf("  User JWT Issuer: %s\n", c.Auth.User.JWTIssuer)
		fmt.Printf("  User JWT Audience: %s\n", c.Auth.User.JWTAudience)
		fmt.Printf("  Employee JWKS URL: %s\n", c.Auth.Employee.JWKSURL)
		fmt.Printf("  Employee JWT Issuer: %s\n", c.Auth.Employee.JWTIssuer)
		fmt.Printf("  Employee JWT Audience: %s\n", c.Auth.Employee.JWTAudience)
	}

	// Display RTP Configuration
	fmt.Println("\nRTP Configuration:")
	fmt.Printf("  Port Range: %d-%d\n", c.RTP.PortMin, c.RTP.PortMax)
	fmt.Printf("  Buffer Size: %d bytes\n", c.RTP.BufferSize)

	// Display Database Configuration
	fmt.Println("\nDatabase Configuration:")
	if c.DB.Enable {
		fmt.Printf("  Enabled: %v\n", c.DB.Enable)
		fmt.Printf("  DSN: %s\n", maskDSN(c.DB.DSN))
		fmt.Printf("  Stats Interval: %d ms\n", c.DB.StatsIntervalMS)
		fmt.Printf("  Log Full SIP: %v\n", c.DB.LogFullSIP)
		fmt.Printf("  Batch Size: %d\n", c.DB.BatchSize)
		fmt.Printf("  Batch Interval: %d ms\n", c.DB.BatchIntervalMS)
		fmt.Printf("  Retention: payloads=%dd, events=%dd, stats=%dd, sessions=%dd\n",
			c.DB.RetentionPayloadsDays, c.DB.RetentionEventsDays,
			c.DB.RetentionStatsDays, c.DB.RetentionSessionsDays)
	} else {
		fmt.Println("  Disabled")
	}

	// Display SIP Public Configuration
	fmt.Println("\nSIP Public Configuration:")
	fmt.Printf("  Register Expires: %d seconds\n", c.SIPPublic.RegisterExpiresSeconds)
	fmt.Printf("  Register Timeout: %d seconds\n", c.SIPPublic.RegisterTimeoutSeconds)
	fmt.Printf("  Idle TTL: %d seconds\n", c.SIPPublic.IdleTTLSeconds)
	fmt.Printf("  Cleanup Interval: %d seconds\n", c.SIPPublic.CleanupIntervalSeconds)
	fmt.Printf("  Max Accounts: %d\n", c.SIPPublic.MaxAccounts)

	// Display SIP Trunk Configuration
	fmt.Println("\nSIP Trunk Configuration:")
	fmt.Printf("  Enabled: %v\n", c.SIPTrunk.Enable)
	if c.SIPTrunk.Enable {
		fmt.Printf("  Lease TTL: %d seconds\n", c.SIPTrunk.LeaseTTLSeconds)
		fmt.Printf("  Lease Renew Interval: %d seconds\n", c.SIPTrunk.LeaseRenewInterval)
		fmt.Printf("  Register Timeout: %d seconds\n", c.SIPTrunk.RegisterTimeout)
	}

	// Display Gateway Configuration
	fmt.Println("\nGateway Configuration:")
	fmt.Printf("  Instance ID: %s\n", c.Gateway.InstanceID)
	if c.Gateway.PublicWSURL != "" {
		fmt.Printf("  Public WS URL: %s\n", c.Gateway.PublicWSURL)
	} else {
		fmt.Println("  Public WS URL: Not configured (resume_redirect disabled)")
	}

	// Display Session Directory Configuration
	fmt.Println("\nSession Directory Configuration:")
	fmt.Printf("  TTL: %d seconds\n", c.SessionDir.TTLSeconds)
	fmt.Printf("  Cleanup Interval: %d seconds\n", c.SessionDir.CleanupIntervalSec)

	// Display Push Notification Configuration
	fmt.Println("\nPush Notification Configuration:")
	fmt.Printf("  Enabled: %v\n", c.PushNotification.Enable)
	if c.PushNotification.Enable {
		fmt.Printf("  TTRS API URL: %s\n", c.PushNotification.TTRSAPIURL)
		fmt.Printf("  TTRS API Timeout: %d ms\n", c.PushNotification.TTRSAPITimeoutMS)
		fmt.Printf("  TTRS Keycloak Token URL: %s\n", c.PushNotification.TTRSKeycloakTokenURL)
		fmt.Printf("  TTRS Token Grant Type: %s\n", c.PushNotification.TTRSTokenGrantType)
		fmt.Printf("  TTRS Client ID: %s\n", c.PushNotification.TTRSClientID)
		if c.PushNotification.TTRSClientSecret != "" {
			fmt.Printf("  TTRS Client Secret: %s\n", maskPassword(c.PushNotification.TTRSClientSecret))
		}
		fmt.Printf("  Firebase Credentials File: %s\n", c.PushNotification.FirebaseCredentialsFile)
		fmt.Printf("  Firebase Project ID: %s\n", c.PushNotification.FirebaseProjectID)
	}

	// Display Translator Configuration
	fmt.Println("\nTranslator Configuration:")
	if c.Translator.Enable {
		fmt.Printf("  Enabled: true\n")
		fmt.Printf("  gRPC Address: %s\n", c.Translator.Addr)
		fmt.Printf("  Source Lang: %s → Target Lang: %s\n", c.Translator.SourceLang, c.Translator.TargetLang)
		fmt.Printf("  TTS Voice: %s\n", c.Translator.TTSVoice)
		fmt.Printf("  Opus Bitrate: %d\n", c.Translator.OpusBitrate)
	} else {
		fmt.Println("  Disabled")
	}

	fmt.Println("\n=================================")
}

func getSIPVideoFeedbackTransport() string {
	value := strings.ToLower(strings.TrimSpace(getEnvWithDefault("SIP_VIDEO_FEEDBACK_TRANSPORT", SIPVideoFeedbackTransportAuto)))
	switch value {
	case SIPVideoFeedbackTransportAuto, SIPVideoFeedbackTransportRTP, SIPVideoFeedbackTransportRTCP, SIPVideoFeedbackTransportDual:
		return value
	default:
		fmt.Printf("Warning: invalid SIP_VIDEO_FEEDBACK_TRANSPORT=%q, using %q\n", value, SIPVideoFeedbackTransportAuto)
		return SIPVideoFeedbackTransportAuto
	}
}

// maskDSN masks password in database DSN
func maskDSN(dsn string) string {
	// postgres://user:password@host:port/db -> postgres://user:****@host:port/db
	if idx := strings.Index(dsn, "://"); idx != -1 {
		rest := dsn[idx+3:]
		if atIdx := strings.Index(rest, "@"); atIdx != -1 {
			userPass := rest[:atIdx]
			if colonIdx := strings.Index(userPass, ":"); colonIdx != -1 {
				user := userPass[:colonIdx]
				return dsn[:idx+3] + user + ":****" + rest[atIdx:]
			}
		}
	}
	return dsn
}

// maskPassword masks a password string for secure display
func maskPassword(password string) string {
	if len(password) <= 4 {
		return "****"
	}
	return password[:2] + strings.Repeat("*", len(password)-4) + password[len(password)-2:]
}

// getEnvAsInt retrieves an environment variable as an integer with a default value
func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		fmt.Printf("Warning: Invalid value for %s: %s, using default: %d\n", key, valueStr, defaultValue)
		return defaultValue
	}
	return value
}

// getEnvAsBool retrieves an environment variable as a boolean with a default value
func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	valueStr = strings.ToLower(valueStr)
	return valueStr == "true" || valueStr == "1" || valueStr == "yes"
}

// getEnvWithDefault retrieves an environment variable or returns a default value
func getEnvWithDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// generateInstanceID generates a random instance ID if not provided
func generateInstanceID() string {
	// Try hostname first
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}
	// Fallback to random hex
	b := make([]byte, 8)
	if _, err := rand.Read(b); err == nil {
		return "gw-" + hex.EncodeToString(b)
	}
	return "gw-unknown"
}
