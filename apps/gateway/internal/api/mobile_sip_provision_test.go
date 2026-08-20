package api

import (
	"context"
	"testing"

	"webrtc-sip-gateway/internal/auth"
	"webrtc-sip-gateway/internal/sip"
	"webrtc-sip-gateway/internal/sipclientauth"
)

type mobileProvisionAuthStub struct {
	account *sipclientauth.Account
}

func (s mobileProvisionAuthStub) RegisterMobile(context.Context, string) (*sipclientauth.Account, error) {
	return s.account, nil
}

type mobileProvisionTrunkStub struct {
	upsertPayload  sip.MobileTrunkPayload
	notifyTrunkID  int64
	notifyUserID   *string
	notifyPlatform *string
	registerID     int64
	calls          []string
}

func (s *mobileProvisionTrunkStub) UpsertMobileTrunk(_ context.Context, payload sip.MobileTrunkPayload) (*sip.Trunk, error) {
	s.upsertPayload = payload
	s.calls = append(s.calls, "upsert")
	return &sip.Trunk{ID: 42, PublicID: "public-42"}, nil
}

func (s *mobileProvisionTrunkStub) SetTrunkNotifyUserIDAndPlatform(_ context.Context, trunkID int64, userID *string, platform *string) error {
	s.notifyTrunkID = trunkID
	if userID != nil {
		copied := *userID
		s.notifyUserID = &copied
	}
	if platform != nil {
		copied := *platform
		s.notifyPlatform = &copied
	}
	s.calls = append(s.calls, "notify")
	return nil
}

func (s *mobileProvisionTrunkStub) RegisterTrunk(trunkID int64, force bool) error {
	s.registerID = trunkID
	s.calls = append(s.calls, "register")
	return nil
}

func TestMobileSIPProvisionerBindsVerifiedSubjectAndPlatformBeforeRegister(t *testing.T) {
	trunks := &mobileProvisionTrunkStub{}
	provisioner := NewMobileSIPProvisioner(
		mobileProvisionAuthStub{account: &sipclientauth.Account{
			Domain:    "sip.example.com",
			Extension: "1001",
			Secret:    "secret",
		}},
		trunks,
	)

	result, err := provisioner.ProvisionMobileSIPTrunk(context.Background(), "jwt-token", &auth.VerifiedClaims{
		Subject: "user-sub-1",
		Realm:   auth.TokenRealmUser,
	}, "ios")
	if err != nil {
		t.Fatalf("ProvisionMobileSIPTrunk failed: %v", err)
	}
	if result.TrunkID != 42 || result.TrunkPublicID != "public-42" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if trunks.upsertPayload.Subject != "user-sub-1" || trunks.upsertPayload.Domain != "sip.example.com" || trunks.upsertPayload.Username != "1001" || trunks.upsertPayload.Password != "secret" {
		t.Fatalf("unexpected upsert payload: %+v", trunks.upsertPayload)
	}
	if trunks.notifyTrunkID != 42 || trunks.notifyUserID == nil || *trunks.notifyUserID != "user-sub-1" {
		t.Fatalf("expected notify binding for user-sub-1 on trunk 42, got trunk=%d user=%v", trunks.notifyTrunkID, trunks.notifyUserID)
	}
	if trunks.notifyPlatform == nil || *trunks.notifyPlatform != "ios" {
		t.Fatalf("expected ios platform binding, got %v", trunks.notifyPlatform)
	}
	if trunks.registerID != 42 {
		t.Fatalf("expected register trunk 42, got %d", trunks.registerID)
	}
	wantCalls := []string{"upsert", "notify", "register"}
	for i, want := range wantCalls {
		if len(trunks.calls) <= i || trunks.calls[i] != want {
			t.Fatalf("expected call order %v, got %v", wantCalls, trunks.calls)
		}
	}
}
