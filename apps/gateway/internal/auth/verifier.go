package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultLeeway = 30 * time.Second
)

// Config controls JWT verification behavior.
type Config struct {
	JWKSURL   string
	Issuer    string
	Audience  string
	TimeoutMS int
}

// VerifiedClaims are claims extracted from a verified token.
type VerifiedClaims struct {
	Subject           string
	Issuer            string
	Audience          []string
	ExpiresAt         *time.Time
	NotBefore         *time.Time
	PreferredUsername string
}

// Verifier validates JWTs against keys loaded from a JWKS endpoint.
type Verifier struct {
	cfg    Config
	client *http.Client

	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// NewVerifier creates a verifier. Call Prefetch during startup.
func NewVerifier(cfg Config) (*Verifier, error) {
	if strings.TrimSpace(cfg.JWKSURL) == "" {
		return nil, fmt.Errorf("jwks url is required")
	}
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, fmt.Errorf("jwt issuer is required")
	}
	if strings.TrimSpace(cfg.Audience) == "" {
		return nil, fmt.Errorf("jwt audience is required")
	}
	timeout := cfg.TimeoutMS
	if timeout <= 0 {
		timeout = 5000
	}

	return &Verifier{
		cfg: Config{
			JWKSURL:   strings.TrimSpace(cfg.JWKSURL),
			Issuer:    strings.TrimSpace(cfg.Issuer),
			Audience:  strings.TrimSpace(cfg.Audience),
			TimeoutMS: timeout,
		},
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Millisecond,
		},
		keys: make(map[string]*rsa.PublicKey),
	}, nil
}

// Prefetch loads JWKS upfront. Intended for startup fail-fast behavior.
func (v *Verifier) Prefetch(ctx context.Context) error {
	return v.refreshJWKS(ctx)
}

// VerifyToken verifies JWT signature and standard claims.
func (v *Verifier) VerifyToken(ctx context.Context, rawToken string) (*VerifiedClaims, error) {
	tokenString := strings.TrimSpace(rawToken)
	if tokenString == "" {
		return nil, fmt.Errorf("token is empty")
	}

	claims := &keycloakClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{
			jwt.SigningMethodRS256.Alg(),
			jwt.SigningMethodRS384.Alg(),
			jwt.SigningMethodRS512.Alg(),
		}),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithAudience(v.cfg.Audience),
		jwt.WithLeeway(defaultLeeway),
	)

	token, err := parser.ParseWithClaims(tokenString, claims, func(parsed *jwt.Token) (interface{}, error) {
		kid, _ := parsed.Header["kid"].(string)
		kid = strings.TrimSpace(kid)
		if kid == "" {
			return nil, fmt.Errorf("missing kid")
		}

		key, ok := v.getKey(kid)
		if ok {
			return key, nil
		}

		if err := v.refreshJWKS(ctx); err != nil {
			return nil, fmt.Errorf("unknown kid %q and jwks refresh failed: %w", kid, err)
		}
		key, ok = v.getKey(kid)
		if !ok {
			return nil, fmt.Errorf("unknown kid %q", kid)
		}
		return key, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}

	return toVerifiedClaims(claims), nil
}

// keycloakClaims embeds standard registered claims and adds Keycloak-specific fields.
type keycloakClaims struct {
	jwt.RegisteredClaims
	PreferredUsername string `json:"preferred_username"`
}

func toVerifiedClaims(claims *keycloakClaims) *VerifiedClaims {
	out := &VerifiedClaims{
		Subject:           claims.Subject,
		Issuer:            claims.Issuer,
		Audience:          append([]string(nil), claims.Audience...),
		PreferredUsername: claims.PreferredUsername,
	}
	if claims.ExpiresAt != nil {
		t := claims.ExpiresAt.Time
		out.ExpiresAt = &t
	}
	if claims.NotBefore != nil {
		t := claims.NotBefore.Time
		out.NotBefore = &t
	}
	return out
}

func (v *Verifier) getKey(kid string) (*rsa.PublicKey, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	key, ok := v.keys[kid]
	return key, ok
}

func (v *Verifier) refreshJWKS(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(v.cfg.TimeoutMS)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, v.cfg.JWKSURL, nil)
	if err != nil {
		return fmt.Errorf("build jwks request: %w", err)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("jwks endpoint returned status %d", resp.StatusCode)
	}

	var payload jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode jwks response: %w", err)
	}

	nextKeys := make(map[string]*rsa.PublicKey)
	for _, key := range payload.Keys {
		if strings.ToUpper(strings.TrimSpace(key.Kty)) != "RSA" {
			continue
		}
		kid := strings.TrimSpace(key.Kid)
		if kid == "" {
			continue
		}
		pub, err := parseRSAPublicKey(key.N, key.E)
		if err != nil {
			return fmt.Errorf("parse jwk kid %q: %w", kid, err)
		}
		nextKeys[kid] = pub
	}

	if len(nextKeys) == 0 {
		return fmt.Errorf("jwks does not contain usable rsa keys")
	}

	v.mu.Lock()
	v.keys = nextKeys
	v.mu.Unlock()
	return nil
}

func parseRSAPublicKey(modulusB64URL, exponentB64URL string) (*rsa.PublicKey, error) {
	modulusBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(modulusB64URL))
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	exponentBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(exponentB64URL))
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}
	if len(modulusBytes) == 0 || len(exponentBytes) == 0 {
		return nil, fmt.Errorf("invalid n/e values")
	}

	modulus := new(big.Int).SetBytes(modulusBytes)
	exponent := 0
	for _, b := range exponentBytes {
		exponent = exponent<<8 + int(b)
	}
	if exponent <= 0 {
		return nil, fmt.Errorf("invalid rsa exponent")
	}

	return &rsa.PublicKey{
		N: modulus,
		E: exponent,
	}, nil
}
