package sip

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mobileTrunkTestDB struct {
	sql  string
	args []any
	row  mobileTrunkTestRow
}

func (db *mobileTrunkTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}

func (db *mobileTrunkTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}

func (db *mobileTrunkTestDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	db.sql = sql
	db.args = append([]any(nil), args...)
	return db.row
}

func (db *mobileTrunkTestDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("unexpected BeginTx")
}

type mobileTrunkTestRow struct {
	trunk Trunk
}

func (r mobileTrunkTestRow) Scan(dest ...any) error {
	now := time.Now()
	trunk := r.trunk
	if trunk.CreatedAt.IsZero() {
		trunk.CreatedAt = now
	}
	if trunk.UpdatedAt.IsZero() {
		trunk.UpdatedAt = now
	}
	*(dest[0].(*int64)) = trunk.ID
	*(dest[1].(*string)) = trunk.PublicID
	*(dest[2].(*string)) = trunk.Name
	*(dest[3].(*string)) = trunk.Domain
	*(dest[4].(*int)) = trunk.Port
	*(dest[5].(*string)) = trunk.Username
	*(dest[6].(*string)) = trunk.Password
	*(dest[7].(*string)) = trunk.Transport
	*(dest[8].(*bool)) = trunk.Enabled
	*(dest[9].(*bool)) = trunk.IsDefault
	*(dest[10].(**string)) = trunk.LeaseOwner
	*(dest[11].(**time.Time)) = trunk.LeaseUntil
	*(dest[12].(**time.Time)) = trunk.LastRegisteredAt
	*(dest[13].(**string)) = trunk.LastError
	*(dest[14].(**string)) = trunk.InUseBy
	*(dest[15].(**string)) = trunk.NotifyUserID
	*(dest[16].(**string)) = trunk.LastOnlinePlatform
	*(dest[17].(**time.Time)) = trunk.LastOnlineAt
	*(dest[18].(**string)) = trunk.PNAppID
	*(dest[19].(**string)) = trunk.PNType
	*(dest[20].(**string)) = trunk.PNToken
	*(dest[21].(**time.Time)) = trunk.PNUpdatedAt
	*(dest[22].(*time.Time)) = trunk.CreatedAt
	*(dest[23].(*time.Time)) = trunk.UpdatedAt
	return nil
}

func TestBuildMobileTrunkName(t *testing.T) {
	t.Parallel()

	name, err := BuildMobileTrunkName("  user-sub-1  ")
	if err != nil {
		t.Fatalf("BuildMobileTrunkName failed: %v", err)
	}
	if name != "sipclient-mobile-user-sub-1" {
		t.Fatalf("unexpected name %q", name)
	}

	if _, err := BuildMobileTrunkName("   "); err == nil {
		t.Fatalf("expected blank subject error")
	}
}

func TestUpsertMobileTrunkUsesDeterministicNameAndCredentials(t *testing.T) {
	t.Parallel()

	db := &mobileTrunkTestDB{
		row: mobileTrunkTestRow{trunk: Trunk{
			ID:        42,
			PublicID:  "public-42",
			Name:      "sipclient-mobile-user-sub-1",
			Domain:    "sipclient.ttrs.or.th",
			Port:      5060,
			Username:  "1429900148716",
			Password:  "secret-1",
			Transport: "tcp",
			Enabled:   true,
		}},
	}
	tm := &TrunkManager{
		db:            db,
		trunks:        make(map[int64]*Trunk),
		trunkByPublic: make(map[string]int64),
	}

	trunk, err := tm.UpsertMobileTrunk(context.Background(), MobileTrunkPayload{
		Subject:  "  user-sub-1  ",
		Domain:   " sipclient.ttrs.or.th ",
		Username: " 1429900148716 ",
		Password: " secret-1 ",
	})
	if err != nil {
		t.Fatalf("UpsertMobileTrunk failed: %v", err)
	}
	if trunk.ID != 42 || trunk.Name != "sipclient-mobile-user-sub-1" {
		t.Fatalf("unexpected trunk: %+v", trunk)
	}
	if !strings.Contains(db.sql, "ON CONFLICT (name) DO UPDATE") {
		t.Fatalf("expected deterministic upsert by name, got SQL %s", db.sql)
	}
	if len(db.args) < 7 {
		t.Fatalf("expected upsert args, got %#v", db.args)
	}
	expected := []any{
		"sipclient-mobile-user-sub-1",
		"sipclient.ttrs.or.th",
		5060,
		"1429900148716",
		"secret-1",
		"tcp",
		true,
	}
	for i, want := range expected {
		if got := db.args[i]; got != want {
			t.Fatalf("arg %d: expected %#v, got %#v", i+1, want, got)
		}
	}
	if tm.trunks[42] != trunk {
		t.Fatalf("expected in-memory cache to contain mobile trunk")
	}
	if tm.trunkByPublic["public-42"] != 42 {
		t.Fatalf("expected public id cache to be updated")
	}
	if strings.Contains(db.sql, "notify_user_id = EXCLUDED.notify_user_id") || strings.Contains(db.sql, "last_online_platform = EXCLUDED.last_online_platform") {
		t.Fatalf("upsert should not assign notify_user_id or platform, got SQL %s", db.sql)
	}
}
