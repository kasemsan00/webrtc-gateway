package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"webrtc-sip-gateway/internal/auth"
	"webrtc-sip-gateway/internal/sip"
)

func (s *Server) unregisterError(client *WSClient, msg WSMessage, err error) {
	s.sendWSMessage(client, WSMessage{Type: "error", Operation: "unregister", RequestID: msg.RequestID, Error: err.Error()})
}

// Reserve an account operation without holding a mutex during network I/O.
func (s *Server) reserveMobileAccount(subject string) (func(), bool) {
	s.mu.Lock()
	if s.mobileOps == nil {
		s.mobileOps = make(map[string]bool)
	}
	if s.mobileOps[subject] {
		s.mu.Unlock()
		return nil, false
	}
	s.mobileOps[subject] = true
	s.mu.Unlock()
	return func() { s.mu.Lock(); delete(s.mobileOps, subject); s.mu.Unlock() }, true
}

// Token clients can only release the trunk provisioned for their verified subject.
// Caller-supplied trunk IDs and subsequently resolved trunks confer no logout authority.
func (s *Server) handleWSUnregister(client *WSClient, msg WSMessage) {
	if client == nil {
		return
	}
	if client.agentDeviceOnly {
		s.handleWSDeviceUnregister(client, msg)
		return
	}
	if client.publicOnly || client.agentOnly || client.authClaims == nil || client.authClaims.Realm != auth.TokenRealmUser || strings.TrimSpace(client.authClaims.Subject) == "" {
		s.unregisterError(client, msg, fmt.Errorf("unregister requires an authenticated mobile connection"))
		return
	}
	releaseAccount, reserved := s.reserveMobileAccount(client.authClaims.Subject)
	if !reserved {
		s.unregisterError(client, msg, fmt.Errorf("another account operation is in progress; retry unregister"))
		return
	}
	defer releaseAccount()
	s.mu.RLock()
	trunkID := client.mobileTrunkID
	s.mu.RUnlock()
	if trunkID <= 0 || s.trunkManager == nil {
		s.unregisterError(client, msg, fmt.Errorf("mobile trunk provisioning is required before unregister"))
		return
	}
	unlock := s.lockAgentTrunkOperation(trunkID)
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	trunk, err := s.trunkManager.GetTrunkByIDFromDB(ctx, trunkID)
	expectedName, nameErr := sip.BuildMobileTrunkName(client.authClaims.Subject)
	if err != nil || nameErr != nil || trunk == nil || trunk.Name != expectedName {
		s.unregisterError(client, msg, fmt.Errorf("mobile trunk ownership could not be verified"))
		return
	}
	s.mu.RLock()
	shared := false
	for other := range s.wsConnections {
		if other != client && (other.mobileTrunkID == trunkID || (other.trunkResolved && other.resolvedTrunkID == trunkID)) {
			shared = true
			break
		}
	}
	s.mu.RUnlock()
	if shared {
		s.unregisterError(client, msg, fmt.Errorf("registration is in use by another connected device; disconnect the other device before retrying"))
		return
	}
	// Keep the authenticated provisioning target for idempotent retry after partial failure.
	if err := s.releaseRegisteredTrunk(ctx, trunkID); err != nil {
		s.unregisterError(client, msg, err)
		return
	}
	for _, id := range s.ownedClientSessionIDs(client) {
		s.unbindClientSession(client, id)
		if s.sessionMgr != nil {
			if sess, ok := s.sessionMgr.GetSession(id); ok {
				s.forceEndSession(sess, "mobile_unregister")
			}
		}
	}
	s.mu.Lock()
	client.trunkResolved = false
	client.resolvedTrunkID = 0
	s.mu.Unlock()
	s.notifyWSClientChanged("updated", client)
	s.sendWSMessage(client, WSMessage{Type: "unregistered", RequestID: msg.RequestID})
}

// Acknowledgement requires successful SIP release and notification cleanup.
// The caller serializes this operation with registration for the same trunk.
func (s *Server) releaseRegisteredTrunk(ctx context.Context, trunkID int64) error {
	if err := s.trunkManager.UnregisterTrunk(trunkID, true); err != nil {
		return fmt.Errorf("SIP unregister failed: %w", err)
	}
	if err := s.trunkManager.ClearTrunkFcmToken(ctx, trunkID); err != nil {
		return fmt.Errorf("push cleanup failed: %w", err)
	}
	if err := s.trunkManager.SetTrunkNotifyUserID(ctx, trunkID, nil); err != nil {
		return fmt.Errorf("notification cleanup failed: %w", err)
	}
	return nil
}
