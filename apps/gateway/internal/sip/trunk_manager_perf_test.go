package sip

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type perfTestCall struct {
	kind       string
	sql        string
	args       []any
	hasTimeout bool
}

type perfTestDB struct {
	calls      []perfTestCall
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	beginTxFn  func(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func (db *perfTestDB) record(ctx context.Context, kind, sql string, args ...any) {
	_, hasDeadline := ctx.Deadline()
	db.calls = append(db.calls, perfTestCall{
		kind:       kind,
		sql:        sql,
		args:       append([]any(nil), args...),
		hasTimeout: hasDeadline,
	})
}

func (db *perfTestDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.record(ctx, "exec", sql, args...)
	if db.execFn != nil {
		return db.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (db *perfTestDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	db.record(ctx, "query", sql, args...)
	if db.queryFn != nil {
		return db.queryFn(ctx, sql, args...)
	}
	return nil, errors.New("unexpected Query")
}

func (db *perfTestDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	db.record(ctx, "queryRow", sql, args...)
	if db.queryRowFn != nil {
		return db.queryRowFn(ctx, sql, args...)
	}
	return errRow{err: errors.New("unexpected QueryRow")}
}

func (db *perfTestDB) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	db.record(ctx, "beginTx", "", nil)
	if db.beginTxFn != nil {
		return db.beginTxFn(ctx, txOptions)
	}
	return nil, errors.New("unexpected BeginTx")
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

type scalarRow struct {
	vals []any
	err  error
}

func (r scalarRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(r.vals[i]))
	}
	return nil
}

type fakeRows struct {
	pgx.Rows
	rows   [][]any
	idx    int
	closed bool
	rowErr error
}

func (f *fakeRows) Close()                                       { f.closed = true }
func (f *fakeRows) Err() error                                   { return f.rowErr }
func (f *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT") }
func (f *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeRows) RawValues() [][]byte                          { return nil }
func (f *fakeRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeRows) Scan(dest ...any) error {
	row := f.rows[f.idx-1]
	if len(row) == 1 {
		if trunk, ok := row[0].(Trunk); ok {
			return mobileTrunkTestRow{trunk: trunk}.Scan(dest...)
		}
	}
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(row[i]))
	}
	return nil
}

type perfTestTx struct {
	calls      []perfTestCall
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	committed  bool
	rolledBack bool
}

func (tx *perfTestTx) Begin(context.Context) (pgx.Tx, error) { panic("unexpected nested tx") }
func (tx *perfTestTx) Commit(context.Context) error          { tx.committed = true; return nil }
func (tx *perfTestTx) Rollback(context.Context) error        { tx.rolledBack = true; return nil }
func (tx *perfTestTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	panic("unexpected CopyFrom")
}
func (tx *perfTestTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	panic("unexpected SendBatch")
}
func (tx *perfTestTx) LargeObjects() pgx.LargeObjects { panic("unexpected LargeObjects") }
func (tx *perfTestTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	panic("unexpected Prepare")
}
func (tx *perfTestTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}
func (tx *perfTestTx) Conn() *pgx.Conn { return nil }
func (tx *perfTestTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.calls = append(tx.calls, perfTestCall{kind: "exec", sql: sql, args: append([]any(nil), args...)})
	if tx.execFn != nil {
		return tx.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (tx *perfTestTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.calls = append(tx.calls, perfTestCall{kind: "queryRow", sql: sql, args: append([]any(nil), args...)})
	if tx.queryRowFn != nil {
		return tx.queryRowFn(ctx, sql, args...)
	}
	return errRow{err: errors.New("unexpected tx QueryRow")}
}

// --- Task 1: Sargable UUID query in GetTrunkIDByPublicID ---

func TestGetTrunkIDByPublicID_UsesSargableUUIDPredicate(t *testing.T) {
	t.Parallel()

	rawUUID := "550E8400-E29B-41D4-A716-446655440000"
	wantNorm := "550e8400-e29b-41d4-a716-446655440000"

	db := &perfTestDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return scalarRow{vals: []any{int64(77)}}
		},
	}
	tm := &TrunkManager{
		db:            db,
		trunkByPublic: make(map[string]int64),
	}

	id, ok := tm.GetTrunkIDByPublicID(rawUUID)
	if !ok || id != 77 {
		t.Fatalf("expected (77, true), got (%d, %v)", id, ok)
	}
	if len(db.calls) != 1 {
		t.Fatalf("expected 1 DB call on cache miss, got %d", len(db.calls))
	}
	sql := db.calls[0].sql
	if !strings.Contains(sql, "WHERE public_id = $1::uuid") {
		t.Fatalf("expected sargable UUID predicate, got SQL: %s", sql)
	}
	if strings.Contains(strings.ToLower(sql), "lower(public_id") {
		t.Fatalf("query must not wrap public_id in lower(): %s", sql)
	}
	if db.calls[0].args[0] != wantNorm {
		t.Fatalf("expected normalized UUID arg %q, got %#v", wantNorm, db.calls[0].args[0])
	}
	if !db.calls[0].hasTimeout {
		t.Fatalf("expected bounded DB timeout context")
	}

	// Second call must hit in-memory cache with zero DB calls.
	id2, ok2 := tm.GetTrunkIDByPublicID(wantNorm)
	if !ok2 || id2 != 77 {
		t.Fatalf("expected cached (77, true), got (%d, %v)", id2, ok2)
	}
	if len(db.calls) != 1 {
		t.Fatalf("expected cache hit with no extra DB call, got %d calls", len(db.calls))
	}

	// Invalid UUID must not hit DB.
	if id3, ok3 := tm.GetTrunkIDByPublicID("not-a-uuid"); ok3 || id3 != 0 {
		t.Fatalf("expected (0, false) for invalid UUID, got (%d, %v)", id3, ok3)
	}
	if len(db.calls) != 1 {
		t.Fatalf("invalid UUID should not query DB, got %d calls", len(db.calls))
	}
}

// --- Task 2: Batched leaseHeartbeatWorker & Stop() ---

func TestRenewOwnedLeasesBatch_SingleQueryPartialLossAndRenewal(t *testing.T) {
	t.Parallel()

	stop1 := make(chan struct{})
	stop2 := make(chan struct{})
	db := &perfTestDB{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			// Only trunk 1 was renewed; trunk 2 lost its lease.
			return &fakeRows{rows: [][]any{{int64(1)}}}, nil
		},
	}
	var events []struct {
		event string
		ids   []int64
	}
	tm := &TrunkManager{
		db:         db,
		cfg:        &config.Config{SIPTrunk: config.SIPTrunkConfig{LeaseTTLSeconds: 60}},
		instanceID: "gw-1",
		trunks: map[int64]*Trunk{
			1: {ID: 1},
			2: {ID: 2},
		},
		ownedLeases:          map[int64]bool{1: true, 2: true},
		refreshWorkers:       map[int64]chan struct{}{1: stop1, 2: stop2},
		leaseRetryRuns:       map[int64]int{1: 3, 2: 1},
		registrarIdentities:  map[int64]*registrarIdentity{1: {TrunkID: 1}, 2: {TrunkID: 2}},
		registrarGenerations: map[int64]uint64{1: 1, 2: 1},
	}
	tm.SetTrunkListChangeCallback(func(eventType string, trunkIDs []int64) {
		events = append(events, struct {
			event string
			ids   []int64
		}{eventType, trunkIDs})
	})

	tm.renewOwnedLeasesBatch([]int64{1, 2})

	if len(db.calls) != 1 {
		t.Fatalf("expected 1 batched query, got %d", len(db.calls))
	}
	sql := db.calls[0].sql
	if !strings.Contains(sql, "WHERE id = ANY($2)") || !strings.Contains(sql, "RETURNING id") {
		t.Fatalf("expected batched ANY($2) ... RETURNING id query, got: %s", sql)
	}
	if !tm.ownedLeases[1] {
		t.Fatalf("expected trunk 1 to remain owned")
	}
	if _, ok := tm.leaseRetryRuns[1]; ok {
		t.Fatalf("expected trunk 1 retry counter to be cleared on renewal")
	}
	if tm.trunks[1].LeaseOwner == nil || *tm.trunks[1].LeaseOwner != "gw-1" || tm.trunks[1].LeaseUntil == nil {
		t.Fatalf("expected trunk 1 lease metadata updated in RAM")
	}
	if tm.ownedLeases[2] {
		t.Fatalf("expected trunk 2 ownership to be dropped")
	}
	if _, ok := tm.refreshWorkers[2]; ok {
		t.Fatalf("expected trunk 2 refresh worker to be removed")
	}
	select {
	case <-stop2:
	default:
		t.Fatalf("expected trunk 2 stop channel to be closed")
	}
	if _, ok := tm.registrarIdentities[2]; ok {
		t.Fatalf("expected trunk 2 registrar identity to be invalidated")
	}
	if len(events) != 1 || events[0].event != "lease_updated" || !reflect.DeepEqual(events[0].ids, []int64{2}) {
		t.Fatalf("expected single lease_updated event for [2], got %+v", events)
	}
}

