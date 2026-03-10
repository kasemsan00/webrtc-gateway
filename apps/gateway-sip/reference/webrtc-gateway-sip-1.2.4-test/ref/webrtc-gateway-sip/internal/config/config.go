package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all application configuration
type Config struct {
	TURN TURNConfig
	SIP  SIPConfig
	API  APIConfig
	RTP  RTPConfig
	DB   DBConfig
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

// TURNConfig holds TURN server configuration
type TURNConfig struct {
	Server   string
	Username string
	Password string
}

// SIPConfig holds SIP server configuration
type SIPConfig struct {
	Domain             string
	Username           string
	Password           string
	Port               int
	LocalPort          int
	LocalIP            string // Local IP to bind listeners (prevents IPv6, default: 0.0.0.0)
	PublicIP           string // Public IP address for NAT traversal (optional)
	DebugSIPMessage    bool   // Enable verbose SIP MESSAGE logging
	DebugSIPInvite     bool   // Enable verbose SIP INVITE logging (header dump)
	SwitchPLIDelayMS   int    // Delay in milliseconds before sending PLI on @switch message (default: 1000)
	AudioUseAVPF       bool   // Use RTP/AVPF profile for audio with RTCP feedback (default: false)
	VideoUseAVPF       bool   // Use RTP/AVPF profile for video with RTCP feedback (PLI/FIR/NACK) (default: false)
	VideoPreserveSTAPA bool   // Preserve STAP-A packets (don't de-aggregate) when they contain SPS+PPS+IDR (default: false)
}

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
			Port:               sipPortNum,
			LocalPort:          sipLocalPortNum,
			LocalIP:            getEnvWithDefault("SIP_LOCAL_IP", "0.0.0.0"),
			PublicIP:           os.Getenv("SIP_PUBLIC_IP"),
			DebugSIPMessage:    getEnvAsBool("DEBUG_SIP_MESSAGE", false),
			DebugSIPInvite:     getEnvAsBool("DEBUG_SIP_INVITE", false),
			SwitchPLIDelayMS:   getEnvAsInt("SWITCH_PLI_DELAY_MS", 1000),
			AudioUseAVPF:       getEnvAsBool("SIP_AUDIO_USE_AVPF", false),
			VideoUseAVPF:       getEnvAsBool("SIP_VIDEO_USE_AVPF", false),
			VideoPreserveSTAPA: getEnvAsBool("SIP_VIDEO_PRESERVE_STAPA", false),
		},
		API: APIConfig{
			Port:           apiPort,
			EnableWS:       getEnvAsBool("API_ENABLE_WS", true),
			EnableREST:     getEnvAsBool("API_ENABLE_REST", true),
			CORSOrigins:    getEnvWithDefault("API_CORS_ORIGINS", "*"),
			DebugWebSocket: getEnvAsBool("DEBUG_WEBSOCKET", false),
			DebugTURN:      getEnvAsBool("DEBUG_TURN", false),
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
	fmt.Printf("  Audio Use AVPF: %v\n", c.SIP.AudioUseAVPF)
	fmt.Printf("  Video Use AVPF: %v\n", c.SIP.VideoUseAVPF)
	fmt.Printf("  Video Preserve STAP-A: %v\n", c.SIP.VideoPreserveSTAPA)

	// Display API Configuration
	fmt.Println("\nAPI Configuration:")
	fmt.Printf("  Port: %d\n", c.API.Port)
	fmt.Printf("  WebSocket: %v\n", c.API.EnableWS)
	fmt.Printf("  REST API: %v\n", c.API.EnableREST)
	fmt.Printf("  CORS Origins: %s\n", c.API.CORSOrigins)
	fmt.Printf("  Debug WebSocket: %v\n", c.API.DebugWebSocket)
	fmt.Printf("  Debug TURN/ICE: %v\n", c.API.DebugTURN)

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

	fmt.Println("\n=================================")
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
