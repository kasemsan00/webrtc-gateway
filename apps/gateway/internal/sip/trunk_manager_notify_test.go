package sip

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type notifyTestDB struct {
	targetUsername string
	targetDomain   string
	targetPort     int
	rowErr         error
	queries        []notifyTestQuery
}

type notifyTestQuery struct {
	sql  string
	args []any
}

func (db *notifyTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}

func (db *notifyTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}

func (db *notifyTestDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	db.queries = append(db.queries, notifyTestQuery{
		sql:  sql,
		args: append([]any(nil), args...),
	})
	return notifyTestRow{
		username: db.targetUsername,
		domain:   db.targetDomain,
		port:     db.targetPort,
		err:      db.rowErr,
	}
}

func (db *notifyTestDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("unexpected BeginTx: SetTrunkNotifyUserIDAndPlatform should use a single statement")
}

type notifyTestRow struct {
	username string
	domain   string
	port     int
	err      error
}

func (r notifyTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*string)) = r.username
	*(dest[1].(*string)) = r.domain
	*(dest[2].(*int)) = r.port
	return nil
}

func TestSetTrunkNotifyUserID_ReassignsUserToTargetAndClearsConflictingIdentities(t *testing.T) {
	oldUserID := "user-1"
	otherUserID := "user-2"
	db := &notifyTestDB{
		targetUsername: "1002",
		targetDomain:   "sip.example.com",
		targetPort:     5060,
	}
	tm := &TrunkManager{
		db: db,
		trunks: map[int64]*Trunk{
			1: {ID: 1, Username: "1001", Domain: "sip.example.com", Port: 5060, NotifyUserID: &oldUserID},
			2: {ID: 2, Username: "1002", Domain: "sip.example.com", Port: 5060, NotifyUserID: &otherUserID},
			3: {ID: 3, Username: "1002", Domain: "sip.example.com", Port: 5060, NotifyUserID: &oldUserID},
			4: {ID: 4, Username: "1002", Domain: "sip-alt.example.com", Port: 5060, NotifyUserID: &oldUserID},
		},
	}

	inputUserID := "  user-1  "
	if err := tm.SetTrunkNotifyUserID(context.Background(), 2, &inputUserID); err != nil {
		t.Fatalf("SetTrunkNotifyUserID failed: %v", err)
	}

	if len(db.queries) != 1 {
		t.Fatalf("expected 1 consolidated writable-CTE query, got %d", len(db.queries))
	}
	q := db.queries[0]
	if !strings.Contains(q.sql, "SET notify_user_id = NULL") || !strings.Contains(q.sql, "SET notify_user_id = $2") {
		t.Fatalf("expected CTE clearing conflicting and setting target notify_user_id, got %s", q.sql)
	}
	if len(q.args) != 2 || q.args[0] != int64(2) || q.args[1] != "user-1" {
		t.Fatalf("expected args [2, user-1], got %#v", q.args)
	}

	if tm.trunks[1].NotifyUserID != nil {
		t.Fatalf("expected conflicting username trunk notify_user_id to be cleared")
	}
	if tm.trunks[2].NotifyUserID == nil || *tm.trunks[2].NotifyUserID != "user-1" {
		t.Fatalf("expected target trunk notify_user_id=user-1, got %#v", tm.trunks[2].NotifyUserID)
	}
	if tm.trunks[3].NotifyUserID == nil || *tm.trunks[3].NotifyUserID != "user-1" {
		t.Fatalf("expected same identity trunk notify_user_id to remain, got %#v", tm.trunks[3].NotifyUserID)
	}
	if tm.trunks[4].NotifyUserID != nil {
		t.Fatalf("expected different domain trunk notify_user_id to be cleared")
	}
}

func TestSetTrunkNotifyUserID_BlankUserIDClearsOnlyTarget(t *testing.T) {
	userID := "user-1"
	db := &notifyTestDB{
		targetUsername: "1002",
		targetDomain:   "sip.example.com",
		targetPort:     5060,
	}
	tm := &TrunkManager{
		db: db,
		trunks: map[int64]*Trunk{
			1: {ID: 1, Username: "1001", Domain: "sip.example.com", Port: 5060, NotifyUserID: &userID},
			2: {ID: 2, Username: "1002", Domain: "sip.example.com", Port: 5060, NotifyUserID: &userID},
		},
	}

	blank := "   "
	if err := tm.SetTrunkNotifyUserID(context.Background(), 2, &blank); err != nil {
		t.Fatalf("SetTrunkNotifyUserID failed: %v", err)
	}

	if len(db.queries) != 1 {
		t.Fatalf("expected 1 single-target clear query, got %d", len(db.queries))
	}
	q := db.queries[0]
	if strings.Contains(q.sql, "WITH target") {
		t.Fatalf("blank userID clear should only update target row, got %s", q.sql)
	}
	if !strings.Contains(q.sql, "SET notify_user_id = NULL") {
		t.Fatalf("expected notify_user_id = NULL update, got %s", q.sql)
	}
	if len(q.args) != 1 || q.args[0] != int64(2) {
		t.Fatalf("expected target trunk id arg [2], got %#v", q.args)
	}
	if tm.trunks[1].NotifyUserID == nil || *tm.trunks[1].NotifyUserID != "user-1" {
		t.Fatalf("expected non-target trunk to remain unchanged, got %#v", tm.trunks[1].NotifyUserID)
	}
	if tm.trunks[2].NotifyUserID != nil {
		t.Fatalf("expected target trunk notify_user_id to be cleared")
	}
}

func TestSetTrunkNotifyUserIDAndPlatform_PersistsPlatform(t *testing.T) {
	userID := "user-1"
	db := &notifyTestDB{
		targetUsername: "1002",
		targetDomain:   "sip.example.com",
		targetPort:     5060,
	}
	tm := &TrunkManager{
		db: db,
		trunks: map[int64]*Trunk{
			2: {ID: 2, Username: "1002", Domain: "sip.example.com", Port: 5060},
		},
	}

	platform := " Android "
	if err := tm.SetTrunkNotifyUserIDAndPlatform(context.Background(), 2, &userID, &platform); err != nil {
		t.Fatalf("SetTrunkNotifyUserIDAndPlatform failed: %v", err)
	}

	if len(db.queries) != 1 {
		t.Fatalf("expected 1 consolidated query, got %d", len(db.queries))
	}
	q := db.queries[0]
	if !strings.Contains(q.sql, "last_online_platform = $3") {
		t.Fatalf("expected platform update SQL, got %s", q.sql)
	}
	if len(q.args) != 3 || q.args[0] != int64(2) || q.args[1] != "user-1" || q.args[2] != "android" {
		t.Fatalf("expected args [2, user-1, android], got %#v", q.args)
	}
	if tm.trunks[2].LastOnlinePlatform == nil || *tm.trunks[2].LastOnlinePlatform != "android" {
		t.Fatalf("expected in-memory platform android, got %#v", tm.trunks[2].LastOnlinePlatform)
	}
	if tm.trunks[2].LastOnlineAt == nil {
		t.Fatalf("expected in-memory last_online_at to be set")
	}
}

func TestSetTrunkNotifyUserID_NotFoundReturnsErrTrunkNotFound(t *testing.T) {
	userID := "user-1"
	db := &notifyTestDB{rowErr: pgx.ErrNoRows}
	tm := &TrunkManager{db: db}

	err := tm.SetTrunkNotifyUserID(context.Background(), 99, &userID)
	if !errors.Is(err, ErrTrunkNotFound) {
		t.Fatalf("expected ErrTrunkNotFound, got %v", err)
	}
}
