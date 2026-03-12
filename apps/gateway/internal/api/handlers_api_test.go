package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
)

type apiHandlerTrunkManagerStub struct {
	byID          map[int64]*sip.Trunk
	listResult    *sip.TrunkListResult
	listErr       error
	listParams    sip.TrunkListParams
	refreshErr    error
	refreshCount  int
	createResult  *sip.Trunk
	createErr     error
	createPayload sip.CreateTrunkPayload
	updateResult  *sip.Trunk
	updateErr     error
	updatePatch   sip.TrunkUpdatePatch
	updateTrunkID int64
	registerErr   error
	registerID    int64
	registerForce bool
	unregisterErr error
	unregisterID  int64
	unregisterFor bool
}

func (s *apiHandlerTrunkManagerStub) GetTrunkByID(id int64) (interface{}, bool) {
	t, ok := s.byID[id]
	return t, ok
}

func (s *apiHandlerTrunkManagerStub) GetTrunkByPublicID(publicID string) (interface{}, bool) {
	for _, t := range s.byID {
		if t.PublicID == publicID {
			return t, true
		}
	}
	return nil, false
}

func (s *apiHandlerTrunkManagerStub) GetTrunkIDByPublicID(publicID string) (int64, bool) {
	for id, t := range s.byID {
		if t.PublicID == publicID {
			return id, true
		}
	}
	return 0, false
}

func (s *apiHandlerTrunkManagerStub) GetDefaultTrunk() (interface{}, bool) {
	for _, t := range s.byID {
		if t.IsDefault {
			return t, true
		}
	}
	return nil, false
}

func (s *apiHandlerTrunkManagerStub) RefreshTrunks() error {
	s.refreshCount++
	return s.refreshErr
}

func (s *apiHandlerTrunkManagerStub) CreateTrunk(_ context.Context, payload sip.CreateTrunkPayload) (*sip.Trunk, error) {
	s.createPayload = payload
	return s.createResult, s.createErr
}

func (s *apiHandlerTrunkManagerStub) UpdateTrunk(_ context.Context, trunkID int64, patch sip.TrunkUpdatePatch) (*sip.Trunk, error) {
	s.updateTrunkID = trunkID
	s.updatePatch = patch
	return s.updateResult, s.updateErr
}

func (s *apiHandlerTrunkManagerStub) RegisterTrunk(trunkID int64, force bool) error {
	s.registerID = trunkID
	s.registerForce = force
	return s.registerErr
}

func (s *apiHandlerTrunkManagerStub) UnregisterTrunk(trunkID int64, force bool) error {
	s.unregisterID = trunkID
	s.unregisterFor = force
	return s.unregisterErr
}

func (s *apiHandlerTrunkManagerStub) ListTrunks(_ context.Context, params sip.TrunkListParams) (*sip.TrunkListResult, error) {
	s.listParams = params
	return s.listResult, s.listErr
}

func (s *apiHandlerTrunkManagerStub) GetTrunkByIDFromDB(_ context.Context, trunkID int64) (*sip.Trunk, error) {
	t, ok := s.byID[trunkID]
	if !ok {
		return nil, errors.New("not found")
	}
	return t, nil
}

func (s *apiHandlerTrunkManagerStub) ListOwnedTrunks() []*sip.Trunk {
	items := make([]*sip.Trunk, 0, len(s.byID))
	for _, t := range s.byID {
		items = append(items, t)
	}
	return items
}

type apiHandlerSIPMakerStub struct {
	makeCallErr error
	hangupErr   error
	dtmfErr     error

	makeCallCount int
	hangupCount   int
	dtmfCount     int

	lastMakeCallSessionID string
	lastDigits            string
}

func (s *apiHandlerSIPMakerStub) MakeCall(_ string, _ string, sess *session.Session) error {
	s.makeCallCount++
	if sess != nil {
		s.lastMakeCallSessionID = sess.ID
	}
	return s.makeCallErr
}

func (s *apiHandlerSIPMakerStub) Hangup(_ *session.Session) error {
	s.hangupCount++
	return s.hangupErr
}

func (s *apiHandlerSIPMakerStub) SendDTMF(_ *session.Session, digits string) error {
	s.dtmfCount++
	s.lastDigits = digits
	return s.dtmfErr
}

func (s *apiHandlerSIPMakerStub) AcceptCall(_ *session.Session) error           { return nil }
func (s *apiHandlerSIPMakerStub) RejectCall(_ *session.Session, _ string) error { return nil }
func (s *apiHandlerSIPMakerStub) SendMessage(_, _, _, _ string) error           { return nil }
func (s *apiHandlerSIPMakerStub) SendMessageToSession(_ *session.Session, _, _ string) error {
	return nil
}

