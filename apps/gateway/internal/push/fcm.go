package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	fcmScope   = "https://www.googleapis.com/auth/firebase.messaging"
	fcmBaseURL = "https://fcm.googleapis.com/v1/projects"
)

// FCMSender sends push notifications via Firebase Cloud Messaging HTTP v1 API.
type FCMSender struct {
	projectID   string
	tokenSource oauth2.TokenSource
	httpClient  *http.Client
	mu          sync.RWMutex
}

// NewFCMSender creates an FCMSender from a service account JSON file.
func NewFCMSender(credentialsFile, projectID string) (*FCMSender, error) {
	data, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("fcm: read credentials file: %w", err)
	}

	conf, err := google.JWTConfigFromJSON(data, fcmScope)
	if err != nil {
		return nil, fmt.Errorf("fcm: parse credentials: %w", err)
	}

	ts := conf.TokenSource(context.Background())

	return &FCMSender{
		projectID:   projectID,
		tokenSource: oauth2.ReuseTokenSource(nil, ts),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// fcmRequest is the top-level FCM v1 send request.
type fcmRequest struct {
	Message fcmMessage `json:"message"`
}

// fcmMessage represents a single FCM message.
type fcmMessage struct {
	Token        string                  `json:"token"`
	Notification *fcmNotificationPayload `json:"notification,omitempty"`
	Data         map[string]string       `json:"data,omitempty"`
}

type fcmNotificationPayload struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// SendPush sends a push notification with notification + data payload.
func (s *FCMSender) SendPush(ctx context.Context, token, title, notificationBody string, data map[string]string) error {
	url := fmt.Sprintf("%s/%s/messages:send", fcmBaseURL, s.projectID)

	var notification *fcmNotificationPayload
	if title != "" || notificationBody != "" {
		notification = &fcmNotificationPayload{
			Title: title,
			Body:  notificationBody,
		}
	}

	payload := fcmRequest{
		Message: fcmMessage{
			Token:        token,
			Notification: notification,
			Data:         data,
		},
	}

	requestBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("fcm: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("fcm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Inject OAuth2 bearer token
	oauthToken, err := s.tokenSource.Token()
	if err != nil {
		return fmt.Errorf("fcm: obtain token: %w", err)
	}
	oauthToken.SetAuthHeader(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fcm: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("fcm: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
