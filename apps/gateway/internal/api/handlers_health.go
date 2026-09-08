package api

import (
	"net/http"
	"regexp"
	"time"

	"webrtc-sip-gateway/internal/logstore"
)

var safeHealthReason = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

type healthState string

const (
	healthDisabled    healthState = "disabled"
	healthConnected   healthState = "connected"
	healthDegraded    healthState = "degraded"
	healthUnavailable healthState = "unavailable"
	healthUnknown     healthState = "unknown"
)

// HealthComponentResponse contains only allowlisted readiness facts. Reasons
// are stable machine-readable labels, never dependency error text.
type HealthComponentResponse struct {
	State         healthState       `json:"state"`
	Reason        string            `json:"reason,omitempty"`
	LastSuccessAt string            `json:"lastSuccessAt,omitempty"`
	LastFailureAt string            `json:"lastFailureAt,omitempty"`
	Details       map[string]uint64 `json:"details,omitempty"`
}

// NewHealthComponentResponse converts an external cached health snapshot to
// the API's bounded state vocabulary without exporting healthState itself.
func NewHealthComponentResponse(state, reason, lastSuccessAt, lastFailureAt string, details map[string]uint64) HealthComponentResponse {
	boundedState := healthUnknown
	switch state {
	case string(healthDisabled):
		boundedState = healthDisabled
	case string(healthConnected):
		boundedState = healthConnected
	case string(healthDegraded):
		boundedState = healthDegraded
	case string(healthUnavailable):
		boundedState = healthUnavailable
	case string(healthUnknown):
		boundedState = healthUnknown
	}
	return sanitizeHealthComponent(HealthComponentResponse{
		State: boundedState, Reason: reason, LastSuccessAt: lastSuccessAt,
		LastFailureAt: lastFailureAt, Details: details,
	})
}

type DetailedHealthResponse struct {
	CheckedAt  string                             `json:"checkedAt"`
	Components map[string]HealthComponentResponse `json:"components"`
}

func sanitizeHealthComponent(component HealthComponentResponse) HealthComponentResponse {
	if !safeHealthReason.MatchString(component.Reason) {
		component.Reason = ""
	}
	if component.LastSuccessAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, component.LastSuccessAt); err != nil {
			component.LastSuccessAt = ""
		}
	}
	if component.LastFailureAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, component.LastFailureAt); err != nil {
			component.LastFailureAt = ""
		}
	}
	if component.Details == nil {
		return component
	}
	bounded := make(map[string]uint64, 6)
	for _, key := range []string{"depth", "capacity", "accepted", "dropped", "exported", "failed"} {
		if value, ok := component.Details[key]; ok {
			bounded[key] = value
		}
	}
	component.Details = bounded
	return component
}

func (s *Server) persistenceHealth() (HealthComponentResponse, logstore.Health) {
	if s.logStore == nil {
		return HealthComponentResponse{State: healthUnavailable, Reason: "store_not_configured"}, logstore.Health{}
	}
	reporter, ok := s.logStore.(logstore.HealthReporter)
	if !ok {
		return HealthComponentResponse{State: healthUnknown, Reason: "readiness_not_reported"}, logstore.Health{}
	}
	health := reporter.LogStoreHealth()
	if !health.Enabled {
		return HealthComponentResponse{State: healthDisabled, Reason: "database_logging_disabled"}, health
	}
	if !health.Connected {
		return HealthComponentResponse{State: healthUnavailable, Reason: "database_pool_unavailable"}, health
	}
	response := HealthComponentResponse{State: healthConnected}
	if health.LastSuccessAt != nil {
		response.LastSuccessAt = health.LastSuccessAt.Format(time.RFC3339Nano)
	}
	if health.Events.Dropped > 0 || health.Stats.Dropped > 0 {
		response.State = healthDegraded
		response.Reason = "persistence_queue_dropping"
	}
	return response, health
}

func queueHealthComponent(enabled bool, queue logstore.QueueHealth) HealthComponentResponse {
	if !enabled {
		return HealthComponentResponse{State: healthDisabled, Reason: "database_logging_disabled"}
	}
	state := healthConnected
	reason := ""
	if queue.Dropped > 0 {
		state = healthDegraded
		reason = "queue_dropping"
	}
	return HealthComponentResponse{
		State:  state,
		Reason: reason,
		Details: map[string]uint64{
			"depth":    uint64(queue.Depth),
			"capacity": uint64(queue.Capacity),
			"accepted": queue.Accepted,
			"dropped":  queue.Dropped,
		},
	}
}

// handleDetailedHealth exposes cached, bounded operational facts. It never
// waits for a network probe, so API reads cannot delay media or signaling.
func (s *Server) handleDetailedHealth(w http.ResponseWriter, _ *http.Request) {
	database, persistence := s.persistenceHealth()
	components := map[string]HealthComponentResponse{
		"database":     database,
		"eventQueue":   queueHealthComponent(persistence.Enabled, persistence.Events),
		"statsQueue":   queueHealthComponent(persistence.Enabled, persistence.Stats),
		"sessionStore": {State: healthUnknown, Reason: "readiness_not_reported"},
		"sipListener":  {State: healthUnknown, Reason: "readiness_not_reported"},
		"translator":   {State: healthDisabled, Reason: "translator_not_configured"},
		"push":         {State: healthDisabled, Reason: "push_not_configured"},
	}
	if s.sessionMgr != nil {
		components["sessionStore"] = HealthComponentResponse{State: healthConnected}
	}
	if s.sipMaker != nil {
		components["sipListener"] = HealthComponentResponse{State: healthUnknown, Reason: "readiness_not_reported"}
	}
	if s.translatorClient != nil {
		components["translator"] = HealthComponentResponse{State: healthUnknown, Reason: "readiness_not_reported"}
	}
	if s.pushService != nil {
		components["push"] = HealthComponentResponse{State: healthUnknown, Reason: "readiness_not_reported"}
	}
	s.mu.RLock()
	for component, provider := range s.healthProviders {
		if provider != nil {
			components[component] = sanitizeHealthComponent(provider.OperationalHealth())
		}
	}
	s.mu.RUnlock()
	s.respondJSON(w, http.StatusOK, DetailedHealthResponse{
		CheckedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Components: components,
	})
}