type apiHandlerLogStoreStub struct {
	logstore.LogStore
	listResult        *logstore.SessionListResult
	listErr           error
	listParams        logstore.SessionListParams
	eventListResult   *logstore.EventListResult
	eventListErr      error
	eventListParams   logstore.EventListParams
	payloadListResult *logstore.PayloadListResult
	payloadListErr    error
	payloadListParams logstore.PayloadListParams
	getPayloadResult  *logstore.PayloadReadRecord
	getPayloadErr     error
	getPayloadID      int64
	dialogListResult  *logstore.DialogListResult
	dialogListErr     error
	dialogListParams  logstore.DialogListParams
	statsListResult   *logstore.StatsListResult
	statsListErr      error
	statsListParams   logstore.StatsListParams
}

func (s *apiHandlerLogStoreStub) ListSessions(_ context.Context, params logstore.SessionListParams) (*logstore.SessionListResult, error) {
	s.listParams = params
	return s.listResult, s.listErr
}

func (s *apiHandlerLogStoreStub) ListEvents(_ context.Context, params logstore.EventListParams) (*logstore.EventListResult, error) {
	s.eventListParams = params
	return s.eventListResult, s.eventListErr
}

func (s *apiHandlerLogStoreStub) ListPayloads(_ context.Context, params logstore.PayloadListParams) (*logstore.PayloadListResult, error) {
	s.payloadListParams = params
	return s.payloadListResult, s.payloadListErr
}

func (s *apiHandlerLogStoreStub) GetPayload(_ context.Context, payloadID int64) (*logstore.PayloadReadRecord, error) {
	s.getPayloadID = payloadID
	return s.getPayloadResult, s.getPayloadErr
}

func (s *apiHandlerLogStoreStub) ListDialogs(_ context.Context, params logstore.DialogListParams) (*logstore.DialogListResult, error) {
	s.dialogListParams = params
	return s.dialogListResult, s.dialogListErr
}

func (s *apiHandlerLogStoreStub) ListStats(_ context.Context, params logstore.StatsListParams) (*logstore.StatsListResult, error) {
	s.statsListParams = params
	return s.statsListResult, s.statsListErr
}

func newAPIHandlerTestServer(t *testing.T, trunkMgr TrunkManager, sipMaker SIPCallMaker, store logstore.LogStore) (*Server, *session.Manager) {
	t.Helper()
	mgr := newTestSessionManager()
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, mgr, sipMaker, nil, trunkMgr, store)
	return srv, mgr
}

func doRequest(t *testing.T, handler func(http.ResponseWriter, *http.Request), method, path string, body string, vars map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func assertJSONError(t *testing.T, rr *httptest.ResponseRecorder, status int, contains string) {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, rr.Code, rr.Body.String())
	}
	var resp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid error json: %v", err)
	}
	if !strings.Contains(resp.Error, contains) {
		t.Fatalf("expected error to contain %q, got %q", contains, resp.Error)
	}
}

func assertJSONDecode[T any](t *testing.T, rr *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, rr.Code, rr.Body.String())
	}
	var out T
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("failed to decode json response: %v", err)
	}
	return out
}

