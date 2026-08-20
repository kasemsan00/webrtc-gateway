package api

import (
	"context"
	"errors"
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

type mobileSIPProvisionerStub struct {
	called   bool
	token    string
	subject  string
	platform string
	result   *MobileSIPProvisionResult
	err      error
}

func (s *mobileSIPProvisionerStub) ProvisionMobileSIPTrunk(_ context.Context, rawToken string, claims *auth.VerifiedClaims, devicePlatform string) (*MobileSIPProvisionResult, error) {
	s.called = true
	s.token = rawToken
	s.platform = devicePlatform
	if claims != nil {
		s.subject = claims.Subject
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &MobileSIPProvisionResult{TrunkID: 42, TrunkPublicID: "public-42"}, nil
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

func TestPublicWebSocketAcceptsWithoutAccessToken(t *testing.T) {
	t.Parallel()

	provisioner := &mobileSIPProvisionerStub{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, _ string, _ auth.TokenRealm) (*auth.VerifiedClaims, error) {
			return nil, context.DeadlineExceeded
		},
	})
	srv.SetMobileSIPProvisioner(provisioner)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handlePublicWebSocket))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected successful public ws upgrade, err=%v status=%d", err, status)
	}
	defer conn.Close()

	if provisioner.called {
		t.Fatalf("public websocket must not trigger mobile SIP provisioning")
	}

	deadline := time.Now().Add(time.Second)
	for {
		srv.mu.RLock()
		var found *WSClient
		for client := range srv.wsConnections {
			found = client
			break
		}
		srv.mu.RUnlock()
		if found != nil {
			if !found.publicOnly {
				t.Fatalf("expected public websocket client to be marked publicOnly")
			}
			if found.authClaims != nil {
				t.Fatalf("public websocket client should not have auth claims")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for public websocket client registration")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWebSocketAuthProvisionsMobileUserTrunk(t *testing.T) {
	t.Parallel()

	provisioner := &mobileSIPProvisionerStub{
		result: &MobileSIPProvisionResult{TrunkID: 42, TrunkPublicID: "public-42"},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw != "valid-token" {
				return nil, context.DeadlineExceeded
			}
			return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
		},
	})
	srv.SetMobileSIPProvisioner(provisioner)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	conn, resp, err := dialer.Dial(wsURL+"?access_token=valid-token&devicePlatform=android", nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected successful ws upgrade, err=%v status=%d", err, status)
	}
	defer conn.Close()

	if !provisioner.called {
		t.Fatalf("expected mobile provisioner to be called")
	}
	if provisioner.token != "valid-token" || provisioner.subject != "user-1" || provisioner.platform != "android" {
		t.Fatalf("unexpected provisioner input token=%q subject=%q platform=%q", provisioner.token, provisioner.subject, provisioner.platform)
	}

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var resolved WSMessage
	if err := conn.ReadJSON(&resolved); err != nil {
		t.Fatalf("expected trunk_resolved message: %v", err)
	}
	if resolved.Type != "trunk_resolved" || resolved.TrunkID != 42 || resolved.TrunkPublicID != "public-42" {
		t.Fatalf("unexpected trunk_resolved message: %+v", resolved)
	}

	deadline := time.Now().Add(time.Second)
	for {
		srv.mu.RLock()
		var found *WSClient
		for client := range srv.wsConnections {
			found = client
			break
		}
		srv.mu.RUnlock()
		if found != nil {
			if !found.trunkResolved || found.resolvedTrunkID != 42 {
				t.Fatalf("expected client resolved to trunk 42, got resolved=%v trunkID=%d", found.trunkResolved, found.resolvedTrunkID)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for websocket client registration")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWebSocketAuthProvisionsMobileUserTrunkWithIOSPlatform(t *testing.T) {
	t.Parallel()

	provisioner := &mobileSIPProvisionerStub{
		result: &MobileSIPProvisionResult{TrunkID: 43, TrunkPublicID: "public-43"},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw != "valid-token" {
				return nil, context.DeadlineExceeded
			}
			return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
		},
	})
	srv.SetMobileSIPProvisioner(provisioner)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	conn, resp, err := dialer.Dial(wsURL+"?access_token=valid-token&devicePlatform=ios", nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected successful ws upgrade, err=%v status=%d", err, status)
	}
	defer conn.Close()

	if provisioner.platform != "ios" {
		t.Fatalf("expected ios platform, got %q", provisioner.platform)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var resolved WSMessage
	if err := conn.ReadJSON(&resolved); err != nil {
		t.Fatalf("expected trunk_resolved message: %v", err)
	}
	if resolved.Type != "trunk_resolved" || resolved.TrunkID != 43 || resolved.TrunkPublicID != "public-43" {
		t.Fatalf("unexpected trunk_resolved message: %+v", resolved)
	}
}

func TestWebSocketAuthRejectsWhenMobileProvisioningFails(t *testing.T) {
	t.Parallel()

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw != "valid-token" {
				return nil, context.DeadlineExceeded
			}
			return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
		},
	})
	srv.SetMobileSIPProvisioner(&mobileSIPProvisionerStub{err: errors.New("provision failed")})

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	conn, resp, err := dialer.Dial(wsURL+"?access_token=valid-token&devicePlatform=android", nil)
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
}

