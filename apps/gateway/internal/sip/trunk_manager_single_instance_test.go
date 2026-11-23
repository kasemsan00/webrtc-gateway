package sip

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"

	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCoWTrunkImmutability_MutationsNeverMutatePreviousSnapshot(t *testing.T) {
	t.Parallel()

	oldOwner := "gw-1"
	oldUntil := time.Now().Add(time.Minute)
	oldUser := "old-user"
	oldNotify := "notify-1"
	oldApp := "app-1"
	oldType := "apple"
	oldTok := "tok-1"
	oldFcm := "fcm-1"
	oldErr := "old-err"

	initial := &Trunk{
		ID:           10,
		PublicID:     "550e8400-e29b-41d4-a716-446655440010",
		Name:         "trunk-10",
		Domain:       "sip.example.com",
		Port:         5060,
		Username:     "1001",
		Password:     "pass-1",
		Transport:    "tcp",
		Enabled:      true,
		LeaseOwner:   &oldOwner,
		LeaseUntil:   &oldUntil,
		InUseBy:      &oldUser,
		NotifyUserID: &oldNotify,
		PNAppID:      &oldApp,
		PNType:       &oldType,
		PNToken:      &oldTok,
		FcmToken:     &oldFcm,
		LastError:    &oldErr,
	}

	tx := &perfTestTx{}
	txCall := 0
	tx.queryRowFn = func(_ context.Context, _ string, _ ...any) pgx.Row {
		txCall++
		if txCall == 1 {
			return mobileTrunkTestRow{trunk: *initial}
		}
		updated := *initial
		updated.Name = "trunk-10-renamed"
		updated.Password = "pass-2"
		return mobileTrunkTestRow{trunk: updated}
	}
	db := &perfTestDB{
		beginTxFn: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "SELECT EXISTS") {
				return scalarRow{vals: []any{true}}
			}
			return notifyTestRow{username: "1001", domain: "sip.example.com", port: 5060}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tm := &TrunkManager{
		db:         db,
		cfg:        &config.Config{SIPTrunk: config.SIPTrunkConfig{LeaseTTLSeconds: 60}},
		instanceID: "gw-1",
		ctx:        ctx,
		cancel:     cancel,
	}
	tm.replaceLoadedTrunks(map[int64]*Trunk{10: initial}, nil)

	snapRaw, ok := tm.GetTrunkByID(10)
	if !ok {
		t.Fatalf("expected trunk 10 in RAM")
	}
	snap := snapRaw.(*Trunk)

	newInUse := "new-user"
	if err := tm.SetTrunkInUseBy(context.Background(), 10, &newInUse); err != nil {
		t.Fatalf("SetTrunkInUseBy failed: %v", err)
	}
	if snap.InUseBy == nil || *snap.InUseBy != "old-user" {
		t.Fatalf("CoW violated on SetTrunkInUseBy: old snapshot mutated to %#v", snap.InUseBy)
	}

	newNotify := "notify-2"
	newPlatform := "ios"
	if err := tm.SetTrunkNotifyUserIDAndPlatform(context.Background(), 10, &newNotify, &newPlatform); err != nil {
		t.Fatalf("SetTrunkNotifyUserIDAndPlatform failed: %v", err)
	}
	if snap.NotifyUserID == nil || *snap.NotifyUserID != "notify-1" || snap.LastOnlinePlatform != nil {
		t.Fatalf("CoW violated on SetTrunkNotifyUserIDAndPlatform: snap=%+v", snap)
	}

	if _, err := tm.SetTrunkPushContact(context.Background(), 10, TrunkPushContact{
		PNAppID: "app-2", PNType: "firebase", PNToken: "tok-2",
	}); err != nil {
		t.Fatalf("SetTrunkPushContact failed: %v", err)
	}
	if snap.PNToken == nil || *snap.PNToken != "tok-1" {
		t.Fatalf("CoW violated on SetTrunkPushContact: old snapshot mutated to %#v", snap.PNToken)
	}

	if err := tm.SetTrunkFcmToken(context.Background(), 10, "fcm-2"); err != nil {
		t.Fatalf("SetTrunkFcmToken failed: %v", err)
	}
	if snap.FcmToken == nil || *snap.FcmToken != "fcm-1" {
		t.Fatalf("CoW violated on SetTrunkFcmToken: old snapshot mutated to %#v", snap.FcmToken)
	}

	if err := tm.ClearTrunkFcmToken(context.Background(), 10); err != nil {
		t.Fatalf("ClearTrunkFcmToken failed: %v", err)
	}
	if snap.FcmToken == nil || *snap.FcmToken != "fcm-1" {
		t.Fatalf("CoW violated on ClearTrunkFcmToken: old snapshot mutated to %#v", snap.FcmToken)
	}

	tm.updateRegistrationError(10, "503 Service Unavailable")
	if snap.LastError == nil || *snap.LastError != "old-err" {
		t.Fatalf("CoW violated on updateRegistrationError: old snapshot mutated to %#v", snap.LastError)
	}

	tm.updateRegistrationSuccessWithContact(10, TrunkPushContact{PNAppID: "app-2", PNType: "firebase", PNToken: "tok-2"}, 0)
	if snap.LastRegisteredAt != nil || snap.LastError == nil || *snap.LastError != "old-err" {
		t.Fatalf("CoW violated on updateRegistrationSuccessWithContact: snap=%+v", snap)
	}

	newName := "trunk-10-renamed"
	newPass := "pass-2"
	if _, err := tm.UpdateTrunk(context.Background(), 10, TrunkUpdatePatch{Name: &newName, Password: &newPass}); err != nil {
		t.Fatalf("UpdateTrunk failed: %v", err)
	}
	if snap.Name != "trunk-10" || snap.Password != "pass-1" {
		t.Fatalf("CoW violated on UpdateTrunk: snap=%+v", snap)
	}

	latestRaw, _ := tm.GetTrunkByID(10)
	latest := latestRaw.(*Trunk)
	if latest == snap {
		t.Fatalf("expected latest *Trunk pointer to differ from initial snapshot")
	}
	if latest.Name != "trunk-10-renamed" || latest.Password != "pass-2" {
		t.Fatalf("expected latest RAM trunk to have updated values, got %+v", latest)
	}
}

