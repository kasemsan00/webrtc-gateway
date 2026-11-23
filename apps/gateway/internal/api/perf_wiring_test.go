package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	pionice "github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/auth"
	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/push"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
	"webrtc-sip-gateway/internal/sipclientauth"
)

// asyncTestLogStore implements both logstore.LogStore and logstore.AsyncLogStore.
type asyncTestLogStore struct {
	logstore.LogStore
	mu              sync.Mutex
	syncStoreCalls  int
	syncEventCalls  int
	syncSessionCall int
	syncDirCalls    int
	asyncPayloads   []*logstore.PayloadRecord
	asyncEvents     []*logstore.Event
	asyncSessions   []*logstore.SessionRecord
	asyncDialogs    []*logstore.DialogRecord
	asyncDirs       []string
}

func (s *asyncTestLogStore) LogEvent(event *logstore.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncEventCalls++
}

func (s *asyncTestLogStore) StorePayload(_ context.Context, _ *logstore.PayloadRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncStoreCalls++
	return 1, nil
}

func (s *asyncTestLogStore) UpsertSession(_ context.Context, _ *logstore.SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncSessionCall++
	return nil
}

func (s *asyncTestLogStore) UpsertSessionDirectory(_ context.Context, _, _, _ string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncDirCalls++
	return nil
}

func (s *asyncTestLogStore) StorePayloadAndLogEventAsync(payload *logstore.PayloadRecord, event *logstore.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asyncPayloads = append(s.asyncPayloads, payload)
	s.asyncEvents = append(s.asyncEvents, event)
}

func (s *asyncTestLogStore) UpsertSessionAsync(sess *logstore.SessionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asyncSessions = append(s.asyncSessions, sess)
}

func (s *asyncTestLogStore) UpsertDialogAsync(dialog *logstore.DialogRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asyncDialogs = append(s.asyncDialogs, dialog)
}

func (s *asyncTestLogStore) UpsertSessionDirectoryAsync(sessionID, _, _ string, _ int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asyncDirs = append(s.asyncDirs, sessionID)
}

// syncOnlyTestLogStore implements logstore.LogStore but NOT logstore.AsyncLogStore.
type syncOnlyTestLogStore struct {
	logstore.LogStore
	payloads []*logstore.PayloadRecord
	events   []*logstore.Event
	sessions []*logstore.SessionRecord
	dirs     []string
}

func (s *syncOnlyTestLogStore) LogEvent(event *logstore.Event) {
	s.events = append(s.events, event)
}

func (s *syncOnlyTestLogStore) StorePayload(_ context.Context, payload *logstore.PayloadRecord) (int64, error) {
	s.payloads = append(s.payloads, payload)
	return int64(len(s.payloads)), nil
}

func (s *syncOnlyTestLogStore) UpsertSession(_ context.Context, sess *logstore.SessionRecord) error {
	s.sessions = append(s.sessions, sess)
	return nil
}

func (s *syncOnlyTestLogStore) UpsertSessionDirectory(_ context.Context, sessionID, _, _ string, _ int) error {
	s.dirs = append(s.dirs, sessionID)
	return nil
}

func TestAPIAsyncLogStoreUsedOnHotPaths(t *testing.T) {
	store := &asyncTestLogStore{}
	srv := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{InstanceID: "gw-1", PublicWSURL: "wss://gw-1.example/ws"},
		config.TranslatorConfig{},
		newTestSessionManager(),
		&stubSIPCallMaker{},
		nil,
		nil,
		store,
	)

	sess, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	sess.SetCallInfo("outbound", "1000", "2000", "call-async")
	sess.UpdateState(session.StateConnecting)

	srv.storePayloadAndLogEvent(context.Background(), &logstore.PayloadRecord{
		SessionID: sess.ID,
		Kind:      "webrtc_sdp_offer",
		BodyText:  "v=0",
	}, &logstore.Event{
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_offer_received",
	})
	srv.logSessionSnapshot(context.Background(), sess, "")
	srv.upsertSessionDirectory(context.Background(), sess.ID)

	if store.syncStoreCalls != 0 || store.syncEventCalls != 0 || store.syncSessionCall != 0 || store.syncDirCalls != 0 {
		t.Fatalf("expected zero sync logstore calls, got store=%d event=%d session=%d dir=%d",
			store.syncStoreCalls, store.syncEventCalls, store.syncSessionCall, store.syncDirCalls)
	}
	if len(store.asyncPayloads) != 1 || len(store.asyncEvents) != 1 {
		t.Fatalf("expected 1 async payload+event, got payloads=%d events=%d", len(store.asyncPayloads), len(store.asyncEvents))
	}
	if len(store.asyncSessions) != 1 || store.asyncSessions[0].SessionID != sess.ID {
		t.Fatalf("expected 1 async session upsert, got %+v", store.asyncSessions)
	}
	if len(store.asyncDirs) != 1 || store.asyncDirs[0] != sess.ID {
		t.Fatalf("expected 1 async session directory upsert, got %+v", store.asyncDirs)
	}
}

