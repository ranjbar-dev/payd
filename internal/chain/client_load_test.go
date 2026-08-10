package chain

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDailyCounterCatchUpAndWorkerBurst(t *testing.T) {
	client := testClient(t)
	client.Read.core.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})

	const calls = 1_000
	var writes atomic.Int64
	persist := client.counter.persist
	client.counter.persist = func(ctx context.Context, day, count int64, at time.Time) error {
		writes.Add(1)
		return persist(ctx, day, count, at)
	}

	sequentialStarted := time.Now()
	for height := range calls {
		if _, err := client.Read.GetBlockByNum(context.Background(), int64(height)); err != nil {
			t.Fatalf("sequential catch-up request: %v", err)
		}
	}
	sequentialElapsed := time.Since(sequentialStarted)

	const workers = 4
	var workerErrors atomic.Int64
	concurrentStarted := time.Now()
	var group sync.WaitGroup
	for worker := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for request := range calls / workers {
				if _, err := client.Read.GetBlockByNum(context.Background(), int64(worker*calls/workers+request)); err != nil {
					workerErrors.Add(1)
				}
			}
		}()
	}
	group.Wait()
	concurrentElapsed := time.Since(concurrentStarted)
	if errors := workerErrors.Load(); errors != 0 {
		t.Fatalf("concurrent worker errors (including SQLITE_BUSY) = %d", errors)
	}
	if got := client.RequestsToday(); got != 2*calls {
		t.Fatalf("request count = %d, want %d", got, 2*calls)
	}
	if got := writes.Load(); got != 0 {
		t.Fatalf("SQLite writes on RPC path = %d, want 0", got)
	}
	if err := client.counter.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := writes.Load(); got != 1 {
		t.Fatalf("SQLite writes after one flush = %d, want 1", got)
	}

	t.Logf("1,000 sequential catch-up calls: %s total, %s/call", sequentialElapsed, sequentialElapsed/calls)
	t.Logf("four-worker shared-client burst: %s total, %s/call, 0 SQLITE_BUSY errors", concurrentElapsed, concurrentElapsed/calls)
}
