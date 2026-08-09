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
	dbStarted := time.Now()
	for range calls {
		if _, err := client.counter.store.RecordTronGridRequest(context.Background(), time.Now()); err != nil {
			t.Fatalf("direct counter write: %v", err)
		}
	}
	dbElapsed := time.Since(dbStarted)

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
	if got := client.RequestsToday(); got != 3*calls {
		t.Fatalf("durable request count = %d, want %d", got, 3*calls)
	}

	dbPerCall := dbElapsed / calls
	t.Logf("daily-counter DB write: %s/call", dbPerCall)
	t.Logf("1,000 sequential catch-up calls: %s total, %s/call", sequentialElapsed, sequentialElapsed/calls)
	t.Logf("four-worker shared-client burst: %s total, %s/call, 0 SQLITE_BUSY errors", concurrentElapsed, concurrentElapsed/calls)
	t.Logf("counter write adds %.3f%% to the follower's 8 req/s catch-up interval", float64(dbPerCall)/float64(catchUpRequestInterval)*100)
}

const catchUpRequestInterval = time.Second / 8 // RL-005 / follower.catchUpInterval.
