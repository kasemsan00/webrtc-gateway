package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"webrtc-sip-gateway/internal/auth"
	"webrtc-sip-gateway/internal/config"
)

func TestAgentWSOptionalAccessToken(t *testing.T) {
	srv := NewServer(config.APIConfig{EnableAgentWS: true}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, hint auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw == "valid-token" && hint == auth.TokenRealmEmployee {
				return &auth.VerifiedClaims{
					Subject:           "agent-sub-1",
					Realm:             auth.TokenRealmEmployee,
					PreferredUsername: "agent1",
				}, nil
			}
			return nil, context.DeadlineExceeded
		},
	})
	provisioner := &mobileSIPProvisionerStub{}
	srv.SetMobileSIPProvisioner(provisioner)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleAgentWebSocket))
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}

	assertUnauthorized := func(query string) {
		t.Helper()
		conn, resp, err := dialer.Dial(wsURL+query, nil)
		if conn != nil {
			_ = conn.Close()
		}
		if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("expected 401 for %q, err=%v status=%d", query, err, status)
		}
	}

	assertUnauthorized("?access_token=bad")
	if provisioner.called {
		t.Fatalf("mobile provisioner must not be invoked for rejected /ws-agent auth")
	}

	noVerifier := NewServer(config.APIConfig{EnableAgentWS: true}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	noVerifierServer := httptest.NewServer(http.HandlerFunc(noVerifier.handleAgentWebSocket))
	defer noVerifierServer.Close()
	noVerifierURL := "ws" + strings.TrimPrefix(noVerifierServer.URL, "http")
	conn, resp, err := dialer.Dial(noVerifierURL+"?access_token=valid-token", nil)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected 401 when token verifier is nil, err=%v status=%d", err, status)
	}

	absent, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("expected upgrade without token, err=%v status=%v", err, resp)
	}
	waitAgentClient := func(server *Server, wantClaims bool) *auth.VerifiedClaims {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			server.mu.RLock()
			var found *WSClient
			for client := range server.wsConnections {
				found = client
				break
			}
			var claims *auth.VerifiedClaims
			if found != nil {
				claims = found.authClaims
			}
			server.mu.RUnlock()
			if found != nil {
				if !found.agentOnly {
					t.Fatalf("expected agentOnly client")
				}
				if wantClaims && (claims == nil || claims.Subject != "agent-sub-1" || claims.Realm != auth.TokenRealmEmployee || claims.PreferredUsername != "agent1") {
					t.Fatalf("expected auth claims on agent client, got %#v", claims)
				}
				if !wantClaims && claims != nil {
					t.Fatalf("expected no auth claims, got %#v", claims)
				}
				return claims
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for agent websocket client")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitAgentClient(srv, false)
	_ = absent.Close()
	waitAgentClientsGone(t, srv)
	if provisioner.called {
		t.Fatalf("mobile provisioner must not be invoked for /ws-agent without a token")
	}

	authed, resp, err := dialer.Dial(wsURL+"?access_token=valid-token&auth_type=employee", nil)
	if err != nil {
		t.Fatalf("expected accepted upgrade, err=%v status=%v", err, resp)
	}
	defer authed.Close()
	waitAgentClient(srv, true)
	if provisioner.called {
		t.Fatalf("mobile provisioner must not be invoked for authenticated /ws-agent")
	}
}

func waitAgentClientsGone(t *testing.T, server *Server) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		server.mu.RLock()
		n := len(server.wsConnections)
		server.mu.RUnlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for agent websocket client to disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
