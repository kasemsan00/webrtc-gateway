package api

import (
	"net/http"

	"k2-gateway/internal/config"
)

// GatewayConfigResponse exposes the effective runtime configuration loaded from env.
type GatewayConfigResponse struct {
	InstanceID string                    `json:"instanceId"`
	Source     string                    `json:"source"`
	Sections   config.PublicConfigSections `json:"sections"`
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.runtimeConfig == nil {
		s.respondError(w, http.StatusServiceUnavailable, "runtime config not available")
		return
	}

	view := s.runtimeConfig.PublicView()
	s.respondJSON(w, http.StatusOK, GatewayConfigResponse{
		InstanceID: s.gatewayConfig.InstanceID,
		Source:     "env",
		Sections:   view.Sections,
	})
}