func TestCoWTrunkImmutability_ConcurrentReadersAndWriters(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var dbMu sync.Mutex
	db := &perfTestDB{
		execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	// Protect perfTestDB.calls under concurrent async writes by wrapping exec.
	safeDB := &concurrentSafeTestDB{inner: db, mu: &dbMu}

	tm := &TrunkManager{
		db:         safeDB,
		cfg:        &config.Config{SIPTrunk: config.SIPTrunkConfig{LeaseTTLSeconds: 60}},
		instanceID: "gw-race",
		publicIP:   "203.0.113.10",
		localPort:  5060,
		ctx:        ctx,
		cancel:     cancel,
	}
	tm.SetSingleInstance(true)
	tm.replaceLoadedTrunks(map[int64]*Trunk{
		1: {ID: 1, PublicID: "550e8400-e29b-41d4-a716-446655440001", Name: "sipclient-agent-1001@sip.example.com:5060", Domain: "sip.example.com", Port: 5060, Username: "1001", Password: "pw", Transport: "tcp", Enabled: true},
		2: {ID: 2, PublicID: "550e8400-e29b-41d4-a716-446655440002", Name: "sipclient-agent-device-1001@sip.example.com:5060", Domain: "sip.example.com", Port: 5060, Username: "1001", Password: "pw", Transport: "tcp", Enabled: true},
	}, nil)
	setRegistrarIdentity(tm, tm.trunks[1])
	setRegistrarIdentity(tm, tm.trunks[2])

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "1001", Host: "sip.example.com", Port: 5060})
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if raw, ok := tm.GetTrunkByID(1); ok {
					tr := raw.(*Trunk)
					_ = tr.Username
					if tr.InUseBy != nil {
						_ = *tr.InUseBy
					}
					if tr.FcmToken != nil {
						_ = *tr.FcmToken
					}
				}
				found, _ := tm.GetTrunksByIDsFromRAM([]int64{1, 2})
				for _, tr := range found {
					if tr.LastError != nil {
						_ = *tr.LastError
					}
				}
				if tr, ok := tm.ResolveTrunkByCredentialsFromRAM("sip.example.com", 5060, "1001", "pw"); ok {
					_ = tr.ID
				}
				_ = tm.ListOwnedTrunks()
				_ = tm.IdentityGroupTrunkIDs(1)
				_ = tm.MatchTrunkFromInviteDetailed(req)
				_, _ = tm.FindTrunkByInUseBy(context.Background(), "caller-1")
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 60; i++ {
			caller := fmt.Sprintf("caller-%d", i%3)
			_, _, _, _ = tm.ValidateAndClaimTrunkForOutboundCall(context.Background(), 1, "gw-race", &caller)
			_ = tm.SetTrunkFcmToken(context.Background(), 1, fmt.Sprintf("tok-%d", i))
			_ = tm.ClearTrunkFcmToken(context.Background(), 1)
			tm.updateRegistrationError(1, fmt.Sprintf("err-%d", i))
			tm.updateRegistrationSuccessWithContact(1, TrunkPushContact{}, 0)
		}
	}()

	wg.Wait()
	tm.wg.Wait()
}

