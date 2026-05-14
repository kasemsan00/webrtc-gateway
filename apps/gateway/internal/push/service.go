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

	incomingRingTimeoutSeconds = 30
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

	title := "SoftPhone Notification"
	body := "You have an incoming call from " + from

	sent := 0
	for _, entry := range tokens {
		data := buildIncomingCallPushData(sessionID, from, to, entry.MobileDevice, time.Now().UTC())
		if err := s.fcm.SendPush(ctx, entry.Token, title, body, data, entry.MobileDevice); err != nil {
			log.Printf("🔔 [Push] FCM send failed for user %s device %s: %v", userID, entry.MobileDevice, err)
			continue
		}
		sent++
		log.Printf("🔔 [Push] FCM sent to user %s device %s (service_id=%s)", userID, entry.MobileDevice, pushServiceID)
		log.Printf("🔔 [Push] FCM token: %s", entry.Token)
		log.Printf("🔔 [Push] FCM mobile device: %s", entry.MobileDevice)
		log.Printf("🔔 [Push] FCM service ID: %s", pushServiceID)
		log.Printf("🔔 [Push] FCM user ID: %s", userID)
		log.Printf("🔔 [Push] FCM session ID: %s", sessionID)
		log.Printf("🔔 [Push] FCM sent: %d", sent)
		log.Printf("🔔 [Push] FCM tokens: %v", tokens)
	}

	log.Printf("🔔 [Push] Incoming call push summary: userID=%s sessionID=%s sent=%d/%d", userID, sessionID, sent, len(tokens))
}

func buildIncomingCallPushData(sessionID, from, to, mobileDevice string, now time.Time) map[string]string {
	return map[string]string{
		"type":      "incoming_call",
		"sessionId": sessionID,
		// FCM data payload has reserved keys (e.g. "from"), so use custom names.
		"caller":             from,
		"callee":             to,
		"expiresAt":          now.Add(time.Duration(incomingRingTimeoutSeconds) * time.Second).Format(time.RFC3339),
		"ringTimeoutSeconds": "30",
		"platform":           pushPlatformFromMobileDevice(mobileDevice),
	}
}

func pushPlatformFromMobileDevice(mobileDevice string) string {
	switch {
	case len(mobileDevice) >= 8 && mobileDevice[:8] == "android_":
		return "android"
	case len(mobileDevice) >= 4 && mobileDevice[:4] == "ios_":
		return "ios"
	default:
		return "unknown"
	}
}
