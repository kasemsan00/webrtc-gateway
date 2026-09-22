package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"webrtc-sip-gateway/internal/chatimage"
	"webrtc-sip-gateway/internal/session"
)

const chatImageMultipartOverhead = 256 * 1024

type chatImageUploadResponse struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	Bytes       int64  `json:"bytes"`
}

type chatImageRateState struct {
	count int
}

func (s *Server) handleUploadChatImage(w http.ResponseWriter, r *http.Request) {
	store := s.chatImageStore
	cfg := s.chatImageConfig()
	if store == nil || !cfg.Enable {
		s.respondError(w, http.StatusServiceUnavailable, "Chat image upload is disabled")
		return
	}

	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+chatImageMultipartOverhead)
	if err := r.ParseMultipartForm(maxBytes + chatImageMultipartOverhead); err != nil {
		log.Printf("🖼️ [ChatImage] rejected multipart: %v", err)
		s.respondError(w, http.StatusBadRequest, "Invalid image upload")
		return
	}

	sessionID := strings.TrimSpace(r.FormValue("sessionId"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.Header.Get("X-Session-Id"))
	}
	if sessionID == "" {
		s.respondError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	if !s.liveChatImageSession(sessionID) {
		s.respondError(w, http.StatusForbidden, "Active call session required")
		return
	}
	if !s.chatImageUploadAllowed(sessionID, cfg.MaxPerSession) {
		s.respondError(w, http.StatusTooManyRequests, "Chat image upload limit exceeded")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Failed to read image")
		return
	}
	if int64(len(data)) > maxBytes {
		s.respondError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Image exceeds %d bytes", maxBytes))
		return
	}

	contentType, ok := chatimage.DetectContentType(data)
	if !ok {
		s.respondError(w, http.StatusBadRequest, "Unsupported image type")
		return
	}

	record, err := store.Put(sessionID, contentType, data)
	if err != nil {
		log.Printf("🖼️ [ChatImage] store failed session=%s: %v", sessionID, err)
		s.respondError(w, http.StatusInternalServerError, "Failed to store image")
		return
	}
	s.noteChatImageUpload(sessionID)

	log.Printf("🖼️ [ChatImage] stored id=%s session=%s type=%s bytes=%d", record.ID, sessionID, record.ContentType, record.Bytes)
	s.respondJSON(w, http.StatusCreated, chatImageUploadResponse{
		ID:          record.ID,
		URL:         s.publicChatImageURL(r, record.ID),
		ContentType: record.ContentType,
		Bytes:       record.Bytes,
	})
}

func (s *Server) handleGetChatImage(w http.ResponseWriter, r *http.Request) {
	store := s.chatImageStore
	cfg := s.chatImageConfig()
	if store == nil || !cfg.Enable {
		s.respondError(w, http.StatusServiceUnavailable, "Chat image upload is disabled")
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	record, data, err := store.Get(id, time.Now().UTC())
	if errors.Is(err, chatimage.ErrInvalidID) {
		s.respondError(w, http.StatusBadRequest, "Invalid image id")
		return
	}
	if errors.Is(err, chatimage.ErrNotFound) {
		s.respondError(w, http.StatusNotFound, "Image not found")
		return
	}
	if err != nil {
		log.Printf("🖼️ [ChatImage] get failed id=%s: %v", id, err)
		s.respondError(w, http.StatusInternalServerError, "Failed to read image")
		return
	}
	w.Header().Set("Content-Type", record.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) liveChatImageSession(sessionID string) bool {
	if s.sessionMgr == nil {
		return false
	}
	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok || sess == nil {
		return false
	}
	return sess.GetState() != session.StateEnded
}

func (s *Server) chatImageUploadAllowed(sessionID string, maxPerSession int) bool {
	if maxPerSession <= 0 {
		maxPerSession = 20
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.chatImageLimits[sessionID]
	if state == nil {
		return true
	}
	return state.count < maxPerSession
}

func (s *Server) noteChatImageUpload(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.chatImageLimits[sessionID]
	if state == nil {
		state = &chatImageRateState{}
		s.chatImageLimits[sessionID] = state
	}
	state.count++
}

func (s *Server) publicChatImageURL(r *http.Request, id string) string {
	base := strings.TrimRight(strings.TrimSpace(s.chatImageConfig().PublicBaseURL), "/")
	if base == "" {
		scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
		if scheme == "" {
			if r.TLS != nil {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}
		host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
		if host == "" {
			host = r.Host
		}
		base = scheme + "://" + host
	}
	escaped, err := url.PathUnescape(id)
	if err != nil {
		escaped = id
	}
	return strings.TrimRight(base, "/") + "/api/chat-images/" + escaped
}

func (s *Server) chatImageConfig() configChatImageView {
	if s.runtimeConfig != nil {
		return configChatImageView{
			Enable:        s.runtimeConfig.ChatImage.Enable,
			PublicBaseURL: s.runtimeConfig.ChatImage.PublicBaseURL,
			MaxBytes:      s.runtimeConfig.ChatImage.MaxBytes,
			MaxPerSession: s.runtimeConfig.ChatImage.MaxPerSession,
		}
	}
	return configChatImageView{}
}

type configChatImageView struct {
	Enable        bool
	PublicBaseURL string
	MaxBytes      int64
	MaxPerSession int
}

func (s *Server) startChatImageStore(ctx context.Context) {
	if s.chatImageStore != nil {
		go s.chatImageStore.StartCleanup(ctx)
		return
	}
	if s.runtimeConfig == nil || !s.runtimeConfig.ChatImage.Enable {
		return
	}
	cfg := s.runtimeConfig.ChatImage
	store, err := chatimage.Open(chatimage.Config{
		Dir:             cfg.Dir,
		TTL:             time.Duration(cfg.TTLSeconds) * time.Second,
		CleanupInterval: time.Duration(cfg.CleanupIntervalSeconds) * time.Second,
	})
	if err != nil {
		log.Printf("🖼️ [ChatImage] disabled: %v", err)
		return
	}
	s.chatImageStore = store
	log.Printf("🖼️ [ChatImage] enabled dir=%s maxBytes=%d ttl=%ds", cfg.Dir, cfg.MaxBytes, cfg.TTLSeconds)
	go store.StartCleanup(ctx)
}
