package push

import (
	"context"
	"log"
	"time"
)

const (
	// pushServiceID is the TTRS notification service_id used for FCM push.
	pushServiceID = "4"

	// pushTimeout is the maximum wall-clock time for the full push flow
	// (TTRS API fetch + FCM send per token).
	pushTimeout = 10 * time.Second
)

// Service orchestrates fetching notification tokens from TTRS and sending FCM pushes.
type Service struct {
	ttrs *TTRSClient
	fcm  *FCMSender
}

// NewService creates a push notification Service.
func NewService(ttrs *TTRSClient, fcm *FCMSender) *Service {
	return &Service{ttrs: ttrs, fcm: fcm}
}

// NotifyIncomingCall fetches the user's FCM tokens from TTRS and sends a push for each.
// Errors are logged but never returned — push is best-effort and must not block the call flow.
func (s *Service) NotifyIncomingCall(userID, sessionID, from, to string) {
	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()

	log.Printf("🔔 [Push] Start incoming call push: userID=%s sessionID=%s", userID, sessionID)

	entries, err := s.ttrs.FetchNotifications(ctx, userID)
	if err != nil {
		log.Printf("🔔 [Push] Failed to fetch notifications for user %s: %v", userID, err)
		return
	}

	tokens := FilterByServiceID(entries, pushServiceID)
	if len(tokens) == 0 {
		log.Printf("🔔 [Push] No service_id=%s tokens found for user %s", pushServiceID, userID)
		return
	}
	log.Printf("🔔 [Push] Found %d token(s) for user %s (service_id=%s)", len(tokens), userID, pushServiceID)

	data := map[string]string{
		"type":      "incoming_call",
		"sessionId": sessionID,
		"from":      from,
		"to":        to,
	}

	sent := 0
	for _, entry := range tokens {
		if err := s.fcm.SendPush(ctx, entry.Token, data); err != nil {
			log.Printf("🔔 [Push] FCM send failed for user %s device %s: %v", userID, entry.MobileDevice, err)
			continue
		}
		sent++
		log.Printf("🔔 [Push] FCM sent to user %s device %s (service_id=%s)", userID, entry.MobileDevice, pushServiceID)
	}

	log.Printf("🔔 [Push] Incoming call push summary: userID=%s sessionID=%s sent=%d/%d", userID, sessionID, sent, len(tokens))
}
