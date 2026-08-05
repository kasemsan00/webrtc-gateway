package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
)

// TrunkResponse represents a SIP trunk entry for REST responses
type TrunkResponse struct {
	ID                 int64    `json:"id"`
	PublicID           string   `json:"public_id"`
	PublicIDCompat     string   `json:"publicId"`
	Name               string   `json:"name"`
	Domain             string   `json:"domain"`
	Port               int      `json:"port"`
	Username           string   `json:"username"`
	Transport          string   `json:"transport"`
	Enabled            bool     `json:"enabled"`
	IsDefault          bool     `json:"isDefault"`
	ActiveCallCount    int      `json:"activeCallCount"`
	ActiveDestinations []string `json:"activeDestinations,omitempty"`
	LeaseOwner         string   `json:"leaseOwner,omitempty"`
	LeaseUntil         string   `json:"leaseUntil,omitempty"`
	LastRegisteredAt   string   `json:"lastRegisteredAt,omitempty"`
	LastUnregisteredAt string   `json:"lastUnregisteredAt,omitempty"`
	IsRegistered       bool     `json:"isRegistered"`
	LastError          string   `json:"lastError,omitempty"`
	SipAutoRegister    bool     `json:"sipAutoRegister"`
	InUseBy            *string  `json:"inUseBy,omitempty"`
	LastOnlinePlatform string   `json:"lastOnlinePlatform,omitempty"`
	LastOnlineAt       string   `json:"lastOnlineAt,omitempty"`
	PNAppID            string   `json:"pnAppId,omitempty"`
	PNType             string   `json:"pnType,omitempty"`
	PNTokenMasked      string   `json:"pnTokenMasked,omitempty"`
	PNUpdatedAt        string   `json:"pnUpdatedAt,omitempty"`
	PushContactReady   bool     `json:"pushContactReady"`
	CreatedAt          string   `json:"createdAt"`
	UpdatedAt          string   `json:"updatedAt"`
}

type UpdateTrunkRequest struct {
	Name      *string `json:"name"`
	Domain    *string `json:"domain"`
	Port      *int    `json:"port"`
	Username  *string `json:"username"`
	Password  *string `json:"password"`
	Transport *string `json:"transport"`
	Enabled   *bool   `json:"enabled"`
	IsDefault *bool   `json:"isDefault"`
	UpdatedBy *string `json:"updatedBy"`
}

// CreateTrunkRequest represents a request to create a new trunk
type CreateTrunkRequest struct {
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Transport string `json:"transport"`
	Enabled   *bool  `json:"enabled"`
	IsDefault *bool  `json:"isDefault"`
}