func TestRenewOwnedLeasesBatch_TransientDBErrorKeepsLeases(t *testing.T) {
	t.Parallel()

	stop1 := make(chan struct{})
	db := &perfTestDB{
		queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return nil, errors.New("connection reset")
		},
	}
	tm := &TrunkManager{
		db:             db,
		cfg:            &config.Config{SIPTrunk: config.SIPTrunkConfig{LeaseTTLSeconds: 60}},
		instanceID:     "gw-1",
		ownedLeases:    map[int64]bool{1: true, 2: true},
		refreshWorkers: map[int64]chan struct{}{1: stop1},
		leaseRetryRuns: map[int64]int{},
	}

	tm.renewOwnedLeasesBatch([]int64{1, 2})

	if !tm.ownedLeases[1] || !tm.ownedLeases[2] {
		t.Fatalf("transient error must not drop owned leases")
	}
	if tm.leaseRetryRuns[1] != 1 || tm.leaseRetryRuns[2] != 1 {
		t.Fatalf("expected retry count 1 for both trunks, got %+v", tm.leaseRetryRuns)
	}
	if _, ok := tm.refreshWorkers[1]; !ok {
		t.Fatalf("transient error must not stop refresh worker")
	}
}

func TestStop_BatchesLeaseReleaseInSingleQuery(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	db := &perfTestDB{}
	owner := "gw-stop"
	until := time.Now().Add(time.Minute)
	tm := &TrunkManager{
		db:         db,
		cfg:        &config.Config{SIPTrunk: config.SIPTrunkConfig{Enable: true}},
		instanceID: owner,
		trunks: map[int64]*Trunk{
			10: {ID: 10, LeaseOwner: &owner, LeaseUntil: &until},
			20: {ID: 20, LeaseOwner: &owner, LeaseUntil: &until},
		},
		ownedLeases: map[int64]bool{10: true, 20: true},
		ctx:         ctx,
		cancel:      cancel,
	}

	tm.Stop()

	if len(db.calls) != 1 {
		t.Fatalf("expected 1 batched lease release query on Stop, got %d", len(db.calls))
	}
	call := db.calls[0]
	if !strings.Contains(call.sql, "SET lease_owner = NULL, lease_until = NULL") || !strings.Contains(call.sql, "WHERE lease_owner = $1") {
		t.Fatalf("unexpected Stop lease release SQL: %s", call.sql)
	}
	if len(call.args) != 1 || call.args[0] != owner {
		t.Fatalf("expected instanceID arg %q, got %#v", owner, call.args)
	}
	if len(tm.ownedLeases) != 0 {
		t.Fatalf("expected ownedLeases cleared after Stop")
	}
	if tm.trunks[10].LeaseOwner != nil || tm.trunks[20].LeaseOwner != nil {
		t.Fatalf("expected cached LeaseOwner cleared after Stop")
	}
}

