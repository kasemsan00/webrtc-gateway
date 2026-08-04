package sip

import (
	"context"
	"strings"
	"testing"
)

func TestBuildAgentTrunkName(t *testing.T) {
	t.Parallel()

	name, err := BuildAgentTrunkName("  SIP.Example.COM ", " 1001 ", 0)
	if err != nil {
		t.Fatalf("BuildAgentTrunkName failed: %v", err)
	}
	if name != "sipclient-agent-1001@sip.example.com:5060" {
		t.Fatalf("unexpected name %q", name)
	}

	name, err = BuildAgentTrunkName("pbx.local", "2002", 5070)
	if err != nil {
		t.Fatalf("BuildAgentTrunkName failed: %v", err)
	}
	if name != "sipclient-agent-2002@pbx.local:5070" {
		t.Fatalf("unexpected name %q", name)
	}

	if _, err := BuildAgentTrunkName(" ", "1001", 5060); err == nil {
		t.Fatalf("expected blank domain error")
	}
	if _, err := BuildAgentTrunkName("sip.example.com", " ", 5060); err == nil {
		t.Fatalf("expected blank username error")
	}
}

func TestAgentTrunkNameNeverCollidesWithMobilePrefix(t *testing.T) {
	t.Parallel()

	agentName, err := BuildAgentTrunkName("sip.example.com", "user-sub-1", 5060)
	if err != nil {
		t.Fatalf("BuildAgentTrunkName failed: %v", err)
	}
	mobileName, err := BuildMobileTrunkName("user-sub-1")
	if err != nil {
		t.Fatalf("BuildMobileTrunkName failed: %v", err)
	}
	if agentName == mobileName {
		t.Fatalf("agent and mobile names collided: %q", agentName)
	}
	if !strings.HasPrefix(agentName, "sipclient-agent-") {
		t.Fatalf("expected agent prefix, got %q", agentName)
	}
	if strings.HasPrefix(agentName, "sipclient-mobile-") {
		t.Fatalf("agent name must not use mobile prefix, got %q", agentName)
	}
	if !strings.HasPrefix(mobileName, "sipclient-mobile-") {
		t.Fatalf("expected mobile prefix, got %q", mobileName)
	}
}

func TestUpsertAgentTrunkUsesDeterministicNameAndCredentials(t *testing.T) {
	t.Parallel()

	db := &mobileTrunkTestDB{
		row: mobileTrunkTestRow{trunk: Trunk{
			ID:        77,
			PublicID:  "public-77",
			Name:      "sipclient-agent-1001@sip.example.com:5060",
			Domain:    "sip.example.com",
			Port:      5060,
			Username:  "1001",
			Password:  "secret-agent",
			Transport: "tcp",
			Enabled:   true,
		}},
	}
	tm := &TrunkManager{
		db:            db,
		trunks:        make(map[int64]*Trunk),
		trunkByPublic: make(map[string]int64),
	}

	trunk, err := tm.UpsertAgentTrunk(context.Background(), AgentTrunkPayload{
		Domain:   " SIP.Example.COM ",
		Username: " 1001 ",
		Password: " secret-agent ",
		Port:     0,
	})
	if err != nil {
		t.Fatalf("UpsertAgentTrunk failed: %v", err)
	}
	if trunk.ID != 77 || trunk.Name != "sipclient-agent-1001@sip.example.com:5060" {
		t.Fatalf("unexpected trunk %#v", trunk)
	}
	expected := []any{
		"sipclient-agent-1001@sip.example.com:5060",
		"sip.example.com",
		5060,
		"1001",
		"secret-agent",
		"tcp",
		true,
	}
	if len(db.args) < len(expected) {
		t.Fatalf("expected upsert args, got %#v", db.args)
	}
	for i, want := range expected {
		if got := db.args[i]; got != want {
			t.Fatalf("arg %d: expected %#v, got %#v", i+1, want, got)
		}
	}
	if tm.trunks[77] != trunk {
		t.Fatalf("expected in-memory cache to contain agent trunk")
	}
	if strings.Contains(db.sql, "notify_user_id = EXCLUDED.notify_user_id") {
		t.Fatalf("agent upsert should not assign notify_user_id, got SQL %s", db.sql)
	}
}
