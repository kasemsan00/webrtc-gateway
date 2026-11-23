package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/push"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

// singleInstanceTestTrunkManager implements TrunkManager plus the optional
// single-instance RAM fast-path interfaces (GetTrunksByIDsFromRAM,
// ResolveTrunkByCredentialsFromRAM) and batchTrunkLoader, while counting DB calls.
type singleInstanceTestTrunkManager struct {
	stubResolveTrunkManager

	mu            sync.Mutex
	ramTrunks     map[int64]*sip.Trunk
	dbTrunks      map[int64]*sip.Trunk
	identityGroup []int64

	ramGetCalls        int
	ramBatchCalls      int
	ramCredCalls       int
	dbGetCalls         int
	dbBatchCalls       int
	dbBatchLastIDs     []int64
	ramBatchDoneCh     chan struct{}
	dbBatchDoneCh      chan struct{}
}

func (m *singleInstanceTestTrunkManager) GetTrunkByID(id int64) (interface{}, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ramGetCalls++
	t, ok := m.ramTrunks[id]
	return t, ok
}

func (m *singleInstanceTestTrunkManager) GetTrunkByIDFromDB(_ context.Context, trunkID int64) (*sip.Trunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbGetCalls++
	t, ok := m.dbTrunks[trunkID]
	if !ok {
		return nil, nil
	}
	return t, nil
}

func (m *singleInstanceTestTrunkManager) GetTrunksByIDsFromRAM(trunkIDs []int64) ([]*sip.Trunk, []int64) {
	m.mu.Lock()
	m.ramBatchCalls++
	found := make([]*sip.Trunk, 0, len(trunkIDs))
	var missing []int64
	for _, id := range trunkIDs {
		if t, ok := m.ramTrunks[id]; ok && t != nil {
			found = append(found, t)
		} else {
			missing = append(missing, id)
		}
	}
	doneCh := m.ramBatchDoneCh
	m.mu.Unlock()
	if doneCh != nil {
		select {
		case doneCh <- struct{}{}:
		default:
		}
	}
	return found, missing
}

func (m *singleInstanceTestTrunkManager) GetTrunksByIDsFromDB(_ context.Context, trunkIDs []int64) ([]*sip.Trunk, error) {
	m.mu.Lock()
	m.dbBatchCalls++
	m.dbBatchLastIDs = slices.Clone(trunkIDs)
	out := make([]*sip.Trunk, 0, len(trunkIDs))
	for _, id := range trunkIDs {
		if t, ok := m.dbTrunks[id]; ok && t != nil {
			out = append(out, t)
		}
	}
	doneCh := m.dbBatchDoneCh
	m.mu.Unlock()
	if doneCh != nil {
		select {
		case doneCh <- struct{}{}:
		default:
		}
	}
	return out, nil
}

func (m *singleInstanceTestTrunkManager) ResolveTrunkByCredentialsFromRAM(domain string, port int, username, password string) (*sip.Trunk, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ramCredCalls++
	for _, t := range m.ramTrunks {
		if t == nil || !t.Enabled {
			continue
		}
		if t.Domain == domain && t.Username == username && t.Password == password && (port == 0 || t.Port == port) {
			return t, true
		}
	}
	return nil, false
}

func (m *singleInstanceTestTrunkManager) IdentityGroupTrunkIDs(trunkID int64) []int64 {
	if len(m.identityGroup) > 0 {
		return m.identityGroup
	}
	return []int64{trunkID}
}

type countingSingleInstanceLogStore struct {
	trunkDetailsResolverStoreStub
	mu                     sync.Mutex
	lookupGatewayCalls     int
	lookupSessionDirCalls  int
	upsertSessionDirCalls  int
	upsertSessionDirAsync  int
}

func (s *countingSingleInstanceLogStore) LookupGatewayInstance(ctx context.Context, instanceID string) (string, bool, error) {
	s.mu.Lock()
	s.lookupGatewayCalls++
	s.mu.Unlock()
	return s.stubResolveStore.LookupGatewayInstance(ctx, instanceID)
}

func (s *countingSingleInstanceLogStore) LookupSessionDirectory(_ context.Context, _ string) (string, string, bool, error) {
	s.mu.Lock()
	s.lookupSessionDirCalls++
	s.mu.Unlock()
	return "gw-other", "wss://gw-other.example/ws", true, nil
}