// --- Task 3: GetTrunksByIDsFromDB ---

func TestGetTrunksByIDsFromDB_SingleBatchQueryPreservesOrder(t *testing.T) {
	t.Parallel()

	db := &perfTestDB{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &fakeRows{
				rows: [][]any{
					{Trunk{ID: 20, PublicID: "pub-20", Username: "u20"}},
					{Trunk{ID: 10, PublicID: "pub-10", Username: "u10"}},
				},
			}, nil
		},
	}
	tm := &TrunkManager{db: db}

	// Empty / non-positive input skips DB entirely.
	empty, err := tm.GetTrunksByIDsFromDB(context.Background(), []int64{0, -1})
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty slice with nil error, got (%v, %v)", empty, err)
	}
	if len(db.calls) != 0 {
		t.Fatalf("expected 0 DB calls for empty IDs, got %d", len(db.calls))
	}

	got, err := tm.GetTrunksByIDsFromDB(context.Background(), []int64{10, 99, 20, 10})
	if err != nil {
		t.Fatalf("GetTrunksByIDsFromDB failed: %v", err)
	}
	if len(db.calls) != 1 {
		t.Fatalf("expected 1 DB query, got %d", len(db.calls))
	}
	if !strings.Contains(db.calls[0].sql, "WHERE id = ANY($1)") {
		t.Fatalf("expected WHERE id = ANY($1), got: %s", db.calls[0].sql)
	}
	if !db.calls[0].hasTimeout {
		t.Fatalf("expected bounded DB timeout when ctx has no deadline")
	}
	if !reflect.DeepEqual(db.calls[0].args[0], []int64{10, 99, 20}) {
		t.Fatalf("expected deduplicated IDs [10 99 20], got %#v", db.calls[0].args[0])
	}
	if len(got) != 2 || got[0].ID != 10 || got[1].ID != 20 {
		t.Fatalf("expected trunks ordered [10, 20], got %+v", got)
	}
}

