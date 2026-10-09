package push

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrPushNotConfigured is returned when FCM or TTRS is not wired.
	ErrPushNotConfigured = errors.New("push sender not configured")
	// ErrEmptyPushToken is returned when a stored token is blank.
	ErrEmptyPushToken = errors.New("empty push token")
	// ErrNoTTRSTokens is returned when TTRS has no service_id=4 tokens.
	ErrNoTTRSTokens = errors.New("no ttrs notification tokens")
)

const (
	// pushServiceID is the TTRS notification service_id used for FCM push.
	pushServiceID = "4"

	// pushTimeout is the maximum wall-clock time for the full push flow
	// (TTRS API fetch + FCM send per token).
	pushTimeout = 10 * time.Second

	incomingRingTimeoutSeconds = 30
)

const (
	TestPushStyleData    = "data"
	TestPushStyleMessage = "message"
)

// FCMClient is the send contract used by incoming-call FCM.
type FCMClient interface {
	SendPush(ctx context.Context, token, title, notificationBody string, data map[string]string, mobileDevice string) error
	SendDisplayPush(ctx context.Context, token, title, notificationBody string, data map[string]string, mobileDevice string) error
}

// NormalizeTestPushStyle accepts data or message. Empty defaults to data.
func NormalizeTestPushStyle(style string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "", TestPushStyleData:
		return TestPushStyleData, nil
	case TestPushStyleMessage:
		return TestPushStyleMessage, nil
	default:
		return "", fmt.Errorf("style must be data or message")
	}
}

// Service orchestrates fetching notification tokens from TTRS and sending FCM pushes.
type Service struct {
	ttrs *TTRSClient
	fcm  FCMClient
	apns *APNSSender
}

// NewService creates a push notification Service.
func NewService(ttrs *TTRSClient, fcm *FCMSender, apns ...*APNSSender) *Service {
	var client FCMClient
	if fcm != nil {
		client = fcm
	}
	var apnsSender *APNSSender
	if len(apns) > 0 {
		apnsSender = apns[0]
	}
	return &Service{ttrs: ttrs, fcm: client, apns: apnsSender}
}

// NewServiceWithFCM builds a Service with an injectable FCM client (tests).
func NewServiceWithFCM(ttrs *TTRSClient, fcm FCMClient, apns *APNSSender) *Service {
	return &Service{ttrs: ttrs, fcm: fcm, apns: apns}
}

// HasFCM reports whether an FCM sender is configured.
func (s *Service) HasFCM() bool {
	return s != nil && s.fcm != nil
}

// HasTTRS reports whether TTRS token lookup is configured.
func (s *Service) HasTTRS() bool {
	return s != nil && s.ttrs != nil && s.fcm != nil
}

// CanSendAPNS reports whether APNs VoIP pushes are configured.
func (s *Service) CanSendAPNS() bool {
	return s != nil && s.apns != nil
}

