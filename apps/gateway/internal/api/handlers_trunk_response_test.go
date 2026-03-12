package api

import (
	"testing"
	"time"

	"k2-gateway/internal/sip"
)

func TestTrunkResponseFrom_IncludesPublicIDBothFields(t *testing.T) {
	now := time.Now()
	trunk := &sip.Trunk{
		ID:               7,
		PublicID:         "e1f7d53d-e06d-4b77-9f78-f04ece6d21a7",
		Name:             "Main",
		Domain:           "sip.example.com",
		Port:             5060,
		Username:         "1001",
		Transport:        "tcp",
		Enabled:          true,
		IsDefault:        false,
		LastRegisteredAt: &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	resp := trunkResponseFrom(trunk, 2, []string{"0891112222", "021234567"})
	if resp.PublicID != trunk.PublicID {
		t.Fatalf("expected public_id=%s, got %s", trunk.PublicID, resp.PublicID)
	}
	if resp.PublicIDCompat != trunk.PublicID {
		t.Fatalf("expected publicId=%s, got %s", trunk.PublicID, resp.PublicIDCompat)
	}
	if !resp.IsRegistered {
		t.Fatalf("expected IsRegistered=true when lastRegisteredAt exists")
	}
	if len(resp.ActiveDestinations) != 2 {
		t.Fatalf("expected 2 active destinations, got %d", len(resp.ActiveDestinations))
	}
	if resp.ActiveDestinations[0] != "0891112222" {
		t.Fatalf("unexpected first destination: %s", resp.ActiveDestinations[0])
	}
}