// --- Task 4: ValidateAndClaimTrunkForOutboundCall ---

func TestValidateAndClaimTrunkForOutboundCall_AtomicClaimHappyPath(t *testing.T) {
	t.Parallel()

	owner := "gw-1"
	until := time.Now().Add(time.Minute)
	user := "alice"
	db := &perfTestDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return mobileTrunkTestRow{trunk: Trunk{
				ID:         5,
				PublicID:   "pub-5",
				Enabled:    true,
				LeaseOwner: &owner,
				LeaseUntil: &until,
				InUseBy:    &user,
			}}
		},
	}
	var usageEvents []int64
	tm := &TrunkManager{
		db:         db,
		instanceID: owner,
		trunks:     map[int64]*Trunk{5: {ID: 5, PublicID: "pub-5", Enabled: true}},
	}
	tm.SetTrunkListChangeCallback(func(eventType string, trunkIDs []int64) {
		if eventType == "usage_updated" {
			usageEvents = append(usageEvents, trunkIDs...)
		}
	})

	trunk, notFound, reason, err := tm.ValidateAndClaimTrunkForOutboundCall(context.Background(), 5, owner, &user)
	if err != nil || notFound || reason != "" || trunk == nil || trunk.PublicID != "pub-5" {
		t.Fatalf("unexpected result: trunk=%+v notFound=%v reason=%q err=%v", trunk, notFound, reason, err)
	}
	if len(db.calls) != 1 {
		t.Fatalf("expected 1 atomic UPDATE ... RETURNING call, got %d", len(db.calls))
	}
	sql := db.calls[0].sql
	if !strings.Contains(sql, "UPDATE sip_trunks") || !strings.Contains(sql, "SET in_use_by = $1") || !strings.Contains(sql, "lease_owner = $3") || !strings.Contains(sql, "lease_until > NOW()") || !strings.Contains(sql, "RETURNING") {
		t.Fatalf("unexpected atomic claim SQL: %s", sql)
	}
	if tm.trunks[5].InUseBy == nil || *tm.trunks[5].InUseBy != "alice" {
		t.Fatalf("expected RAM InUseBy=alice, got %#v", tm.trunks[5].InUseBy)
	}
	if !reflect.DeepEqual(usageEvents, []int64{5}) {
		t.Fatalf("expected usage_updated callback for [5], got %v", usageEvents)
	}
}

func TestValidateAndClaimTrunkForOutboundCall_FallbackDiagnostics(t *testing.T) {
	t.Parallel()

	user := "alice"
	otherOwner := "gw-2"
	ownInstance := "gw-1"
	future := time.Now().Add(time.Minute)
	past := time.Now().Add(-time.Minute)

	cases := []struct {
		name         string
		fallbackRow  pgx.Row
		wantNotFound bool
		wantReason   string
	}{
		{
			name:         "not found",
			fallbackRow:  errRow{err: pgx.ErrNoRows},
			wantNotFound: true,
			wantReason:   "Trunk 5 not found",
		},
		{
			name:         "expired lease",
			fallbackRow:  mobileTrunkTestRow{trunk: Trunk{ID: 5, Enabled: true, LeaseOwner: &ownInstance, LeaseUntil: &past}},
			wantNotFound: false,
			wantReason:   "Trunk lease not active",
		},
		{
			name:         "owned by another instance",
			fallbackRow:  mobileTrunkTestRow{trunk: Trunk{ID: 5, Enabled: true, LeaseOwner: &otherOwner, LeaseUntil: &future}},
			wantNotFound: false,
			wantReason:   "Trunk is owned by another gateway instance (gw-2)",
		},
		{
			name:         "disabled trunk",
			fallbackRow:  mobileTrunkTestRow{trunk: Trunk{ID: 5, Enabled: false, LeaseOwner: &ownInstance, LeaseUntil: &future}},
			wantNotFound: false,
			wantReason:   "Trunk 5 is disabled",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			callCount := 0
			db := &perfTestDB{
				queryRowFn: func(context.Context, string, ...any) pgx.Row {
					callCount++
					if callCount == 1 {
						return errRow{err: pgx.ErrNoRows}
					}
					return tc.fallbackRow
				},
			}
			tm := &TrunkManager{db: db, instanceID: ownInstance}
			trunk, notFound, reason, err := tm.ValidateAndClaimTrunkForOutboundCall(context.Background(), 5, ownInstance, &user)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if trunk != nil {
				t.Fatalf("expected nil trunk, got %+v", trunk)
			}
			if notFound != tc.wantNotFound || reason != tc.wantReason {
				t.Fatalf("got (notFound=%v, reason=%q), want (notFound=%v, reason=%q)", notFound, reason, tc.wantNotFound, tc.wantReason)
			}
			if callCount != 2 {
				t.Fatalf("expected atomic UPDATE + 1 fallback SELECT, got %d calls", callCount)
			}
		})
	}
}

