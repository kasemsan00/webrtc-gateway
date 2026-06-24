package api

import (
	"context"
	"fmt"
	"strings"

	"k2-gateway/internal/auth"
	"k2-gateway/internal/sip"
	"k2-gateway/internal/sipclientauth"
)

// MobileSIPProvisionResult identifies the trunk provisioned for a mobile client.
type MobileSIPProvisionResult struct {
	TrunkID       int64
	TrunkPublicID string
}

// MobileSIPProvisioner provisions SIP credentials for authenticated mobile users.
type MobileSIPProvisioner interface {
	ProvisionMobileSIPTrunk(ctx context.Context, rawToken string, claims *auth.VerifiedClaims, devicePlatform string) (*MobileSIPProvisionResult, error)
}

type mobileSIPAuthClient interface {
	RegisterMobile(ctx context.Context, token string) (*sipclientauth.Account, error)
}

type mobileSIPTrunkManager interface {
	UpsertMobileTrunk(ctx context.Context, payload sip.MobileTrunkPayload) (*sip.Trunk, error)
	SetTrunkNotifyUserIDAndPlatform(ctx context.Context, trunkID int64, userID *string, platform *string) error
	RegisterTrunk(trunkID int64, force bool) error
}

type mobileSIPProvisioner struct {
	authClient   mobileSIPAuthClient
	trunkManager mobileSIPTrunkManager
}

// NewMobileSIPProvisioner creates the default mobile SIP provisioner.
func NewMobileSIPProvisioner(authClient mobileSIPAuthClient, trunkManager mobileSIPTrunkManager) MobileSIPProvisioner {
	if authClient == nil || trunkManager == nil {
		return nil
	}
	return &mobileSIPProvisioner{
		authClient:   authClient,
		trunkManager: trunkManager,
	}
}

func (p *mobileSIPProvisioner) ProvisionMobileSIPTrunk(ctx context.Context, rawToken string, claims *auth.VerifiedClaims, devicePlatform string) (*MobileSIPProvisionResult, error) {
	if claims == nil || strings.TrimSpace(claims.Subject) == "" {
		return nil, fmt.Errorf("authenticated subject is required")
	}
	devicePlatform = strings.TrimSpace(devicePlatform)
	if devicePlatform == "" {
		return nil, fmt.Errorf("device platform is required")
	}
	account, err := p.authClient.RegisterMobile(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("register mobile sip account: %w", err)
	}

	trunk, err := p.trunkManager.UpsertMobileTrunk(ctx, sip.MobileTrunkPayload{
		Subject:  claims.Subject,
		Domain:   account.Domain,
		Username: account.Extension,
		Password: account.Secret,
	})
	if err != nil {
		return nil, fmt.Errorf("persist mobile sip trunk: %w", err)
	}
	if trunk == nil || trunk.ID <= 0 {
		return nil, fmt.Errorf("persist mobile sip trunk returned no trunk")
	}
	subject := strings.TrimSpace(claims.Subject)
	if err := p.trunkManager.SetTrunkNotifyUserIDAndPlatform(ctx, trunk.ID, &subject, &devicePlatform); err != nil {
		return nil, fmt.Errorf("bind mobile sip trunk notification target: %w", err)
	}
	if err := p.trunkManager.RegisterTrunk(trunk.ID, true); err != nil {
		return nil, fmt.Errorf("register mobile sip trunk: %w", err)
	}

	return &MobileSIPProvisionResult{
		TrunkID:       trunk.ID,
		TrunkPublicID: trunk.PublicID,
	}, nil
}