func makeTestTrunk(id int64, publicID string) *sip.Trunk {
	now := time.Now().UTC()
	return &sip.Trunk{
		ID:        id,
		PublicID:  publicID,
		Name:      "Main",
		Domain:    "sip.example.com",
		Port:      5060,
		Username:  "1001",
		Password:  "secret",
		Transport: "tcp",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func createTestSessionWithTrunk(t *testing.T, mgr *session.Manager, trunkID int64, to string) *session.Session {
	t.Helper()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.SetCallInfo("outbound", "1001", to, "call-1")
	sess.SetSIPAuthContext("trunk", "", trunkID, "sip.example.com", "1001", "secret", 5060)
	sess.SetState(session.StateActive)
	return sess
}

func TestHandleCreateTrunk_Table(t *testing.T) {
	trunk := makeTestTrunk(9, "11111111-1111-4111-8111-111111111111")
	cases := []struct {
		name           string
		srv            *Server
		body           string
		wantStatus     int
		wantErrContain string
		wantPayload    *sip.CreateTrunkPayload
	}{
		{
			name:           "manager missing",
			srv:            NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, newTestSessionManager(), nil, nil, nil, nil),
			body:           `{}`,
			wantStatus:     http.StatusServiceUnavailable,
			wantErrContain: "Trunk manager not available",
		},
		{
			name: "validation normalizes defaults then creates",
			srv: func() *Server {
				stub := &apiHandlerTrunkManagerStub{createResult: trunk}
				srv, _ := newAPIHandlerTestServer(t, stub, nil, nil)
				return srv
			}(),
			body:       `{"name":"  Main  ","domain":" sip.example.com ","port":0,"username":" 1001 ","password":" pw ","transport":"ws"}`,
			wantStatus: http.StatusCreated,
			wantPayload: &sip.CreateTrunkPayload{
				Name:      "Main",
				Domain:    "sip.example.com",
				Port:      5060,
				Username:  "1001",
				Password:  "pw",
				Transport: "tcp",
				Enabled:   true,
				IsDefault: false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doRequest(t, tc.srv.handleCreateTrunk, http.MethodPost, "/trunks", tc.body, nil)
			if tc.wantErrContain != "" {
				assertJSONError(t, rr, tc.wantStatus, tc.wantErrContain)
				return
			}
			_ = assertJSONDecode[TrunkResponse](t, rr, tc.wantStatus)
		})
	}

	stub := &apiHandlerTrunkManagerStub{createErr: fmt.Errorf("%w: bad payload", sip.ErrTrunkValidation)}
	srv, _ := newAPIHandlerTestServer(t, stub, nil, nil)
	rr := doRequest(t, srv.handleCreateTrunk, http.MethodPost, "/trunks", `{"name":"x","domain":"d","port":5060,"username":"u","password":"p","transport":"tcp"}`, nil)
	assertJSONError(t, rr, http.StatusBadRequest, "trunk validation")

	stub = &apiHandlerTrunkManagerStub{createErr: fmt.Errorf("%w: duplicate", sip.ErrTrunkConflict)}
	srv, _ = newAPIHandlerTestServer(t, stub, nil, nil)
	rr = doRequest(t, srv.handleCreateTrunk, http.MethodPost, "/trunks", `{"name":"x","domain":"d","port":5060,"username":"u","password":"p","transport":"tcp"}`, nil)
	assertJSONError(t, rr, http.StatusConflict, "trunk conflict")
}

func TestHandleListAndGetTrunks(t *testing.T) {
	trunk := makeTestTrunk(11, "22222222-2222-4222-8222-222222222222")
	tm := &apiHandlerTrunkManagerStub{
		byID: map[int64]*sip.Trunk{11: trunk},
		listResult: &sip.TrunkListResult{
			Items:    []*sip.Trunk{trunk},
			Total:    1,
			Page:     2,
			PageSize: 5,
		},
	}
	srv, mgr := newAPIHandlerTestServer(t, tm, nil, nil)
	createTestSessionWithTrunk(t, mgr, 11, "15551234567")

	rr := doRequest(t, srv.handleListTrunks, http.MethodGet, "/trunks?page=2&pageSize=5&trunkPublicId=22222222-2222-4222-8222-222222222222&search=Main", "", nil)
	resp := assertJSONDecode[TrunkListResponse](t, rr, http.StatusOK)
	if resp.Total != 1 || len(resp.Items) != 1 {
		t.Fatalf("unexpected list response: %+v", resp)
	}
	if resp.Items[0].ActiveCallCount != 1 {
		t.Fatalf("expected active call count 1, got %d", resp.Items[0].ActiveCallCount)
	}
	if len(resp.Items[0].ActiveDestinations) != 1 || resp.Items[0].ActiveDestinations[0] != "15551234567" {
		t.Fatalf("unexpected active destinations: %+v", resp.Items[0].ActiveDestinations)
	}
	if tm.listParams.Page != 2 || tm.listParams.PageSize != 5 || tm.listParams.Search != "Main" {
		t.Fatalf("unexpected list params: %+v", tm.listParams)
	}

	rr = doRequest(t, srv.handleGetTrunk, http.MethodGet, "/trunks/11", "", map[string]string{"id": "11"})
	getResp := assertJSONDecode[TrunkResponse](t, rr, http.StatusOK)
	if getResp.ID != 11 || getResp.ActiveCallCount != 1 {
		t.Fatalf("unexpected get trunk response: %+v", getResp)
	}
}

func TestHandleListTrunks_NegativePaths(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, newTestSessionManager(), nil, nil, nil, nil)
	rr := doRequest(t, srv.handleListTrunks, http.MethodGet, "/trunks", "", nil)
	assertJSONError(t, rr, http.StatusServiceUnavailable, "Trunk manager not available")

	tm := &apiHandlerTrunkManagerStub{}
	srv, _ = newAPIHandlerTestServer(t, tm, nil, nil)
	rr = doRequest(t, srv.handleListTrunks, http.MethodGet, "/trunks?trunkPublicId=BAD", "", nil)
	assertJSONError(t, rr, http.StatusBadRequest, "Invalid trunk public ID")

	tm.listErr = errors.New("db down")
	rr = doRequest(t, srv.handleListTrunks, http.MethodGet, "/trunks", "", nil)
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to list trunks")
}