func TestValidateAndClaimTrunkForOutboundCall_NilClaimUsernameReadsOnly(t *testing.T) {
	t.Parallel()

	owner := "gw-1"
	future := time.Now().Add(time.Minute)
	db := &perfTestDB{
		queryRowFn: func(context.Context, string, ...any) pgx.Row {
			return mobileTrunkTestRow{trunk: Trunk{
				ID:         9,
				PublicID:   "pub-9",
				Enabled:    true,
				LeaseOwner: &owner,
				LeaseUntil: &future,
			}}
		},
	}
	tm := &TrunkManager{db: db, instanceID: owner}

	trunk, notFound, reason, err := tm.ValidateAndClaimTrunkForOutboundCall(context.Background(), 9, owner, nil)
	if err != nil || notFound || reason != "" || trunk == nil || trunk.ID != 9 {
		t.Fatalf("unexpected result: trunk=%+v notFound=%v reason=%q err=%v", trunk, notFound, reason, err)
	}
	if len(db.calls) != 1 || !strings.Contains(db.calls[0].sql, "SELECT") {
		t.Fatalf("expected 1 SELECT query when claimUsername is nil, got %+v", db.calls)
	}
}

// --- Task 5: EnsureTrunkRegistered & No-Op Upsert Suppression ---

func newHealthyRegisteredTrunkFixture() (*TrunkManager, *perfTestDB) {
	db := &perfTestDB{}
	owner := "gw-1"
	now := time.Now()
	until := now.Add(55 * time.Second)
	regAt := now.Add(-10 * time.Second)
	appID := "app-1"
	pnType := "apple"
	pnToken := "tok-1"
	tm := &TrunkManager{
		db:         db,
		cfg:        &config.Config{SIPTrunk: config.SIPTrunkConfig{LeaseTTLSeconds: 60}, SIPPublic: config.SIPPublicConfig{RegisterExpiresSeconds: 300}},
		instanceID: owner,
		trunks: map[int64]*Trunk{
			42: {
				ID:               42,
				PublicID:         "pub-42",
				Name:             "sipclient-mobile-user-1",
				Domain:           "sip.example.com",
				Port:             5060,
				Username:         "1001",
				Password:         "secret-1",
				Transport:        "tcp",
				Enabled:          true,
				SipAutoRegister:  true,
				LeaseOwner:       &owner,
				LeaseUntil:       &until,
				LastRegisteredAt: &regAt,
				PNAppID:          &appID,
				PNType:           &pnType,
				PNToken:          &pnToken,
			},
		},
		trunkByPublic:  map[string]int64{"pub-42": 42},
		ownedLeases:    map[int64]bool{42: true},
		refreshWorkers: map[int64]chan struct{}{42: make(chan struct{})},
		registeredPushContacts: map[int64]TrunkPushContact{
			42: {PNAppID: appID, PNType: pnType, PNToken: pnToken},
		},
	}
	return tm, db
}

func TestEnsureTrunkRegistered_SkipsRedundantRegisterWhenHealthy(t *testing.T) {
	t.Parallel()

	tm, db := newHealthyRegisteredTrunkFixture()
	if err := tm.EnsureTrunkRegistered(42, true); err != nil {
		t.Fatalf("expected nil on healthy fast-path, got %v", err)
	}
	if len(db.calls) != 0 {
		t.Fatalf("fast-path must perform 0 DB calls, got %d", len(db.calls))
	}
}

