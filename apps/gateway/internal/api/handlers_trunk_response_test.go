package api

import (
	"testing"
	"time"

	"webrtc-sip-gateway/internal/sip"
)

func TestTrunkResponseFrom_IncludesPublicIDBothFields(t *testing.T) {
	now := time.Now()
	inUseBy := "alice"
	pnAppID := "th.or.ttrs.video.prod"
	pnType := "apple"
	pnToken := "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"
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
		InUseBy:          &inUseBy,
		LastRegisteredAt: &now,
		PNAppID:          &pnAppID,
		PNType:           &pnType,
		PNToken:          &pnToken,
		PNUpdatedAt:      &now,
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
	if resp.InUseBy == nil || *resp.InUseBy != inUseBy {
		t.Fatalf("expected inUseBy=%s, got %v", inUseBy, resp.InUseBy)
	}
	if !resp.PushContactReady || resp.PNAppID != pnAppID || resp.PNType != pnType {
		t.Fatalf("unexpected push contact fields: %+v", resp)
	}
	if resp.PNTokenMasked != "D6F5DF...A74CCF" {
		t.Fatalf("expected masked token, got %q", resp.PNTokenMasked)
	}
	if resp.PNTokenMasked == pnToken {
		t.Fatalf("full pn_token leaked in response")
	}
}

func TestTrunkResponseFrom_IncludesLastUnregisteredAt(t *testing.T) {
	now := time.Now()
	unregisteredAt := now.Add(-time.Hour)
	trunk := &sip.Trunk{
		ID:                 3,
		PublicID:           "public-3",
		Name:               "Backup",
		Domain:             "sip.example.com",
		Port:               5060,
		Username:           "1002",
		Transport:          "tcp",
		Enabled:            true,
		SipAutoRegister:    false,
		LastUnregisteredAt: &unregisteredAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	resp := trunkResponseFrom(trunk, 0, nil)
	if resp.IsRegistered {
		t.Fatalf("expected IsRegistered=false when lastRegisteredAt is nil")
	}
	if resp.LastUnregisteredAt != unregisteredAt.Format(time.RFC3339) {
		t.Fatalf("expected lastUnregisteredAt=%s, got %s", unregisteredAt.Format(time.RFC3339), resp.LastUnregisteredAt)
	}
	if resp.SipAutoRegister {
		t.Fatalf("expected sipAutoRegister=false")
	}
}
