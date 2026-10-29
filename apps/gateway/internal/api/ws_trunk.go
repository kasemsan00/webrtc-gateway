package api

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"webrtc-sip-gateway/internal/auth"
	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/sip"
)

const (
	trunkPNType = "apple"
)

func normalizeTrunkPNAppID(appID string) string {
	value := strings.TrimSpace(appID)
	if value == "" {
		return config.DefaultTrunkPNAppID
	}
	return value
}

func hasTrunkPushContact(msg WSMessage) bool {
	return strings.TrimSpace(msg.PNAppID) != "" ||
		strings.TrimSpace(msg.PNType) != "" ||
		strings.TrimSpace(msg.PNToken) != ""
}

func validateTrunkPushContact(msg WSMessage, expectedAppID string) (sip.TrunkPushContact, error) {
	contact := sip.TrunkPushContact{
		PNAppID: strings.TrimSpace(msg.PNAppID),
		PNType:  strings.TrimSpace(msg.PNType),
		PNToken: strings.TrimSpace(msg.PNToken),
	}
	expectedAppID = normalizeTrunkPNAppID(expectedAppID)
	if contact.PNAppID != expectedAppID {
		return contact, fmt.Errorf("pnAppId must be %s", expectedAppID)
	}
	if contact.PNType != trunkPNType {
		return contact, fmt.Errorf("pnType must be %s", trunkPNType)
	}
	if len(contact.PNToken) < 32 || len(contact.PNToken) > 256 {
		return contact, fmt.Errorf("pnToken length is invalid")
	}
	for _, r := range contact.PNToken {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return contact, fmt.Errorf("pnToken must be hex")
		}
	}
	return contact, nil
}

func (s *Server) updateTrunkPushContact(ctx context.Context, client *WSClient, trunkID int64, msg WSMessage) error {
	if s.trunkManager == nil {
		return fmt.Errorf("trunk manager not available")
	}
	if client == nil || client.authClaims == nil || strings.TrimSpace(client.authClaims.Subject) == "" {
		return fmt.Errorf("authenticated client required for trunk push token update")
	}

	contact, err := validateTrunkPushContact(msg, s.config.TrunkPNAppID)
	if err != nil {
		return err
	}
	changed, err := s.trunkManager.SetTrunkPushContact(ctx, trunkID, contact)
	if err != nil {
		return err
	}
	if !changed {
		log.Printf("📲 Trunk push contact unchanged: trunkID=%d appID=%s pnType=%s action=unchanged_skip_reregister", trunkID, contact.PNAppID, contact.PNType)
		return nil
	}
	if err := s.trunkManager.RegisterTrunk(trunkID, true); err != nil {
		return fmt.Errorf("trunk push token persisted but re-register failed: %w", err)
	}
	log.Printf("📲 Trunk push contact updated: trunkID=%d appID=%s pnType=%s action=updated_and_reregistered", trunkID, contact.PNAppID, contact.PNType)
	return nil
}

func (s *Server) handleWSTrunkPushToken(client *WSClient, msg WSMessage) {
	ctx := context.Background()
	if _, ok := normalizeDevicePlatform(msg.DevicePlatform); !ok {
		s.sendWSError(client, msg.SessionID, "Invalid devicePlatform")
		return
	}
	if s.trunkManager == nil {
		s.sendWSError(client, msg.SessionID, "Trunk manager not available")
		return
	}
	if !client.trunkResolved || client.resolvedTrunkID <= 0 {
		s.sendWSError(client, msg.SessionID, "Trunk must be resolved before updating push token")
		return
	}

	targetTrunkID := msg.TrunkID
	if targetTrunkID == 0 && strings.TrimSpace(msg.TrunkPublicID) != "" {
		publicID, ok := sip.NormalizeTrunkPublicID(msg.TrunkPublicID)
		if !ok {
			s.sendWSError(client, msg.SessionID, "Invalid trunkPublicId")
			return
		}
		resolvedID, ok := s.trunkManager.GetTrunkIDByPublicID(publicID)
		if !ok {
			s.sendWSError(client, msg.SessionID, "Trunk not found")
			return
		}
		targetTrunkID = resolvedID
	}
	if targetTrunkID == 0 {
		targetTrunkID = client.resolvedTrunkID
	}
	if targetTrunkID != client.resolvedTrunkID {
		s.sendWSError(client, msg.SessionID, "Push token trunk mismatch")
		return
	}

	if err := s.updateTrunkPushContact(ctx, client, targetTrunkID, msg); err != nil {
		s.sendWSError(client, msg.SessionID, err.Error())
		return
	}
}