func TestAPISyncLogStoreFallback(t *testing.T) {
	store := &syncOnlyTestLogStore{}
	srv := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{InstanceID: "gw-1", PublicWSURL: "wss://gw-1.example/ws"},
		config.TranslatorConfig{},
		newTestSessionManager(),
		nil,
		nil,
		nil,
		store,
	)

	sess := &session.Session{ID: "sess-sync", State: session.StateConnecting, CreatedAt: time.Now()}
	sess.SetCallInfo("outbound", "1000", "2000", "call-sync")

	srv.storePayloadAndLogEvent(context.Background(), &logstore.PayloadRecord{
		SessionID: sess.ID,
		Kind:      "webrtc_sdp_offer",
		BodyText:  "v=0",
	}, &logstore.Event{
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_offer_received",
	})
	srv.logSessionSnapshot(context.Background(), sess, "")
	srv.upsertSessionDirectory(context.Background(), sess.ID)

	if len(store.payloads) != 1 || len(store.events) != 1 || store.events[0].PayloadID == nil || *store.events[0].PayloadID != 1 {
		t.Fatalf("expected sync payload+event fallback with linked PayloadID=1, got payloads=%d events=%+v", len(store.payloads), store.events)
	}
	if len(store.sessions) != 1 || len(store.dirs) != 1 {
		t.Fatalf("expected sync session+dir fallback, got sessions=%d dirs=%d", len(store.sessions), len(store.dirs))
	}
}

func TestHandleWSAccept_UpsertsSessionDirectoryAsyncWithoutDuplicateSnapshot(t *testing.T) {
	store := &asyncTestLogStore{}
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession incoming error: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "sip:alice@example.com", "sip:bob@example.com", "call-in-1")

	webrtcSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession webrtc error: %v", err)
	}

	srv := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{InstanceID: "gw-1", PublicWSURL: "wss://gw-1.example/ws"},
		config.TranslatorConfig{},
		mgr,
		&stubSIPCallMaker{},
		nil,
		nil,
		store,
	)
	client := &WSClient{send: make(chan []byte, 8), sessionID: webrtcSess.ID}

	srv.handleWSAccept(client, WSMessage{Type: "accept", SessionID: incomingSess.ID})

	if len(store.asyncDirs) != 1 || store.asyncDirs[0] != webrtcSess.ID {
		t.Fatalf("expected async session directory upsert for %s, got %+v", webrtcSess.ID, store.asyncDirs)
	}
	if len(store.asyncSessions) != 0 || store.syncSessionCall != 0 {
		t.Fatalf("expected no duplicate API session snapshot on accept (SIP layer records it), got async=%d sync=%d", len(store.asyncSessions), store.syncSessionCall)
	}
}

// atomicClaimTrunkManagerStub embeds stubResolveTrunkManager and adds
// ValidateAndClaimTrunkForOutboundCall and GetTrunksByIDsFromDB.
type atomicClaimTrunkManagerStub struct {
	stubResolveTrunkManager
	mu                sync.Mutex
	getByDBCalls      int
	setInUseCalls     int
	validateClaimCall int
	lastClaimUser     *string
	batchCalls        int
	lastBatchIDs      []int64
	identityGroup     []int64
	claimTrunk        *sip.Trunk
	claimNotFound     bool
	claimNotReady     string
	claimErr          error
	batchDoneCh       chan struct{}
}

func (s *atomicClaimTrunkManagerStub) GetTrunkByIDFromDB(ctx context.Context, trunkID int64) (*sip.Trunk, error) {
	s.mu.Lock()
	s.getByDBCalls++
	s.mu.Unlock()
	return s.stubResolveTrunkManager.GetTrunkByIDFromDB(ctx, trunkID)
}

func (s *atomicClaimTrunkManagerStub) SetTrunkInUseBy(_ context.Context, _ int64, _ *string) error {
	s.mu.Lock()
	s.setInUseCalls++
	s.mu.Unlock()
	return nil
}

func (s *atomicClaimTrunkManagerStub) ValidateAndClaimTrunkForOutboundCall(
	_ context.Context,
	_ int64,
	_ string,
	claimUsername *string,
) (*sip.Trunk, bool, string, error) {
	s.mu.Lock()
	s.validateClaimCall++
	if claimUsername != nil {
		copied := *claimUsername
		s.lastClaimUser = &copied
	} else {
		s.lastClaimUser = nil
	}
	s.mu.Unlock()
	return s.claimTrunk, s.claimNotFound, s.claimNotReady, s.claimErr
}

func (s *atomicClaimTrunkManagerStub) GetTrunksByIDsFromDB(_ context.Context, trunkIDs []int64) ([]*sip.Trunk, error) {
	s.mu.Lock()
	s.batchCalls++
	s.lastBatchIDs = append([]int64(nil), trunkIDs...)
	ch := s.batchDoneCh
	s.mu.Unlock()

	trunks := make([]*sip.Trunk, 0, len(trunkIDs))
	for _, id := range trunkIDs {
		if t, ok := s.byID[id]; ok && t != nil {
			trunks = append(trunks, t)
		}
	}
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return trunks, nil
}