func (s *countingSingleInstanceLogStore) UpsertSessionDirectory(_ context.Context, _, _, _ string, _ int) error {
	s.mu.Lock()
	s.upsertSessionDirCalls++
	s.mu.Unlock()
	return nil
}

func TestHandleWSOffer_LazyIncomingSessionInitializesPeerConnection(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateIncomingSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateIncomingSession: %v", err)
	}
	defer mgr.DeleteSession(sess.ID)
	sess.UpdateState(session.StateIncoming)

	if sess.HasPeerConnection() {
		t.Fatalf("expected lazy incoming session to start without PeerConnection")
	}

	srv := NewServer(
		config.APIConfig{OfferICEGatherTimeoutMS: 250},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1"},
		config.TranslatorConfig{},
		mgr,
		nil,
		nil,
		nil,
		nil,
	)
	srv.SetOfferICEGatherTimeout(250 * time.Millisecond)
	srv.SetOfferICEHasNAT1To1(true)

	client := &WSClient{send: make(chan []byte, 8)}
	srv.handleWSoffer(client, WSMessage{
		Type:      "offer",
		SessionID: sess.ID,
		SDP:       createBrowserOfferSDP(t),
	})

	if !sess.HasPeerConnection() {
		t.Fatalf("expected handleWSoffer to lazily allocate PeerConnection")
	}
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "answer" || msgs[0].SessionID != sess.ID || msgs[0].SDP == "" {
		t.Fatalf("expected answer with SDP for lazy incoming session, got %+v", msgs)
	}
	if sess.GetState() != session.StateIncoming {
		t.Fatalf("expected incoming session to remain in StateIncoming until accept, got %s", sess.GetState())
	}
}

func TestHandleOfferREST_LazyIncomingSessionInitializesPeerConnection(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateIncomingSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateIncomingSession: %v", err)
	}
	defer mgr.DeleteSession(sess.ID)
	sess.UpdateState(session.StateIncoming)

	if sess.HasPeerConnection() {
		t.Fatalf("expected lazy incoming session to start without PeerConnection")
	}

	srv := NewServer(
		config.APIConfig{OfferICEGatherTimeoutMS: 250},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1"},
		config.TranslatorConfig{},
		mgr,
		nil,
		nil,
		nil,
		nil,
	)
	srv.SetOfferICEGatherTimeout(250 * time.Millisecond)
	srv.SetOfferICEHasNAT1To1(true)

	body, err := json.Marshal(OfferRequest{
		SessionID: sess.ID,
		SDP:       createBrowserOfferSDP(t),
	})
	if err != nil {
		t.Fatalf("marshal OfferRequest: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/offer", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleOffer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from handleOffer, got %d: %s", rec.Code, rec.Body.String())
	}
	if !sess.HasPeerConnection() {
		t.Fatalf("expected handleOffer to lazily allocate PeerConnection")
	}
	var resp OfferResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode OfferResponse: %v", err)
	}
	if resp.SessionID != sess.ID || resp.SDP == "" {
		t.Fatalf("unexpected OfferResponse: %+v", resp)
	}
}

