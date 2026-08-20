package logstore

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNoopStoreHealthReportsDisabled(t *testing.T) {
	health := (&noopStore{}).LogStoreHealth()
	if health.Enabled || health.Connected {
		t.Fatalf("expected disabled noop store, got %#v", health)
	}
}

func TestStatsQueueFullDropsNewestWithoutBlocking(t *testing.T) {
	store := &logStore{
		pool:       &pgxpool.Pool{},
		statsQueue: make(chan *StatsRecord, 1),
		eventQueue: make(chan *Event, 1),
	}
	store.statsQueue <- &StatsRecord{SessionID: "first"}
	store.RecordStats(&StatsRecord{SessionID: "dropped"})

	health := store.LogStoreHealth()
	if health.Stats.Dropped != 1 || health.Stats.Accepted != 0 {
		t.Fatalf("unexpected stats queue counters: %#v", health.Stats)
	}
	if got := (<-store.statsQueue).SessionID; got != "first" {
		t.Fatalf("full queue must preserve earlier sample, got %q", got)
	}
}

func TestQueueHealthTracksAcceptedRecords(t *testing.T) {
	store := &logStore{
		pool:       &pgxpool.Pool{},
		statsQueue: make(chan *StatsRecord, 1),
		eventQueue: make(chan *Event, 1),
	}
	store.RecordStats(&StatsRecord{SessionID: "accepted"})
	store.LogEvent(&Event{Category: "test", Name: "accepted"})
	health := store.LogStoreHealth()
	if health.Stats.Accepted != 1 || health.Events.Accepted != 1 {
		t.Fatalf("unexpected accepted counters: %#v", health)
	}
}