func (s *atomicClaimTrunkManagerStub) IdentityGroupTrunkIDs(trunkID int64) []int64 {
	if len(s.identityGroup) > 0 {
		return s.identityGroup
	}
	return []int64{trunkID}
}

func TestHandleWSCall_UsesAtomicValidateAndClaimTrunk(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	trunk := readyAgentTrunk(55)
	tm := &atomicClaimTrunkManagerStub{
		stubResolveTrunkManager: stubResolveTrunkManager{
			byID: map[int64]*sip.Trunk{55: trunk},
		},
		claimTrunk: trunk,
	}
	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, mgr, sipMaker, nil, tm, nil)
	client := &WSClient{
		send:            make(chan []byte, 8),
		trunkResolved:   true,
		resolvedTrunkID: 55,
		sessionID:       sess.ID,
		authClaims: &auth.VerifiedClaims{
			Subject:           "user-sub",
			PreferredUsername: "alice",
		},
	}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1001",
		TrunkID:     55,
	})

	waitForMakeCallCount(t, sipMaker, 1)
	if tm.validateClaimCall != 1 {
		t.Fatalf("expected 1 ValidateAndClaimTrunkForOutboundCall call, got %d", tm.validateClaimCall)
	}
	if tm.lastClaimUser == nil || *tm.lastClaimUser != "alice" {
		t.Fatalf("expected claimUsername=alice, got %v", tm.lastClaimUser)
	}
	if tm.getByDBCalls != 0 {
		t.Fatalf("expected 0 GetTrunkByIDFromDB calls when atomic helper is used, got %d", tm.getByDBCalls)
	}
	if tm.setInUseCalls != 0 {
		t.Fatalf("expected 0 SetTrunkInUseBy calls when atomic helper already claimed, got %d", tm.setInUseCalls)
	}
}

func TestHandleCall_UsesAtomicValidateAndClaimTrunk(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	trunk := readyAgentTrunk(56)
	tm := &atomicClaimTrunkManagerStub{
		stubResolveTrunkManager: stubResolveTrunkManager{
			byID: map[int64]*sip.Trunk{56: trunk},
		},
		claimTrunk: trunk,
	}
	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, mgr, sipMaker, nil, tm, nil)

	body, _ := json.Marshal(CallRequest{
		SessionID:   sess.ID,
		Destination: "1001",
		TrunkID:     56,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/call", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), authClaimsContextKey{}, &auth.VerifiedClaims{
		Subject:           "user-sub",
		PreferredUsername: "bob",
	}))
	rr := httptest.NewRecorder()
	srv.handleCall(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d body=%s", rr.Code, rr.Body.String())
	}
	if tm.validateClaimCall != 1 || tm.lastClaimUser == nil || *tm.lastClaimUser != "bob" {
		t.Fatalf("expected atomic claim with bob, calls=%d user=%v", tm.validateClaimCall, tm.lastClaimUser)
	}
	if tm.setInUseCalls != 0 {
		t.Fatalf("expected 0 fallback SetTrunkInUseBy calls, got %d", tm.setInUseCalls)
	}
}

func TestResolveTrunkName_RAMFirstThenDBFallback(t *testing.T) {
	tm := &atomicClaimTrunkManagerStub{
		stubResolveTrunkManager: stubResolveTrunkManager{
			byID: map[int64]*sip.Trunk{
				10: {ID: 10, Name: "ram-trunk"},
			},
		},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, tm, nil)

	if got := srv.resolveTrunkName(context.Background(), 10); got != "ram-trunk" {
		t.Fatalf("got %q want ram-trunk", got)
	}
	if tm.getByDBCalls != 0 {
		t.Fatalf("expected 0 DB calls on RAM hit, got %d", tm.getByDBCalls)
	}

	// RAM miss should fall back to GetTrunkByIDFromDB.
	if got := srv.resolveTrunkName(context.Background(), 999); got != "" {
		t.Fatalf("expected empty on missing trunk, got %q", got)
	}
	if tm.getByDBCalls != 1 {
		t.Fatalf("expected 1 DB fallback call on RAM miss, got %d", tm.getByDBCalls)
	}
}