func TestLazyIncomingSession_RejectCancelIceAcceptBeforeOfferNeverPanics(t *testing.T) {
	mgr := newTestSessionManager()
	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1"},
		config.TranslatorConfig{},
		mgr,
		sipMaker,
		nil,
		nil,
		nil,
	)

	// 1. ICE + Accept before offer -> clean errors, no PC allocated, no panic.
	sess1, err := mgr.CreateIncomingSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateIncomingSession: %v", err)
	}
	sess1.UpdateState(session.StateIncoming)

	client := &WSClient{send: make(chan []byte, 8), sessionID: sess1.ID}
	srv.handleWSIce(client, WSMessage{
		Type:      "ice",
		SessionID: sess1.ID,
		Candidate: json.RawMessage(`{"candidate":"candidate:1 1 udp 2122252543 192.0.2.10 54321 typ host"}`),
	})
	iceMsgs := readWSMessages(t, client.send)
	if len(iceMsgs) != 1 || iceMsgs[0].Type != "error" {
		t.Fatalf("expected error on ICE before offer, got %+v", iceMsgs)
	}

	srv.handleWSAccept(client, WSMessage{Type: "accept", SessionID: sess1.ID})
	acceptMsgs := readWSMessages(t, client.send)
	if len(acceptMsgs) != 1 || acceptMsgs[0].Type != "error" {
		t.Fatalf("expected error on accept before offer, got %+v", acceptMsgs)
	}
	if sess1.HasPeerConnection() {
		t.Fatalf("expected no PeerConnection allocated during rejected ice/accept")
	}

	// 2. Reject before offer -> ends and deletes cleanly without allocating PC.
	srv.handleWSReject(client, WSMessage{Type: "reject", SessionID: sess1.ID, Reason: "busy"})
	rejectMsgs := readWSMessages(t, client.send)
	if len(rejectMsgs) != 1 || rejectMsgs[0].Type != "state" || rejectMsgs[0].State != string(session.StateEnded) {
		t.Fatalf("expected state=ended on reject, got %+v", rejectMsgs)
	}
	if _, ok := mgr.GetSession(sess1.ID); ok {
		t.Fatalf("expected rejected incoming session to be deleted")
	}

	// 3. Gateway-initiated cancel/reject before offer (e.g. offline/busy/timeout).
	sess2, err := mgr.CreateIncomingSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateIncomingSession: %v", err)
	}
	sess2.UpdateState(session.StateIncoming)
	srv.rejectIncomingSession(sess2.ID, "offline", "offline")
	if sess2.HasPeerConnection() {
		t.Fatalf("expected no PeerConnection allocated on offline reject")
	}
	if _, ok := mgr.GetSession(sess2.ID); ok {
		t.Fatalf("expected offline-rejected incoming session to be deleted")
	}

	// 4. Hangup before offer -> ends and deletes cleanly without panic.
	sess3, err := mgr.CreateIncomingSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateIncomingSession: %v", err)
	}
	sess3.UpdateState(session.StateIncoming)
	srv.handleWSHangup(client, WSMessage{Type: "hangup", SessionID: sess3.ID})
	hangupMsgs := readWSMessages(t, client.send)
	if len(hangupMsgs) != 1 || hangupMsgs[0].Type != "state" || hangupMsgs[0].State != string(session.StateEnded) {
		t.Fatalf("expected state=ended on hangup before offer, got %+v", hangupMsgs)
	}
	if _, ok := mgr.GetSession(sess3.ID); ok {
		t.Fatalf("expected hung-up incoming session to be deleted")
	}
}