type concurrentSafeTestDB struct {
	mu    *sync.Mutex
	inner *perfTestDB
}

func (db *concurrentSafeTestDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.inner.Exec(ctx, sql, args...)
}
func (db *concurrentSafeTestDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.inner.Query(ctx, sql, args...)
}
func (db *concurrentSafeTestDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.inner.QueryRow(ctx, sql, args...)
}
func (db *concurrentSafeTestDB) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.inner.BeginTx(ctx, txOptions)
}

func TestSecondaryIndexes_InsertUpdateDisableAndIdentityGroup(t *testing.T) {
	t.Parallel()

	tm := NewTrunkManager(nil, &config.Config{}, nil, "gw-1")

	t1 := &Trunk{ID: 1, PublicID: "550E8400-E29B-41D4-A716-446655440001", Name: "sipclient-agent-1001@sip.example.com:5060", Domain: "SIP.Example.COM", Port: 5060, Username: " 1001 ", Password: "p1", Enabled: true}
	t2 := &Trunk{ID: 2, PublicID: "550e8400-e29b-41d4-a716-446655440002", Name: "sipclient-agent-device-1001@sip.example.com:5060", Domain: "sip.example.com", Port: 5060, Username: "1001", Password: "p1", Enabled: true}
	t3 := &Trunk{ID: 3, PublicID: "550e8400-e29b-41d4-a716-446655440003", Name: "sipclient-mobile-sub", Domain: "sip.example.com", Port: 5060, Username: "1001", Password: "p1", Enabled: true}

	tm.cacheUpsertedTrunk(t3)
	tm.cacheUpsertedTrunk(t1)
	tm.cacheUpsertedTrunk(t2)

	tm.mu.RLock()
	gotUserIDs := append([]int64(nil), tm.trunksByUsername["1001"]...)
	gotIdentIDs := append([]int64(nil), tm.trunksByIdentity[makeTrunkIdentityKey("1001", "sip.example.com", 5060)]...)
	tm.ownedLeases[1] = true
	tm.ownedLeases[2] = true
	tm.ownedLeases[3] = true
	tm.mu.RUnlock()

	if !reflect.DeepEqual(gotUserIDs, []int64{1, 2, 3}) {
		t.Fatalf("expected sorted trunksByUsername [1 2 3], got %v", gotUserIDs)
	}
	if !reflect.DeepEqual(gotIdentIDs, []int64{1, 2, 3}) {
		t.Fatalf("expected sorted trunksByIdentity [1 2 3], got %v", gotIdentIDs)
	}
	if id, ok := tm.GetTrunkIDByPublicID("550e8400-e29b-41d4-a716-446655440001"); !ok || id != 1 {
		t.Fatalf("expected normalized public_id lookup -> 1, got (%d, %v)", id, ok)
	}

	// IdentityGroupTrunkIDs uses O(1) trunksByIdentity and excludes non-agent mobile trunk 3.
	if sibs := tm.IdentityGroupTrunkIDs(1); !reflect.DeepEqual(sibs, []int64{2}) {
		t.Fatalf("expected IdentityGroupTrunkIDs(1)=[2], got %v", sibs)
	}
	if sibs := tm.IdentityGroupTrunkIDs(2); !reflect.DeepEqual(sibs, []int64{1}) {
		t.Fatalf("expected IdentityGroupTrunkIDs(2)=[1], got %v", sibs)
	}

	// Rename username & domain of trunk 3 via cacheUpsertedTrunk: old index entry must be updated.
	t3Renamed := &Trunk{ID: 3, PublicID: "550e8400-e29b-41d4-a716-446655440003", Name: "sipclient-mobile-sub", Domain: "alt.example.com", Port: 5080, Username: "2002", Password: "p3", Enabled: true}
	tm.cacheUpsertedTrunk(t3Renamed)

	tm.mu.RLock()
	if !reflect.DeepEqual(tm.trunksByUsername["1001"], []int64{1, 2}) {
		t.Fatalf("expected trunksByUsername[1001]=[1 2], got %v", tm.trunksByUsername["1001"])
	}
	if !reflect.DeepEqual(tm.trunksByUsername["2002"], []int64{3}) {
		t.Fatalf("expected trunksByUsername[2002]=[3], got %v", tm.trunksByUsername["2002"])
	}
	if !reflect.DeepEqual(tm.trunksByIdentity[makeTrunkIdentityKey("2002", "alt.example.com", 5080)], []int64{3}) {
		t.Fatalf("expected new identity bucket for trunk 3")
	}
	tm.mu.RUnlock()

	// Disable trunk 3: must be removed from all secondary indexes.
	t3Disabled := *t3Renamed
	t3Disabled.Enabled = false
	tm.cacheUpsertedTrunk(&t3Disabled)

	tm.mu.RLock()
	if _, exists := tm.trunksByUsername["2002"]; exists {
		t.Fatalf("expected disabled trunk username bucket removed")
	}
	if _, exists := tm.trunksByIdentity[makeTrunkIdentityKey("2002", "alt.example.com", 5080)]; exists {
		t.Fatalf("expected disabled trunk identity bucket removed")
	}
	if _, exists := tm.trunkByPublic["550e8400-e29b-41d4-a716-446655440003"]; exists {
		t.Fatalf("expected disabled trunk public_id removed")
	}
	tm.mu.RUnlock()
}