// NotifyIncomingCall fetches the user's FCM tokens from TTRS and sends a push for each.
// Errors are logged but never returned — push is best-effort and must not block the call flow.
func (s *Service) NotifyIncomingCall(userID, sessionID, from, to string, hasVideo bool) {
	if s == nil || s.ttrs == nil || s.fcm == nil {
		log.Printf("🔔 [Push] FCM fallback skipped: sender not configured (userID=%s sessionID=%s)", userID, sessionID)
		return
	}
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
		data := buildIncomingCallPushData(sessionID, from, to, entry.MobileDevice, time.Now().UTC(), hasVideo)
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

// NotifyIncomingCallFCMToken sends an incoming-call FCM using a gateway-stored token.
// It does not look up TTRS notification tokens and must not log the FCM token.
func (s *Service) NotifyIncomingCallFCMToken(token, sessionID, from, to string, hasVideo bool) {
	if err := s.SendIncomingCallFCMToken(token, sessionID, from, to, hasVideo); err != nil {
		switch {
		case errors.Is(err, ErrPushNotConfigured):
			log.Printf("🔔 [Push] Stored FCM skipped: sender not configured (sessionID=%s)", sessionID)
		case errors.Is(err, ErrEmptyPushToken):
			log.Printf("🔔 [Push] Stored FCM skipped: empty token (sessionID=%s)", sessionID)
		default:
			log.Printf("🔔 [Push] Stored FCM send failed: sessionID=%s err=%v", sessionID, err)
		}
		return
	}
	log.Printf("🔔 [Push] Stored FCM sent: sessionID=%s", sessionID)
}

// SendIncomingCallFCMToken sends stored-token FCM and returns provider errors.
func (s *Service) SendIncomingCallFCMToken(token, sessionID, from, to string, hasVideo bool) error {
	if s == nil || s.fcm == nil {
		return ErrPushNotConfigured
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrEmptyPushToken
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()
	return s.sendStoredFCM(ctx, token, sessionID, from, to, hasVideo, TestPushStyleData)
}

// SendIncomingCallFCMTokenStyled sends stored-token FCM as data-only or visible message.
func (s *Service) SendIncomingCallFCMTokenStyled(token, sessionID, from, to string, hasVideo bool, style string) error {
	if s == nil || s.fcm == nil {
		return ErrPushNotConfigured
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrEmptyPushToken
	}
	normalized, err := NormalizeTestPushStyle(style)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()
	return s.sendStoredFCM(ctx, token, sessionID, from, to, hasVideo, normalized)
}

func (s *Service) sendStoredFCM(ctx context.Context, token, sessionID, from, to string, hasVideo bool, style string) error {
	title := "SoftPhone Notification"
	body := "You have an incoming call from " + from
	data := buildIncomingCallPushData(sessionID, from, to, "android_agent_device", time.Now().UTC(), hasVideo)
	if style == TestPushStyleMessage {
		return s.fcm.SendDisplayPush(ctx, token, title, body, data, "android_agent_device")
	}
	return s.fcm.SendPush(ctx, token, title, body, data, "android_agent_device")
}

// SendIncomingCall looks up TTRS tokens and sends FCM for each. It returns an
// error when none are sent so admin test APIs can surface the outcome.
func (s *Service) SendIncomingCall(userID, sessionID, from, to string, hasVideo bool) error {
	return s.SendIncomingCallStyled(userID, sessionID, from, to, hasVideo, TestPushStyleData)
}

// SendIncomingCallStyled looks up TTRS tokens and sends FCM as data or message.
func (s *Service) SendIncomingCallStyled(userID, sessionID, from, to string, hasVideo bool, style string) error {
	if s == nil || s.ttrs == nil || s.fcm == nil {
		return ErrPushNotConfigured
	}
	normalized, err := NormalizeTestPushStyle(style)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()

	entries, err := s.ttrs.FetchNotifications(ctx, userID)
	if err != nil {
		return err
	}
	tokens := FilterByServiceID(entries, pushServiceID)
	if len(tokens) == 0 {
		return ErrNoTTRSTokens
	}

	title := "SoftPhone Notification"
	body := "You have an incoming call from " + from
	sent := 0
	var lastErr error
	for _, entry := range tokens {
		data := buildIncomingCallPushData(sessionID, from, to, entry.MobileDevice, time.Now().UTC(), hasVideo)
		var sendErr error
		if normalized == TestPushStyleMessage {
			sendErr = s.fcm.SendDisplayPush(ctx, entry.Token, title, body, data, entry.MobileDevice)
		} else {
			sendErr = s.fcm.SendPush(ctx, entry.Token, title, body, data, entry.MobileDevice)
		}
		if sendErr != nil {
			lastErr = sendErr
			continue
		}
		sent++
	}
	if sent == 0 {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("fcm send failed for all tokens")
	}
	return nil
}

// NotifyIncomingCallAPNS sends a PushKit VoIP push to the stored iOS token.
func (s *Service) NotifyIncomingCallAPNS(token, sessionID, from, to string, hasVideo bool) {
	if err := s.SendIncomingCallAPNS(token, sessionID, from, to, hasVideo); err != nil {
		if errors.Is(err, ErrPushNotConfigured) {
			return
		}
		log.Printf("🔔 [PushKit] APNs send failed: sessionID=%s err=%v", sessionID, err)
		return
	}
	log.Printf("🔔 [PushKit] APNs VoIP push sent: sessionID=%s", sessionID)
}

// SendIncomingCallAPNS sends a PushKit VoIP push and returns provider errors.
func (s *Service) SendIncomingCallAPNS(token, sessionID, from, to string, hasVideo bool) error {
	if s == nil || s.apns == nil {
		return ErrPushNotConfigured
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()

	data := buildIncomingCallPushData(sessionID, from, to, "ios_pushkit", time.Now().UTC(), hasVideo)
	return s.apns.SendVoIPPush(ctx, token, data)
}

func buildIncomingCallPushData(sessionID, from, to, mobileDevice string, now time.Time, hasVideo bool) map[string]string {
	return map[string]string{
		"type":      "incoming_call",
		"sessionId": sessionID,
		// FCM data payload has reserved keys (e.g. "from"), so use custom names.
		"caller":             from,
		"callee":             to,
		"expiresAt":          now.Add(time.Duration(incomingRingTimeoutSeconds) * time.Second).Format(time.RFC3339),
		"ringTimeoutSeconds": "30",
		"platform":           pushPlatformFromMobileDevice(mobileDevice),
		"hasVideo":           strconv.FormatBool(hasVideo),
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
