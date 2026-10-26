package api

import (
	"net/http"

	"webrtc-sip-gateway/internal/buildinfo"
)

// handleVersion reports which gateway build is running. It is public like
// /api/logs so an operator can check a deploy without a token.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.respondJSON(w, http.StatusOK, buildinfo.Get())
}
