package push

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// NotificationEntry represents a single notification record from the TTRS API.
type NotificationEntry struct {
	UserID       string `json:"user_id"`
	ServiceID    string `json:"service_id"`
	Token        string `json:"token"`
	MobileDevice string `json:"mobile_device"`
}

// NotificationResponse is the envelope returned by the TTRS Notification API.
type NotificationResponse struct {
	Data    []NotificationEntry `json:"data"`
	Message string              `json:"message"`
	Status  string              `json:"status"`
}

// TTRSClient fetches push notification tokens from the TTRS Notification API.
type TTRSClient struct {
	baseURL     string
	httpClient  *http.Client
	tokenSource oauth2.TokenSource
}

// NewTTRSClient creates a TTRSClient that authenticates via Keycloak client credentials.
func NewTTRSClient(baseURL, tokenURL, grantType, clientID, clientSecret string, timeoutMS int) *TTRSClient {
	ccConfig := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
	}
	if grantType != "" {
		ccConfig.EndpointParams = url.Values{
			"grant_type": []string{grantType},
		}
	}
	ts := oauth2.ReuseTokenSource(nil, ccConfig.TokenSource(context.Background()))

	// The OAuth2 HTTP client handles token fetch, caching, and auto-refresh.
	oauthClient := oauth2.NewClient(context.Background(), ts)
	oauthClient.Timeout = time.Duration(timeoutMS) * time.Millisecond

	// Wrap the transport so the base transport inherits the OAuth2 token injection
	// but we can still set a global timeout on the outer client.
	return &TTRSClient{
		baseURL:     baseURL,
		httpClient:  oauthClient,
		tokenSource: ts,
	}
}

// FetchNotifications retrieves notification entries for a given user ID.
func (c *TTRSClient) FetchNotifications(ctx context.Context, userID string) ([]NotificationEntry, error) {
	url := fmt.Sprintf("%s/employees/v3/accounts/%s/notifications", c.baseURL, userID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("ttrs: build request: %w", err)
	}

	if c.tokenSource != nil {
		token, err := c.tokenSource.Token()
		if err != nil {
			return nil, fmt.Errorf("ttrs: obtain token: %w", err)
		}
		token.SetAuthHeader(req)

		if shouldDebugTTRSBearerToken() {
			log.Printf("🔔 [Push][TTRS] Debug bearer token for user %s: %s", userID, token.AccessToken)
		}
	}

	if shouldDebugTTRSBearerToken() {
		log.Printf("🔔 [Push][TTRS] Debug request: method=%s url=%s", req.Method, req.URL.String())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ttrs: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ttrs: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result NotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ttrs: decode response: %w", err)
	}

	return result.Data, nil
}

func shouldDebugTTRSBearerToken() bool {
	raw := os.Getenv("PUSH_DEBUG_TTRS_TOKEN")
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return enabled
}

// FilterByServiceID returns only entries matching the given service ID.
func FilterByServiceID(entries []NotificationEntry, serviceID string) []NotificationEntry {
	var filtered []NotificationEntry
	for _, e := range entries {
		if e.ServiceID == serviceID {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// tokenSourceTransport is an http.RoundTripper that injects an OAuth2 bearer token.
// This is kept internal; the public API only exposes NewTTRSClient.
type tokenSourceTransport struct {
	base   http.RoundTripper
	source oauth2.TokenSource
}

func (t *tokenSourceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.source.Token()
	if err != nil {
		return nil, fmt.Errorf("ttrs: obtain token: %w", err)
	}
	req = req.Clone(req.Context())
	token.SetAuthHeader(req)
	return t.base.RoundTrip(req)
}
