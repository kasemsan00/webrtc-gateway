package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"k2-gateway/internal/logstore"
)

// PublicAccountResponse represents a public SIP account for REST responses
type PublicAccountResponse struct {
	Key                 string `json:"key"`
	Domain              string `json:"domain"`
	Port                int    `json:"port"`
	Username            string `json:"username"`
	IsRegistered        bool   `json:"isRegistered"`
	RefCountActiveCalls int    `json:"refCountActiveCalls"`
	LastUsedAt          string `json:"lastUsedAt"`
	ExpiresAt           string `json:"expiresAt"`
	LastError           string `json:"lastError,omitempty"`
}

// DashboardResponse represents gateway health and summary statistics
type DashboardResponse struct {
	InstanceID       string `json:"instanceId"`
	UptimeSeconds    int64  `json:"uptimeSeconds"`
	ActiveSessions   int    `json:"activeSessions"`
	TotalTrunks      int    `json:"totalTrunks"`
	EnabledTrunks    int    `json:"enabledTrunks"`
	RegisteredTrunks int    `json:"registeredTrunks"`
	PublicAccounts   int    `json:"publicAccounts"`
	WSClients        int    `json:"wsClients"`
	DBConnected      bool   `json:"dbConnected"`
}

type DashboardSummaryMetricsResponse struct {
	PeriodSessions      int     `json:"periodSessions"`
	ActiveSessions      int     `json:"activeSessions"`
	TotalTrunks         int     `json:"totalTrunks"`
	EnabledTrunks       int     `json:"enabledTrunks"`
	RegisteredTrunks    int     `json:"registeredTrunks"`
	PublicAccounts      int     `json:"publicAccounts"`
	SessionDirectoryNow int     `json:"sessionDirectoryNow"`
	WSClients           int     `json:"wsClients"`
	AvgDurationSec      float64 `json:"avgDurationSec"`
	MaxDurationSec      int     `json:"maxDurationSec"`
}

