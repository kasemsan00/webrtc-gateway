package sipclientauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 5 * time.Second

// Config controls the SIP client auth register client.
type Config struct {
	RegisterURL string
	Timeout     time.Duration
}

// Account contains SIP credentials returned by the SIP client auth register API.
type Account struct {
	Name      string
	Extension string
	Secret    string
	Domain    string
}

// Client calls the SIP client auth register API.
type Client struct {
	registerURL string
	httpClient  *http.Client
}

type registerResponse struct {
	Status  string       `json:"status"`
	Message string       `json:"message"`
	Data    registerData `json:"data"`
}

type registerData struct {
	Name    string `json:"name"`
	Ext     string `json:"ext"`
	Secret  string `json:"secret"`
	Domain  string `json:"domain"`
	WebSock string `json:"websocket"`
}

// NewClient creates a client for the SIP auth register API.
func NewClient(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		registerURL: strings.TrimSpace(cfg.RegisterURL),
		httpClient:  &http.Client{Timeout: timeout},
	}
}

// RegisterMobile exchanges a mobile access token for SIP credentials.
func (c *Client) RegisterMobile(ctx context.Context, token string) (*Account, error) {
	if c == nil || strings.TrimSpace(c.registerURL) == "" {
		return nil, fmt.Errorf("sip client auth register url is required")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("access token is required")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("token", token); err != nil {
		return nil, fmt.Errorf("write token field: %w", err)
	}
	if err := writer.WriteField("type", "mobile"); err != nil {
		return nil, fmt.Errorf("write type field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finalize register form: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.registerURL, &body)
	if err != nil {
		return nil, fmt.Errorf("build register request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call sip client auth register: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("sip client auth register returned status %d", resp.StatusCode)
	}

	var payload registerResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode sip client auth register response: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Status), "OK") {
		msg := strings.TrimSpace(payload.Message)
		if msg == "" {
			msg = "non-OK response"
		}
		return nil, fmt.Errorf("sip client auth register failed: %s", msg)
	}

	account := &Account{
		Name:      strings.TrimSpace(payload.Data.Name),
		Extension: strings.TrimSpace(payload.Data.Ext),
		Secret:    strings.TrimSpace(payload.Data.Secret),
		Domain:    strings.TrimSpace(payload.Data.Domain),
	}
	if account.Domain == "" {
		return nil, fmt.Errorf("sip client auth register response missing domain")
	}
	if account.Extension == "" {
		return nil, fmt.Errorf("sip client auth register response missing ext")
	}
	if account.Secret == "" {
		return nil, fmt.Errorf("sip client auth register response missing secret")
	}
	return account, nil
}
