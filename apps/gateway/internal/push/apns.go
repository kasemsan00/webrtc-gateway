package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	apnsProductionBaseURL = "https://api.push.apple.com"
	apnsSandboxBaseURL    = "https://api.sandbox.push.apple.com"
	apnsJWTTTL            = 50 * time.Minute
)

// APNSConfig contains APNs token-auth settings for iOS VoIP pushes.
type APNSConfig struct {
	Environment string
	KeyFile     string
	KeyID       string
	TeamID      string
	BundleID    string
	Topic       string
}

// APNSSender sends PushKit VoIP notifications through APNs.
type APNSSender struct {
	keyID      string
	teamID     string
	topic      string
	privateKey *ecdsa.PrivateKey
	httpClient *http.Client
	baseURL    string

	mu        sync.Mutex
	cachedJWT string
	jwtExpiry time.Time
}

// NewAPNSSender creates an APNs VoIP sender from a token-auth .p8 key.
func NewAPNSSender(cfg APNSConfig) (*APNSSender, error) {
	keyID := strings.TrimSpace(cfg.KeyID)
	teamID := strings.TrimSpace(cfg.TeamID)
	keyFile := strings.TrimSpace(cfg.KeyFile)
	topic := normalizeAPNSTopic(cfg.BundleID, cfg.Topic)
	if keyID == "" || teamID == "" || keyFile == "" || topic == "" {
		return nil, fmt.Errorf("apns: key file, key id, team id, and bundle/topic are required")
	}

	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("apns: read key file: %w", err)
	}
	privateKey, err := jwt.ParseECPrivateKeyFromPEM(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("apns: parse .p8 private key: %w", err)
	}

	return &APNSSender{
		keyID:      keyID,
		teamID:     teamID,
		topic:      topic,
		privateKey: privateKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    apnsBaseURL(cfg.Environment),
	}, nil
}

func normalizeAPNSTopic(bundleID, topic string) string {
	if trimmed := strings.TrimSpace(topic); trimmed != "" {
		return trimmed
	}
	bundleID = strings.TrimSpace(bundleID)
	if bundleID == "" {
		return ""
	}
	return bundleID + ".voip"
}

func apnsBaseURL(environment string) string {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "sandbox", "development", "dev":
		return apnsSandboxBaseURL
	default:
		return apnsProductionBaseURL
	}
}

// SendVoIPPush sends one incoming-call PushKit notification.
func (s *APNSSender) SendVoIPPush(ctx context.Context, token string, data map[string]string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("apns: token is empty")
	}
	body, err := json.Marshal(buildAPNSVoIPPayload(data))
	if err != nil {
		return fmt.Errorf("apns: marshal payload: %w", err)
	}
	req, err := s.buildRequest(ctx, token, body)
	if err != nil {
		return err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("apns: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("apns: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func buildAPNSVoIPPayload(data map[string]string) map[string]interface{} {
	payload := map[string]interface{}{
		"aps": map[string]interface{}{
			"content-available": 1,
		},
	}
	for key, value := range data {
		payload[key] = value
	}
	return payload
}

func (s *APNSSender) buildRequest(ctx context.Context, token string, body []byte) (*http.Request, error) {
	authToken, err := s.authToken()
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/3/device/%s", strings.TrimRight(s.baseURL, "/"), token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("apns: build request: %w", err)
	}
	req.Header.Set("authorization", "bearer "+authToken)
	req.Header.Set("apns-push-type", "voip")
	req.Header.Set("apns-topic", s.topic)
	req.Header.Set("apns-priority", "10")
	req.Header.Set("apns-expiration", fmt.Sprintf("%d", time.Now().Add(time.Duration(incomingRingTimeoutSeconds)*time.Second).Unix()))
	req.Header.Set("content-type", "application/json")
	return req, nil
}

func (s *APNSSender) authToken() (string, error) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cachedJWT != "" && now.Before(s.jwtExpiry) {
		return s.cachedJWT, nil
	}
	claims := jwt.MapClaims{
		"iss": s.teamID,
		"iat": now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = s.keyID
	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("apns: sign jwt: %w", err)
	}
	s.cachedJWT = signed
	s.jwtExpiry = now.Add(apnsJWTTTL)
	return signed, nil
}
