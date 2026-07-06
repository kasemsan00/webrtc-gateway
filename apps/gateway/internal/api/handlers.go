package api

import (
	"encoding/json"
	"net/http"
)

// OfferRequest represents a WebRTC offer request
type OfferRequest struct {
	SDP       string `json:"sdp"`
	SessionID string `json:"sessionId,omitempty"`
}

// OfferResponse contains the WebRTC answer
type OfferResponse struct {
	SDP       string `json:"sdp"`
	SessionID string `json:"sessionId"`
}

// CallRequest represents an outbound call request
type CallRequest struct {
	SessionID     string `json:"sessionId"`
	Destination   string `json:"destination"`
	From          string `json:"from,omitempty"`
	TrunkID       int64  `json:"trunkId,omitempty"`
	TrunkPublicID string `json:"trunkPublicId,omitempty"`
}

// CallResponse contains call initiation result
type CallResponse struct {
	SessionID string `json:"sessionId"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
}

// SessionResponse represents session information
type SessionResponse struct {
	ID                 string `json:"id"`
	State              string `json:"state"`
	Direction          string `json:"direction,omitempty"`
	From               string `json:"from,omitempty"`
	To                 string `json:"to,omitempty"`
	SIPCallID          string `json:"sipCallId,omitempty"`
	AuthMode           string `json:"authMode,omitempty"`
	TrunkID            int64  `json:"trunkId,omitempty"`
	TrunkName          string `json:"trunkName,omitempty"`
	SIPUsername        string `json:"sipUsername,omitempty"`
	DurationSec        int64  `json:"durationSec"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
	TranslatorEnabled  bool   `json:"translatorEnabled,omitempty"`
	TranslatorSrcLang  string `json:"translatorSrcLang,omitempty"`
	TranslatorTgtLang  string `json:"translatorTgtLang,omitempty"`
	TranslatorTTSVoice string `json:"translatorTtsVoice,omitempty"`
}

// DTMFRequest represents a DTMF request
type DTMFRequest struct {
	Digits string `json:"digits"`
}

// SwitchRequest represents a REST switch trigger request.
type SwitchRequest struct {
	SessionID     string `json:"sessionId"`
	QueueNumber   string `json:"queueNumber"`
	AgentUsername string `json:"agentUsername"`
}

// SwitchResponse contains switch trigger result.
type SwitchResponse struct {
	Status        string `json:"status"`
	SessionID     string `json:"sessionId"`
	QueueNumber   string `json:"queueNumber"`
	AgentUsername string `json:"agentUsername"`
	AutoMode      bool   `json:"autoMode,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// respondJSON sends a JSON response
func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError sends an error response
func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	s.respondJSON(w, status, ErrorResponse{Error: message})
}
