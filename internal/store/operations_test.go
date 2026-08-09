package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// OPS-008: worker_health is only useful if a stalled loop is distinguishable from a
// failing one, so last_tick_at must always advance while last_error and error_count
// accumulate independently.
func TestRecordWorkerTickTracksLivenessSeparatelyFromFailures(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()

	read := func() WorkerHealth {
		t.Helper()
		workers, err := database.WorkerHealth(ctx, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(workers) != 1 {
			t.Fatalf("worker rows = %d, want 1", len(workers))
		}
		return workers[0]
	}

	start := time.Unix(1_700_000_000, 0).UTC()
	if err := database.RecordWorkerTick(ctx, "follower", nil, start); err != nil {
		t.Fatal(err)
	}
	healthy := read()
	if healthy.LastTickAt == nil || *healthy.LastTickAt != start.Unix() || healthy.LastError != "" || healthy.ErrorCount != 0 {
		t.Fatalf("healthy tick = %#v", healthy)
	}

	if err := database.RecordWorkerTick(ctx, "follower", errors.New("boom"), start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if failed := read(); failed.LastError != "boom" || failed.ErrorCount != 1 || *failed.LastTickAt != start.Add(time.Second).Unix() {
		t.Fatalf("failed tick = %#v", failed)
	}

	// Recovery advances liveness but must not erase the fault an operator has not seen yet.
	if err := database.RecordWorkerTick(ctx, "follower", nil, start.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	recovered := read()
	if recovered.LastError != "boom" || recovered.ErrorCount != 1 || *recovered.LastTickAt != start.Add(2*time.Second).Unix() {
		t.Fatalf("recovered tick = %#v", recovered)
	}

	if err := database.RecordWorkerTick(ctx, "follower", errors.New("second"), start.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if repeated := read(); repeated.ErrorCount != 2 || repeated.LastError != "second" {
		t.Fatalf("second failure = %#v", repeated)
	}

	// A cancelled tick is shutdown, not a fault: it must not inflate error_count.
	if err := database.RecordWorkerTick(ctx, "follower", context.Canceled, start.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if cancelled := read(); cancelled.ErrorCount != 2 || *cancelled.LastTickAt != start.Add(3*time.Second).Unix() {
		t.Fatalf("cancelled tick = %#v", cancelled)
	}

	// The final tick still lands once the worker's context is already cancelled.
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if err := database.RecordWorkerTick(stopped, "follower", errors.New(strings.Repeat("x", 900)), start.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	shutdown := read()
	if *shutdown.LastTickAt != start.Add(5*time.Second).Unix() {
		t.Fatalf("detached tick did not land: %#v", shutdown)
	}
	if len(shutdown.LastError) != 500 {
		t.Fatalf("last_error was not truncated: %d chars", len(shutdown.LastError))
	}
}