func TestNotifyIncomingCall_BatchLoadsAndReusesTrunksForPush(t *testing.T) {
	tok1 := "fcm-token-1"
	tok2 := "fcm-token-2"
	tm := &atomicClaimTrunkManagerStub{
		stubResolveTrunkManager: stubResolveTrunkManager{
			byID: map[int64]*sip.Trunk{
				11: {ID: 11, Name: "sipclient-agent-device-1001@sip:5060", FcmToken: &tok1},
				12: {ID: 12, Name: "sipclient-agent-device-1001@sip:5060", FcmToken: &tok2},
				13: {ID: 13, Name: "sipclient-agent-1001@sip:5060"},
			},
		},
		identityGroup: []int64{11, 12, 13},
		batchDoneCh:   make(chan struct{}, 4),
	}
	srv := NewServer(config.APIConfig{IncomingRingTimeoutSeconds: 30}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, tm, nil)
	srv.SetPushService(push.NewService(nil, nil))

	srv.NotifyIncomingCall("sess-batch-push", "sip:caller@example.com", "sip:agent@example.com", 11)

	select {
	case <-tm.batchDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for incoming push batch DB lookup")
	}
	// Wait briefly to ensure dispatchIncomingPush goroutine has run without issuing a 2nd DB batch call.
	time.Sleep(50 * time.Millisecond)
	tm.mu.Lock()
	incomingBatchCalls := tm.batchCalls
	incomingSingleCalls := tm.getByDBCalls
	tm.mu.Unlock()

	if incomingBatchCalls != 1 {
		t.Fatalf("expected exactly 1 batch DB lookup across check+dispatch, got %d", incomingBatchCalls)
	}
	if incomingSingleCalls != 0 {
		t.Fatalf("expected 0 per-trunk GetTrunkByIDFromDB calls, got %d", incomingSingleCalls)
	}

	// Now trigger call_ended push and verify it also uses 1 batch lookup (1+1 total).
	srv.dispatchCallEndedPush("sess-batch-push", push.CallEndedAnsweredElsewhere, &WSClient{resolvedTrunkID: 11})
	select {
	case <-tm.batchDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for call_ended batch DB lookup")
	}
	tm.mu.Lock()
	totalBatchCalls := tm.batchCalls
	totalSingleCalls := tm.getByDBCalls
	tm.mu.Unlock()
	if totalBatchCalls != 2 {
		t.Fatalf("expected 2 total batch DB lookups (1 incoming + 1 call_ended), got %d", totalBatchCalls)
	}
	if totalSingleCalls != 0 {
		t.Fatalf("expected 0 per-trunk DB calls, got %d", totalSingleCalls)
	}
}

type trunkDetailsResolverStoreStub struct {
	stubResolveStore
	detailsCalls     int
	legacyCalls      int
	publicID         string
	resolvedUsername string
}

func (s *trunkDetailsResolverStoreStub) ResolveTrunkByCredentials(ctx context.Context, domain string, port int, username, password string) (int64, *string, *time.Time, bool, error) {
	s.legacyCalls++
	return s.stubResolveStore.ResolveTrunkByCredentials(ctx, domain, port, username, password)
}

func (s *trunkDetailsResolverStoreStub) ResolveTrunkDetailsByCredentials(_ context.Context, _ string, _ int, _, _ string) (int64, string, string, *string, *time.Time, bool, error) {
	s.detailsCalls++
	return s.resolveTrunkID, s.publicID, s.resolvedUsername, s.resolveLeaseOwner, s.resolveLeaseUntil, s.resolveFound, s.resolveErr
}

func TestHandleWSTrunkResolve_UsesTrunkDetailsResolverWithoutExtraDBLookup(t *testing.T) {
	owner := "gw-1"
	until := time.Now().Add(2 * time.Minute)
	store := &trunkDetailsResolverStoreStub{
		stubResolveStore: stubResolveStore{
			resolveTrunkID:    77,
			resolveLeaseOwner: &owner,
			resolveLeaseUntil: &until,
			resolveFound:      true,
		},
		publicID:         "11111111-2222-3333-4444-555555555555",
		resolvedUsername: "agent77",
	}
	tm := &atomicClaimTrunkManagerStub{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, tm, store)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:        "trunk_resolve",
		SIPDomain:   "pbx.example.com",
		SIPUsername: "agent77",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	if store.detailsCalls != 1 || store.legacyCalls != 0 {
		t.Fatalf("expected ResolveTrunkDetailsByCredentials=1 legacy=0, got details=%d legacy=%d", store.detailsCalls, store.legacyCalls)
	}
	if tm.getByDBCalls != 0 {
		t.Fatalf("expected 0 extra GetTrunkByIDFromDB calls, got %d", tm.getByDBCalls)
	}
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" || msgs[0].TrunkID != 77 || msgs[0].TrunkPublicID != store.publicID {
		t.Fatalf("unexpected trunk_resolved response: %+v", msgs)
	}
}

type ensureRegisterTrunkStub struct {
	mobileProvisionTrunkStub
	ensureID    int64
	ensureForce bool
	ensureCalls int
}

func (s *ensureRegisterTrunkStub) EnsureTrunkRegistered(trunkID int64, forceLeaseSteal bool) error {
	s.ensureID = trunkID
	s.ensureForce = forceLeaseSteal
	s.ensureCalls++
	s.calls = append(s.calls, "ensure_register")
	return nil
}

