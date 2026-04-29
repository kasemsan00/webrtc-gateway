package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"k2-gateway/internal/auth"
	"k2-gateway/internal/config"
)

type tokenVerifierStub struct {
	verify func(ctx context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error)
}

func (s tokenVerifierStub) VerifyToken(ctx context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
	return s.verify(ctx, raw, hint)
}

func TestAuthMiddlewareForREST(t *testing.T) {
	t.Parallel()

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw == "valid-token" {
				return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
			}
			if raw == "employee-token" {
				if hint == auth.TokenRealmUser {
					return nil, context.DeadlineExceeded
				}
				return &auth.VerifiedClaims{Subject: "employee-1", Realm: auth.TokenRealmEmployee}, nil
			}
			return nil, context.DeadlineExceeded
		},
	})

	router := mux.NewRouter()
	apiRouter := router.PathPrefix("/api").Subrouter()
	apiRouter.Use(srv.authMiddleware)
	apiRouter.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthClaimsFromContext(r.Context()); !ok {
			t.Fatalf("expected claims in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	t.Run("missing token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("employee token with explicit hint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Authorization", "Bearer employee-token")
		req.Header.Set("X-Auth-Type", "employee")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})
}

func TestWebSocketAuthAccessToken(t *testing.T) {
	t.Parallel()

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw == "valid-token" {
				return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
			}
			if raw == "employee-token" {
				if hint == auth.TokenRealmUser {
					return nil, context.DeadlineExceeded
				}
				return &auth.VerifiedClaims{Subject: "employee-1", Realm: auth.TokenRealmEmployee}, nil
			}
			return nil, context.DeadlineExceeded
		},
	})

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	t.Run("missing token", func(t *testing.T) {
		conn, resp, err := dialer.Dial(wsURL, nil)
		if conn != nil {
			_ = conn.Close()
		}
		if err == nil {
			t.Fatalf("expected dial error")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("expected 401, got %d", status)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		conn, resp, err := dialer.Dial(wsURL+"?access_token=bad", nil)
		if conn != nil {
			_ = conn.Close()
		}
		if err == nil {
			t.Fatalf("expected dial error")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("expected 401, got %d", status)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		conn, resp, err := dialer.Dial(wsURL+"?access_token=valid-token", nil)
		if err != nil {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("expected successful ws upgrade, err=%v status=%d", err, status)
		}
		_ = conn.Close()
	})

	t.Run("employee token with explicit auth_type", func(t *testing.T) {
		conn, resp, err := dialer.Dial(wsURL+"?access_token=employee-token&auth_type=employee", nil)
		if err != nil {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("expected successful ws upgrade, err=%v status=%d", err, status)
		}
		_ = conn.Close()
	})
}