func TestMatchTrunkFromInviteDetailed_UsesUsernameIndex(t *testing.T) {
	t.Parallel()

	tm := NewTrunkManager(nil, &config.Config{SIP: config.SIPConfig{PublicIP: "203.0.113.10", LocalPort: 5090}}, nil, "gw-1")
	loaded := make(map[int64]*Trunk, 300)
	for i := int64(1); i <= 300; i++ {
		user := fmt.Sprintf("u%04d", i)
		if i == 150 {
			user = "target-user"
		}
		loaded[i] = &Trunk{
			ID:        i,
			Domain:    "sip.example.com",
			Port:      5060,
			Username:  user,
			Transport: "tcp",
			Enabled:   true,
		}
	}
	tm.replaceLoadedTrunks(loaded, nil)
	for i := int64(1); i <= 300; i++ {
		tm.ownedLeases[i] = true
		setRegistrarIdentity(tm, tm.trunks[i])
	}

	// Normal INVITE matching Request-URI user via O(1) trunksByUsername lookup.
	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "target-user", Host: "sip.example.com", Port: 5060})
	res := tm.MatchTrunkFromInviteDetailed(req)
	if res.Trunk == nil || res.Trunk.ID != 150 || !res.Owned || res.Rule != "ruri_user_domain_port" {
		t.Fatalf("unexpected match result: %+v", res)
	}

	// Different Request-URI user (nonexistent) and To user ("target-user") must still match via To header!
	reqTo := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "unknown-user", Host: "203.0.113.10", Port: 5090})
	reqTo.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "target-user", Host: "sip.example.com", Port: 5060}})
	resTo := tm.MatchTrunkFromInviteDetailed(reqTo)
	if resTo.Trunk == nil || resTo.Trunk.ID != 150 || resTo.Rule != "to_user_domain_port" {
		t.Fatalf("unexpected To-header match result: %+v", resTo)
	}

	// Rare userless INVITE (empty Request-URI user and empty To user) must still support domain+port fallback.
	tmSingle := NewTrunkManager(nil, &config.Config{}, nil, "gw-1")
	tmSingle.replaceLoadedTrunks(map[int64]*Trunk{
		9: {ID: 9, Domain: "pbx.example.com", Port: 5060, Username: "acct9", Transport: "tcp", Enabled: true},
	}, nil)
	tmSingle.ownedLeases[9] = true
	setRegistrarIdentity(tmSingle, tmSingle.trunks[9])
	userlessReq := gosip.NewRequest(gosip.INVITE, gosip.Uri{Host: "pbx.example.com", Port: 5060})
	userlessRes := tmSingle.MatchTrunkFromInviteDetailed(userlessReq)
	if userlessRes.Trunk == nil || userlessRes.Trunk.ID != 9 || userlessRes.Rule != "ruri_domain_port_fallback" {
		t.Fatalf("unexpected userless INVITE fallback result: %+v", userlessRes)
	}
}

func TestGetTrunksByIDsFromRAM_OrderDedupAndMissing(t *testing.T) {
	t.Parallel()

	tm := NewTrunkManager(nil, &config.Config{}, nil, "gw-1")
	tm.replaceLoadedTrunks(map[int64]*Trunk{
		10: {ID: 10, Username: "u10", Enabled: true},
		20: {ID: 20, Username: "u20", Enabled: true},
	}, nil)

	found, missing := tm.GetTrunksByIDsFromRAM([]int64{20, 0, -5, 99, 10, 20, 99, 77})
	if len(found) != 2 || found[0].ID != 20 || found[1].ID != 10 {
		t.Fatalf("expected found in requested order [20 10], got %+v", found)
	}
	if !reflect.DeepEqual(missing, []int64{99, 77}) {
		t.Fatalf("expected missing [99 77], got %v", missing)
	}

	emptyFound, emptyMissing := tm.GetTrunksByIDsFromRAM([]int64{0, -1})
	if len(emptyFound) != 0 || len(emptyMissing) != 0 {
		t.Fatalf("expected empty result for non-positive IDs, got (%v, %v)", emptyFound, emptyMissing)
	}
}