func TestMobileSIPProvisioner_UsesEnsureTrunkRegisteredFastPath(t *testing.T) {
	trunks := &ensureRegisterTrunkStub{}
	provisioner := NewMobileSIPProvisioner(
		mobileProvisionAuthStub{account: &sipclientauth.Account{
			Domain:    "sip.example.com",
			Extension: "1001",
			Secret:    "secret",
		}},
		trunks,
	)

	res, err := provisioner.ProvisionMobileSIPTrunk(context.Background(), "jwt-token", &auth.VerifiedClaims{
		Subject: "user-sub-fast",
		Realm:   auth.TokenRealmUser,
	}, "ios")
	if err != nil {
		t.Fatalf("ProvisionMobileSIPTrunk failed: %v", err)
	}
	if res.TrunkID != 42 {
		t.Fatalf("unexpected trunkID %d", res.TrunkID)
	}
	if trunks.ensureCalls != 1 || trunks.ensureID != 42 || !trunks.ensureForce {
		t.Fatalf("expected EnsureTrunkRegistered(42, true), got calls=%d id=%d force=%v", trunks.ensureCalls, trunks.ensureID, trunks.ensureForce)
	}
	if trunks.registerID != 0 {
		t.Fatalf("expected RegisterTrunk not to be called directly when EnsureTrunkRegistered is implemented, got id=%d", trunks.registerID)
	}
}

type blockingPublicRegistryStub struct {
	mu           sync.Mutex
	started      chan struct{}
	unblock      chan struct{}
	ctxCanceled  chan struct{}
	respectCtx   bool
	accountKey   string
	regErr       error
	incRefCalls  int
	decRefCalls  int
	lastIncKey   string
}

func (s *blockingPublicRegistryStub) AcquireAndRegister(ctx context.Context, _, _, _ string, _ int) (string, error) {
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.unblock != nil {
		if s.respectCtx {
			select {
			case <-ctx.Done():
				if s.ctxCanceled != nil {
					select {
					case s.ctxCanceled <- struct{}{}:
					default:
					}
				}
				return "", ctx.Err()
			case <-s.unblock:
			}
		} else {
			<-s.unblock
		}
	}
	return s.accountKey, s.regErr
}

func (s *blockingPublicRegistryStub) IncrementRefCount(accountKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.incRefCalls++
	s.lastIncKey = accountKey
}

func (s *blockingPublicRegistryStub) DecrementRefCount(_ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decRefCalls++
}

func (s *blockingPublicRegistryStub) ListAccounts() []*sip.PublicAccount { return nil }

func TestHandleWSCall_PublicRegisterRunsAsynchronously(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	reg := &blockingPublicRegistryStub{
		started:    make(chan struct{}, 1),
		unblock:    make(chan struct{}),
		accountKey: "userA@example.com:5060",
	}
	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, reg, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	done := make(chan struct{})
	go func() {
		srv.handleWSCall(client, WSMessage{
			Type:        "call",
			SessionID:   sess.ID,
			Destination: "1002",
			From:        "callerA",
			SIPDomain:   "example.com",
			SIPUsername: "userA",
			SIPPassword: "secret",
			SIPPort:     5060,
		})
		close(done)
	}()

	// handleWSCall must return immediately even while AcquireAndRegister is blocked.
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleWSCall blocked WS read pump waiting for AcquireAndRegister")
	}

	// Connecting ack should already be sent.
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "state" || msgs[0].State != string(session.StateConnecting) {
		t.Fatalf("expected immediate connecting ack, got %+v", msgs)
	}

	// Wait until AcquireAndRegister started in runWSCall, then unblock it.
	select {
	case <-reg.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected runWSCall to invoke AcquireAndRegister")
	}
	close(reg.unblock)

	waitForMakeCallCount(t, sipMaker, 1)
	reg.mu.Lock()
	incCalls := reg.incRefCalls
	incKey := reg.lastIncKey
	reg.mu.Unlock()
	if incCalls != 1 || incKey != "userA@example.com:5060" {
		t.Fatalf("expected IncrementRefCount(userA@example.com:5060), got calls=%d key=%q", incCalls, incKey)
	}
	mode, accountKey, _, domain, username, _, _ := sess.GetSIPAuthContext()
	if mode != "public" || accountKey != "userA@example.com:5060" || domain != "example.com" || username != "userA" {
		t.Fatalf("unexpected session auth context after async register: mode=%q key=%q domain=%q user=%q", mode, accountKey, domain, username)
	}
}

func TestHandleWSCall_PublicRegisterFailureSendsWSError(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	reg := &blockingPublicRegistryStub{
		regErr: errors.New("403 forbidden"),
	}
	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, reg, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1002",
		SIPDomain:   "example.com",
		SIPUsername: "userA",
		SIPPassword: "wrong",
		SIPPort:     5060,
	})

	deadline := time.Now().Add(time.Second)
	var msgs []WSMessage
	for len(msgs) < 2 && time.Now().Before(deadline) {
		msgs = append(msgs, readWSMessages(t, client.send)...)
		if len(msgs) < 2 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if len(msgs) != 2 || msgs[0].Type != "state" || msgs[1].Type != "error" || !strings.Contains(msgs[1].Error, "Failed to register SIP account: 403 forbidden") {
		t.Fatalf("expected connecting state then register error, got %+v", msgs)
	}
	if sipMaker.makeCallCount != 0 {
		t.Fatalf("expected MakeCall not to be called after register failure, got %d", sipMaker.makeCallCount)
	}
}