func TestEnsureTrunkRegistered_DelegatesToRegisterTrunkWhenStateNotFresh(t *testing.T) {
	t.Parallel()

	staleReg := time.Now().Add(-290 * time.Second)
	expiringSoon := time.Now().Add(2 * time.Second)
	otherOwner := "gw-2"
	errMsg := "REGISTER failed: 503"
	newTok := "tok-2"

	cases := []struct {
		name   string
		mutate func(tm *TrunkManager)
	}{
		{"not owned", func(tm *TrunkManager) { delete(tm.ownedLeases, 42) }},
		{"foreign lease owner", func(tm *TrunkManager) { tm.trunks[42].LeaseOwner = &otherOwner }},
		{"lease expiring soon", func(tm *TrunkManager) { tm.trunks[42].LeaseUntil = &expiringSoon }},
		{"no refresh worker", func(tm *TrunkManager) { delete(tm.refreshWorkers, 42) }},
		{"stale LastRegisteredAt", func(tm *TrunkManager) { tm.trunks[42].LastRegisteredAt = &staleReg }},
		{"LastError set", func(tm *TrunkManager) { tm.trunks[42].LastError = &errMsg }},
		{"SipAutoRegister false", func(tm *TrunkManager) { tm.trunks[42].SipAutoRegister = false }},
		{"needsReRegister flagged", func(tm *TrunkManager) { tm.needsReRegister = map[int64]bool{42: true} }},
		{"push contact changed", func(tm *TrunkManager) { tm.trunks[42].PNToken = &newTok }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tm, db := newHealthyRegisteredTrunkFixture()
			sentinel := errors.New("delegated-to-register")
			db.queryRowFn = func(context.Context, string, ...any) pgx.Row {
				return errRow{err: sentinel}
			}
			tc.mutate(tm)

			err := tm.EnsureTrunkRegistered(42, true)
			if !errors.Is(err, sentinel) {
				t.Fatalf("expected delegation to RegisterTrunk (sentinel error), got %v", err)
			}
			if len(db.calls) != 1 {
				t.Fatalf("expected RegisterTrunk DB call, got %d", len(db.calls))
			}
			// forceLeaseSteal=true must preserve force lease SQL (no lease_owner predicate).
			if strings.Contains(db.calls[0].sql, "lease_owner = $1)") {
				t.Fatalf("expected forceLeaseSteal=true to use force lease SQL, got: %s", db.calls[0].sql)
			}
		})
	}
}

func TestUpsertMobileTrunk_NoOpSQLAndPasswordChangeInvalidatesFastPath(t *testing.T) {
	t.Parallel()

	tm, db := newHealthyRegisteredTrunkFixture()
	owner := "gw-1"
	until := time.Now().Add(55 * time.Second)
	regAt := time.Now().Add(-10 * time.Second)
	appID := "app-1"
	pnType := "apple"
	pnToken := "tok-1"

	// 1. Unchanged upsert keeps fast-path valid and uses IS DISTINCT FROM CTE.
	db.queryRowFn = func(context.Context, string, ...any) pgx.Row {
		return mobileTrunkTestRow{trunk: Trunk{
			ID:               42,
			PublicID:         "pub-42",
			Name:             "sipclient-mobile-user-1",
			Domain:           "sip.example.com",
			Port:             5060,
			Username:         "1001",
			Password:         "secret-1",
			Transport:        "tcp",
			Enabled:          true,
			SipAutoRegister:  true,
			LeaseOwner:       &owner,
			LeaseUntil:       &until,
			LastRegisteredAt: &regAt,
			PNAppID:          &appID,
			PNType:           &pnType,
			PNToken:          &pnToken,
		}}
	}
	if _, err := tm.UpsertMobileTrunk(context.Background(), MobileTrunkPayload{
		Subject:  "user-1",
		Domain:   "sip.example.com",
		Username: "1001",
		Password: "secret-1",
	}); err != nil {
		t.Fatalf("UpsertMobileTrunk unchanged failed: %v", err)
	}
	if !strings.Contains(db.calls[0].sql, "IS DISTINCT FROM") || !strings.Contains(db.calls[0].sql, "NOT EXISTS (SELECT 1 FROM upserted)") {
		t.Fatalf("expected no-op suppressing CTE upsert SQL, got: %s", db.calls[0].sql)
	}
	if tm.needsReRegister[42] {
		t.Fatalf("unchanged upsert must not mark trunk as needing re-register")
	}
	if !tm.canSkipRegister(42, time.Now()) {
		t.Fatalf("unchanged upsert should preserve EnsureTrunkRegistered fast-path")
	}

	// 2. Password change marks needsReRegister so EnsureTrunkRegistered must re-register.
	db.queryRowFn = func(context.Context, string, ...any) pgx.Row {
		return mobileTrunkTestRow{trunk: Trunk{
			ID:               42,
			PublicID:         "pub-42",
			Name:             "sipclient-mobile-user-1",
			Domain:           "sip.example.com",
			Port:             5060,
			Username:         "1001",
			Password:         "secret-rotated",
			Transport:        "tcp",
			Enabled:          true,
			SipAutoRegister:  true,
			LeaseOwner:       &owner,
			LeaseUntil:       &until,
			LastRegisteredAt: &regAt,
			PNAppID:          &appID,
			PNType:           &pnType,
			PNToken:          &pnToken,
		}}
	}
	if _, err := tm.UpsertMobileTrunk(context.Background(), MobileTrunkPayload{
		Subject:  "user-1",
		Domain:   "sip.example.com",
		Username: "1001",
		Password: "secret-rotated",
	}); err != nil {
		t.Fatalf("UpsertMobileTrunk password change failed: %v", err)
	}
	if !tm.needsReRegister[42] {
		t.Fatalf("password change must mark needsReRegister=true")
	}
	if tm.canSkipRegister(42, time.Now()) {
		t.Fatalf("EnsureTrunkRegistered must NOT skip after password change")
	}
}