type DashboardSummarySeriesPointResponse struct {
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

type DashboardSummaryStateResponse struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

type DashboardSummaryTrunkResponse struct {
	TrunkKey  string `json:"trunkKey"`
	TrunkName string `json:"trunkName"`
	Count     int    `json:"count"`
}

type DashboardSummaryDirectionResponse struct {
	Direction string `json:"direction"`
	Count     int    `json:"count"`
}

type DashboardSummaryTerminalOutcomeResponse struct {
	Outcome       string `json:"outcome"`
	Direction     string `json:"direction"`
	SIPStatusCode int    `json:"sipStatusCode"`
	Count         int    `json:"count"`
}

type DashboardSummaryTerminalTrunkResponse struct {
	TrunkKey  string `json:"trunkKey"`
	TrunkName string `json:"trunkName"`
	Outcome   string `json:"outcome"`
	Count     int    `json:"count"`
}

type DashboardSummaryResponse struct {
	Period           string                                    `json:"period"`
	AnchorDate       string                                    `json:"anchorDate"`
	Timezone         string                                    `json:"timezone"`
	RangeStart       string                                    `json:"rangeStart"`
	RangeEnd         string                                    `json:"rangeEnd"`
	Metrics          DashboardSummaryMetricsResponse           `json:"metrics"`
	Series           []DashboardSummarySeriesPointResponse     `json:"series"`
	States           []DashboardSummaryStateResponse           `json:"states"`
	Directions       []DashboardSummaryDirectionResponse       `json:"directions"`
	TopTrunks        []DashboardSummaryTrunkResponse           `json:"topTrunks"`
	TerminalOutcomes []DashboardSummaryTerminalOutcomeResponse `json:"terminalOutcomes"`
	TerminalTrunks   []DashboardSummaryTerminalTrunkResponse   `json:"terminalTrunks"`
}

// WSClientResponse represents a connected WebSocket client
type WSClientResponse struct {
	ClientID              string `json:"clientId"`
	SessionID             string `json:"sessionId,omitempty"`
	ConnectedAt           string `json:"connectedAt"`
	TrunkResolved         bool   `json:"trunkResolved"`
	ResolvedTrunkID       int64  `json:"resolvedTrunkId,omitempty"`
	ResolvedTrunkPublicID string `json:"resolvedTrunkPublicId,omitempty"`
	Availability          string `json:"availability,omitempty"`
	CallState             string `json:"callState,omitempty"`
	AuthSubject           string `json:"authSubject,omitempty"`
	PublicOnly            bool   `json:"publicOnly,omitempty"`
	AgentOnly             bool   `json:"agentOnly,omitempty"`
	PresenceMode          string `json:"presenceMode,omitempty"`
	AgentTrunkRefCount    int    `json:"agentTrunkRefCount,omitempty"`
}

// --- Ops Handlers ---

// GatewayInstanceResponse represents a gateway instance entry
type GatewayInstanceResponse struct {
	InstanceID string `json:"instanceId"`
	WSURL      string `json:"wsUrl"`
	ExpiresAt  string `json:"expiresAt"`
	UpdatedAt  string `json:"updatedAt"`
	IsExpired  bool   `json:"isExpired"`
}

// GatewayInstanceListResponse represents a paginated list of instances
type GatewayInstanceListResponse struct {
	Items    []GatewayInstanceResponse `json:"items"`
	Total    int                       `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"pageSize"`
}

// handleListGatewayInstances returns gateway instances
func (s *Server) handleListGatewayInstances(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	result, err := s.logStore.ListGatewayInstances(r.Context(), logstore.GatewayInstanceListParams{
		Page:     page,
		PageSize: pageSize,
		Search:   q.Get("search"),
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list instances: %v", err))
		return
	}

	now := time.Now()
	items := make([]GatewayInstanceResponse, 0, len(result.Items))
	for _, gi := range result.Items {
		items = append(items, GatewayInstanceResponse{
			InstanceID: gi.InstanceID,
			WSURL:      gi.WSURL,
			ExpiresAt:  gi.ExpiresAt.Format(time.RFC3339),
			UpdatedAt:  gi.UpdatedAt.Format(time.RFC3339),
			IsExpired:  gi.ExpiresAt.Before(now),
		})
	}

	s.respondJSON(w, http.StatusOK, GatewayInstanceListResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
}

// SessionDirectoryResponse represents a session directory entry
type SessionDirectoryResponse struct {
	SessionID       string `json:"sessionId"`
	OwnerInstanceID string `json:"ownerInstanceId"`
	WSURL           string `json:"wsUrl"`
	ExpiresAt       string `json:"expiresAt"`
	UpdatedAt       string `json:"updatedAt"`
	IsExpired       bool   `json:"isExpired"`
}

// SessionDirectoryListResponse represents a paginated list of session directory entries
type SessionDirectoryListResponse struct {
	Items    []SessionDirectoryResponse `json:"items"`
	Total    int                        `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"pageSize"`
}

// handleListSessionDirectory returns session directory entries
func (s *Server) handleListSessionDirectory(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	result, err := s.logStore.ListSessionDirectory(r.Context(), logstore.SessionDirectoryListParams{
		Page:     page,
		PageSize: pageSize,
		Search:   q.Get("search"),
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list session directory: %v", err))
		return
	}

	now := time.Now()
	items := make([]SessionDirectoryResponse, 0, len(result.Items))
	for _, sd := range result.Items {
		items = append(items, SessionDirectoryResponse{
			SessionID:       sd.SessionID,
			OwnerInstanceID: sd.OwnerInstanceID,
			WSURL:           sd.WSURL,
			ExpiresAt:       sd.ExpiresAt.Format(time.RFC3339),
			UpdatedAt:       sd.UpdatedAt.Format(time.RFC3339),
			IsExpired:       sd.ExpiresAt.Before(now),
		})
	}

	s.respondJSON(w, http.StatusOK, SessionDirectoryListResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
}

// handleListPublicAccounts returns all registered public SIP accounts
func (s *Server) handleListPublicAccounts(w http.ResponseWriter, r *http.Request) {
	if s.publicRegistry == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Public account registry not available")
		return
	}

	accounts := s.publicRegistry.ListAccounts()
	response := make([]PublicAccountResponse, 0, len(accounts))

	for _, acc := range accounts {
		// Note: acc fields might be subject to race conditions, but ListAccounts
		// already provides a snapshot under the registry mutex
		item := PublicAccountResponse{
			Key:                 acc.Key,
			Domain:              acc.Domain,
			Port:                acc.Port,
			Username:            acc.Username,
			IsRegistered:        acc.IsRegistered,
			RefCountActiveCalls: acc.RefCountActiveCalls,
			LastUsedAt:          acc.LastUsedAt.Format(time.RFC3339),
			ExpiresAt:           acc.ExpiresAt.Format(time.RFC3339),
			LastError:           acc.LastError,
		}
		response = append(response, item)
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleListWSClients returns all connected WebSocket clients (including idle)
func (s *Server) handleListWSClients(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	clients := make([]WSClientResponse, 0, len(s.wsConnections))
	for client := range s.wsConnections {
		clients = append(clients, s.buildWSClientResponse(client))
	}
	s.mu.RUnlock()

	s.respondJSON(w, http.StatusOK, clients)
}

// handleDashboard returns gateway health and summary statistics
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Count active sessions
	activeSessions := 0
	if s.sessionMgr != nil {
		activeSessions = len(s.sessionMgr.ListSessions())
	}

	// Count trunks
	totalTrunks := 0
	enabledTrunks := 0
	registeredTrunks := 0
	if s.trunkManager != nil {
		trunks := s.trunkManager.ListOwnedTrunks()
		totalTrunks = len(trunks)
		for _, trunk := range trunks {
			if trunk.Enabled {
				enabledTrunks++
			}
			if trunk.LastRegisteredAt != nil {
				registeredTrunks++
			}
		}
	}

	// Count public accounts
	publicAccounts := 0
	if s.publicRegistry != nil {
		publicAccounts = len(s.publicRegistry.ListAccounts())
	}

	// Count WS clients
	s.mu.RLock()
	wsClients := len(s.wsClients)
	s.mu.RUnlock()

	// Check DB connection
	dbConnected := s.logStore != nil

	// Calculate uptime
	uptime := int64(time.Since(s.startTime).Seconds())

	response := DashboardResponse{
		InstanceID:       s.gatewayConfig.InstanceID,
		UptimeSeconds:    uptime,
		ActiveSessions:   activeSessions,
		TotalTrunks:      totalTrunks,
		EnabledTrunks:    enabledTrunks,
		RegisteredTrunks: registeredTrunks,
		PublicAccounts:   publicAccounts,
		WSClients:        wsClients,
		DBConnected:      dbConnected,
	}

	s.respondJSON(w, http.StatusOK, response)
}

func dashboardSummaryLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Bangkok")
	if err == nil {
		return location
	}

	// Fallback when tzdata is unavailable in runtime images.
	return time.FixedZone("Asia/Bangkok", 7*60*60)
}

func parseDashboardSummaryRange(period, anchorDate string) (time.Time, time.Time, string, error) {
	location := dashboardSummaryLocation()

	nowInLocation := time.Now().In(location)
	if anchorDate == "" {
		anchorDate = nowInLocation.Format("2006-01-02")
	}

	anchor, err := time.ParseInLocation("2006-01-02", anchorDate, location)
	if err != nil {
		return time.Time{}, time.Time{}, "", fmt.Errorf("anchorDate must be YYYY-MM-DD")
	}

	anchor = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, location)

	var startLocal time.Time
	var endLocal time.Time

	switch period {
	case "day":
		startLocal = anchor
		endLocal = startLocal.AddDate(0, 0, 1)
	case "month":
		startLocal = time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, location)
		endLocal = startLocal.AddDate(0, 1, 0)
	case "year":
		startLocal = time.Date(anchor.Year(), time.January, 1, 0, 0, 0, 0, location)
		endLocal = startLocal.AddDate(1, 0, 0)
	default:
		return time.Time{}, time.Time{}, "", fmt.Errorf("period must be day, month, or year")
	}

	return startLocal.UTC(), endLocal.UTC(), anchor.Format("2006-01-02"), nil
}

// handleDashboardSummary returns aggregate dashboard data for a selected period.
func (s *Server) handleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Database logging not available")
		return
	}

	query := r.URL.Query()
	period := strings.ToLower(strings.TrimSpace(query.Get("period")))
	if period == "" {
		period = "day"
	}

	rangeStartUTC, rangeEndUTC, normalizedAnchor, err := parseDashboardSummaryRange(period, strings.TrimSpace(query.Get("anchorDate")))
	if err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	summary, err := s.logStore.GetDashboardSummary(r.Context(), logstore.DashboardSummaryParams{
		Period:        period,
		RangeStartUTC: rangeStartUTC,
		RangeEndUTC:   rangeEndUTC,
		Timezone:      "Asia/Bangkok",
		TopTrunks:     10,
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load dashboard summary: %v", err))
		return
	}

	activeSessions := 0
	if s.sessionMgr != nil {
		activeSessions = len(s.sessionMgr.ListSessions())
	}

	totalTrunks := 0
	enabledTrunks := 0
	registeredTrunks := 0
	if s.trunkManager != nil {
		trunks := s.trunkManager.ListOwnedTrunks()
		totalTrunks = len(trunks)
		for _, trunk := range trunks {
			if trunk.Enabled {
				enabledTrunks++
			}
			if trunk.LastRegisteredAt != nil {
				registeredTrunks++
			}
		}
	}

	publicAccounts := 0
	if s.publicRegistry != nil {
		publicAccounts = len(s.publicRegistry.ListAccounts())
	}

	s.mu.RLock()
	wsClients := len(s.wsClients)
	s.mu.RUnlock()

	series := make([]DashboardSummarySeriesPointResponse, 0, len(summary.Series))
	for _, point := range summary.Series {
		series = append(series, DashboardSummarySeriesPointResponse{
			Bucket: point.Bucket,
			Count:  point.Count,
		})
	}

	states := make([]DashboardSummaryStateResponse, 0, len(summary.States))
	for _, state := range summary.States {
		states = append(states, DashboardSummaryStateResponse{
			State: state.State,
			Count: state.Count,
		})
	}

	topTrunks := make([]DashboardSummaryTrunkResponse, 0, len(summary.TopTrunks))
	for _, trunk := range summary.TopTrunks {
		topTrunks = append(topTrunks, DashboardSummaryTrunkResponse{
			TrunkKey:  trunk.TrunkKey,
			TrunkName: trunk.TrunkName,
			Count:     trunk.Count,
		})
	}

	directions := make([]DashboardSummaryDirectionResponse, 0, len(summary.Directions))
	for _, dir := range summary.Directions {
		directions = append(directions, DashboardSummaryDirectionResponse{
			Direction: dir.Direction,
			Count:     dir.Count,
		})
	}

	terminalOutcomes := make([]DashboardSummaryTerminalOutcomeResponse, 0, len(summary.TerminalOutcomes))
	for _, outcome := range summary.TerminalOutcomes {
		terminalOutcomes = append(terminalOutcomes, DashboardSummaryTerminalOutcomeResponse{
			Outcome:       outcome.Outcome,
			Direction:     outcome.Direction,
			SIPStatusCode: outcome.SIPStatusCode,
			Count:         outcome.Count,
		})
	}

	terminalTrunks := make([]DashboardSummaryTerminalTrunkResponse, 0, len(summary.TerminalTrunks))
	for _, trunk := range summary.TerminalTrunks {
		terminalTrunks = append(terminalTrunks, DashboardSummaryTerminalTrunkResponse{
			TrunkKey:  trunk.TrunkKey,
			TrunkName: trunk.TrunkName,
			Outcome:   trunk.Outcome,
			Count:     trunk.Count,
		})
	}

	s.respondJSON(w, http.StatusOK, DashboardSummaryResponse{
		Period:     period,
		AnchorDate: normalizedAnchor,
		Timezone:   "Asia/Bangkok",
		RangeStart: rangeStartUTC.Format(time.RFC3339),
		RangeEnd:   rangeEndUTC.Format(time.RFC3339),
		Metrics: DashboardSummaryMetricsResponse{
			PeriodSessions:      summary.TotalSessions,
			ActiveSessions:      activeSessions,
			TotalTrunks:         totalTrunks,
			EnabledTrunks:       enabledTrunks,
			RegisteredTrunks:    registeredTrunks,
			PublicAccounts:      publicAccounts,
			SessionDirectoryNow: summary.SessionDirectoryCount,
			WSClients:           wsClients,
			AvgDurationSec:      summary.AvgDurationSec,
			MaxDurationSec:      summary.MaxDurationSec,
		},
		Series:           series,
		States:           states,
		Directions:       directions,
		TopTrunks:        topTrunks,
		TerminalOutcomes: terminalOutcomes,
		TerminalTrunks:   terminalTrunks,
	})
}