// TrunkListResponse represents a paginated list of trunks
type TrunkListResponse struct {
	Items    []TrunkResponse `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

// handleCreateTrunk creates a new SIP trunk
func (s *Server) handleCreateTrunk(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	var req CreateTrunkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		s.respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	domain := strings.TrimSpace(req.Domain)
	if domain == "" {
		s.respondError(w, http.StatusBadRequest, "domain is required")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		s.respondError(w, http.StatusBadRequest, "username is required")
		return
	}
	password := strings.TrimSpace(req.Password)
	if password == "" {
		s.respondError(w, http.StatusBadRequest, "password is required")
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		req.Port = 5060
	}
	transport := strings.ToLower(strings.TrimSpace(req.Transport))
	if transport != "tcp" && transport != "udp" {
		transport = "tcp"
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	isDefault := false
	if req.IsDefault != nil {
		isDefault = *req.IsDefault
	}

	payload := sip.CreateTrunkPayload{
		Name:      name,
		Domain:    domain,
		Port:      req.Port,
		Username:  username,
		Password:  password,
		Transport: transport,
		Enabled:   enabled,
		IsDefault: isDefault,
	}

	trunk, err := s.trunkManager.CreateTrunk(r.Context(), payload)
	if err != nil {
		switch {
		case errors.Is(err, sip.ErrTrunkValidation):
			s.respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, sip.ErrTrunkConflict):
			s.respondError(w, http.StatusConflict, err.Error())
		default:
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create trunk: %v", err))
		}
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunk_created",
		Data: map[string]interface{}{
			"trunkId":  trunk.ID,
			"name":     trunk.Name,
			"domain":   trunk.Domain,
			"username": trunk.Username,
		},
	})
	createdID := trunk.ID
	s.notifyTrunkListChanged("created", &createdID)

	s.respondJSON(w, http.StatusCreated, trunkResponseFrom(trunk, 0, nil))
}

// handleListTrunks returns trunks with pagination, search, and time filtering
func (s *Server) handleListTrunks(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	q := r.URL.Query()

	// Parse pagination
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	// Parse filters
	trunkID, _ := strconv.ParseInt(q.Get("trunkId"), 10, 64)
	trunkPublicID := q.Get("trunkPublicId")
	if trunkPublicID != "" {
		normalized, ok := sip.NormalizeTrunkPublicID(trunkPublicID)
		if !ok {
			s.respondError(w, http.StatusBadRequest, "Invalid trunk public ID")
			return
		}
		trunkPublicID = normalized
	}
	search := q.Get("search")

	var createdAfter, createdBefore *time.Time
	if v := q.Get("createdAfter"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			createdAfter = &t
		}
	}
	if v := q.Get("createdBefore"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			createdBefore = &t
		}
	}

	// Parse sort parameters
	sortBy := q.Get("sortBy")
	sortDir := q.Get("sortDir")

	params := sip.TrunkListParams{
		Page:          page,
		PageSize:      pageSize,
		TrunkID:       trunkID,
		TrunkPublicID: trunkPublicID,
		Search:        search,
		CreatedAfter:  createdAfter,
		CreatedBefore: createdBefore,
		SortBy:        sortBy,
		SortDir:       sortDir,
	}

	result, err := s.trunkManager.ListTrunks(r.Context(), params)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list trunks: %v", err))
		return
	}

	// Build active-call count per trunk from in-memory sessions
	callInfoByTrunk := s.collectActiveCallsByTrunk()

	items := make([]TrunkResponse, 0, len(result.Items))
	for _, trunk := range result.Items {
		active := callInfoByTrunk[trunk.ID]
		items = append(items, trunkResponseFrom(trunk, active.Count, active.Destinations))
	}

	s.respondJSON(w, http.StatusOK, TrunkListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	})
}

// handleGetTrunk returns a single trunk by ID with active call count
func (s *Server) handleGetTrunk(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]
	trunkID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || trunkID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid trunk ID")
		return
	}

	trunk, err := s.trunkManager.GetTrunkByIDFromDB(r.Context(), trunkID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("Trunk not found: %v", err))
		return
	}

	callInfoByTrunk := s.collectActiveCallsByTrunk()
	active := callInfoByTrunk[trunk.ID]
	s.respondJSON(w, http.StatusOK, trunkResponseFrom(trunk, active.Count, active.Destinations))
}

// handleUpdateTrunk updates a trunk by ID with partial patch semantics.
func (s *Server) handleUpdateTrunk(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]
	trunkID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || trunkID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid trunk ID")
		return
	}

	var req UpdateTrunkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validation + normalization
	if req.Name != nil {
		v := strings.TrimSpace(*req.Name)
		if v == "" {
			s.respondError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		req.Name = &v
	}
	if req.Domain != nil {
		v := strings.TrimSpace(*req.Domain)
		if v == "" {
			s.respondError(w, http.StatusBadRequest, "domain cannot be empty")
			return
		}
		req.Domain = &v
	}
	if req.Username != nil {
		v := strings.TrimSpace(*req.Username)
		if v == "" {
			s.respondError(w, http.StatusBadRequest, "username cannot be empty")
			return
		}
		req.Username = &v
	}
	if req.Port != nil && (*req.Port < 1 || *req.Port > 65535) {
		s.respondError(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if req.Transport != nil {
		v := strings.ToLower(strings.TrimSpace(*req.Transport))
		if v != "tcp" && v != "udp" {
			s.respondError(w, http.StatusBadRequest, "transport must be tcp or udp")
			return
		}
		req.Transport = &v
	}
	if req.Password != nil {
		trimmed := strings.TrimSpace(*req.Password)
		if trimmed == "" {
			// Optional replace: empty means no password change.
			req.Password = nil
		} else {
			req.Password = &trimmed
		}
	}

	callInfoByTrunk := s.collectActiveCallsByTrunk()
	activeCallCount := callInfoByTrunk[trunkID].Count
	if activeCallCount > 0 {
		if req.Enabled != nil && !*req.Enabled {
			s.respondError(w, http.StatusConflict, "cannot disable trunk while active calls exist")
			return
		}
		if req.Domain != nil || req.Port != nil || req.Username != nil || req.Transport != nil {
			s.respondError(w, http.StatusConflict, "cannot update domain/port/username/transport while active calls exist")
			return
		}
	}

	patch := sip.TrunkUpdatePatch{
		Name:      req.Name,
		Domain:    req.Domain,
		Port:      req.Port,
		Username:  req.Username,
		Password:  req.Password,
		Transport: req.Transport,
		Enabled:   req.Enabled,
		IsDefault: req.IsDefault,
		UpdatedBy: req.UpdatedBy,
	}

	trunk, err := s.trunkManager.UpdateTrunk(r.Context(), trunkID, patch)
	if err != nil {
		switch {
		case errors.Is(err, sip.ErrTrunkValidation):
			s.respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, sip.ErrTrunkNotFound):
			s.respondError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, sip.ErrTrunkConflict):
			s.respondError(w, http.StatusConflict, err.Error())
		default:
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update trunk: %v", err))
		}
		return
	}

	eventData := map[string]interface{}{
		"trunkId": trunkID,
	}
	if req.UpdatedBy != nil {
		eventData["updatedBy"] = *req.UpdatedBy
	}
	if req.Enabled != nil {
		eventData["enabled"] = *req.Enabled
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunk_updated",
		Data:      eventData,
	})
	updatedID := trunk.ID
	s.notifyTrunkListChanged("updated", &updatedID)

	callInfoByTrunk = s.collectActiveCallsByTrunk()
	active := callInfoByTrunk[trunk.ID]
	s.respondJSON(w, http.StatusOK, trunkResponseFrom(trunk, active.Count, active.Destinations))
}

// handleRefreshTrunks triggers a trunk reload from the database
func (s *Server) handleRefreshTrunks(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	if err := s.trunkManager.RefreshTrunks(); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to refresh trunks: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunks_refreshed",
	})
	s.notifyTrunkListChanged("refreshed", nil)

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

type trunkActiveCallInfo struct {
	Count        int
	Destinations []string
}

// collectActiveCallsByTrunk returns a map of trunkID -> active call summary.
func (s *Server) collectActiveCallsByTrunk() map[int64]trunkActiveCallInfo {
	infoByTrunk := make(map[int64]trunkActiveCallInfo)
	for _, sess := range s.sessionMgr.ListSessions() {
		state := sess.GetState()
		if state == session.StateEnded {
			continue
		}
		mode, _, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
		if mode == "trunk" && trunkID > 0 {
			activeInfo := infoByTrunk[trunkID]
			activeInfo.Count++
			_, _, to, _ := sess.GetCallInfo()
			if destination := strings.TrimSpace(to); destination != "" {
				activeInfo.Destinations = append(activeInfo.Destinations, destination)
			}
			infoByTrunk[trunkID] = activeInfo
		}
	}
	return infoByTrunk
}

// handleTrunkUnregister unregisters a trunk (force)
func (s *Server) handleTrunkRegister(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]
	trunkID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || trunkID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid trunk ID")
		return
	}

	if err := s.trunkManager.RegisterTrunk(trunkID, true); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to register trunk: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunk_registered",
		Data:      map[string]interface{}{"trunkId": trunkID, "force": true},
	})
	s.notifyTrunkListChanged("registered", &trunkID)

	s.respondJSON(w, http.StatusOK, map[string]interface{}{"trunkId": trunkID, "status": "registered"})
}

// handleTrunkUnregister unregisters a trunk (force)
func (s *Server) handleTrunkUnregister(w http.ResponseWriter, r *http.Request) {
	if s.trunkManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Trunk manager not available")
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]
	trunkID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || trunkID <= 0 {
		s.respondError(w, http.StatusBadRequest, "Invalid trunk ID")
		return
	}

	if err := s.trunkManager.UnregisterTrunk(trunkID, true); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to unregister trunk: %v", err))
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		Category:  "rest",
		Name:      "rest_trunk_unregistered",
		Data:      map[string]interface{}{"trunkId": trunkID, "force": true},
	})
	s.notifyTrunkListChanged("unregistered", &trunkID)

	s.respondJSON(w, http.StatusOK, map[string]interface{}{"trunkId": trunkID, "status": "unregistered"})
}

func trunkResponseFrom(trunk *sip.Trunk, activeCallCount int, activeDestinations []string) TrunkResponse {
	isRegistered := trunk.LastRegisteredAt != nil
	if trunk.LastError != nil && *trunk.LastError != "" {
		isRegistered = false
	}

	response := TrunkResponse{
		ID:                 trunk.ID,
		PublicID:           trunk.PublicID,
		PublicIDCompat:     trunk.PublicID,
		Name:               trunk.Name,
		Domain:             trunk.Domain,
		Port:               trunk.Port,
		Username:           trunk.Username,
		Transport:          trunk.Transport,
		Enabled:            trunk.Enabled,
		IsDefault:          trunk.IsDefault,
		ActiveCallCount:    activeCallCount,
		ActiveDestinations: append([]string(nil), activeDestinations...),
		LeaseUntil:         formatOptionalTime(trunk.LeaseUntil),
		LastRegisteredAt:   formatOptionalTime(trunk.LastRegisteredAt),
		LastUnregisteredAt: formatOptionalTime(trunk.LastUnregisteredAt),
		IsRegistered:       isRegistered,
		SipAutoRegister:    trunk.SipAutoRegister,
		CreatedAt:          trunk.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          trunk.UpdatedAt.Format(time.RFC3339),
	}
	if trunk.LeaseOwner != nil {
		response.LeaseOwner = *trunk.LeaseOwner
	}
	if trunk.LastError != nil {
		response.LastError = *trunk.LastError
	}
	if trunk.InUseBy != nil {
		response.InUseBy = trunk.InUseBy
	}
	if trunk.LastOnlinePlatform != nil {
		response.LastOnlinePlatform = *trunk.LastOnlinePlatform
	}
	if trunk.LastOnlineAt != nil {
		response.LastOnlineAt = trunk.LastOnlineAt.Format(time.RFC3339)
	}
	if trunk.PNAppID != nil {
		response.PNAppID = *trunk.PNAppID
	}
	if trunk.PNType != nil {
		response.PNType = *trunk.PNType
	}
	if trunk.PNToken != nil {
		response.PNTokenMasked = maskPushToken(*trunk.PNToken)
	}
	if trunk.PNUpdatedAt != nil {
		response.PNUpdatedAt = trunk.PNUpdatedAt.Format(time.RFC3339)
	}
	response.PushContactReady = response.PNAppID != "" && response.PNType != "" && response.PNTokenMasked != ""
	return response
}

func maskPushToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 12 {
		return "****"
	}
	return token[:6] + "..." + token[len(token)-6:]
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

// handleUserTrunkHeartbeat checks if a trunk is assigned to the authenticated user
// and refreshes its updated_at timestamp. If no trunk is found, logs an alert.
func (s *Server) handleUserTrunkHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, hasClaims := AuthClaimsFromContext(ctx)
	if !hasClaims {
		s.respondError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	preferredUsername := claims.PreferredUsername
	if preferredUsername == "" {
		s.respondError(w, http.StatusBadRequest, "Token missing preferred_username claim")
		return
	}

	trunk, err := s.trunkManager.FindTrunkByInUseBy(ctx, preferredUsername)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to lookup trunk: %v", err))
		return
	}

	if trunk == nil {
		log.Printf("⚠️ [User Trunk] Trunk does not exist for user %s (sub=%s), alert!", preferredUsername, claims.Subject)
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("No trunk assigned to user %s", preferredUsername))
		return
	}

	// Refresh updated_at by re-setting in_use_by to the same value.
	if err := s.trunkManager.SetTrunkInUseBy(ctx, trunk.ID, &preferredUsername); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to refresh trunk: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"trunkId":   trunk.ID,
		"publicId":  trunk.PublicID,
		"name":      trunk.Name,
		"domain":    trunk.Domain,
		"port":      trunk.Port,
		"username":  trunk.Username,
		"inUseBy":   preferredUsername,
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
	})
}