func TestHandleUpdateTrunk(t *testing.T) {
	trunk := makeTestTrunk(7, "33333333-3333-4333-8333-333333333333")
	tm := &apiHandlerTrunkManagerStub{
		byID:         map[int64]*sip.Trunk{7: trunk},
		updateResult: trunk,
	}
	srv, _ := newAPIHandlerTestServer(t, tm, nil, nil)

	rr := doRequest(t, srv.handleUpdateTrunk, http.MethodPatch, "/trunks/7", `{"name":"  New Name  ","transport":"UDP","password":"   "}`, map[string]string{"id": "7"})
	_ = assertJSONDecode[TrunkResponse](t, rr, http.StatusOK)
	if tm.updateTrunkID != 7 {
		t.Fatalf("expected update trunk id 7, got %d", tm.updateTrunkID)
	}
	if tm.updatePatch.Name == nil || *tm.updatePatch.Name != "New Name" {
		t.Fatalf("name was not normalized: %+v", tm.updatePatch.Name)
	}
	if tm.updatePatch.Transport == nil || *tm.updatePatch.Transport != "udp" {
		t.Fatalf("transport was not normalized: %+v", tm.updatePatch.Transport)
	}
	if tm.updatePatch.Password != nil {
		t.Fatalf("expected empty password to be treated as nil patch")
	}
}

func TestHandleUpdateTrunk_NegativePaths(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, newTestSessionManager(), nil, nil, nil, nil)
	rr := doRequest(t, srv.handleUpdateTrunk, http.MethodPatch, "/trunks/1", `{}`, map[string]string{"id": "1"})
	assertJSONError(t, rr, http.StatusServiceUnavailable, "Trunk manager not available")

	tm := &apiHandlerTrunkManagerStub{}
	srv, mgr := newAPIHandlerTestServer(t, tm, nil, nil)
	createTestSessionWithTrunk(t, mgr, 5, "1002")

	rr = doRequest(t, srv.handleUpdateTrunk, http.MethodPatch, "/trunks/x", `{}`, map[string]string{"id": "x"})
	assertJSONError(t, rr, http.StatusBadRequest, "Invalid trunk ID")

	rr = doRequest(t, srv.handleUpdateTrunk, http.MethodPatch, "/trunks/5", `{"enabled":false}`, map[string]string{"id": "5"})
	assertJSONError(t, rr, http.StatusConflict, "cannot disable trunk while active calls exist")

	rr = doRequest(t, srv.handleUpdateTrunk, http.MethodPatch, "/trunks/5", `{"port":70000}`, map[string]string{"id": "5"})
	assertJSONError(t, rr, http.StatusBadRequest, "port must be between")

	tm.updateErr = fmt.Errorf("%w: missing", sip.ErrTrunkNotFound)
	rr = doRequest(t, srv.handleUpdateTrunk, http.MethodPatch, "/trunks/9", `{"name":"x"}`, map[string]string{"id": "9"})
	assertJSONError(t, rr, http.StatusNotFound, "trunk not found")
}

func TestHandleSessionHandlers(t *testing.T) {
	trunk := makeTestTrunk(55, "55555555-5555-4555-8555-555555555555")
	tm := &apiHandlerTrunkManagerStub{byID: map[int64]*sip.Trunk{55: trunk}}
	srv, mgr := newAPIHandlerTestServer(t, tm, nil, nil)
	sess := createTestSessionWithTrunk(t, mgr, 55, "1002")

	rr := doRequest(t, srv.handleListSessions, http.MethodGet, "/sessions", "", nil)
	items := assertJSONDecode[[]SessionResponse](t, rr, http.StatusOK)
	if len(items) != 1 || items[0].ID != sess.ID || items[0].TrunkName != "Main" {
		t.Fatalf("unexpected sessions response: %+v", items)
	}

	rr = doRequest(t, srv.handleGetSession, http.MethodGet, "/sessions/1", "", map[string]string{"sessionId": sess.ID})
	out := assertJSONDecode[SessionResponse](t, rr, http.StatusOK)
	if out.ID != sess.ID || out.TrunkID != 55 {
		t.Fatalf("unexpected get session response: %+v", out)
	}

	rr = doRequest(t, srv.handleGetSession, http.MethodGet, "/sessions/missing", "", map[string]string{"sessionId": "missing"})
	assertJSONError(t, rr, http.StatusNotFound, "Session not found")
}

