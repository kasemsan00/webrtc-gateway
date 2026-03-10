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

// DialogRecord represents SIP dialog state
type DialogRecord struct {
	SessionID     string
	Timestamp     time.Time
	SIPCallID     string
	FromTag       string
	ToTag         string
	RemoteContact string
	CSeq          int
	RouteSet      []string
}
