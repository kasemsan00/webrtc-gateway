package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

func readWSTestMessage(t *testing.T, client *WSClient) WSMessage {
	t.Helper()

	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("failed to unmarshal ws message: %v", err)
		}
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket message")
		return WSMessage{}
	}
}

func assertNoWSTestMessage(t *testing.T, client *WSClient) {
	t.Helper()

	select {
	case data := <-client.send:
		t.Fatalf("unexpected websocket message: %s", string(data))
	default:
	}
}

func TestHandleWSIceMessageDoesNotReturnUnknownType(t *testing.T) {
	server := &Server{}
	client := &WSClient{send: make(chan []byte, 1)}

	server.handleWSMessage(client, []byte(`{"type":"ice","candidate":{"candidate":"candidate:1 1 udp 2122252543 192.0.2.1 54545 typ host","sdpMid":"0","sdpMLineIndex":0}}`))

	msg := readWSTestMessage(t, client)
	if msg.Type != "error" {
		t.Fatalf("expected error response, got %q", msg.Type)
	}
	if msg.Error == "Unknown message type" {
		t.Fatalf("ice message should not be treated as unknown type")
	}
	if msg.Error != "Session ID required for ICE candidate" {
		t.Fatalf("expected session ID error, got %q", msg.Error)
	}
}

func TestHandleWSIceIgnoresNullCandidate(t *testing.T) {
	server := &Server{}
	client := &WSClient{send: make(chan []byte, 1)}

	server.handleWSMessage(client, []byte(`{"type":"ice","candidate":null}`))

	assertNoWSTestMessage(t, client)
}

type snapshotTestStore struct {
	logstore.LogStore
	upsertCount int
	last        *logstore.SessionRecord
	err         error
}

func (s *snapshotTestStore) UpsertSession(ctx context.Context, record *logstore.SessionRecord) error {
	s.upsertCount++
	s.last = record
	return s.err
}

func TestLogSessionSnapshotSkipsEmptyDirection(t *testing.T) {
	store := &snapshotTestStore{}
	server := &Server{logStore: store}
	sess := &session.Session{
		ID:        "sess-empty-direction",
		State:     session.StateConnecting,
		CreatedAt: time.Now(),
	}

	server.logSessionSnapshot(context.Background(), sess, "")

	if store.upsertCount != 0 {
		t.Fatalf("expected no upsert for empty direction, got %d", store.upsertCount)
	}
}

func TestLogSessionSnapshotUpsertsValidDirection(t *testing.T) {
	store := &snapshotTestStore{}
	server := &Server{logStore: store}
	sess := &session.Session{
		ID:        "sess-outbound",
		State:     session.StateConnecting,
		CreatedAt: time.Now(),
	}
	sess.SetCallInfo("outbound", "1000", "9999", "call-id")

	server.logSessionSnapshot(context.Background(), sess, "")

	if store.upsertCount != 1 {
		t.Fatalf("expected one upsert for outbound direction, got %d", store.upsertCount)
	}
	if store.last == nil || store.last.Direction != "outbound" {
		t.Fatalf("expected outbound snapshot, got %#v", store.last)
	}
}

func TestLogSessionSnapshotLogsUpsertErrorsWithoutPanic(t *testing.T) {
	store := &snapshotTestStore{err: errors.New("db unavailable")}
	server := &Server{logStore: store}
	sess := &session.Session{
		ID:        "sess-upsert-error",
		State:     session.StateConnecting,
		CreatedAt: time.Now(),
	}
	sess.SetCallInfo("outbound", "1000", "9999", "call-id")

	server.logSessionSnapshot(context.Background(), sess, "")

	if store.upsertCount != 1 {
		t.Fatalf("expected one upsert attempt, got %d", store.upsertCount)
	}
}
