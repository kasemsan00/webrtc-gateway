package logstore

import "time"

// SessionRecord represents a call session snapshot
type SessionRecord struct {
	SessionID     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	EndedAt       *time.Time
	Direction     string // "inbound" | "outbound"
	FromURI       string
	ToURI         string
	SIPCallID     string
	FinalState    string
	EndReason     string
	RTPAudioPort  int
	RTPVideoPort  int
	RTCPAudioPort int
	RTCPVideoPort int
	SIPOpusPT     int
	AudioProfile  string
	VideoProfile  string
	VideoRejected bool
	AuthMode      string // "public" | "trunk" | ""
	TrunkID       *int64
	TrunkName     string
	SIPUsername   string
	Meta          map[string]interface{}
}

// Event represents a timeline event
type Event struct {
	Timestamp     time.Time
	SessionID     string
	Category      string // "ws", "rest", "sip", "sdp", "ice", "media"
	Name          string // "ws_offer_received", "sip_invite_sent", etc.
	SIPMethod     string
	SIPStatusCode int
	SIPCallID     string
	State         string
	PayloadID     *int64
	Data          map[string]interface{}
}

// PayloadRecord represents large payloads (SDP, SIP messages)
type PayloadRecord struct {
	SessionID    string
	Timestamp    time.Time
	Kind         string // "webrtc_sdp_offer", "sip_sdp_answer", "sip_message", etc.
	ContentType  string
	BodyText     string
	BodyBytesB64 string
	Parsed       map[string]interface{}
}

// StatsRecord represents periodic RTP/RTCP stats
type StatsRecord struct {
	Timestamp      time.Time
	SessionID      string
	PLISent        int
	PLIResponse    int
	LastPLISentAt  *time.Time
	LastKeyframeAt *time.Time
	AudioRTCPRR    int
	AudioRTCPSR    int
	VideoRTCPRR    int
	VideoRTCPSR    int
	Data           map[string]interface{}
}

// SessionListParams defines query parameters for listing call sessions with pagination, search, and filtering.
type SessionListParams struct {
	Page          int        // 1-based page number (default 1)
	PageSize      int        // Items per page (default 20, max 100)
	SessionID     string     // Filter by exact session ID
	Direction     string     // Filter by direction: "inbound" or "outbound"
	Search        string     // Search by from_uri, to_uri, session_id, or sip_call_id (ILIKE)
	State         string     // Filter by final_state
	CreatedAfter  *time.Time // Filter sessions created after this time
	CreatedBefore *time.Time // Filter sessions created before this time
}

// SessionListResult contains paginated session list results.
type SessionListResult struct {
	Items    []*SessionRecord `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

// DialogRecord represents SIP dialog state
type DialogRecord struct {
	ID            int64
	SessionID     string
	Timestamp     time.Time
	SIPCallID     string
	FromTag       string
	ToTag         string
	RemoteContact string
	CSeq          int
	RouteSet      []string
}

// EventRecord represents a read-back event row (includes ID)
type EventRecord struct {
	ID            int64
	Timestamp     time.Time
	SessionID     string
	Category      string
	Name          string
	SIPMethod     string
	SIPStatusCode int
	SIPCallID     string
	State         string
	PayloadID     *int64
	Data          map[string]interface{}
}

// PayloadReadRecord represents a read-back payload row (includes PayloadID)
type PayloadReadRecord struct {
	PayloadID    int64
	Timestamp    time.Time
	SessionID    string
	Kind         string
	ContentType  string
	BodyText     string
	BodyBytesB64 string
	Parsed       map[string]interface{}
}

// StatsReadRecord represents a read-back stats row (includes ID)
type StatsReadRecord struct {
	ID             int64
	Timestamp      time.Time
	SessionID      string
	PLISent        int
	PLIResponse    int
	LastPLISentAt  *time.Time
	LastKeyframeAt *time.Time
	AudioRTCPRR    int
	AudioRTCPSR    int
	VideoRTCPRR    int
	VideoRTCPSR    int
	Data           map[string]interface{}
}

// GatewayInstanceRecord represents a gateway instance row
type GatewayInstanceRecord struct {
	InstanceID string
	WSURL      string
	ExpiresAt  time.Time
	UpdatedAt  time.Time
}

// SessionDirectoryRecord represents a session directory row
type SessionDirectoryRecord struct {
	SessionID       string
	OwnerInstanceID string
	WSURL           string
	ExpiresAt       time.Time
	UpdatedAt       time.Time
}

// --- List params/results for new queries ---

// EventListParams defines query parameters for listing events
type EventListParams struct {
	Page      int
	PageSize  int
	SessionID string // required
	Category  string
	Name      string
}

// EventListResult contains paginated event list results
type EventListResult struct {
	Items    []*EventRecord `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

// PayloadListParams defines query parameters for listing payloads
type PayloadListParams struct {
	Page      int
	PageSize  int
	SessionID string // required
	Kind      string
}

// PayloadListResult contains paginated payload list results
type PayloadListResult struct {
	Items    []*PayloadReadRecord `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"pageSize"`
}

// DialogListParams defines query parameters for listing dialogs
type DialogListParams struct {
	Page      int
	PageSize  int
	SessionID string // required
}

// DialogListResult contains paginated dialog list results
type DialogListResult struct {
	Items    []*DialogRecord `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

// StatsListParams defines query parameters for listing stats
type StatsListParams struct {
	Page      int
	PageSize  int
	SessionID string // required
}

// StatsListResult contains paginated stats list results
type StatsListResult struct {
	Items    []*StatsReadRecord `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

// GatewayInstanceListParams defines query parameters for listing gateway instances
type GatewayInstanceListParams struct {
	Page     int
	PageSize int
	Search   string
}

// GatewayInstanceListResult contains paginated gateway instance list results
type GatewayInstanceListResult struct {
	Items    []*GatewayInstanceRecord `json:"items"`
	Total    int                      `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"pageSize"`
}

// SessionDirectoryListParams defines query parameters for listing session directory
type SessionDirectoryListParams struct {
	Page     int
	PageSize int
	Search   string
}

// SessionDirectoryListResult contains paginated session directory list results
type SessionDirectoryListResult struct {
	Items    []*SessionDirectoryRecord `json:"items"`
	Total    int                       `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"pageSize"`
}