func newLoopbackBrowserPeerConnection(t *testing.T) *webrtc.PeerConnection {
	t.Helper()
	var se webrtc.SettingEngine
	se.SetIncludeLoopbackCandidate(true)
	se.SetIPFilter(func(ip net.IP) bool {
		return ip.IsLoopback()
	})
	se.SetICEMulticastDNSMode(pionice.MulticastDNSModeDisabled)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	return pc
}

func createBrowserOfferSDP(t *testing.T) string {
	t.Helper()
	pc := newLoopbackBrowserPeerConnection(t)
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv}); err != nil {
		t.Fatalf("AddTransceiver audio: %v", err)
	}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv}); err != nil {
		t.Fatalf("AddTransceiver video: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	return offer.SDP
}

func TestHandleWSOffer_FlushesPendingRemoteICEAndUsesBoundedICEGather(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	defer mgr.DeleteSession(sess.ID)

	// Queue a remote ICE candidate before SetRemoteDescription so FlushPendingRemoteICE has work to do.
	mid := "0"
	mLine := uint16(0)
	queued, err := sess.AddRemoteICECandidate(webrtc.ICECandidateInit{
		Candidate:     "candidate:1 1 udp 2122252543 192.0.2.10 54321 typ host",
		SDPMid:        &mid,
		SDPMLineIndex: &mLine,
	})
	if err != nil || !queued {
		t.Fatalf("expected early remote ICE candidate to be queued, queued=%v err=%v", queued, err)
	}

	store := &asyncTestLogStore{}
	srv := NewServer(config.APIConfig{OfferICEGatherTimeoutMS: 250}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, store)
	srv.SetOfferICEGatherTimeout(250 * time.Millisecond)
	srv.SetOfferICEHasNAT1To1(true)
	client := &WSClient{send: make(chan []byte, 8)}

	offerSDP := createBrowserOfferSDP(t)
	start := time.Now()
	srv.handleWSoffer(client, WSMessage{
		Type:      "offer",
		SessionID: sess.ID,
		SDP:       offerSDP,
	})
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("handleWSoffer took too long with bounded ICE gather: %s", elapsed)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "answer" || msgs[0].SDP == "" {
		t.Fatalf("expected single answer message with SDP, got %+v", msgs)
	}
	if remaining := sess.FlushPendingRemoteICE(); remaining != 0 {
		t.Fatalf("expected pending remote ICE queue to already be flushed after handleWSoffer, remaining=%d", remaining)
	}
	if len(store.asyncPayloads) != 2 || len(store.asyncEvents) != 2 || store.syncStoreCalls != 0 {
		t.Fatalf("expected offer+answer payloads via AsyncLogStore, got asyncPayloads=%d syncStore=%d", len(store.asyncPayloads), store.syncStoreCalls)
	}
}

func TestHandleOffer_FlushesPendingRemoteICEAndUsesBoundedICEGather(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	defer mgr.DeleteSession(sess.ID)

	mid := "0"
	mLine := uint16(0)
	queued, err := sess.AddRemoteICECandidate(webrtc.ICECandidateInit{
		Candidate:     "candidate:1 1 udp 2122252543 192.0.2.11 54322 typ host",
		SDPMid:        &mid,
		SDPMLineIndex: &mLine,
	})
	if err != nil || !queued {
		t.Fatalf("expected early remote ICE candidate to be queued, queued=%v err=%v", queued, err)
	}

	store := &asyncTestLogStore{}
	srv := NewServer(config.APIConfig{OfferICEGatherTimeoutMS: 250}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, store)
	srv.SetOfferICEHasNAT1To1(true)

	body, _ := json.Marshal(OfferRequest{
		SessionID: sess.ID,
		SDP:       createBrowserOfferSDP(t),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/offer", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.handleOffer(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d body=%s", rr.Code, rr.Body.String())
	}
	if remaining := sess.FlushPendingRemoteICE(); remaining != 0 {
		t.Fatalf("expected pending remote ICE queue to already be flushed after handleOffer, remaining=%d", remaining)
	}
	if len(store.asyncPayloads) != 2 || store.syncStoreCalls != 0 {
		t.Fatalf("expected offer+answer payloads via AsyncLogStore, got asyncPayloads=%d syncStore=%d", len(store.asyncPayloads), store.syncStoreCalls)
	}
}

type blockingMakeCallStub struct {
	stubSIPCallMaker
	mu             sync.Mutex
	makeCallStart  chan struct{}
	makeCallDone   chan struct{}
	waitForSessEnd bool
}

func (s *blockingMakeCallStub) MakeCall(destination, from string, sess *session.Session) error {
	s.mu.Lock()
	s.makeCallCount++
	s.mu.Unlock()
	if s.makeCallStart != nil {
		select {
		case s.makeCallStart <- struct{}{}:
		default:
		}
	}
	if s.waitForSessEnd && sess != nil {
		<-sess.Done()
	}
	defer func() {
		if s.makeCallDone != nil {
			close(s.makeCallDone)
		}
	}()
	return s.makeCallErr
}

func (s *blockingMakeCallStub) CancelPendingCall(sess *session.Session) error {
	if sess != nil {
		sess.MarkPendingCancelRequested()
		sess.UpdateState(session.StateEnded)
	}
	return nil
}

func TestHandleWSCall_ConcurrentHangupDuringAcquireAndRegisterCancelsContextAndDoesNotLeakRefCount(t *testing.T) {
	t.Run("cancels_inflight_register_context", func(t *testing.T) {
		mgr := newTestSessionManager()
		sess, err := mgr.CreateSession(config.TURNConfig{})
		if err != nil {
			t.Fatalf("CreateSession error: %v", err)
		}

		reg := &blockingPublicRegistryStub{
			started:     make(chan struct{}, 1),
			unblock:     make(chan struct{}),
			ctxCanceled: make(chan struct{}, 1),
			respectCtx:  true,
			accountKey:  "userA@example.com:5060",
		}
		sipMaker := &blockingMakeCallStub{}
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, reg, nil, nil)
		client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

		srv.handleWSCall(client, WSMessage{
			Type:        "call",
			SessionID:   sess.ID,
			Destination: "1002",
			From:        "callerA",
			SIPDomain:   "example.com",
			SIPUsername: "userA",
			SIPPassword: "secret",
			SIPPort:     5060,
		})

		select {
		case <-reg.started:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("expected runWSCall to invoke AcquireAndRegister")
		}

		// Hangup while AcquireAndRegister is still in flight.
		srv.handleWSHangup(client, WSMessage{Type: "hangup", SessionID: sess.ID})

		select {
		case <-reg.ctxCanceled:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("expected AcquireAndRegister context to be canceled immediately on hangup")
		}

		time.Sleep(20 * time.Millisecond)
		reg.mu.Lock()
		inc, dec := reg.incRefCalls, reg.decRefCalls
		reg.mu.Unlock()
		if inc != 0 || dec != 0 {
			t.Fatalf("expected 0 refcount ops after canceled register, got inc=%d dec=%d", inc, dec)
		}
		sipMaker.mu.Lock()
		calls := sipMaker.makeCallCount
		sipMaker.mu.Unlock()
		if calls != 0 {
			t.Fatalf("expected MakeCall not to run after hangup, got %d", calls)
		}
	})

	t.Run("hangup_races_register_completion_no_leak", func(t *testing.T) {
		mgr := newTestSessionManager()
		sess, err := mgr.CreateSession(config.TURNConfig{})
		if err != nil {
			t.Fatalf("CreateSession error: %v", err)
		}

		// Simulate AcquireAndRegister finishing right as hangup arrives (ignoring ctx cancel).
		reg := &blockingPublicRegistryStub{
			started:    make(chan struct{}, 1),
			unblock:    make(chan struct{}),
			respectCtx: false,
			accountKey: "userA@example.com:5060",
		}
		sipMaker := &blockingMakeCallStub{}
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, reg, nil, nil)
		client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

		srv.handleWSCall(client, WSMessage{
			Type:        "call",
			SessionID:   sess.ID,
			Destination: "1002",
			From:        "callerA",
			SIPDomain:   "example.com",
			SIPUsername: "userA",
			SIPPassword: "secret",
			SIPPort:     5060,
		})

		select {
		case <-reg.started:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("expected runWSCall to invoke AcquireAndRegister")
		}

		srv.handleWSHangup(client, WSMessage{Type: "hangup", SessionID: sess.ID})
		close(reg.unblock)
		time.Sleep(30 * time.Millisecond)

		reg.mu.Lock()
		inc, dec := reg.incRefCalls, reg.decRefCalls
		reg.mu.Unlock()
		if inc-dec != 0 {
			t.Fatalf("expected net refcount == 0 (no leak), got inc=%d dec=%d", inc, dec)
		}
		sipMaker.mu.Lock()
		calls := sipMaker.makeCallCount
		sipMaker.mu.Unlock()
		if calls != 0 {
			t.Fatalf("expected MakeCall not to run after concurrent hangup, got %d", calls)
		}
	})

	t.Run("hangup_during_inflight_makecall_decrements_once", func(t *testing.T) {
		mgr := newTestSessionManager()
		sess, err := mgr.CreateSession(config.TURNConfig{})
		if err != nil {
			t.Fatalf("CreateSession error: %v", err)
		}

		reg := &blockingPublicRegistryStub{
			accountKey: "userA@example.com:5060",
		}
		sipMaker := &blockingMakeCallStub{
			stubSIPCallMaker: stubSIPCallMaker{makeCallErr: errors.New("canceled")},
			makeCallStart:    make(chan struct{}, 1),
			makeCallDone:     make(chan struct{}),
			waitForSessEnd:   true,
		}
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, reg, nil, nil)
		client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

		srv.handleWSCall(client, WSMessage{
			Type:        "call",
			SessionID:   sess.ID,
			Destination: "1002",
			From:        "callerA",
			SIPDomain:   "example.com",
			SIPUsername: "userA",
			SIPPassword: "secret",
			SIPPort:     5060,
		})

		select {
		case <-sipMaker.makeCallStart:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("expected MakeCall to start")
		}

		srv.handleWSHangup(client, WSMessage{Type: "hangup", SessionID: sess.ID})

		select {
		case <-sipMaker.makeCallDone:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("expected MakeCall to finish after hangup")
		}
		time.Sleep(20 * time.Millisecond)

		reg.mu.Lock()
		inc, dec := reg.incRefCalls, reg.decRefCalls
		reg.mu.Unlock()
		if inc != 1 || dec != 1 {
			t.Fatalf("expected exact 1:1 refcount inc=1 dec=1, got inc=%d dec=%d", inc, dec)
		}
	})

	t.Run("nil_sipMaker_does_not_increment_refcount", func(t *testing.T) {
		mgr := newTestSessionManager()
		sess, err := mgr.CreateSession(config.TURNConfig{})
		if err != nil {
			t.Fatalf("CreateSession error: %v", err)
		}

		reg := &blockingPublicRegistryStub{
			started:    make(chan struct{}, 1),
			accountKey: "userA@example.com:5060",
		}
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, reg, nil, nil)
		client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

		srv.handleWSCall(client, WSMessage{
			Type:        "call",
			SessionID:   sess.ID,
			Destination: "1002",
			From:        "callerA",
			SIPDomain:   "example.com",
			SIPUsername: "userA",
			SIPPassword: "secret",
			SIPPort:     5060,
		})

		select {
		case <-reg.started:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("expected AcquireAndRegister to be called")
		}
		time.Sleep(20 * time.Millisecond)

		reg.mu.Lock()
		inc, dec := reg.incRefCalls, reg.decRefCalls
		reg.mu.Unlock()
		if inc != 0 || dec != 0 {
			t.Fatalf("expected no refcount increment when sipMaker is nil, got inc=%d dec=%d", inc, dec)
		}
	})
}