func TestHandleListSessionHistory(t *testing.T) {
	srv, _ := newAPIHandlerTestServer(t, nil, nil, nil)
	rr := doRequest(t, srv.handleListSessionHistory, http.MethodGet, "/session-history", "", nil)
	assertJSONError(t, rr, http.StatusServiceUnavailable, "Database logging not available")

	now := time.Now().UTC()
	store := &apiHandlerLogStoreStub{
		listResult: &logstore.SessionListResult{
			Items: []*logstore.SessionRecord{
				{
					SessionID:  "s-1",
					CreatedAt:  now.Add(-2 * time.Minute),
					UpdatedAt:  now.Add(-1 * time.Minute),
					Direction:  "outbound",
					FromURI:    "1001",
					ToURI:      "1002",
					SIPCallID:  "call-1",
					FinalState: "ended",
					EndReason:  "hangup",
				},
			},
			Total:    1,
			Page:     1,
			PageSize: 10,
		},
	}
	srv, _ = newAPIHandlerTestServer(t, nil, nil, store)
	rr = doRequest(t, srv.handleListSessionHistory, http.MethodGet, "/session-history?page=1&pageSize=10&direction=outbound&search=1001", "", nil)
	resp := assertJSONDecode[SessionHistoryListResponse](t, rr, http.StatusOK)
	if len(resp.Items) != 1 || resp.Items[0].SessionID != "s-1" {
		t.Fatalf("unexpected history response: %+v", resp)
	}
	if store.listParams.Direction != "outbound" || store.listParams.Search != "1001" {
		t.Fatalf("unexpected list params: %+v", store.listParams)
	}

	store.listErr = errors.New("query failed")
	rr = doRequest(t, srv.handleListSessionHistory, http.MethodGet, "/session-history", "", nil)
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to list sessions")
}

func TestHandleOfferCallHangupDTMF_ValidationAndFailures(t *testing.T) {
	srv, mgr := newAPIHandlerTestServer(t, nil, nil, nil)
	rr := doRequest(t, srv.handleOffer, http.MethodPost, "/offer", `{`, nil)
	assertJSONError(t, rr, http.StatusBadRequest, "Invalid request body")
	rr = doRequest(t, srv.handleOffer, http.MethodPost, "/offer", `{"sdp":""}`, nil)
	assertJSONError(t, rr, http.StatusBadRequest, "SDP is required")
	rr = doRequest(t, srv.handleOffer, http.MethodPost, "/offer", `{"sdp":"v=0","sessionId":"missing"}`, nil)
	assertJSONError(t, rr, http.StatusNotFound, "Session not found")

	rr = doRequest(t, srv.handleCall, http.MethodPost, "/call", `{`, nil)
	assertJSONError(t, rr, http.StatusBadRequest, "Invalid request body")
	rr = doRequest(t, srv.handleCall, http.MethodPost, "/call", `{"destination":"1002"}`, nil)
	assertJSONError(t, rr, http.StatusBadRequest, "Session ID is required")
	rr = doRequest(t, srv.handleCall, http.MethodPost, "/call", `{"sessionId":"x"}`, nil)
	assertJSONError(t, rr, http.StatusBadRequest, "Destination is required")
	rr = doRequest(t, srv.handleCall, http.MethodPost, "/call", `{"sessionId":"missing","destination":"1002"}`, nil)
	assertJSONError(t, rr, http.StatusNotFound, "Session not found")

	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sipMaker := &apiHandlerSIPMakerStub{makeCallErr: errors.New("dial error"), dtmfErr: errors.New("dtmf error")}
	srv.sipMaker = sipMaker

	rr = doRequest(t, srv.handleCall, http.MethodPost, "/call", `{"sessionId":"`+sess.ID+`","destination":"1002","from":"1001"}`, nil)
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to make call")

	sipMaker.makeCallErr = nil
	rr = doRequest(t, srv.handleCall, http.MethodPost, "/call", `{"sessionId":"`+sess.ID+`","destination":"1002","from":"1001"}`, nil)
	callResp := assertJSONDecode[CallResponse](t, rr, http.StatusOK)
	if callResp.SessionID != sess.ID || callResp.Message != "Call initiated" {
		t.Fatalf("unexpected call response: %+v", callResp)
	}

	rr = doRequest(t, srv.handleHangup, http.MethodPost, "/sessions//hangup", "", map[string]string{"sessionId": ""})
	assertJSONError(t, rr, http.StatusBadRequest, "Session ID is required")
	rr = doRequest(t, srv.handleHangup, http.MethodPost, "/sessions/missing/hangup", "", map[string]string{"sessionId": "missing"})
	assertJSONError(t, rr, http.StatusNotFound, "Session not found")
	rr = doRequest(t, srv.handleHangup, http.MethodPost, "/sessions/"+sess.ID+"/hangup", "", map[string]string{"sessionId": sess.ID})
	hangupResp := assertJSONDecode[CallResponse](t, rr, http.StatusOK)
	if hangupResp.State != string(session.StateEnded) {
		t.Fatalf("unexpected hangup response: %+v", hangupResp)
	}
	if _, ok := mgr.GetSession(sess.ID); ok {
		t.Fatalf("expected session to be deleted")
	}

	newSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	rr = doRequest(t, srv.handleDTMF, http.MethodPost, "/sessions/"+newSess.ID+"/dtmf", `{`, map[string]string{"sessionId": newSess.ID})
	assertJSONError(t, rr, http.StatusBadRequest, "Invalid request body")
	rr = doRequest(t, srv.handleDTMF, http.MethodPost, "/sessions/"+newSess.ID+"/dtmf", `{"digits":""}`, map[string]string{"sessionId": newSess.ID})
	assertJSONError(t, rr, http.StatusBadRequest, "Digits are required")
	rr = doRequest(t, srv.handleDTMF, http.MethodPost, "/sessions/missing/dtmf", `{"digits":"1"}`, map[string]string{"sessionId": "missing"})
	assertJSONError(t, rr, http.StatusNotFound, "Session not found")
	rr = doRequest(t, srv.handleDTMF, http.MethodPost, "/sessions/"+newSess.ID+"/dtmf", `{"digits":"1"}`, map[string]string{"sessionId": newSess.ID})
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to send DTMF")

	sipMaker.dtmfErr = nil
	rr = doRequest(t, srv.handleDTMF, http.MethodPost, "/sessions/"+newSess.ID+"/dtmf", `{"digits":"123"}`, map[string]string{"sessionId": newSess.ID})
	okResp := assertJSONDecode[map[string]string](t, rr, http.StatusOK)
	if okResp["status"] != "ok" || sipMaker.lastDigits != "123" {
		t.Fatalf("unexpected dtmf response=%+v digits=%q", okResp, sipMaker.lastDigits)
	}
}

type noFlushWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *noFlushWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *noFlushWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *noFlushWriter) WriteHeader(statusCode int)  { w.status = statusCode }

type flushRecorder struct {
	mu         sync.Mutex
	header     http.Header
	body       bytes.Buffer
	status     int
	flushCount int
}

func (w *flushRecorder) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *flushRecorder) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(b)
}

func (w *flushRecorder) WriteHeader(statusCode int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = statusCode
}

func (w *flushRecorder) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushCount++
}

func TestHandleSSEStreams_Contract(t *testing.T) {
	srv, _ := newAPIHandlerTestServer(t, nil, nil, nil)

	noFlush := &noFlushWriter{}
	req := httptest.NewRequest(http.MethodGet, "/trunks/stream", nil)
	srv.handleTrunkStream(noFlush, req)
	if noFlush.status != http.StatusInternalServerError {
		t.Fatalf("expected 500 for non-flusher writer, got %d", noFlush.status)
	}
	if !strings.Contains(noFlush.body.String(), "Streaming unsupported") {
		t.Fatalf("expected streaming unsupported error, got %s", noFlush.body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req = httptest.NewRequest(http.MethodGet, "/sessions/stream", nil).WithContext(ctx)
	writer := &flushRecorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleSessionStream(writer, req)
	}()

	deadline := time.Now().Add(750 * time.Millisecond)
	for {
		writer.mu.Lock()
		wroteConnected := strings.Contains(writer.body.String(), "event: connected")
		flushCount := writer.flushCount
		writer.mu.Unlock()
		if wroteConnected && flushCount > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session stream did not write connected event/flush in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("session stream handler did not stop after context cancellation")
	}
}

func TestHandleRefreshTrunks(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, newTestSessionManager(), nil, nil, nil, nil)
	rr := doRequest(t, srv.handleRefreshTrunks, http.MethodPost, "/trunks/refresh", "", nil)
	assertJSONError(t, rr, http.StatusServiceUnavailable, "Trunk manager not available")

	tm := &apiHandlerTrunkManagerStub{}
	srv, _ = newAPIHandlerTestServer(t, tm, nil, nil)
	rr = doRequest(t, srv.handleRefreshTrunks, http.MethodPost, "/trunks/refresh", "", nil)
	out := assertJSONDecode[map[string]string](t, rr, http.StatusOK)
	if out["status"] != "refreshed" || tm.refreshCount != 1 {
		t.Fatalf("unexpected refresh response=%+v refreshCount=%d", out, tm.refreshCount)
	}

	tm.refreshErr = errors.New("reload failed")
	rr = doRequest(t, srv.handleRefreshTrunks, http.MethodPost, "/trunks/refresh", "", nil)
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to refresh trunks")
}

