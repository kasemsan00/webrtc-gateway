package sip

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func testTrunkManagerCtx() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

type regErrorTestDB struct{}

func (db *regErrorTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (db *regErrorTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}
func (db *regErrorTestDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow")
}
func (db *regErrorTestDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("unexpected BeginTx")
}

func TestEmitTrunkListChange_RegistrationError(t *testing.T) {
	t.Parallel()

	var calls []struct {
		eventType string
		ids       []int64
	}
	ctx, cancel := testTrunkManagerCtx()
	defer cancel()
	tm := &TrunkManager{
		db:     &regErrorTestDB{},
		trunks: map[int64]*Trunk{3: {ID: 3}},
		ctx:    ctx,
	}
	tm.SetTrunkListChangeCallback(func(eventType string, trunkIDs []int64) {
		calls = append(calls, struct {
			eventType string
			ids       []int64
		}{eventType, trunkIDs})
	})

	tm.updateRegistrationError(3, "REGISTER failed: 401 Unauthorized")

	if len(calls) != 1 {
		t.Fatalf("expected 1 callback, got %d", len(calls))
	}
	if calls[0].eventType != "registration_updated" || len(calls[0].ids) != 1 || calls[0].ids[0] != 3 {
		t.Fatalf("unexpected callback: %+v", calls[0])
	}
	if tm.trunks[3].LastError == nil || *tm.trunks[3].LastError != "REGISTER failed: 401 Unauthorized" {
		t.Fatalf("expected last error on trunk, got %#v", tm.trunks[3].LastError)
	}
}