func TestHandleWSTrunkResolve_SingleInstance_ByIDPublicIDAndCredentials_ZeroDBCalls(t *testing.T) {
	otherOwner := "gw-other"
	expired := time.Now().Add(-5 * time.Minute)
	publicID := "11111111-2222-3333-4444-555555555555"
	trunk := &sip.Trunk{
		ID:         42,
		PublicID:   publicID,
		Domain:     "pbx.example.com",
		Port:       5060,
		Username:   "1042",
		Password:   "secret42",
		Enabled:    true,
		LeaseOwner: &otherOwner, // Even stale/other lease owner is treated as locally owned in single-instance mode.
		LeaseUntil: &expired,
	}

	tm := &singleInstanceTestTrunkManager{
		stubResolveTrunkManager: stubResolveTrunkManager{
			byPublicID: map[string]int64{publicID: 42},
		},
		ramTrunks: map[int64]*sip.Trunk{42: trunk},
		dbTrunks:  map[int64]*sip.Trunk{42: trunk},
	}
	store := &countingSingleInstanceLogStore{
		trunkDetailsResolverStoreStub: trunkDetailsResolverStoreStub{
			stubResolveStore: stubResolveStore{
				lookupWSURL: "wss://gw-other.example/ws",
				lookupFound: true,
			},
		},
	}

	srv := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1"},
		config.TranslatorConfig{},
		nil,
		nil,
		nil,
		tm,
		store,
	)

	cases := []struct {
		name string
		msg  WSMessage
	}{
		{
			name: "by_trunkId",
			msg:  WSMessage{Type: "trunk_resolve", TrunkID: 42},
		},
		{
			name: "by_trunkPublicId",
			msg:  WSMessage{Type: "trunk_resolve", TrunkPublicID: publicID},
		},
		{
			name: "by_credentials",
			msg: WSMessage{
				Type:        "trunk_resolve",
				SIPDomain:   "pbx.example.com",
				SIPPort:     5060,
				SIPUsername: "1042",
				SIPPassword: "secret42",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &WSClient{send: make(chan []byte, 8)}
			srv.handleWSTrunkResolve(client, tc.msg)

			msgs := readWSMessages(t, client.send)
			if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" || msgs[0].TrunkID != 42 || msgs[0].TrunkPublicID != publicID {
				t.Fatalf("expected trunk_resolved for trunk 42, got %+v", msgs)
			}
			if !client.trunkResolved || client.resolvedTrunkID != 42 {
				t.Fatalf("expected client trunkResolved=true resolvedTrunkID=42, got resolved=%v id=%d", client.trunkResolved, client.resolvedTrunkID)
			}
		})
	}

	if tm.dbGetCalls != 0 {
		t.Fatalf("expected 0 GetTrunkByIDFromDB calls, got %d", tm.dbGetCalls)
	}
	if store.detailsCalls != 0 || store.legacyCalls != 0 {
		t.Fatalf("expected 0 DB credential resolve calls, got details=%d legacy=%d", store.detailsCalls, store.legacyCalls)
	}
	if store.lookupGatewayCalls != 0 {
		t.Fatalf("expected 0 LookupGatewayInstance calls in single-instance mode, got %d", store.lookupGatewayCalls)
	}
	if tm.ramGetCalls < 2 || tm.ramCredCalls != 1 {
		t.Fatalf("expected RAM lookups to be exercised, got ramGet=%d ramCred=%d", tm.ramGetCalls, tm.ramCredCalls)
	}
}

func TestHandleWSTrunkResolve_SingleInstance_DisabledAndRAMMissFallback(t *testing.T) {
	disabled := &sip.Trunk{ID: 50, PublicID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", Enabled: false}
	dbOnly := &sip.Trunk{ID: 51, PublicID: "12345678-1234-1234-1234-1234567890ab", Username: "1051", Enabled: true}

	tm := &singleInstanceTestTrunkManager{
		ramTrunks: map[int64]*sip.Trunk{50: disabled},
		dbTrunks:  map[int64]*sip.Trunk{50: disabled, 51: dbOnly},
	}
	otherOwner := "gw-old"
	until := time.Now().Add(time.Minute)
	store := &countingSingleInstanceLogStore{
		trunkDetailsResolverStoreStub: trunkDetailsResolverStoreStub{
			stubResolveStore: stubResolveStore{
				resolveTrunkID:    51,
				resolveLeaseOwner: &otherOwner,
				resolveLeaseUntil: &until,
				resolveFound:      true,
				lookupWSURL:       "wss://gw-old.example/ws",
				lookupFound:       true,
			},
			publicID:         dbOnly.PublicID,
			resolvedUsername: dbOnly.Username,
		},
	}

	srv := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1"},
		config.TranslatorConfig{},
		nil,
		nil,
		nil,
		tm,
		store,
	)

	// Disabled trunk in RAM -> trunk_not_ready, 0 DB calls, 0 redirects.
	c1 := &WSClient{send: make(chan []byte, 8)}
	srv.handleWSTrunkResolve(c1, WSMessage{Type: "trunk_resolve", TrunkID: 50})
	msgs1 := readWSMessages(t, c1.send)
	if len(msgs1) != 1 || msgs1[0].Type != "trunk_not_ready" {
		t.Fatalf("expected trunk_not_ready for disabled trunk, got %+v", msgs1)
	}
	if tm.dbGetCalls != 0 || store.lookupGatewayCalls != 0 {
		t.Fatalf("expected 0 DB/gateway calls for disabled RAM trunk, got dbGet=%d gwLookup=%d", tm.dbGetCalls, store.lookupGatewayCalls)
	}

	// Missing from RAM -> falls back to GetTrunkByIDFromDB, resolves locally without redirect.
	c2 := &WSClient{send: make(chan []byte, 8)}
	srv.handleWSTrunkResolve(c2, WSMessage{Type: "trunk_resolve", TrunkID: 51})
	msgs2 := readWSMessages(t, c2.send)
	if len(msgs2) != 1 || msgs2[0].Type != "trunk_resolved" || msgs2[0].TrunkID != 51 {
		t.Fatalf("expected trunk_resolved on DB fallback for trunk 51, got %+v", msgs2)
	}
	if tm.dbGetCalls != 1 || store.lookupGatewayCalls != 0 {
		t.Fatalf("expected dbGet=1 gwLookup=0, got dbGet=%d gwLookup=%d", tm.dbGetCalls, store.lookupGatewayCalls)
	}

	// Credentials miss in RAM -> falls back to ResolveTrunkDetailsByCredentials, still resolves locally without redirect.
	c3 := &WSClient{send: make(chan []byte, 8)}
	srv.handleWSTrunkResolve(c3, WSMessage{
		Type:        "trunk_resolve",
		SIPDomain:   "pbx.example.com",
		SIPPort:     5060,
		SIPUsername: "1051",
		SIPPassword: "secret",
	})
	msgs3 := readWSMessages(t, c3.send)
	if len(msgs3) != 1 || msgs3[0].Type != "trunk_resolved" || msgs3[0].TrunkID != 51 {
		t.Fatalf("expected trunk_resolved on credentials DB fallback, got %+v", msgs3)
	}
	if store.detailsCalls != 1 || store.lookupGatewayCalls != 0 {
		t.Fatalf("expected detailsCalls=1 gwLookup=0, got details=%d gwLookup=%d", store.detailsCalls, store.lookupGatewayCalls)
	}
}