func TestHandleTrunkRegisterUnregister(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, newTestSessionManager(), nil, nil, nil, nil)
	rr := doRequest(t, srv.handleTrunkRegister, http.MethodPost, "/trunks/1/register", "", map[string]string{"id": "1"})
	assertJSONError(t, rr, http.StatusServiceUnavailable, "Trunk manager not available")
	rr = doRequest(t, srv.handleTrunkUnregister, http.MethodPost, "/trunks/1/unregister", "", map[string]string{"id": "1"})
	assertJSONError(t, rr, http.StatusServiceUnavailable, "Trunk manager not available")

	tm := &apiHandlerTrunkManagerStub{}
	srv, _ = newAPIHandlerTestServer(t, tm, nil, nil)

	rr = doRequest(t, srv.handleTrunkRegister, http.MethodPost, "/trunks/x/register", "", map[string]string{"id": "x"})
	assertJSONError(t, rr, http.StatusBadRequest, "Invalid trunk ID")
	rr = doRequest(t, srv.handleTrunkUnregister, http.MethodPost, "/trunks/x/unregister", "", map[string]string{"id": "x"})
	assertJSONError(t, rr, http.StatusBadRequest, "Invalid trunk ID")

	tm.registerErr = errors.New("register failed")
	rr = doRequest(t, srv.handleTrunkRegister, http.MethodPost, "/trunks/11/register", "", map[string]string{"id": "11"})
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to register trunk")

	tm.registerErr = nil
	rr = doRequest(t, srv.handleTrunkRegister, http.MethodPost, "/trunks/11/register", "", map[string]string{"id": "11"})
	registerResp := assertJSONDecode[map[string]interface{}](t, rr, http.StatusOK)
	if registerResp["status"] != "registered" || tm.registerID != 11 || !tm.registerForce {
		t.Fatalf("unexpected register resp=%+v id=%d force=%v", registerResp, tm.registerID, tm.registerForce)
	}

	tm.unregisterErr = errors.New("unregister failed")
	rr = doRequest(t, srv.handleTrunkUnregister, http.MethodPost, "/trunks/11/unregister", "", map[string]string{"id": "11"})
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to unregister trunk")

	tm.unregisterErr = nil
	rr = doRequest(t, srv.handleTrunkUnregister, http.MethodPost, "/trunks/11/unregister", "", map[string]string{"id": "11"})
	unregisterResp := assertJSONDecode[map[string]interface{}](t, rr, http.StatusOK)
	if unregisterResp["status"] != "unregistered" || tm.unregisterID != 11 || !tm.unregisterFor {
		t.Fatalf("unexpected unregister resp=%+v id=%d force=%v", unregisterResp, tm.unregisterID, tm.unregisterFor)
	}
}

func TestHandleSessionDetailHandlers(t *testing.T) {
	now := time.Now().UTC()
	pid := int64(17)
	store := &apiHandlerLogStoreStub{
		eventListResult: &logstore.EventListResult{
			Items: []*logstore.EventRecord{
				{
					ID:            1,
					Timestamp:     now,
					SessionID:     "s-1",
					Category:      "rest",
					Name:          "event1",
					SIPMethod:     "INVITE",
					SIPStatusCode: 200,
					PayloadID:     &pid,
					Data:          map[string]interface{}{"ok": true},
				},
			},
			Total: 1, Page: 2, PageSize: 3,
		},
		payloadListResult: &logstore.PayloadListResult{
			Items: []*logstore.PayloadReadRecord{
				{
					PayloadID:   91,
					Timestamp:   now,
					SessionID:   "s-1",
					Kind:        "webrtc_sdp_offer",
					ContentType: "application/sdp",
					BodyText:    "v=0",
				},
			},
			Total: 1, Page: 1, PageSize: 10,
		},
		getPayloadResult: &logstore.PayloadReadRecord{
			PayloadID:   91,
			Timestamp:   now,
			SessionID:   "s-1",
			Kind:        "webrtc_sdp_offer",
			ContentType: "application/sdp",
			BodyText:    "v=0",
		},
		dialogListResult: &logstore.DialogListResult{
			Items: []*logstore.DialogRecord{
				{
					ID:            5,
					SessionID:     "s-1",
					Timestamp:     now,
					SIPCallID:     "call-1",
					FromTag:       "from-a",
					ToTag:         "to-b",
					RemoteContact: "sip:1001@example.com",
					CSeq:          9,
					RouteSet:      []string{"sip:proxy.example.com"},
				},
			},
			Total: 1, Page: 1, PageSize: 20,
		},
		statsListResult: &logstore.StatsListResult{
			Items: []*logstore.StatsReadRecord{
				{
					ID:          8,
					Timestamp:   now,
					SessionID:   "s-1",
					PLISent:     3,
					PLIResponse: 2,
					AudioRTCPRR: 1,
					AudioRTCPSR: 2,
					VideoRTCPRR: 3,
					VideoRTCPSR: 4,
					Data:        map[string]interface{}{"loss": 0.1},
				},
			},
			Total: 1, Page: 1, PageSize: 20,
		},
	}
	srv, _ := newAPIHandlerTestServer(t, nil, nil, store)

	rr := doRequest(t, srv.handleListSessionEvents, http.MethodGet, "/sessions/s-1/events?page=2&pageSize=3&category=rest&name=event1", "", map[string]string{"sessionId": "s-1"})
	evResp := assertJSONDecode[EventListResponse](t, rr, http.StatusOK)
	if len(evResp.Items) != 1 || evResp.Items[0].Name != "event1" {
		t.Fatalf("unexpected events response: %+v", evResp)
	}
	if store.eventListParams.SessionID != "s-1" || store.eventListParams.Category != "rest" || store.eventListParams.Page != 2 {
		t.Fatalf("unexpected events params: %+v", store.eventListParams)
	}

	rr = doRequest(t, srv.handleListSessionPayloads, http.MethodGet, "/sessions/s-1/payloads?page=1&pageSize=10&kind=webrtc_sdp_offer", "", map[string]string{"sessionId": "s-1"})
	plResp := assertJSONDecode[PayloadListResponse](t, rr, http.StatusOK)
	if len(plResp.Items) != 1 || plResp.Items[0].PayloadID != 91 {
		t.Fatalf("unexpected payload list response: %+v", plResp)
	}

	rr = doRequest(t, srv.handleGetPayload, http.MethodGet, "/payloads/91", "", map[string]string{"payloadId": "91"})
	pResp := assertJSONDecode[PayloadResponse](t, rr, http.StatusOK)
	if pResp.PayloadID != 91 || store.getPayloadID != 91 {
		t.Fatalf("unexpected payload response: %+v getPayloadID=%d", pResp, store.getPayloadID)
	}

	rr = doRequest(t, srv.handleListSessionDialogs, http.MethodGet, "/sessions/s-1/dialogs?page=1&pageSize=20", "", map[string]string{"sessionId": "s-1"})
	dResp := assertJSONDecode[DialogListResponse](t, rr, http.StatusOK)
	if len(dResp.Items) != 1 || dResp.Items[0].ID != 5 {
		t.Fatalf("unexpected dialog response: %+v", dResp)
	}

	rr = doRequest(t, srv.handleListSessionStats, http.MethodGet, "/sessions/s-1/stats?page=1&pageSize=20", "", map[string]string{"sessionId": "s-1"})
	sResp := assertJSONDecode[StatsListResponse](t, rr, http.StatusOK)
	if len(sResp.Items) != 1 || sResp.Items[0].ID != 8 {
		t.Fatalf("unexpected stats response: %+v", sResp)
	}
}

