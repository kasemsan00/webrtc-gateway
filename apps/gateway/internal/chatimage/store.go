package chatimage

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var (
	ErrNotFound   = errors.New("chat image not found")
	ErrInvalidID  = errors.New("invalid chat image id")
	ErrEmptyImage = errors.New("empty chat image")
)

var imageIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Config is the on-disk store configuration.
type Config struct {
	Dir             string
	TTL             time.Duration
	CleanupInterval time.Duration
}

// Record is persisted metadata for one stored image.
type Record struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"sessionId"`
	ContentType string    `json:"contentType"`
	Bytes       int64     `json:"bytes"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Store writes opaque UUID-named files under Dir.
type Store struct {
	cfg Config
	mu  sync.Mutex
}

// Open creates Dir if needed and returns a store.
func Open(cfg Config) (*Store, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("chat image directory is required")
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 24 * time.Hour
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 15 * time.Minute
	}
	if err := os.MkdirAll(cfg.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("create chat image directory: %w", err)
	}
	return &Store{cfg: cfg}, nil
}

// Put writes image bytes and sidecar metadata. id is a generated UUID.
func (s *Store) Put(sessionID, contentType string, data []byte) (*Record, error) {
	if s == nil {
		return nil, errors.New("chat image store is not configured")
	}
	if len(data) == 0 {
		return nil, ErrEmptyImage
	}
	id, err := newImageID()
	if err != nil {
		return nil, err
	}
	record := Record{
		ID:          id,
		SessionID:   sessionID,
		ContentType: contentType,
		Bytes:       int64(len(data)),
		CreatedAt:   time.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.WriteFile(s.blobPath(id), data, 0o640); err != nil {
		return nil, fmt.Errorf("write chat image: %w", err)
	}
	meta, err := json.Marshal(record)
	if err != nil {
		_ = os.Remove(s.blobPath(id))
		return nil, err
	}
	if err := os.WriteFile(s.metaPath(id), meta, 0o640); err != nil {
		_ = os.Remove(s.blobPath(id))
		return nil, fmt.Errorf("write chat image metadata: %w", err)
	}
	return &record, nil
}

// Get returns metadata and bytes for a still-valid image.
func (s *Store) Get(id string, now time.Time) (*Record, []byte, error) {
	if s == nil {
		return nil, nil, errors.New("chat image store is not configured")
	}
	if !ValidID(id) {
		return nil, nil, ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.readMetaLocked(id)
	if err != nil {
		return nil, nil, err
	}
	if s.expired(record, now) {
		s.removeLocked(id)
		return nil, nil, ErrNotFound
	}
	data, err := os.ReadFile(s.blobPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	return record, data, nil
}

// DeleteExpired removes images older than TTL. Returns the number of images removed.
func (s *Store) DeleteExpired(now time.Time) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.cfg.Dir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		id := name[:len(name)-len(".json")]
		if !ValidID(id) {
			continue
		}
		record, err := s.readMetaLocked(id)
		if err != nil {
			continue
		}
		if s.expired(record, now) {
			s.removeLocked(id)
			removed++
		}
	}
	return removed
}

// StartCleanup runs DeleteExpired until ctx is cancelled.
func (s *Store) StartCleanup(ctx context.Context) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(s.cfg.CleanupInterval)
	defer ticker.Stop()
	s.DeleteExpired(time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.DeleteExpired(now.UTC())
		}
	}
}

// ValidID reports whether id is a UUID filename, not a path fragment.
func ValidID(id string) bool {
	return imageIDPattern.MatchString(id)
}

func (s *Store) expired(record *Record, now time.Time) bool {
	return now.Sub(record.CreatedAt) > s.cfg.TTL
}

func (s *Store) blobPath(id string) string {
	return filepath.Join(s.cfg.Dir, id)
}

func (s *Store) metaPath(id string) string {
	return filepath.Join(s.cfg.Dir, id+".json")
}

func (s *Store) readMetaLocked(id string) (*Record, error) {
	raw, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	if record.ID != id {
		return nil, ErrNotFound
	}
	return &record, nil
}

func (s *Store) removeLocked(id string) {
	_ = os.Remove(s.blobPath(id))
	_ = os.Remove(s.metaPath(id))
}

func newImageID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