func TestResolveTrunkByCredentialsFromRAM_ExactAndDefaultPort(t *testing.T) {
	t.Parallel()

	tm := NewTrunkManager(nil, &config.Config{}, nil, "gw-1")
	tm.replaceLoadedTrunks(map[int64]*Trunk{
		1: {ID: 1, Domain: "sip.example.com", Port: 5060, Username: "1001", Password: "secret", Enabled: true},
		2: {ID: 2, Domain: "srv.example.com", Port: 0, Username: "1002", Password: "secret", Enabled: true},
		3: {ID: 3, Domain: "sip.example.com", Port: 5080, Username: "1001", Password: "secret", Enabled: true},
		4: {ID: 4, Domain: "disabled.example.com", Port: 5060, Username: "1004", Password: "secret", Enabled: false},
	}, nil)

	// Exact port 5060 match.
	tr, ok := tm.ResolveTrunkByCredentialsFromRAM("SIP.Example.COM", 5060, "1001", "secret")
	if !ok || tr == nil || tr.ID != 1 {
		t.Fatalf("expected trunk 1, got (%+v, %v)", tr, ok)
	}
	// port==0 matches default normalized port 5060 trunk.
	tr, ok = tm.ResolveTrunkByCredentialsFromRAM("sip.example.com", 0, "1001", "secret")
	if !ok || tr == nil || tr.ID != 1 {
		t.Fatalf("expected port=0 to match default port 5060 trunk 1, got (%+v, %v)", tr, ok)
	}
	// port==0 matches Port==0 SRV trunk.
	tr, ok = tm.ResolveTrunkByCredentialsFromRAM("srv.example.com", 0, "1002", "secret")
	if !ok || tr == nil || tr.ID != 2 {
		t.Fatalf("expected trunk 2, got (%+v, %v)", tr, ok)
	}
	// Custom port 5080 matches only trunk 3.
	tr, ok = tm.ResolveTrunkByCredentialsFromRAM("sip.example.com", 5080, "1001", "secret")
	if !ok || tr == nil || tr.ID != 3 {
		t.Fatalf("expected trunk 3, got (%+v, %v)", tr, ok)
	}
	// Wrong password rejected.
	if tr, ok := tm.ResolveTrunkByCredentialsFromRAM("sip.example.com", 5060, "1001", "wrong"); ok || tr != nil {
		t.Fatalf("expected wrong password to fail, got (%+v, %v)", tr, ok)
	}
	// Disabled trunk rejected.
	if tr, ok := tm.ResolveTrunkByCredentialsFromRAM("disabled.example.com", 5060, "1004", "secret"); ok || tr != nil {
		t.Fatalf("expected disabled trunk to fail, got (%+v, %v)", tr, ok)
	}
}

func TestListOwnedTrunks_FiltersByOwnedLeases(t *testing.T) {
	t.Parallel()

	tm := NewTrunkManager(nil, &config.Config{}, nil, "gw-1")
	tm.replaceLoadedTrunks(map[int64]*Trunk{
		1: {ID: 1, Username: "u1", Enabled: true},
		2: {ID: 2, Username: "u2", Enabled: true},
		3: {ID: 3, Username: "u3", Enabled: true},
	}, nil)

	// Multi-instance mode with empty ownedLeases returns 0 trunks.
	if owned := tm.ListOwnedTrunks(); len(owned) != 0 {
		t.Fatalf("expected 0 owned trunks when ownedLeases is empty, got %d", len(owned))
	}

	tm.mu.Lock()
	tm.ownedLeases[2] = true
	tm.mu.Unlock()
	owned := tm.ListOwnedTrunks()
	if len(owned) != 1 || owned[0].ID != 2 {
		t.Fatalf("expected only trunk 2 owned, got %+v", owned)
	}

	// Enabling single-instance marks all loaded enabled trunks as owned.
	tm.SetSingleInstance(true)
	owned = tm.ListOwnedTrunks()
	if len(owned) != 3 || owned[0].ID != 1 || owned[1].ID != 2 || owned[2].ID != 3 {
		t.Fatalf("expected all 3 enabled trunks owned in single-instance mode, got %+v", owned)
	}
	for _, tr := range owned {
		if tr.LeaseOwner == nil || *tr.LeaseOwner != "gw-1" {
			t.Fatalf("expected RAM LeaseOwner=gw-1 on trunk %d, got %#v", tr.ID, tr.LeaseOwner)
		}
	}
}