func TestHandleSessionDetailHandlers_NegativePaths(t *testing.T) {
	srv, _ := newAPIHandlerTestServer(t, nil, nil, nil)
	rr := doRequest(t, srv.handleListSessionEvents, http.MethodGet, "/sessions/s-1/events", "", map[string]string{"sessionId": "s-1"})
	assertJSONError(t, rr, http.StatusServiceUnavailable, "Database logging not available")

	store := &apiHandlerLogStoreStub{}
	srv, _ = newAPIHandlerTestServer(t, nil, nil, store)

	rr = doRequest(t, srv.handleListSessionEvents, http.MethodGet, "/sessions//events", "", map[string]string{"sessionId": ""})
	assertJSONError(t, rr, http.StatusBadRequest, "Session ID is required")
	store.eventListErr = errors.New("events failed")
	rr = doRequest(t, srv.handleListSessionEvents, http.MethodGet, "/sessions/s-1/events", "", map[string]string{"sessionId": "s-1"})
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to list events")

	rr = doRequest(t, srv.handleListSessionPayloads, http.MethodGet, "/sessions//payloads", "", map[string]string{"sessionId": ""})
	assertJSONError(t, rr, http.StatusBadRequest, "Session ID is required")
	store.payloadListErr = errors.New("payloads failed")
	rr = doRequest(t, srv.handleListSessionPayloads, http.MethodGet, "/sessions/s-1/payloads", "", map[string]string{"sessionId": "s-1"})
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to list payloads")

	rr = doRequest(t, srv.handleGetPayload, http.MethodGet, "/payloads/x", "", map[string]string{"payloadId": "x"})
	assertJSONError(t, rr, http.StatusBadRequest, "Invalid payload ID")
	store.getPayloadErr = errors.New("missing")
	rr = doRequest(t, srv.handleGetPayload, http.MethodGet, "/payloads/99", "", map[string]string{"payloadId": "99"})
	assertJSONError(t, rr, http.StatusNotFound, "Payload not found")

	rr = doRequest(t, srv.handleListSessionDialogs, http.MethodGet, "/sessions//dialogs", "", map[string]string{"sessionId": ""})
	assertJSONError(t, rr, http.StatusBadRequest, "Session ID is required")
	store.dialogListErr = errors.New("dialogs failed")
	rr = doRequest(t, srv.handleListSessionDialogs, http.MethodGet, "/sessions/s-1/dialogs", "", map[string]string{"sessionId": "s-1"})
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to list dialogs")

	rr = doRequest(t, srv.handleListSessionStats, http.MethodGet, "/sessions//stats", "", map[string]string{"sessionId": ""})
	assertJSONError(t, rr, http.StatusBadRequest, "Session ID is required")
	store.statsListErr = errors.New("stats failed")
	rr = doRequest(t, srv.handleListSessionStats, http.MethodGet, "/sessions/s-1/stats", "", map[string]string{"sessionId": "s-1"})
	assertJSONError(t, rr, http.StatusInternalServerError, "Failed to list stats")
}
