package logstore

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
)

func TestSingleInstanceConfigurableInterface(t *testing.T) {
	noop := Noop()
	cfgNoop, ok := noop.(SingleInstanceConfigurable)
	if !ok {
		t.Fatal("expected Noop() to implement SingleInstanceConfigurable")
	}
	cfgNoop.SetSingleInstance(true)
	cfgNoop.SetSingleInstance(false)

	store, err := New(config.DBConfig{Enable: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	cfgStore, ok := store.(SingleInstanceConfigurable)
	if !ok {
		t.Fatal("expected *logStore to implement SingleInstanceConfigurable")
	}
	cfgStore.SetSingleInstance(true)
	if !store.(*logStore).singleInstance.Load() {
		t.Fatal("expected singleInstance to be true after SetSingleInstance(true)")
	}
}

func TestSingleInstanceSkipsUpsertSessionDirectorySyncAndAsync(t *testing.T) {
	var asyncWrites atomic.Int64

	store := &logStore{
		config: config.DBConfig{
			BatchSize:       10,
			BatchIntervalMS: 50,
		},
		upsertSessionDirHook: func(ctx context.Context, sessionID, ownerInstanceID, wsURL string, ttlSeconds int) error {
			asyncWrites.Add(1)
			return nil
		},
	}
	store.SetSingleInstance(true)
	store.startAsyncWorkers()

	// Sync call returns nil immediately without touching DB pool (even with nil pool).
	if err := store.UpsertSessionDirectory(context.Background(), "sess-1", "gw-1", "wss://gw-1/ws", 3600); err != nil {
		t.Fatalf("UpsertSessionDirectory() in single-instance mode error = %v, want nil", err)
	}

	// Async call is a no-op: does not enqueue or invoke hook.
	store.UpsertSessionDirectoryAsync("sess-1", "gw-1", "wss://gw-1/ws", 3600)

	if err := store.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := asyncWrites.Load(); got != 0 {
		t.Fatalf("expected 0 session_directory writes in single-instance mode, got %d", got)
	}
	if got := store.asyncAccepted.Load(); got != 0 {
		t.Fatalf("expected 0 asyncAccepted in single-instance mode, got %d", got)
	}
}

func TestMultiInstancePreservesUpsertSessionDirectoryBehavior(t *testing.T) {
	store := &logStore{}
	// Default singleInstance is false; sync call must still attempt pool access.
	if err := store.UpsertSessionDirectory(context.Background(), "sess-1", "gw-1", "wss://gw-1/ws", 3600); err == nil {
		t.Fatal("expected error with nil pool when singleInstance=false")
	}
}

func TestSingleInstanceSkipsCleanupWorkersWithoutStopDeadlock(t *testing.T) {
	t.Run("singleInstance=true skips cleanup workers and stops cleanly", func(t *testing.T) {
		var sessionDirCleanups atomic.Int64
		var gatewayInstanceCleanups atomic.Int64

		store := &logStore{
			config: config.DBConfig{
				BatchSize:       10,
				BatchIntervalMS: 50,
			},
			sessionDirCleanupHook: func() {
				sessionDirCleanups.Add(1)
			},
			gatewayInstanceCleanupHook: func() {
				gatewayInstanceCleanups.Add(1)
			},
		}
		store.SetSingleInstance(true)
		store.startAsyncWorkers()
		store.startBackgroundWorkers()

		// Give goroutines a moment to start if they were mistakenly spawned.
		time.Sleep(25 * time.Millisecond)

		stopDone := make(chan error, 1)
		go func() {
			stopDone <- store.Stop()
		}()

		select {
		case err := <-stopDone:
			if err != nil {
				t.Fatalf("Stop() error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Stop() deadlocked in single-instance mode")
		}

		if sessionDirCleanups.Load() != 0 {
			t.Fatalf("expected sessionDirectoryCleanupWorker not to run, got %d runs", sessionDirCleanups.Load())
		}
		if gatewayInstanceCleanups.Load() != 0 {
			t.Fatalf("expected gatewayInstanceCleanupWorker not to run, got %d runs", gatewayInstanceCleanups.Load())
		}
	})

	t.Run("singleInstance=false launches both cleanup workers", func(t *testing.T) {
		sessionDirRan := make(chan struct{}, 1)
		gatewayInstanceRan := make(chan struct{}, 1)

		store := &logStore{
			config: config.DBConfig{
				BatchSize:       10,
				BatchIntervalMS: 50,
			},
			sessionDirCleanupHook: func() {
				select {
				case sessionDirRan <- struct{}{}:
				default:
				}
			},
			gatewayInstanceCleanupHook: func() {
				select {
				case gatewayInstanceRan <- struct{}{}:
				default:
				}
			},
		}
		store.SetSingleInstance(false)
		store.startAsyncWorkers()
		store.startBackgroundWorkers()

		select {
		case <-sessionDirRan:
		case <-time.After(time.Second):
			t.Fatal("expected sessionDirectoryCleanupWorker to run when singleInstance=false")
		}
		select {
		case <-gatewayInstanceRan:
		case <-time.After(time.Second):
			t.Fatal("expected gatewayInstanceCleanupWorker to run when singleInstance=false")
		}

		if err := store.Stop(); err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	})
}
