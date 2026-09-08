package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"webrtc-sip-gateway/internal/auth"
	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/logger"
)

func setupLogFileHandlers(t *testing.T, srv *Server) *mux.Router {
	t.Helper()

	router := mux.NewRouter()
	router.HandleFunc("/api/logs", srv.handleListLogFiles).Methods("GET", "OPTIONS")
	router.HandleFunc("/api/logs/current", srv.handleGetCurrentLog).Methods("GET", "OPTIONS")
	router.HandleFunc("/api/logs/{name}", srv.handleGetLogFile).Methods("GET", "OPTIONS")
	apiRouter := router.PathPrefix("/api").Subrouter()
	if srv.restAuthEnabled() {
		apiRouter.Use(srv.authMiddleware)
	}
	apiRouter.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}).Methods("GET", "OPTIONS")
	return router
}

func overrideLogFileFuncs(t *testing.T, list func() ([]logger.LogFileInfo, error), current func(int) (*logger.LogTail, error), named func(string, int) (*logger.LogTail, error)) {
	t.Helper()

	prevList := listGatewayLogFiles
	prevCurrent := readCurrentLogTail
	prevNamed := readNamedLogTail
	listGatewayLogFiles = list
	readCurrentLogTail = current
	readNamedLogTail = named
	t.Cleanup(func() {
		listGatewayLogFiles = prevList
		readCurrentLogTail = prevCurrent
		readNamedLogTail = prevNamed
	})
}

func decodeLogJSON[T any](t *testing.T, rr *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, rr.Code, rr.Body.String())
	}
	var response T
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v body=%s", err, rr.Body.String())
	}
	return response
}

func TestHandleListLogFiles(t *testing.T) {
	modified := time.Date(2026, 5, 25, 10, 30, 0, 0, time.UTC)
	overrideLogFileFuncs(t,
		func() ([]logger.LogFileInfo, error) {
			return []logger.LogFileInfo{
				{Name: "webrtc-sip-gateway-2026-05-25_10-30-00.log", Size: 123, ModifiedAt: modified, Current: true},
				{Name: "webrtc-sip-gateway-2026-05-25_09-30-00.log", Size: 45, ModifiedAt: modified.Add(-time.Hour), Current: false},
			}, nil
		},
		nil,
		nil,
	)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rr := httptest.NewRecorder()

	srv.handleListLogFiles(rr, req)

	response := decodeLogJSON[LogFileListResponse](t, rr, http.StatusOK)
	if len(response.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(response.Items))
	}
	if !response.Items[0].Current || response.Items[1].Current {
		t.Fatalf("expected only first item to be current: %+v", response.Items)
	}
	if response.Items[0].Name != "webrtc-sip-gateway-2026-05-25_10-30-00.log" || response.Items[0].Size != 123 {
		t.Fatalf("unexpected first item: %+v", response.Items[0])
	}
}

func TestHandleGetCurrentLogTail(t *testing.T) {
	overrideLogFileFuncs(t,
		nil,
		func(tail int) (*logger.LogTail, error) {
			if tail != 2 {
				t.Fatalf("expected tail=2, got %d", tail)
			}
			return &logger.LogTail{
				Name:      "webrtc-sip-gateway-2026-05-25_10-30-00.log",
				Current:   true,
				Tail:      tail,
				Lines:     []string{"line 2", "line 3"},
				Truncated: true,
			}, nil
		},
		nil,
	)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/logs/current?tail=2", nil)
	rr := httptest.NewRecorder()

	srv.handleGetCurrentLog(rr, req)

	response := decodeLogJSON[LogTailResponse](t, rr, http.StatusOK)
	if !response.Current || response.Tail != 2 || !response.Truncated {
		t.Fatalf("unexpected current log response: %+v", response)
	}
	if len(response.Lines) != 2 || response.Lines[0] != "line 2" || response.Lines[1] != "line 3" {
		t.Fatalf("unexpected lines: %+v", response.Lines)
	}
}

func TestHandleGetSelectedLogTail(t *testing.T) {
	overrideLogFileFuncs(t,
		nil,
		nil,
		func(name string, tail int) (*logger.LogTail, error) {
			if name != "webrtc-sip-gateway-2026-05-25_09-30-00.log" {
				t.Fatalf("unexpected name: %s", name)
			}
			if tail != 1 {
				t.Fatalf("expected tail=1, got %d", tail)
			}
			return &logger.LogTail{Name: name, Current: false, Tail: tail, Lines: []string{"old line"}}, nil
		},
	)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/logs/webrtc-sip-gateway-2026-05-25_09-30-00.log?tail=1", nil)
	req = mux.SetURLVars(req, map[string]string{"name": "webrtc-sip-gateway-2026-05-25_09-30-00.log"})
	rr := httptest.NewRecorder()

	srv.handleGetLogFile(rr, req)

	response := decodeLogJSON[LogTailResponse](t, rr, http.StatusOK)
	if response.Current || response.Tail != 1 || len(response.Lines) != 1 || response.Lines[0] != "old line" {
		t.Fatalf("unexpected selected log response: %+v", response)
	}
}

func TestHandleLogFileErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		varName    string
		currentErr error
		namedErr   error
		wantStatus int
	}{
		{name: "invalid tail", path: "/api/logs/current?tail=abc", wantStatus: http.StatusBadRequest},
		{name: "path traversal", path: "/api/logs/..%2Fsecret?tail=1", varName: "../secret", namedErr: logger.ErrInvalidLogFilename, wantStatus: http.StatusBadRequest},
		{name: "non gateway filename", path: "/api/logs/app.log?tail=1", varName: "app.log", namedErr: logger.ErrInvalidLogFilename, wantStatus: http.StatusBadRequest},
		{name: "missing selected file", path: "/api/logs/webrtc-sip-gateway-missing.log?tail=1", varName: "webrtc-sip-gateway-missing.log", namedErr: logger.ErrLogFileNotFound, wantStatus: http.StatusNotFound},
		{name: "missing current file", path: "/api/logs/current?tail=1", currentErr: logger.ErrLogFileNotFound, wantStatus: http.StatusNotFound},
		{name: "read failure", path: "/api/logs/webrtc-sip-gateway-bad.log?tail=1", varName: "webrtc-sip-gateway-bad.log", namedErr: errors.New("disk failed"), wantStatus: http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			overrideLogFileFuncs(t,
				nil,
				func(int) (*logger.LogTail, error) {
					return nil, tc.currentErr
				},
				func(string, int) (*logger.LogTail, error) {
					return nil, tc.namedErr
				},
			)

			srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.varName != "" {
				req = mux.SetURLVars(req, map[string]string{"name": tc.varName})
			}
			rr := httptest.NewRecorder()

			if tc.varName == "" {
				srv.handleGetCurrentLog(rr, req)
			} else {
				srv.handleGetLogFile(rr, req)
			}
			if rr.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestAllLogRoutesArePublicWhenAuthConfigured(t *testing.T) {
	calledCurrent := false
	calledNamed := false
	overrideLogFileFuncs(t,
		func() ([]logger.LogFileInfo, error) {
			return []logger.LogFileInfo{}, nil
		},
		func(tail int) (*logger.LogTail, error) {
			calledCurrent = true
			return &logger.LogTail{Name: "webrtc-sip-gateway-current.log", Current: true, Tail: tail, Lines: []string{}}, nil
		},
		func(name string, tail int) (*logger.LogTail, error) {
			calledNamed = true
			return &logger.LogTail{Name: name, Current: false, Tail: tail, Lines: []string{}}, nil
		},
	)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, _ auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw == "valid-token" {
				return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
			}
			return nil, context.DeadlineExceeded
		},
	})
	router := setupLogFileHandlers(t, srv)

	for _, path := range []string{"/api/logs", "/api/logs/webrtc-sip-gateway-2026-05-25_10-30-00.log"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %s without token to return 200, got %d body=%s", path, rr.Code, rr.Body.String())
		}
	}

	current := httptest.NewRecorder()
	router.ServeHTTP(current, httptest.NewRequest(http.MethodGet, "/api/logs/current", nil))
	if current.Code != http.StatusOK {
		t.Fatalf("expected current log without token to return 200, got %d body=%s", current.Code, current.Body.String())
	}
	if !calledCurrent || !calledNamed {
		t.Fatalf("expected current and named log handlers to be called, calledCurrent=%v calledNamed=%v", calledCurrent, calledNamed)
	}
}

func TestAllLogRoutesArePublicWhenOnlyAdminPasswordConfigured(t *testing.T) {
	overrideLogFileFuncs(t,
		func() ([]logger.LogFileInfo, error) {
			return []logger.LogFileInfo{}, nil
		},
		func(tail int) (*logger.LogTail, error) {
			return &logger.LogTail{Name: "webrtc-sip-gateway-current.log", Current: true, Tail: tail, Lines: []string{}}, nil
		},
		func(name string, tail int) (*logger.LogTail, error) {
			return &logger.LogTail{Name: name, Current: false, Tail: tail, Lines: []string{}}, nil
		},
	)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetAdminPassword("admin-password")
	router := setupLogFileHandlers(t, srv)

	for _, path := range []string{
		"/api/logs",
		"/api/logs/current",
		"/api/logs/webrtc-sip-gateway-2026-05-25_10-30-00.log",
	} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %s without password to return 200, got %d body=%s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestNonLogAPIRoutesStillUseAuthMiddleware(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetTokenVerifier(tokenVerifierStub{
		verify: func(_ context.Context, raw string, _ auth.TokenRealm) (*auth.VerifiedClaims, error) {
			if raw == "valid-token" {
				return &auth.VerifiedClaims{Subject: "user-1", Realm: auth.TokenRealmUser}, nil
			}
			return nil, context.DeadlineExceeded
		},
	})
	router := setupLogFileHandlers(t, srv)

	missingToken := httptest.NewRecorder()
	router.ServeHTTP(missingToken, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if missingToken.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing token 401, got %d", missingToken.Code)
	}

	validToken := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	router.ServeHTTP(validToken, req)
	if validToken.Code != http.StatusOK {
		t.Fatalf("expected valid token 200, got %d body=%s", validToken.Code, validToken.Body.String())
	}
}

func TestLogCurrentRoutePrecedesNamedRoute(t *testing.T) {
	calledCurrent := false
	calledNamed := false
	overrideLogFileFuncs(t,
		nil,
		func(tail int) (*logger.LogTail, error) {
			calledCurrent = true
			return &logger.LogTail{Name: "webrtc-sip-gateway-current.log", Current: true, Tail: tail, Lines: []string{}}, nil
		},
		func(name string, tail int) (*logger.LogTail, error) {
			calledNamed = true
			return nil, logger.ErrInvalidLogFilename
		},
	)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	router := setupLogFileHandlers(t, srv)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/logs/current", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !calledCurrent || calledNamed {
		t.Fatalf("expected current route only, calledCurrent=%v calledNamed=%v", calledCurrent, calledNamed)
	}
}