func TestHandleWSHangup_PreInvitePersistsEndedSnapshot(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	store := &asyncTestLogStore{}
	reg := &blockingPublicRegistryStub{
		started:     make(chan struct{}, 1),
		unblock:     make(chan struct{}),
		ctxCanceled: make(chan struct{}, 1),
		respectCtx:  true,
		accountKey:  "userA@example.com:5060",
	}
	sipMaker := &blockingMakeCallStub{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, reg, nil, store)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1002",
		From:        "callerA",
		SIPDomain:   "example.com",
		SIPUsername: "userA",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	select {
	case <-reg.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected AcquireAndRegister to start")
	}

	srv.handleWSHangup(client, WSMessage{Type: "hangup", SessionID: sess.ID})

	store.mu.Lock()
	snaps := append([]*logstore.SessionRecord(nil), store.asyncSessions...)
	syncCalls := store.syncSessionCall
	store.mu.Unlock()

	if syncCalls != 0 {
		t.Fatalf("expected async session persistence only, got syncSessionCall=%d", syncCalls)
	}
	if len(snaps) < 2 {
		t.Fatalf("expected initial connecting snapshot + terminal ended snapshot, got %d", len(snaps))
	}
	last := snaps[len(snaps)-1]
	if last.SessionID != sess.ID || last.FinalState != string(session.StateEnded) || last.EndedAt == nil {
		t.Fatalf("expected final ended snapshot with EndedAt != nil, got %+v", last)
	}
}