func TestIncomingPushAndCallEnded_SingleInstance_ZeroDBCalls(t *testing.T) {
	tok1 := "fcm-token-1"
	tok2 := "fcm-token-2"
	tm := &singleInstanceTestTrunkManager{
		ramTrunks: map[int64]*sip.Trunk{
			11: {ID: 11, Name: "sipclient-agent-device-1001@sip:5060", FcmToken: &tok1, Enabled: true},
			12: {ID: 12, Name: "sipclient-agent-device-1001@sip:5060", FcmToken: &tok2, Enabled: true},
		},
		dbTrunks:       map[int64]*sip.Trunk{},
		identityGroup:  []int64{11, 12},
		ramBatchDoneCh: make(chan struct{}, 4),
	}

	srv := NewServer(
		config.APIConfig{IncomingRingTimeoutSeconds: 30},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1"},
		config.TranslatorConfig{},
		nil,
		nil,
		nil,
		tm,
		nil,
	)
	srv.SetPushService(push.NewService(nil, nil))

	trunks, ok := srv.findIncomingPushTargetTrunks(11)
	if !ok || len(trunks) != 2 {
		t.Fatalf("expected 2 push target trunks from RAM, got ok=%v len=%d", ok, len(trunks))
	}
	srv.dispatchIncomingPush("sess-si-push", "sip:caller@example.com", "sip:agent@example.com", 11, false, trunks...)
	srv.dispatchCallEndedPush("sess-si-push", push.CallEndedAnsweredElsewhere, &WSClient{resolvedTrunkID: 11})

	// Wait for the async dispatchCallEndedPush goroutine to hit GetTrunksByIDsFromRAM (2nd RAM batch call).
	for i := 0; i < 2; i++ {
		select {
		case <-tm.ramBatchDoneCh:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for RAM batch lookup #%d", i+1)
		}
	}

	tm.mu.Lock()
	dbBatch := tm.dbBatchCalls
	dbGet := tm.dbGetCalls
	ramBatch := tm.ramBatchCalls
	tm.mu.Unlock()

	if dbBatch != 0 || dbGet != 0 {
		t.Fatalf("expected 0 DB calls in single-instance push flow, got dbBatch=%d dbGet=%d", dbBatch, dbGet)
	}
	if ramBatch != 2 {
		t.Fatalf("expected 2 RAM batch calls (findIncomingPushTargetTrunks + dispatchCallEndedPush), got %d", ramBatch)
	}
}