func TestSingleInstance_StartSkipsHeartbeatAndAcquireLease_StopSkipsLeaseRelease(t *testing.T) {
	t.Parallel()

	ua, err := sipgo.NewUA()
	if err != nil {
		t.Fatalf("NewUA failed: %v", err)
	}
	defer ua.Close()

	db := &perfTestDB{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &fakeRows{
				rows: [][]any{
					{Trunk{ID: 1, PublicID: "550e8400-e29b-41d4-a716-446655440001", Name: "t1", Domain: "sip.example.com", Port: 5060, Username: "1001", Password: "pw", Transport: "tcp", Enabled: true, SipAutoRegister: false}},
					{Trunk{ID: 2, PublicID: "550e8400-e29b-41d4-a716-446655440002", Name: "t2", Domain: "sip.example.com", Port: 5060, Username: "1002", Password: "pw", Transport: "tcp", Enabled: true, SipAutoRegister: false}},
				},
			}, nil
		},
	}

	cfg := &config.Config{
		SIP: config.SIPConfig{PublicIP: "203.0.113.10", LocalPort: 5060},
		SIPTrunk: config.SIPTrunkConfig{
			Enable:             true,
			LeaseTTLSeconds:    60,
			LeaseRenewInterval: 1,
			RegisterTimeout:    1,
		},
		SIPPublic: config.SIPPublicConfig{RegisterExpiresSeconds: 300},
	}

	ctx, cancel := context.WithCancel(context.Background())
	tm := &TrunkManager{
		db:                     db,
		cfg:                    cfg,
		userAgent:              ua,
		instanceID:             "gw-single",
		publicIP:               cfg.SIP.PublicIP,
		localPort:              cfg.SIP.LocalPort,
		trunks:                 make(map[int64]*Trunk),
		trunkByPublic:          make(map[string]int64),
		trunksByUsername:       make(map[string][]int64),
		trunksByIdentity:       make(map[trunkIdentityKey][]int64),
		ownedLeases:            make(map[int64]bool),
		refreshWorkers:         make(map[int64]chan struct{}),
		leaseRetryRuns:         make(map[int64]int),
		registrationAnnounced:  make(map[int64]bool),
		needsReRegister:        make(map[int64]bool),
		trunkConfigGen:         make(map[int64]uint64),
		registeredPushContacts: make(map[int64]TrunkPushContact),
		registrarIdentities:    make(map[int64]*registrarIdentity),
		registrarGenerations:   make(map[int64]uint64),
		resolver:               defaultRegistrarResolver{},
		ctx:                    ctx,
		cancel:                 cancel,
	}
	tm.SetSingleInstance(true)
	if !tm.IsSingleInstance() {
		t.Fatalf("expected IsSingleInstance() == true")
	}

	if err := tm.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Only the single loadTrunks SELECT query should have executed — 0 acquireLease UPDATEs.
	if len(db.calls) != 1 || db.calls[0].kind != "query" {
		t.Fatalf("expected exactly 1 loadTrunks query in single-instance Start(), got %+v", db.calls)
	}
	if !tm.IsTrunkOwned(1) || !tm.IsTrunkOwned(2) {
		t.Fatalf("expected loaded enabled trunks 1 and 2 marked locally owned")
	}
	if tm.trunks[1].LeaseOwner == nil || *tm.trunks[1].LeaseOwner != "gw-single" {
		t.Fatalf("expected trunk 1 RAM LeaseOwner=gw-single, got %#v", tm.trunks[1].LeaseOwner)
	}

	// Wait briefly past what would be a heartbeat tick check — no heartbeat worker should run.
	tm.Stop()

	// Stop() in single-instance mode must NOT run DB lease_owner = NULL update.
	if len(db.calls) != 1 {
		t.Fatalf("expected 0 extra DB calls on single-instance Stop(), got %+v", db.calls)
	}
	if len(tm.ownedLeases) != 0 {
		t.Fatalf("expected local ownedLeases cleared after Stop()")
	}
}

