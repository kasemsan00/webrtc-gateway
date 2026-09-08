package config

import "strings"

// RedactedValue is returned for sensitive config fields in public views.
const RedactedValue = "****"

// PublicConfigView is a sanitized snapshot of runtime configuration.
type PublicConfigView struct {
	Sections PublicConfigSections `json:"sections"`
}

// PublicConfigSections groups configuration by subsystem.
type PublicConfigSections struct {
	TURN          TURNConfig                `json:"turn"`
	SIP           SIPConfig                 `json:"sip"`
	API           APIConfig                 `json:"api"`
	Auth          AuthConfig                `json:"auth"`
	RTP           RTPConfig                 `json:"rtp"`
	DB            DBConfig                  `json:"db"`
	SIPPublic     SIPPublicConfig           `json:"sipPublic"`
	SIPTrunk      SIPTrunkConfig            `json:"sipTrunk"`
	Gateway       GatewayConfig             `json:"gateway"`
	SessionDir    SessionDirectoryConfig    `json:"sessionDir"`
	Push          PushNotificationConfig    `json:"push"`
	Translator    TranslatorConfig          `json:"translator"`
	Observability PublicObservabilityConfig `json:"observability"`
}

// PublicObservabilityConfig deliberately omits endpoint, headers and arbitrary
// resource attributes because they may disclose private topology or secrets.
type PublicObservabilityConfig struct {
	Enable             bool    `json:"enable"`
	EndpointConfigured bool    `json:"endpointConfigured"`
	Protocol           string  `json:"protocol"`
	ServiceName        string  `json:"serviceName"`
	Environment        string  `json:"environment"`
	MetricsIntervalMS  int     `json:"metricsIntervalMs"`
	TracesEnable       bool    `json:"tracesEnable"`
	TraceSampleRatio   float64 `json:"traceSampleRatio"`
	MaxQueueSize       int     `json:"maxQueueSize"`
	MaxExportBatchSize int     `json:"maxExportBatchSize"`
	ScheduleDelayMS    int     `json:"scheduleDelayMs"`
	ExportTimeoutMS    int     `json:"exportTimeoutMs"`
	ShutdownTimeoutMS  int     `json:"shutdownTimeoutMs"`
}

// PublicView returns a copy of the configuration with secrets redacted.
func (c *Config) PublicView() PublicConfigView {
	if c == nil {
		return PublicConfigView{}
	}

	turn := c.TURN
	turn.Password = redactSecret(turn.Password)

	sip := c.SIP
	sip.Password = redactSecret(sip.Password)

	db := c.DB
	db.DSN = redactDSNForPublic(db.DSN)

	push := c.PushNotification
	push.TTRSClientSecret = redactSecret(push.TTRSClientSecret)
	push.FirebaseCredentialsFile = redactSecret(push.FirebaseCredentialsFile)
	push.APNSKeyFile = redactSecret(push.APNSKeyFile)
	push.APNSCertFile = redactSecret(push.APNSCertFile)
	push.APNSCertKeyFile = redactSecret(push.APNSCertKeyFile)

	auth := c.Auth
	auth.FrontendPassword = redactSecret(auth.FrontendPassword)

	observability := PublicObservabilityConfig{
		Enable:             c.Observability.Enable,
		EndpointConfigured: c.Observability.Endpoint != "",
		Protocol:           c.Observability.Protocol,
		ServiceName:        c.Observability.ServiceName,
		Environment:        c.Observability.Environment,
		MetricsIntervalMS:  c.Observability.MetricsIntervalMS,
		TracesEnable:       c.Observability.TracesEnable,
		TraceSampleRatio:   c.Observability.TraceSampleRatio,
		MaxQueueSize:       c.Observability.MaxQueueSize,
		MaxExportBatchSize: c.Observability.MaxExportBatchSize,
		ScheduleDelayMS:    c.Observability.ScheduleDelayMS,
		ExportTimeoutMS:    c.Observability.ExportTimeoutMS,
		ShutdownTimeoutMS:  c.Observability.ShutdownTimeoutMS,
	}

	return PublicConfigView{
		Sections: PublicConfigSections{
			TURN:          turn,
			SIP:           sip,
			API:           c.API,
			Auth:          auth,
			RTP:           c.RTP,
			DB:            db,
			SIPPublic:     c.SIPPublic,
			SIPTrunk:      c.SIPTrunk,
			Gateway:       c.Gateway,
			SessionDir:    c.SessionDir,
			Push:          push,
			Translator:    c.Translator,
			Observability: observability,
		},
	}
}

func redactSecret(value string) string {
	if value == "" {
		return ""
	}
	return RedactedValue
}

func redactDSNForPublic(dsn string) string {
	if dsn == "" {
		return ""
	}
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
	return RedactedValue
}