func TestHandleRESTHangup_PreDialogPersistsEndedSnapshot(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	sess.SetCallInfo("outbound", "1000", "2000", "")
	sess.UpdateState(session.StateConnecting)

	store := &asyncTestLogStore{}
	reg := &blockingPublicRegistryStub{accountKey: "userA@example.com:5060"}
	if !sess.BindPublicAccountKeyIfActive("userA@example.com:5060") {
		t.Fatal("expected BindPublicAccountKeyIfActive to succeed")
	}
	reg.IncrementRefCount("userA@example.com:5060")

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, &stubSIPCallMaker{}, reg, nil, store)

	req := httptest.NewRequest(http.MethodPost, "/api/hangup/"+sess.ID, nil)
	req = mux.SetURLVars(req, map[string]string{"sessionId": sess.ID})
	rr := httptest.NewRecorder()
	srv.handleHangup(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d body=%s", rr.Code, rr.Body.String())
	}

	store.mu.Lock()
	snaps := append([]*logstore.SessionRecord(nil), store.asyncSessions...)
	syncCalls := store.syncSessionCall
	store.mu.Unlock()

	if syncCalls != 0 {
		t.Fatalf("expected async session persistence only, got syncSessionCall=%d", syncCalls)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 terminal ended snapshot, got %d", len(snaps))
	}
	if snaps[0].SessionID != sess.ID || snaps[0].FinalState != string(session.StateEnded) || snaps[0].EndedAt == nil {
		t.Fatalf("expected ended snapshot with EndedAt != nil, got %+v", snaps[0])
	}

	reg.mu.Lock()
	inc, dec := reg.incRefCalls, reg.decRefCalls
	reg.mu.Unlock()
	if inc != 1 || dec != 1 {
		t.Fatalf("expected REST hangup to release public account refcount (1:1), got inc=%d dec=%d", inc, dec)
	}
}
