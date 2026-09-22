package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"webrtc-sip-gateway/internal/chatimage"
	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

var chatImagePNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54,
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
	0x0d, 0x0a, 0x2d, 0xb4,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
	0xae, 0x42, 0x60, 0x82,
}

func newChatImageServer(t *testing.T, maxBytes int64, maxPerSession int) (*Server, *session.Session) {
	t.Helper()
	store, err := chatimage.Open(chatimage.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	mgr := session.NewManager(&config.Config{})
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sess.SetState(session.StateActive)
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	srv.chatImageStore = store
	srv.SetRuntimeConfig(&config.Config{
		ChatImage: config.ChatImageConfig{
			Enable:        true,
			PublicBaseURL: "https://k2-gateway.example.test",
			MaxBytes:      maxBytes,
			MaxPerSession: maxPerSession,
		},
	})
	return srv, sess
}

func chatImageRouter(srv *Server) *mux.Router {
	router := mux.NewRouter()
	router.HandleFunc("/api/chat-images", srv.handleUploadChatImage).Methods("POST")
	router.HandleFunc("/api/chat-images/{id}", srv.handleGetChatImage).Methods("GET")
	return router
}

func postChatImage(t *testing.T, router http.Handler, sessionID string, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if sessionID != "" {
		if err := writer.WriteField("sessionId", sessionID); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-images", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestChatImageUploadAndGet(t *testing.T) {
	srv, sess := newChatImageServer(t, 2*1024*1024, 20)
	router := chatImageRouter(srv)

	rr := postChatImage(t, router, sess.ID, "pic.png", chatImagePNG)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", rr.Code, rr.Body.String())
	}
	var created chatImageUploadResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ContentType != chatimage.ContentTypePNG {
		t.Fatalf("contentType = %q", created.ContentType)
	}
	if created.URL != "https://k2-gateway.example.test/api/chat-images/"+created.ID {
		t.Fatalf("url = %q", created.URL)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/chat-images/"+created.ID, nil)
	got := httptest.NewRecorder()
	router.ServeHTTP(got, get)
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", got.Code, got.Body.String())
	}
	if got.Header().Get("Content-Type") != chatimage.ContentTypePNG {
		t.Fatalf("get content-type = %q", got.Header().Get("Content-Type"))
	}
	if !bytes.Equal(got.Body.Bytes(), chatImagePNG) {
		t.Fatalf("get body mismatch")
	}
}

func TestChatImageUploadRejectsMissingAndEndedSession(t *testing.T) {
	srv, sess := newChatImageServer(t, 2*1024*1024, 20)
	router := chatImageRouter(srv)

	missing := postChatImage(t, router, "", "pic.png", chatImagePNG)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing session status = %d", missing.Code)
	}
	unknown := postChatImage(t, router, "not-a-session", "pic.png", chatImagePNG)
	if unknown.Code != http.StatusForbidden {
		t.Fatalf("unknown session status = %d", unknown.Code)
	}
	sess.SetState(session.StateEnded)
	ended := postChatImage(t, router, sess.ID, "pic.png", chatImagePNG)
	if ended.Code != http.StatusForbidden {
		t.Fatalf("ended session status = %d", ended.Code)
	}
}

func TestChatImageUploadRejectsUnsupportedAndOversize(t *testing.T) {
	srv, sess := newChatImageServer(t, 64, 20)
	router := chatImageRouter(srv)

	unsupported := postChatImage(t, router, sess.ID, "note.txt", []byte("this is not an image file"))
	if unsupported.Code != http.StatusBadRequest {
		t.Fatalf("unsupported status = %d body=%s", unsupported.Code, unsupported.Body.String())
	}

	oversize := append([]byte(nil), chatImagePNG...)
	oversize = append(oversize, bytes.Repeat([]byte{0}, 128)...)
	tooBig := postChatImage(t, router, sess.ID, "pic.png", oversize)
	if tooBig.Code != http.StatusRequestEntityTooLarge && tooBig.Code != http.StatusBadRequest {
		t.Fatalf("oversize status = %d body=%s", tooBig.Code, tooBig.Body.String())
	}
}

func TestChatImageUploadRateLimit(t *testing.T) {
	srv, sess := newChatImageServer(t, 2*1024*1024, 1)
	router := chatImageRouter(srv)
	first := postChatImage(t, router, sess.ID, "pic.png", chatImagePNG)
	if first.Code != http.StatusCreated {
		t.Fatalf("first upload status = %d body=%s", first.Code, first.Body.String())
	}
	second := postChatImage(t, router, sess.ID, "pic.png", chatImagePNG)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second upload status = %d body=%s", second.Code, second.Body.String())
	}
}

func TestChatImageGetNotFoundAndTraversal(t *testing.T) {
	srv, _ := newChatImageServer(t, 2*1024*1024, 20)
	router := chatImageRouter(srv)

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/chat-images/00000000-0000-4000-8000-000000000000", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing get status = %d", missing.Code)
	}

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/chat-images/not-a-uuid", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d", invalid.Code)
	}
}

func TestChatImageDisabled(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	router := chatImageRouter(srv)
	rr := postChatImage(t, router, "sess", "pic.png", chatImagePNG)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled upload status = %d", rr.Code)
	}
	got := httptest.NewRecorder()
	router.ServeHTTP(got, httptest.NewRequest(http.MethodGet, "/api/chat-images/00000000-0000-4000-8000-000000000000", nil))
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled get status = %d", got.Code)
	}
}

func TestPublicChatImageURLUsesForwardedHost(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	srv.SetRuntimeConfig(&config.Config{ChatImage: config.ChatImageConfig{Enable: true}})
	req := httptest.NewRequest(http.MethodPost, "/api/chat-images", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "k2-gateway.kasemsan.com")
	got := srv.publicChatImageURL(req, "11111111-1111-4111-8111-111111111111")
	if got != "https://k2-gateway.kasemsan.com/api/chat-images/11111111-1111-4111-8111-111111111111" {
		t.Fatalf("url = %q", got)
	}
}

func TestChatImageGetUsesStoreBytes(t *testing.T) {
	srv, sess := newChatImageServer(t, 2*1024*1024, 20)
	router := chatImageRouter(srv)
	rr := postChatImage(t, router, sess.ID, "pic.png", chatImagePNG)
	var created chatImageUploadResponse
	if err := json.NewDecoder(io.NopCloser(bytes.NewReader(rr.Body.Bytes()))).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Bytes != int64(len(chatImagePNG)) {
		t.Fatalf("bytes = %d", created.Bytes)
	}
}