// handleWSTrunkResolve resolves trunk ownership/route from either credentials or trunk ID/public ID.
func (s *Server) handleWSTrunkResolve(client *WSClient, msg WSMessage) {
	ctx := context.Background()
	devicePlatform, ok := normalizeDevicePlatform(msg.DevicePlatform)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Invalid devicePlatform")
		return
	}
	trunkID := int64(0)
	trunkPublicID := ""
	sipUsername := strings.TrimSpace(msg.SIPUsername)
	resolveBy := "credentials"
	var leaseOwner *string
	var leaseUntil *time.Time
	found := false

	if msg.TrunkID > 0 || strings.TrimSpace(msg.TrunkPublicID) != "" {
		if s.trunkManager == nil {
			s.sendWSError(client, msg.SessionID, "Trunk manager not available")
			return
		}

		if msg.TrunkID > 0 {
			trunkID = msg.TrunkID
			resolveBy = "trunkId"
		} else {
			resolveBy = "trunkPublicId"
			publicID, ok := sip.NormalizeTrunkPublicID(msg.TrunkPublicID)
			if !ok {
				reason := "Invalid trunkPublicId"
				s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
				return
			}
			resolvedID, ok := s.trunkManager.GetTrunkIDByPublicID(publicID)
			if !ok {
				reason := "No matching trunk ID/public ID"
				s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
				return
			}
			trunkID = resolvedID
		}

		trunk, err := s.trunkManager.GetTrunkByIDFromDB(ctx, trunkID)
		if err != nil || trunk == nil {
			reason := "No matching trunk ID/public ID"
			s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
			return
		}

		found = true
		leaseOwner = trunk.LeaseOwner
		leaseUntil = trunk.LeaseUntil
		trunkPublicID = trunk.PublicID
		sipUsername = strings.TrimSpace(trunk.Username)
	} else {
		if msg.SIPDomain == "" || msg.SIPUsername == "" || msg.SIPPassword == "" {
			s.sendWSError(client, msg.SessionID, "sipDomain, sipUsername, and sipPassword are required")
			return
		}
		// Use port as-is from client (0 means "not specified" for hostname domains)
		// This allows DNS SRV resolution for hostnames without explicit port
		port := msg.SIPPort

		if s.logStore == nil {
			s.sendWSError(client, msg.SessionID, "LogStore not available")
			return
		}

		var err error
		trunkID, leaseOwner, leaseUntil, found, err = s.logStore.ResolveTrunkByCredentials(ctx, msg.SIPDomain, port, msg.SIPUsername, msg.SIPPassword)
		if err != nil {
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to resolve trunk: %v", err))
			return
		}
		if !found {
			reason := "No matching trunk credentials"
			s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
			s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
			return
		}

		if s.trunkManager != nil {
			if trunk, getErr := s.trunkManager.GetTrunkByIDFromDB(ctx, trunkID); getErr == nil && trunk != nil {
				trunkPublicID = trunk.PublicID
				sipUsername = strings.TrimSpace(trunk.Username)
			}
		}
	}

	if !found {
		reason := "No matching trunk"
		s.sendWSMessage(client, WSMessage{Type: "trunk_not_found", Reason: reason})
		s.sendWSError(client, msg.SessionID, fmt.Sprintf("Trunk not found: %s", reason))
		return
	}

	if leaseOwner == nil || *leaseOwner == "" || leaseUntil == nil || leaseUntil.Before(time.Now()) {
		s.sendWSMessage(client, WSMessage{Type: "trunk_not_ready", Reason: "Trunk lease not active"})
		return
	}

	if *leaseOwner == s.gatewayConfig.InstanceID {
		s.mu.Lock()
		client.trunkResolved = true
		client.resolvedTrunkID = trunkID
		s.mu.Unlock()
		s.notifyWSClientChanged("updated", client)

		// Persist Keycloak sub (UUID) for offline push notifications.
		if client.authClaims != nil && s.trunkManager != nil {
			if sub := client.authClaims.Subject; sub != "" {
				preferredUsername := strings.TrimSpace(client.authClaims.PreferredUsername)
				var platformPtr *string
				if devicePlatform != "" {
					platformPtr = &devicePlatform
				}
				if err := s.trunkManager.SetTrunkNotifyUserIDAndPlatform(ctx, trunkID, &sub, platformPtr); err != nil {
					log.Printf(
						"⚠️ [Trunk NotifyUserID] update_failed sessionID=%s trunkID=%d trunkPublicID=%s preferredUsername=%s sipUsername=%s authSub=%s error=%v",
						msg.SessionID,
						trunkID,
						trunkPublicID,
						preferredUsername,
						sipUsername,
						sub,
						err,
					)
				} else {
					log.Printf(
						"📝 [Trunk NotifyUserID] updated sessionID=%s trunkID=%d trunkPublicID=%s preferredUsername=%s sipUsername=%s authSub=%s",
						msg.SessionID,
						trunkID,
						trunkPublicID,
						preferredUsername,
						sipUsername,
						sub,
					)
				}
			}
		}
		if hasTrunkPushContact(msg) {
			if err := s.updateTrunkPushContact(ctx, client, trunkID, msg); err != nil {
				log.Printf("⚠️ Failed to update push contact for trunk %d: %v", trunkID, err)
				s.sendWSError(client, msg.SessionID, err.Error())
			}
		}
		if client.authClaims != nil &&
			client.authClaims.Realm == auth.TokenRealmUser &&
			strings.TrimSpace(client.authClaims.Subject) != "" {
			log.Printf(
				"📱 [WS Trunk Resolve] mobile_resolved sessionID=%s trunkID=%d trunkPublicID=%s authSub=%s resolveBy=%s",
				msg.SessionID,
				trunkID,
				trunkPublicID,
				client.authClaims.Subject,
				resolveBy,
			)
		}

		s.sendWSMessage(client, WSMessage{
			Type:          "trunk_resolved",
			TrunkID:       trunkID,
			TrunkPublicID: trunkPublicID,
		})
		s.notifyPendingIncomingForClient(client, trunkID)
		return
	}

	if s.logStore == nil {
		s.sendWSError(client, msg.SessionID, "LogStore not available")
		return
	}

	// Not owned by this instance - redirect to owner
	wsURL, found, err := s.logStore.LookupGatewayInstance(ctx, *leaseOwner)
	if err != nil {
		s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to lookup gateway instance: %v", err))
		return
	}
	if !found || wsURL == "" {
		s.sendWSMessage(client, WSMessage{Type: "trunk_not_ready", Reason: "Owner instance not discoverable"})
		return
	}

	s.sendWSMessage(client, WSMessage{Type: "trunk_redirect", RedirectURL: wsURL})
}
