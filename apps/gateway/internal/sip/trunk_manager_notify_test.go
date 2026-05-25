package sip

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type notifyTestDB struct {
	tx *notifyTestTx
}

func (db *notifyTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec outside transaction")
}

func (db *notifyTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query outside transaction")
}

func (db *notifyTestDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow outside transaction")
}

func (db *notifyTestDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return db.tx, nil
}

type notifyTestTx struct {
	targetUsername string
	targetDomain   string
	targetPort     int
	execs          []notifyTestExec
	committed      bool
	rolledBack     bool
}

type notifyTestExec struct {
	sql  string
	args []any
}

func (tx *notifyTestTx) Begin(context.Context) (pgx.Tx, error) {
	panic("unexpected nested transaction")
}

func (tx *notifyTestTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *notifyTestTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

func (tx *notifyTestTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	panic("unexpected CopyFrom")
}

func (tx *notifyTestTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	panic("unexpected SendBatch")
}

func (tx *notifyTestTx) LargeObjects() pgx.LargeObjects {
	panic("unexpected LargeObjects")
}

func (tx *notifyTestTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	panic("unexpected Prepare")
}

func (tx *notifyTestTx) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	tx.execs = append(tx.execs, notifyTestExec{sql: sql, args: append([]any(nil), arguments...)})
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *notifyTestTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}

func (tx *notifyTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return notifyTestRow{
		username: tx.targetUsername,
		domain:   tx.targetDomain,
		port:     tx.targetPort,
	}
}

func (tx *notifyTestTx) Conn() *pgx.Conn {
	return nil
}

type notifyTestRow struct {
	username string
	domain   string
	port     int
}

func (r notifyTestRow) Scan(dest ...any) error {
	*(dest[0].(*string)) = r.username
	*(dest[1].(*string)) = r.domain
	*(dest[2].(*int)) = r.port
	return nil
}

func TestSetTrunkNotifyUserID_ReassignsUserToTargetAndClearsConflictingIdentities(t *testing.T) {
	oldUserID := "user-1"
	otherUserID := "user-2"
	tx := &notifyTestTx{
		targetUsername: "1002",
		targetDomain:   "sip.example.com",
		targetPort:     5060,
	}
	tm := &TrunkManager{
		db: &notifyTestDB{tx: tx},
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

	if !tx.committed {
		t.Fatalf("expected transaction commit")
	}
	if len(tx.execs) != 2 {
		t.Fatalf("expected clear + set execs, got %d", len(tx.execs))
	}
	clearExec := tx.execs[0]
	if !strings.Contains(clearExec.sql, "SET notify_user_id = NULL") {
		t.Fatalf("expected first exec to clear notify_user_id, got %s", clearExec.sql)
	}
	if got := clearExec.args[0]; got != "user-1" {
		t.Fatalf("expected clear userID user-1, got %#v", got)
	}
	if got := clearExec.args[1]; got != int64(2) {
		t.Fatalf("expected clear to exclude trunk 2, got %#v", got)
	}
	if got := clearExec.args[2]; got != "1002" {
		t.Fatalf("expected target username 1002, got %#v", got)
	}
	if got := clearExec.args[3]; got != "sip.example.com" {
		t.Fatalf("expected target domain sip.example.com, got %#v", got)
	}
	if got := clearExec.args[4]; got != 5060 {
		t.Fatalf("expected target port 5060, got %#v", got)
	}

	setExec := tx.execs[1]
	if !strings.Contains(setExec.sql, "UPDATE sip_trunks SET notify_user_id = $1") {
		t.Fatalf("expected second exec to set notify_user_id, got %s", setExec.sql)
	}
	setUserID, ok := setExec.args[0].(*string)
	if !ok || setUserID == nil || *setUserID != "user-1" {
		t.Fatalf("expected normalized target userID user-1, got %#v", setExec.args[0])
	}
	if got := setExec.args[1]; got != int64(2) {
		t.Fatalf("expected target trunk 2, got %#v", got)
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
	tx := &notifyTestTx{
		targetUsername: "1002",
		targetDomain:   "sip.example.com",
		targetPort:     5060,
	}
	tm := &TrunkManager{
		db: &notifyTestDB{tx: tx},
		trunks: map[int64]*Trunk{
			1: {ID: 1, Username: "1001", Domain: "sip.example.com", Port: 5060, NotifyUserID: &userID},
			2: {ID: 2, Username: "1002", Domain: "sip.example.com", Port: 5060, NotifyUserID: &userID},
		},
	}

	blank := "   "
	if err := tm.SetTrunkNotifyUserID(context.Background(), 2, &blank); err != nil {
		t.Fatalf("SetTrunkNotifyUserID failed: %v", err)
	}

	if len(tx.execs) != 1 {
		t.Fatalf("expected only target set/clear exec, got %d", len(tx.execs))
	}
	userIDArg, ok := tx.execs[0].args[0].(*string)
	if !ok {
		t.Fatalf("expected userID arg to be *string, got %#v", tx.execs[0].args[0])
	}
	if userIDArg != nil {
		t.Fatalf("expected blank userID to be normalized to nil, got %#v", tx.execs[0].args[0])
	}
	if tm.trunks[1].NotifyUserID == nil || *tm.trunks[1].NotifyUserID != "user-1" {
		t.Fatalf("expected non-target trunk to remain unchanged, got %#v", tm.trunks[1].NotifyUserID)
	}
	if tm.trunks[2].NotifyUserID != nil {
		t.Fatalf("expected target trunk notify_user_id to be cleared")
	}
}