func TestSingleInstance_ValidateAndClaimOutboundCall_ZeroSyncDBAndAsyncPersist(t *testing.T) {
	t.Parallel()

	execDone := make(chan struct {
		sql        string
		args       []any
		hasTimeout bool
	}, 1)

	db := &perfTestDB{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			_, hasDeadline := ctx.Deadline()
			execDone <- struct {
				sql        string
				args       []any
				hasTimeout bool
			}{sql: sql, args: append([]any(nil), args...), hasTimeout: hasDeadline}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}

	tm := &TrunkManager{
		db:         db,
		instanceID: "gw-single",
	}
	tm.SetSingleInstance(true)
	tm.replaceLoadedTrunks(map[int64]*Trunk{
		5: {ID: 5, PublicID: "pub-5", Username: "1001", Domain: "sip.example.com", Port: 5060, Enabled: true},
	}, nil)

	beforeSnapRaw, _ := tm.GetTrunkByID(5)
	beforeSnap := beforeSnapRaw.(*Trunk)

	var usageEvents []int64
	tm.SetTrunkListChangeCallback(func(eventType string, trunkIDs []int64) {
		if eventType == "usage_updated" {
			usageEvents = append(usageEvents, trunkIDs...)
		}
	})

	// 1. Nil claimUsername reads from RAM with 0 DB calls.
	tr, notFound, reason, err := tm.ValidateAndClaimTrunkForOutboundCall(context.Background(), 5, "gw-single", nil)
	if err != nil || notFound || reason != "" || tr == nil || tr.ID != 5 {
		t.Fatalf("unexpected nil-claim result: tr=%+v notFound=%v reason=%q err=%v", tr, notFound, reason, err)
	}
	select {
	case unexpected := <-execDone:
		t.Fatalf("expected 0 DB calls for nil claimUsername, got %+v", unexpected)
	default:
	}

	// 2. Non-nil claimUsername updates RAM via CoW immediately and persists async.
	caller := "alice"
	claimed, notFound, reason, err := tm.ValidateAndClaimTrunkForOutboundCall(context.Background(), 5, "gw-single", &caller)
	if err != nil || notFound || reason != "" || claimed == nil || claimed.InUseBy == nil || *claimed.InUseBy != "alice" {
		t.Fatalf("unexpected claim result: tr=%+v notFound=%v reason=%q err=%v", claimed, notFound, reason, err)
	}
	if beforeSnap.InUseBy != nil {
		t.Fatalf("CoW violated: previous snapshot InUseBy mutated to %#v", beforeSnap.InUseBy)
	}
	if !reflect.DeepEqual(usageEvents, []int64{5}) {
		t.Fatalf("expected synchronous usage_updated event for [5], got %v", usageEvents)
	}

	select {
	case asyncCall := <-execDone:
		if !strings.Contains(asyncCall.sql, "UPDATE sip_trunks SET in_use_by = $1") || !strings.Contains(asyncCall.sql, "WHERE id = $2") {
			t.Fatalf("unexpected async claim SQL: %s", asyncCall.sql)
		}
		if !asyncCall.hasTimeout {
			t.Fatalf("expected bounded timeout on async claim DB write")
		}
		if len(asyncCall.args) != 2 || asyncCall.args[1] != int64(5) {
			t.Fatalf("unexpected async claim args: %#v", asyncCall.args)
		}
		if gotUser, ok := asyncCall.args[0].(*string); !ok || gotUser == nil || *gotUser != "alice" {
			t.Fatalf("expected arg0=*alice, got %#v", asyncCall.args[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for async in_use_by DB write")
	}

	tm.wg.Wait()
}

func TestSingleInstance_ValidateAndClaimOutboundCall_BoundedSemaphoreAndMissFallback(t *testing.T) {
	t.Parallel()

	// Part A: When semaphore is saturated, outbound claim still succeeds immediately without blocking.
	blockCh := make(chan struct{})
	var execCount atomic.Int32
	db := &perfTestDB{
		execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			execCount.Add(1)
			<-blockCh
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		queryRowFn: func(context.Context, string, ...any) pgx.Row {
			return errRow{err: pgx.ErrNoRows}
		},
	}
	tm := &TrunkManager{
		db:         db,
		instanceID: "gw-single",
		asyncDBSem: make(chan struct{}, 1), // capacity 1 to deterministically saturate
	}
	tm.SetSingleInstance(true)
	tm.replaceLoadedTrunks(map[int64]*Trunk{
		5: {ID: 5, Enabled: true},
	}, nil)

	u1 := "u1"
	u2 := "u2"
	if _, _, _, err := tm.ValidateAndClaimTrunkForOutboundCall(context.Background(), 5, "gw-single", &u1); err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	// Second claim hits full semaphore: must NOT block and must still update RAM.
	got2, _, _, err := tm.ValidateAndClaimTrunkForOutboundCall(context.Background(), 5, "gw-single", &u2)
	if err != nil || got2 == nil || got2.InUseBy == nil || *got2.InUseBy != "u2" {
		t.Fatalf("second claim should succeed in RAM even when async sem full: got=%+v err=%v", got2, err)
	}
	close(blockCh)
	tm.wg.Wait()
	if execCount.Load() != 1 {
		t.Fatalf("expected exactly 1 async DB write while semaphore full, got %d", execCount.Load())
	}

	// Part B: RAM miss falls back to existing DB path (returns notFound=true).
	_, notFound, reason, err := tm.ValidateAndClaimTrunkForOutboundCall(context.Background(), 999, "gw-single", &u1)
	if err != nil || !notFound || reason != "Trunk 999 not found" {
		t.Fatalf("expected DB fallback notFound for trunk 999, got notFound=%v reason=%q err=%v", notFound, reason, err)
	}
}

func TestSingleInstance_FindTrunkByInUseBy_RAMFastPathAndDBFallback(t *testing.T) {
	t.Parallel()

	user := "alice"
	dbFallbackCalled := 0
	db := &perfTestDB{
		queryRowFn: func(context.Context, string, ...any) pgx.Row {
			dbFallbackCalled++
			return mobileTrunkTestRow{trunk: Trunk{ID: 77, InUseBy: &user}}
		},
	}

	tm := &TrunkManager{db: db, instanceID: "gw-single"}
	tm.SetSingleInstance(true)
	tm.replaceLoadedTrunks(map[int64]*Trunk{
		10: {ID: 10, Username: "1001", Enabled: true, InUseBy: &user},
	}, nil)

	// RAM hit: 0 DB queries!
	found, err := tm.FindTrunkByInUseBy(context.Background(), "alice")
	if err != nil || found == nil || found.ID != 10 {
		t.Fatalf("expected RAM hit trunk 10, got (%+v, %v)", found, err)
	}
	if dbFallbackCalled != 0 {
		t.Fatalf("expected 0 DB queries on single-instance RAM hit, got %d", dbFallbackCalled)
	}

	// RAM miss: falls back to DB.
	found2, err := tm.FindTrunkByInUseBy(context.Background(), "bob")
	if err != nil || found2 == nil || found2.ID != 77 {
		t.Fatalf("expected DB fallback trunk 77, got (%+v, %v)", found2, err)
	}
	if dbFallbackCalled != 1 {
		t.Fatalf("expected 1 DB fallback query on RAM miss, got %d", dbFallbackCalled)
	}

	// Multi-instance mode always queries DB.
	tm.SetSingleInstance(false)
	found3, err := tm.FindTrunkByInUseBy(context.Background(), "alice")
	if err != nil || found3 == nil || found3.ID != 77 {
		t.Fatalf("expected DB query in multi-instance mode, got (%+v, %v)", found3, err)
	}
	if dbFallbackCalled != 2 {
		t.Fatalf("expected 2 total DB queries after multi-instance lookup, got %d", dbFallbackCalled)
	}
}

func TestSingleInstance_EnsureTrunkRegistered_SkipsLeaseExpiryCheck(t *testing.T) {
	t.Parallel()

	tm, db := newHealthyRegisteredTrunkFixture()
	tm.SetSingleInstance(true)

	// Clear LeaseUntil and set an expired/nil lease: in single-instance mode (where
	// leaseHeartbeatWorker is not running), EnsureTrunkRegistered must STILL skip REGISTER.
	past := time.Now().Add(-10 * time.Minute)
	tm.mu.Lock()
	tm.updateTrunkCoWLocked(42, func(tr *Trunk) {
		tr.LeaseUntil = &past
	})
	tm.mu.Unlock()

	if err := tm.EnsureTrunkRegistered(42, true); err != nil {
		t.Fatalf("expected single-instance EnsureTrunkRegistered fast-path skip, got %v", err)
	}
	if len(db.calls) != 0 {
		t.Fatalf("expected 0 DB calls on single-instance EnsureTrunkRegistered skip, got %d", len(db.calls))
	}

	// Also works with LeaseUntil == nil.
	tm.mu.Lock()
	tm.updateTrunkCoWLocked(42, func(tr *Trunk) {
		tr.LeaseUntil = nil
	})
	tm.mu.Unlock()
	if err := tm.EnsureTrunkRegistered(42, false); err != nil {
		t.Fatalf("expected single-instance EnsureTrunkRegistered fast-path skip with nil LeaseUntil, got %v", err)
	}
	if len(db.calls) != 0 {
		t.Fatalf("expected 0 DB calls with nil LeaseUntil, got %d", len(db.calls))
	}

	// Still delegates to RegisterTrunk when LastError is set or needsReRegister is flagged.
	sentinel := errors.New("delegated-to-register")
	db.queryRowFn = func(context.Context, string, ...any) pgx.Row {
		return errRow{err: sentinel}
	}
	tm.mu.Lock()
	tm.needsReRegister = map[int64]bool{42: true}
	tm.mu.Unlock()
	if err := tm.EnsureTrunkRegistered(42, true); !errors.Is(err, sentinel) {
		t.Fatalf("expected delegation when needsReRegister=true, got %v", err)
	}
}