// --- Task 6: Consolidated DB Writes ---

func TestSetTrunkPushContact_SingleCTEAndMarksReRegisterOnChange(t *testing.T) {
	t.Parallel()

	tm, db := newHealthyRegisteredTrunkFixture()

	// Unchanged push contact returns changed=false in 1 query and keeps fast-path.
	db.queryRowFn = func(context.Context, string, ...any) pgx.Row {
		return scalarRow{vals: []any{false}}
	}
	changed, err := tm.SetTrunkPushContact(context.Background(), 42, TrunkPushContact{
		PNAppID: "app-1",
		PNType:  "apple",
		PNToken: "tok-1",
	})
	if err != nil || changed {
		t.Fatalf("expected (false, nil), got (%v, %v)", changed, err)
	}
	if len(db.calls) != 1 || db.calls[0].kind != "queryRow" {
		t.Fatalf("expected 1 single CTE QueryRow, got %+v", db.calls)
	}
	if tm.needsReRegister[42] {
		t.Fatalf("unchanged push contact must not set needsReRegister")
	}

	// Changed push contact returns changed=true in 1 query and sets needsReRegister.
	db.queryRowFn = func(context.Context, string, ...any) pgx.Row {
		return scalarRow{vals: []any{true}}
	}
	changed, err = tm.SetTrunkPushContact(context.Background(), 42, TrunkPushContact{
		PNAppID: "app-1",
		PNType:  "apple",
		PNToken: "tok-new",
	})
	if err != nil || !changed {
		t.Fatalf("expected (true, nil), got (%v, %v)", changed, err)
	}
	if !tm.needsReRegister[42] {
		t.Fatalf("changed push contact must set needsReRegister=true")
	}
	if tm.trunks[42].PNToken == nil || *tm.trunks[42].PNToken != "tok-new" {
		t.Fatalf("expected RAM PNToken updated to tok-new")
	}
}

func TestUpdateTrunk_UsesReturningWithoutReloadSelect(t *testing.T) {
	t.Parallel()

	tx := &perfTestTx{}
	callNum := 0
	tx.queryRowFn = func(_ context.Context, sql string, _ ...any) pgx.Row {
		callNum++
		if callNum == 1 {
			return mobileTrunkTestRow{trunk: Trunk{
				ID: 11, PublicID: "pub-11", Name: "old", Domain: "sip.example.com",
				Port: 5060, Username: "1001", Password: "p1", Transport: "tcp", Enabled: true,
			}}
		}
		return mobileTrunkTestRow{trunk: Trunk{
			ID: 11, PublicID: "pub-11", Name: "new", Domain: "sip.example.com",
			Port: 5060, Username: "1001", Password: "p2", Transport: "tcp", Enabled: true,
		}}
	}
	db := &perfTestDB{
		beginTxFn: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
	}
	tm := &TrunkManager{
		db:            db,
		trunks:        map[int64]*Trunk{11: {ID: 11, PublicID: "pub-11", Password: "p1", Enabled: true}},
		trunkByPublic: map[string]int64{"pub-11": 11},
	}

	newName := "new"
	newPass := "p2"
	updated, err := tm.UpdateTrunk(context.Background(), 11, TrunkUpdatePatch{
		Name:     &newName,
		Password: &newPass,
	})
	if err != nil {
		t.Fatalf("UpdateTrunk failed: %v", err)
	}
	if !tx.committed {
		t.Fatalf("expected tx commit")
	}
	if len(tx.calls) != 2 {
		t.Fatalf("expected exactly 2 tx calls (SELECT FOR UPDATE + UPDATE RETURNING), got %d", len(tx.calls))
	}
	if !strings.Contains(tx.calls[1].sql, "UPDATE sip_trunks SET") || !strings.Contains(tx.calls[1].sql, "RETURNING") {
		t.Fatalf("expected UPDATE ... RETURNING, got: %s", tx.calls[1].sql)
	}
	if updated.Name != "new" || updated.Password != "p2" {
		t.Fatalf("unexpected updated trunk: %+v", updated)
	}
	if !tm.needsReRegister[11] {
		t.Fatalf("password update must mark needsReRegister=true")
	}
}

