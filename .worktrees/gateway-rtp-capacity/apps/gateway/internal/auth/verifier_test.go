package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifierVerifyTokenSuccess(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey(t)
	server := newJWKSStubServer(t, []rsa.PublicKey{key.PublicKey})
	defer server.Close()

	verifier, err := NewVerifier(Config{
		JWKSURL:   server.URL,
		Issuer:    "https://issuer.example.com/realms/app",
		Audience:  "gateway-api",
		TimeoutMS: 2000,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if err := verifier.Prefetch(context.Background()); err != nil {
		t.Fatalf("prefetch: %v", err)
	}

	token := mustSignToken(t, key, "key-0", "https://issuer.example.com/realms/app", "gateway-api", time.Now().Add(5*time.Minute))
	claims, err := verifier.VerifyToken(context.Background(), token)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Fatalf("expected subject user-1, got %q", claims.Subject)
	}
}

func TestVerifierVerifyTokenRejectsIssuerAudienceAndExpiry(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey(t)
	server := newJWKSStubServer(t, []rsa.PublicKey{key.PublicKey})
	defer server.Close()

	verifier, err := NewVerifier(Config{
		JWKSURL:   server.URL,
		Issuer:    "https://issuer.example.com/realms/app",
		Audience:  "gateway-api",
		TimeoutMS: 2000,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if err := verifier.Prefetch(context.Background()); err != nil {
		t.Fatalf("prefetch: %v", err)
	}

	tests := []struct {
		name      string
		issuer    string
		audience  string
		expiresAt time.Time
	}{
		{
			name:      "invalid issuer",
			issuer:    "https://another-issuer",
			audience:  "gateway-api",
			expiresAt: time.Now().Add(5 * time.Minute),
		},
		{
			name:      "invalid audience",
			issuer:    "https://issuer.example.com/realms/app",
			audience:  "other-api",
			expiresAt: time.Now().Add(5 * time.Minute),
		},
		{
			name:      "expired token",
			issuer:    "https://issuer.example.com/realms/app",
			audience:  "gateway-api",
			expiresAt: time.Now().Add(-10 * time.Minute),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			token := mustSignToken(t, key, "key-0", tc.issuer, tc.audience, tc.expiresAt)
			if _, err := verifier.VerifyToken(context.Background(), token); err == nil {
				t.Fatalf("expected verify error")
			}
		})
	}
}

func TestVerifierUnknownKIDRefreshSuccessAndFail(t *testing.T) {
	t.Parallel()

	key1 := mustGenerateRSAKey(t)
	key2 := mustGenerateRSAKey(t)

	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		keys := []jwkPayload{{Kid: "key-1", PublicKey: key1.PublicKey}}
		if n >= 2 {
			keys = []jwkPayload{{Kid: "key-2", PublicKey: key2.PublicKey}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": buildJWKSKeys(keys),
		})
	}))
	defer server.Close()

	verifier, err := NewVerifier(Config{
		JWKSURL:   server.URL,
		Issuer:    "https://issuer.example.com/realms/app",
		Audience:  "gateway-api",
		TimeoutMS: 2000,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if err := verifier.Prefetch(context.Background()); err != nil {
		t.Fatalf("prefetch: %v", err)
	}

	tokenWithNewKid := mustSignToken(t, key2, "key-2", "https://issuer.example.com/realms/app", "gateway-api", time.Now().Add(5*time.Minute))
	if _, err := verifier.VerifyToken(context.Background(), tokenWithNewKid); err != nil {
		t.Fatalf("verify with refreshed kid failed: %v", err)
	}

	tokenUnknown := mustSignToken(t, key2, "key-unknown", "https://issuer.example.com/realms/app", "gateway-api", time.Now().Add(5*time.Minute))
	if _, err := verifier.VerifyToken(context.Background(), tokenUnknown); err == nil {
		t.Fatalf("expected unknown kid error")
	}
}

type jwkPayload struct {
	Kid       string
	PublicKey rsa.PublicKey
}

func newJWKSStubServer(t *testing.T, keys []rsa.PublicKey) *httptest.Server {
	t.Helper()
	payload := make([]jwkPayload, 0, len(keys))
	for i, key := range keys {
		payload = append(payload, jwkPayload{
			Kid:       "key-" + string(rune('0'+i)),
			PublicKey: key,
		})
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": buildJWKSKeys(payload),
		})
	}))
}

func buildJWKSKeys(keys []jwkPayload) []map[string]string {
	out := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, map[string]string{
			"kty": "RSA",
			"kid": key.Kid,
			"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(bigEndianBytes(key.PublicKey.E)),
		})
	}
	return out
}

func bigEndianBytes(v int) []byte {
	if v == 0 {
		return []byte{0}
	}
	buf := make([]byte, 0, 4)
	for v > 0 {
		buf = append([]byte{byte(v & 0xff)}, buf...)
		v >>= 8
	}
	return buf
}

func mustGenerateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

func mustSignToken(t *testing.T, key *rsa.PrivateKey, kid, issuer, audience string, exp time.Time) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		Subject:   "user-1",
		Issuer:    issuer,
		Audience:  jwt.ClaimStrings{audience},
		ExpiresAt: jwt.NewNumericDate(exp),
		NotBefore: jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}