func TestIncomingPush_SingleInstance_PartialRAMMissFallsBackOnlyForMissingIDs(t *testing.T) {
	tok1 := "fcm-token-ram"
	tok2 := "fcm-token-db"
	tm := &singleInstanceTestTrunkManager{
		ramTrunks: map[int64]*sip.Trunk{
			11: {ID: 11, Name: "sipclient-agent-device-1001@sip:5060", FcmToken: &tok1, Enabled: true},
		},
		dbTrunks: map[int64]*sip.Trunk{
			12: {ID: 12, Name: "sipclient-agent-device-1001@sip:5060", FcmToken: &tok2, Enabled: true},
		},
		identityGroup: []int64{11, 12},
	}

	srv := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1"},
		config.TranslatorConfig{},
		nil,
		nil,
		nil,
		tm,
		nil,
	)
	srv.SetPushService(push.NewService(nil, nil))

	trunks, ok := srv.findIncomingPushTargetTrunks(11)
	if !ok || len(trunks) != 2 {
		t.Fatalf("expected merged 2 trunks (1 RAM + 1 DB fallback), got ok=%v len=%d", ok, len(trunks))
	}
	if tm.dbBatchCalls != 1 {
		t.Fatalf("expected 1 DB batch fallback call, got %d", tm.dbBatchCalls)
	}
	if !slices.Equal(tm.dbBatchLastIDs, []int64{12}) {
		t.Fatalf("expected DB fallback to query only missing ID [12], got %v", tm.dbBatchLastIDs)
	}
}

func TestHandleWSResume_SingleInstance_SkipsSessionDirectoryLookup(t *testing.T) {
	store := &countingSingleInstanceLogStore{}
	srv := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1", PublicWSURL: "wss://gw-1.example/ws"},
		config.TranslatorConfig{},
		newTestSessionManager(),
		nil,
		nil,
		nil,
		store,
	)

	client := &WSClient{send: make(chan []byte, 8)}
	srv.handleWSResume(client, WSMessage{
		Type:      "resume",
		SessionID: "missing-session-single-instance",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "resume_failed" {
		t.Fatalf("expected resume_failed in single-instance mode, got %+v", msgs)
	}
	if store.lookupSessionDirCalls != 0 {
		t.Fatalf("expected 0 LookupSessionDirectory calls in single-instance mode, got %d", store.lookupSessionDirCalls)
	}
}

func TestUpsertSessionDirectory_SingleInstance_NoOp(t *testing.T) {
	asyncStore := &asyncTestLogStore{}
	srvAsync := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1", PublicWSURL: "wss://gw-1.example/ws"},
		config.TranslatorConfig{},
		nil,
		nil,
		nil,
		nil,
		asyncStore,
	)
	srvAsync.upsertSessionDirectory(context.Background(), "sess-si-async")
	if asyncStore.syncDirCalls != 0 || len(asyncStore.asyncDirs) != 0 {
		t.Fatalf("expected 0 session directory calls with AsyncLogStore in single-instance mode, got sync=%d async=%d",
			asyncStore.syncDirCalls, len(asyncStore.asyncDirs))
	}

	syncStore := &syncOnlyTestLogStore{}
	srvSync := NewServer(
		config.APIConfig{},
		config.TURNConfig{},
		config.GatewayConfig{SingleInstance: true, InstanceID: "gw-1", PublicWSURL: "wss://gw-1.example/ws"},
		config.TranslatorConfig{},
		nil,
		nil,
		nil,
		nil,
		syncStore,
	)
	srvSync.upsertSessionDirectory(context.Background(), "sess-si-sync")
	if len(syncStore.dirs) != 0 {
		t.Fatalf("expected 0 session directory calls with sync LogStore in single-instance mode, got %d", len(syncStore.dirs))
	}
}

// Compile-time check that *sip.TrunkManager satisfies all optional single-instance
// RAM fast-path interfaces used by internal/api.
var (
	_ ramBatchTrunkLoader         = (*sip.TrunkManager)(nil)
	_ ramCredentialsTrunkResolver = (*sip.TrunkManager)(nil)
	_ outboundTrunkValidatorClaimer = (*sip.TrunkManager)(nil)
	_ logstore.SingleInstanceConfigurable = (logstore.SingleInstanceConfigurable)(nil)
)
