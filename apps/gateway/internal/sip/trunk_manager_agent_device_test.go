package sip

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestBuildAgentDeviceTrunkNameDistinctFromAgentAndMobile(t *testing.T) {
	t.Parallel()

	device, err := BuildAgentDeviceTrunkName("  SIP.Example.COM ", " 1001 ", 0)
	if err != nil {
		t.Fatalf("BuildAgentDeviceTrunkName failed: %v", err)
	}
	if device != "sipclient-agent-device-1001@sip.example.com:5060" {
		t.Fatalf("unexpected device name %q", device)
	}
	agent, err := BuildAgentTrunkName("sip.example.com", "1001", 5060)
	if err != nil {
		t.Fatalf("BuildAgentTrunkName failed: %v", err)
	}
	mobile, err := BuildMobileTrunkName("1001")
	if err != nil {
		t.Fatalf("BuildMobileTrunkName failed: %v", err)
	}
	if device == agent || device == mobile {
		t.Fatalf("device name collided: device=%q agent=%q mobile=%q", device, agent, mobile)
	}
	if !IsAgentDeviceTrunkName(device) || IsAgentTrunkName(device) {
		t.Fatalf("expected device namespace, got %q", device)
	}
	if IsAgentDeviceTrunkName(agent) || !IsAgentTrunkName(agent) {
		t.Fatalf("expected agent namespace, got %q", agent)
	}
}

func TestUpsertAgentDeviceTrunkUsesDeterministicName(t *testing.T) {
	t.Parallel()

	db := &mobileTrunkTestDB{
		row: mobileTrunkTestRow{trunk: Trunk{
			ID:        88,
			PublicID:  "public-88",
			Name:      "sipclient-agent-device-1001@sip.example.com:5060",
			Domain:    "sip.example.com",
			Port:      5060,
			Username:  "1001",
			Password:  "secret-device",
			Transport: "tcp",
			Enabled:   true,
		}},
	}
	tm := &TrunkManager{
		db:            db,
		trunks:        make(map[int64]*Trunk),
		trunkByPublic: make(map[string]int64),
	}

	trunk, err := tm.UpsertAgentDeviceTrunk(context.Background(), AgentTrunkPayload{
		Domain:   " SIP.Example.COM ",
		Username: " 1001 ",
		Password: " secret-device ",
		Port:     0,
	})
	if err != nil {
		t.Fatalf("UpsertAgentDeviceTrunk failed: %v", err)
	}
	if trunk.ID != 88 || trunk.Name != "sipclient-agent-device-1001@sip.example.com:5060" {
		t.Fatalf("unexpected trunk %#v", trunk)
	}
	if got, _ := db.args[0].(string); got != "sipclient-agent-device-1001@sip.example.com:5060" {
		t.Fatalf("expected agent-device name arg, got %#v", db.args)
	}
}

func TestIdentityGroupTrunkIDsPairsAgentAndDevice(t *testing.T) {
	t.Parallel()

	agent := &Trunk{ID: 1, Name: "sipclient-agent-1001@sip.example.com:5060", Username: "1001", Domain: "sip.example.com", Port: 5060}
	device := &Trunk{ID: 2, Name: "sipclient-agent-device-1001@sip.example.com:5060", Username: "1001", Domain: "sip.example.com", Port: 5060}
	other := &Trunk{ID: 3, Name: "sipclient-mobile-sub", Username: "1001", Domain: "sip.example.com", Port: 5060}
	tm := &TrunkManager{
		trunks:      map[int64]*Trunk{1: agent, 2: device, 3: other},
		ownedLeases: map[int64]bool{1: true, 2: true, 3: true},
	}

	got := tm.IdentityGroupTrunkIDs(1)
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("expected device sibling 2, got %v", got)
	}
	got = tm.IdentityGroupTrunkIDs(2)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected agent sibling 1, got %v", got)
	}
}

type fcmTrunkTestDB struct {
	sql  string
	args []any
}

func (db *fcmTrunkTestDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.sql = sql
	db.args = append([]any(nil), args...)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (db *fcmTrunkTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}
func (db *fcmTrunkTestDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow")
}
func (db *fcmTrunkTestDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("unexpected BeginTx")
}

func TestSetAndClearTrunkFcmToken(t *testing.T) {
	t.Parallel()

	db := &fcmTrunkTestDB{}
	now := time.Now()
	tm := &TrunkManager{
		db:     db,
		trunks: map[int64]*Trunk{7: {ID: 7, UpdatedAt: now}},
	}

	if err := tm.SetTrunkFcmToken(context.Background(), 7, "  fcm-token-1 "); err != nil {
		t.Fatalf("SetTrunkFcmToken failed: %v", err)
	}
	if tm.trunks[7].FcmToken == nil || *tm.trunks[7].FcmToken != "fcm-token-1" {
		t.Fatalf("expected stored fcm token, got %#v", tm.trunks[7].FcmToken)
	}
	if !strings.Contains(db.sql, "SET fcm_token") {
		t.Fatalf("expected fcm update, got %s", db.sql)
	}

	if err := tm.ClearTrunkFcmToken(context.Background(), 7); err != nil {
		t.Fatalf("ClearTrunkFcmToken failed: %v", err)
	}
	if tm.trunks[7].FcmToken != nil {
		t.Fatalf("expected cleared fcm token")
	}
	if !strings.Contains(db.sql, "fcm_token = NULL") {
		t.Fatalf("expected fcm clear, got %s", db.sql)
	}
}

func TestCollapseAgentIdentityGroupDoesNotAmbiguate(t *testing.T) {
	t.Parallel()

	agent := &Trunk{ID: 4, Name: "sipclient-agent-1001@sip.example.com:5060", Username: "1001", Domain: "sip.example.com", Port: 5060}
	device := &Trunk{ID: 9, Name: "sipclient-agent-device-1001@sip.example.com:5060", Username: "1001", Domain: "sip.example.com", Port: 5060}
	result := selectSingleMatch([]*Trunk{agent, device}, "ruri_user_domain_port")
	if result.Ambiguous || result.Trunk == nil {
		t.Fatalf("expected collapsed match, got %#v", result)
	}
	if result.Trunk.ID != 9 {
		t.Fatalf("expected device canonical trunk, got %d", result.Trunk.ID)
	}
	if len(result.CandidateIDs) != 2 {
		t.Fatalf("expected both candidate ids retained, got %v", result.CandidateIDs)
	}
}
