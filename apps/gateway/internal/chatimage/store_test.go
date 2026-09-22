package chatimage

import (
	"bytes"
	"testing"
	"time"
)

// 1x1 PNG
var png1x1 = []byte{
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

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
		ok   bool
	}{
		{name: "png", data: png1x1, want: ContentTypePNG, ok: true},
		{name: "jpeg", data: []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01}, want: ContentTypeJPEG, ok: true},
		{name: "gif", data: []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00\x00"), want: ContentTypeGIF, ok: true},
		{name: "webp", data: append([]byte("RIFF\x0c\x00\x00\x00WEBP"), bytes.Repeat([]byte{0}, 8)...), want: ContentTypeWEBP, ok: true},
		{name: "text", data: []byte("hello world!!!!"), ok: false},
		{name: "short", data: []byte("png"), ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DetectContentType(tt.data)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("DetectContentType() = %q, %v want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestStorePutGetAndExpire(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Config{Dir: dir, TTL: time.Hour, CleanupInterval: time.Minute})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	record, err := store.Put("sess-1", ContentTypePNG, png1x1)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !ValidID(record.ID) {
		t.Fatalf("invalid id %q", record.ID)
	}
	got, data, err := store.Get(record.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SessionID != "sess-1" || got.ContentType != ContentTypePNG {
		t.Fatalf("unexpected record %#v", got)
	}
	if !bytes.Equal(data, png1x1) {
		t.Fatalf("blob mismatch")
	}
	if _, _, err := store.Get("../etc/passwd", time.Now().UTC()); err != ErrInvalidID {
		t.Fatalf("path traversal Get error = %v, want ErrInvalidID", err)
	}
	expired, data, err := store.Get(record.ID, record.CreatedAt.Add(2*time.Hour))
	if err != ErrNotFound || expired != nil || data != nil {
		t.Fatalf("expired Get = %#v %v %v", expired, data, err)
	}
}

func TestDeleteExpired(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Config{Dir: dir, TTL: time.Minute, CleanupInterval: time.Minute})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	record, err := store.Put("sess-1", ContentTypePNG, png1x1)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if removed := store.DeleteExpired(record.CreatedAt.Add(30 * time.Second)); removed != 0 {
		t.Fatalf("removed fresh image: %d", removed)
	}
	if removed := store.DeleteExpired(record.CreatedAt.Add(2 * time.Minute)); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, _, err := store.Get(record.ID, time.Now().UTC()); err != ErrNotFound {
		t.Fatalf("Get after expire = %v", err)
	}
}