func TestRegisterTrunk_NonForceLeaseHeldByAnotherReturnsErrTrunkLeaseLost(t *testing.T) {
	t.Parallel()

	callNum := 0
	other := "gw-2"
	db := &perfTestDB{
		queryRowFn: func(context.Context, string, ...any) pgx.Row {
			callNum++
			if callNum == 1 {
				return errRow{err: pgx.ErrNoRows}
			}
			return mobileTrunkTestRow{trunk: Trunk{ID: 7, Enabled: true, LeaseOwner: &other}}
		},
	}
	tm := &TrunkManager{
		db:         db,
		cfg:        &config.Config{SIPTrunk: config.SIPTrunkConfig{LeaseTTLSeconds: 60}},
		instanceID: "gw-1",
	}

	err := tm.RegisterTrunk(7, false)
	if !errors.Is(err, ErrTrunkLeaseLost) {
		t.Fatalf("expected ErrTrunkLeaseLost, got %v", err)
	}
	if !strings.Contains(db.calls[0].sql, "SET sip_auto_register = true") || !strings.Contains(db.calls[0].sql, "RETURNING") {
		t.Fatalf("expected consolidated RegisterTrunk UPDATE ... RETURNING, got: %s", db.calls[0].sql)
	}
}

func TestUnregisterTrunk_ConsolidatesToTwoDBQueries(t *testing.T) {
	t.Parallel()

	owner := "gw-1"
	until := time.Now().Add(time.Minute)
	regAt := time.Now().Add(-5 * time.Second)
	db := &perfTestDB{
		queryRowFn: func(context.Context, string, ...any) pgx.Row {
			return mobileTrunkTestRow{trunk: Trunk{
				ID:               15,
				Name:             "sipclient-agent-1001@sip.example.com:5060",
				Username:         "1001",
				Domain:           "sip.example.com",
				Port:             5060,
				Enabled:          true,
				SipAutoRegister:  false,
				LeaseOwner:       &owner,
				LeaseUntil:       &until,
				LastRegisteredAt: &regAt,
			}}
		},
	}
	// Sibling 16 keeps shared contact alive so releaseTrunkContact succeeds without SIP client.
	sibling := &Trunk{
		ID:       16,
		Name:     "sipclient-agent-device-1001@sip.example.com:5060",
		Username: "1001",
		Domain:   "sip.example.com",
		Port:     5060,
	}
	tm := &TrunkManager{
		db:         db,
		instanceID: owner,
		trunks: map[int64]*Trunk{
			15: {ID: 15, SipAutoRegister: true, LeaseOwner: &owner, LeaseUntil: &until, LastRegisteredAt: &regAt},
			16: sibling,
		},
		ownedLeases: map[int64]bool{15: true, 16: true},
		refreshWorkers: map[int64]chan struct{}{
			15: make(chan struct{}),
			16: make(chan struct{}),
		},
		registrarIdentities: map[int64]*registrarIdentity{
			16: {ExpiresAt: time.Now().Add(time.Hour)},
		},
	}

	if err := tm.UnregisterTrunk(15, false); err != nil {
		t.Fatalf("UnregisterTrunk failed: %v", err)
	}
	if len(db.calls) != 2 {
		t.Fatalf("expected 2 consolidated DB queries (pre-SIP UPDATE RETURNING + post-SIP UPDATE), got %d", len(db.calls))
	}
	if !strings.Contains(db.calls[0].sql, "SET sip_auto_register = false") || !strings.Contains(db.calls[0].sql, "RETURNING") {
		t.Fatalf("expected pre-SIP UPDATE ... RETURNING, got: %s", db.calls[0].sql)
	}
	if !strings.Contains(db.calls[1].sql, "CASE WHEN lease_owner = $3 THEN NULL ELSE lease_owner END") || !strings.Contains(db.calls[1].sql, "last_unregistered_at = $1") {
		t.Fatalf("expected post-SIP consolidated non-force lease+status UPDATE, got: %s", db.calls[1].sql)
	}
	if tm.trunks[15].LastRegisteredAt != nil || tm.trunks[15].LastUnregisteredAt == nil || tm.trunks[15].SipAutoRegister {
		t.Fatalf("expected RAM registration state cleared after UnregisterTrunk: %+v", tm.trunks[15])
	}
}