func TestWebSocketAuthRejectsMobileProvisioningWithoutDevicePlatform(t *testing.T) {
	t.Parallel()

	provisioner := &mobileSIPProvisionerStub{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw != "valid-token" {
				return nil, context.DeadlineExceeded
			}
			return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
		},
	})
	srv.SetMobileSIPProvisioner(provisioner)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	conn, resp, err := dialer.Dial(wsURL+"?access_token=valid-token", nil)
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
	if provisioner.called {
		t.Fatalf("provisioner should not be called without devicePlatform")
	}
}

func TestWebSocketAuthRejectsMobileProvisioningWithInvalidDevicePlatform(t *testing.T) {
	t.Parallel()

	provisioner := &mobileSIPProvisionerStub{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw != "valid-token" {
				return nil, context.DeadlineExceeded
			}
			return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
		},
	})
	srv.SetMobileSIPProvisioner(provisioner)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	conn, resp, err := dialer.Dial(wsURL+"?access_token=valid-token&devicePlatform=mobile", nil)
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
	if provisioner.called {
		t.Fatalf("provisioner should not be called with invalid devicePlatform")
	}
}

func TestWebSocketAuthSkipsMobileProvisioningForEmployeeRealm(t *testing.T) {
	t.Parallel()

	provisioner := &mobileSIPProvisionerStub{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw != "employee-token" {
				return nil, context.DeadlineExceeded
			}
			return &auth.VerifiedClaims{Subject: "employee-1", Realm: auth.TokenRealmEmployee}, nil
		},
	})
	srv.SetMobileSIPProvisioner(provisioner)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	conn, resp, err := dialer.Dial(wsURL+"?access_token=employee-token&auth_type=employee", nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected successful ws upgrade, err=%v status=%d", err, status)
	}
	_ = conn.Close()

	if provisioner.called {
		t.Fatalf("did not expect employee realm to be auto-provisioned")
	}
}

func TestAdminPasswordAuthForREST(t *testing.T) {
	t.Parallel()

	const adminPassword = "ops-secret"

	newRouter := func(srv *Server) *mux.Router {
		router := mux.NewRouter()
		router.HandleFunc("/api/client-diagnostics", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}).Methods("GET")
		apiRouter := router.PathPrefix("/api").Subrouter()
		if srv.restAuthEnabled() {
			apiRouter.Use(srv.authMiddleware)
		}
		apiRouter.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			claims, ok := AuthClaimsFromContext(r.Context())
			if !ok {
				t.Fatalf("expected claims in context")
			}
			if claims.PreferredUsername == "" {
				t.Fatalf("expected preferred username on claims")
			}
			w.WriteHeader(http.StatusOK)
		})
		return router
	}

	t.Run("password only accepts matching bearer", func(t *testing.T) {
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
		srv.SetAdminPassword(adminPassword)
		router := newRouter(srv)

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Authorization", "Bearer "+adminPassword)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("password set rejects wrong bearer when jwt disabled", func(t *testing.T) {
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
		srv.SetAdminPassword(adminPassword)
		router := newRouter(srv)

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Authorization", "Bearer wrong-secret")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("jwt still works when password is also set", func(t *testing.T) {
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
		srv.SetAdminPassword(adminPassword)
		srv.SetTokenVerifier(tokenVerifierStub{
			verify: func(_ context.Context, raw string, _ auth.TokenRealm) (*auth.VerifiedClaims, error) {
				if raw == "valid-token" {
					return &auth.VerifiedClaims{Subject: "user-1", PreferredUsername: "alice", Realm: auth.TokenRealmUser}, nil
				}
				return nil, errors.New("invalid")
			},
		})
		router := newRouter(srv)

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("jwt still required when password unset", func(t *testing.T) {
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
		srv.SetTokenVerifier(tokenVerifierStub{
			verify: func(_ context.Context, raw string, _ auth.TokenRealm) (*auth.VerifiedClaims, error) {
				if raw == "valid-token" {
					return &auth.VerifiedClaims{Subject: "user-1", PreferredUsername: "alice", Realm: auth.TokenRealmUser}, nil
				}
				return nil, errors.New("invalid")
			},
		})
		router := newRouter(srv)

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}

		req = httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("public get client diagnostics stays unauthenticated", func(t *testing.T) {
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
		srv.SetAdminPassword(adminPassword)
		router := newRouter(srv)

		req := httptest.NewRequest(http.MethodGet, "/api/client-diagnostics", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 without auth, got %d", rr.Code)
		}
	})
}

func TestWebSocketRejectsAdminPassword(t *testing.T) {
	t.Parallel()

	const adminPassword = "ops-secret"
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetAdminPassword(adminPassword)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, _ auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw == "valid-token" {
				return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
			}
			return nil, errors.New("invalid")
		},
	})

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	conn, resp, err := dialer.Dial(wsURL+"?access_token="+adminPassword, nil)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatalf("expected dial error for admin password")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected 401, got %d", status)
	}

	conn, resp, err = dialer.Dial(wsURL+"?access_token=valid-token", nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected successful ws upgrade, err=%v status=%d", err, status)
	}
	_ = conn.Close()
}
