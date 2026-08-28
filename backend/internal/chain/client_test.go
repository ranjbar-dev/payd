package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"payd/internal/config"
	"payd/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func testClient(t *testing.T) *Client {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	client, err := New(config.Tron{
		Endpoints:         []config.Endpoint{{URL: "https://node.example", APIKey: "secret", Weight: 1}},
		SolidityURL:       "https://solidity.example",
		RequestTimeout:    10 * time.Second,
		DailyRequestQuota: 100_000,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), database)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestBroadcastNeverRetriesOnNetworkError(t *testing.T) {
	client := testClient(t)
	var attempts atomic.Int32
	client.Broadcast.core.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts.Add(1)
		if request.URL.Path != "/wallet/broadcasthex" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		var payload struct {
			Transaction string `json:"transaction"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Transaction != "deadbeef" {
			t.Fatalf("transaction = %q", payload.Transaction)
		}
		if got := request.Header.Get("TRON-PRO-API-KEY"); got != "secret" {
			t.Fatalf("API key header = %q", got)
		}
		return nil, errors.New("simulated connection reset")
	})

	if _, err := client.Broadcast.Send(context.Background(), []byte(`{"transaction":"deadbeef"}`)); err == nil {
		t.Fatal("broadcast network error was hidden")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("broadcast attempts = %d, want exactly 1 (CHN-024a/WDR-014a)", got)
	}
}

func TestBroadcastReturnsHTTPError(t *testing.T) {
	client := testClient(t)
	client.Broadcast.core.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/wallet/broadcasthex" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader(`node failure`)), Header: make(http.Header)}, nil
	})

	response, err := client.Broadcast.Send(context.Background(), []byte(`{"transaction":"deadbeef"}`))
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("error = %v, want HTTP 500 error", err)
	}
	if string(response.Body) != "node failure" {
		t.Fatalf("response body = %q", response.Body)
	}
}

func TestDelegateResourceSendsDynamicResourceAndUnlockedPayload(t *testing.T) {
	client := testClient(t)
	client.Read.core.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/wallet/delegateresource" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		var payload struct {
			Owner    string `json:"owner_address"`
			Receiver string `json:"receiver_address"`
			Resource string `json:"resource"`
			Balance  int64  `json:"balance"`
			Lock     bool   `json:"lock"`
			Visible  bool   `json:"visible"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Owner != "resource-wallet" || payload.Receiver != "deposit-wallet" || payload.Resource != "BANDWIDTH" ||
			payload.Balance != 1_000_000 || payload.Lock || !payload.Visible {
			t.Fatalf("delegation payload = %#v", payload)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"txID":"id","raw_data_hex":"00"}`)), Header: make(http.Header)}, nil
	})
	if _, err := client.Read.DelegateResource(context.Background(), "resource-wallet", "deposit-wallet", "BANDWIDTH", 1_000_000); err != nil {
		t.Fatal(err)
	}
}

func TestThrottlingBacksOffAndFailsOver(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "payd.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			client, err := New(config.Tron{
				Endpoints: []config.Endpoint{
					{URL: "https://one.example", APIKey: "one", Weight: 1},
					{URL: "https://two.example", APIKey: "two", Weight: 1},
				},
				SolidityURL:       "https://solidity.example",
				RequestTimeout:    10 * time.Second,
				DailyRequestQuota: 100_000,
			}, slog.New(slog.NewTextHandler(io.Discard, nil)), database)
			if err != nil {
				t.Fatal(err)
			}
			var slept time.Duration
			client.Read.core.sleep = func(_ context.Context, duration time.Duration) error {
				slept = duration
				return nil
			}
			client.Read.core.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Hostname() == "one.example" {
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("limited")), Header: make(http.Header)}, nil
				}
				if got := request.Header.Get("TRON-PRO-API-KEY"); got != "two" {
					t.Fatalf("failover API key header = %q", got)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
			})

			if _, err := client.Read.GetNowBlock(context.Background()); err != nil {
				t.Fatal(err)
			}
			if slept != time.Second {
				t.Fatalf("first backoff = %s, want 1s", slept)
			}
		})
	}
}

func TestReadsRetryTwiceAndSendAPIKey(t *testing.T) {
	client := testClient(t)
	var attempts atomic.Int32
	client.Read.core.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempt := attempts.Add(1)
		if got := request.Header.Get("TRON-PRO-API-KEY"); got != "secret" {
			t.Fatalf("API key header = %q", got)
		}
		if attempt < 3 {
			return nil, errors.New("simulated network error")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})

	if _, err := client.Read.GetNowBlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("read attempts = %d, want initial plus two retries", got)
	}
	if got := client.RequestsToday(); got != 3 {
		t.Fatalf("daily requests = %d, want 3", got)
	}
}

func TestCircuitOpensAfterFiveFailures(t *testing.T) {
	client := testClient(t)
	var attempts atomic.Int32
	client.Read.core.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("failed")), Header: make(http.Header)}, nil
	})
	for range 5 {
		_, _ = client.Read.GetNowBlock(context.Background())
	}
	if _, err := client.Read.GetNowBlock(context.Background()); !errors.Is(err, errNoEndpoint) {
		t.Fatalf("open circuit error = %v", err)
	}
	if got := attempts.Load(); got != 5 {
		t.Fatalf("transport attempts with open circuit = %d, want 5", got)
	}

	client.Read.core.pool.mu.Lock()
	client.Read.core.pool.endpoints[0].unavailableUntil = time.Now().Add(-time.Second)
	client.Read.core.pool.mu.Unlock()
	_, _ = client.Read.GetNowBlock(context.Background())
	if got := attempts.Load(); got != 6 {
		t.Fatalf("transport attempts after circuit retry window = %d, want 6", got)
	}
}

func TestDailyCounterRollsAtUTCMidnight(t *testing.T) {
	client := testClient(t)
	written := make(map[int64]int64)
	client.counter.persist = func(_ context.Context, day, count int64, _ time.Time) error {
		written[day] = count
		return nil
	}
	now := time.Date(2026, 8, 7, 23, 59, 59, 0, time.UTC)
	client.counter.now = func() time.Time { return now }
	client.counter.increment()
	if got := client.RequestsToday(); got != 1 {
		t.Fatalf("first UTC day count = %d", got)
	}
	now = now.Add(time.Second)
	if got := client.RequestsToday(); got != 0 {
		t.Fatalf("new UTC day count = %d, want 0", got)
	}
	client.counter.increment()
	if err := client.counter.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("persisted UTC days = %v, want both sides of midnight", written)
	}
	if got := client.SoftCap(); got != 70_000 {
		t.Fatalf("soft cap = %d, want 70000", got)
	}
}

func TestCounterPersistenceFailureDoesNotAbortRPC(t *testing.T) {
	client := testClient(t)
	client.Read.core.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})
	client.counter.persist = func(context.Context, int64, int64, time.Time) error {
		return errors.New("sqlite busy")
	}

	if _, err := client.Read.GetNowBlock(context.Background()); err != nil {
		t.Fatalf("RPC failed because counter persistence failed: %v", err)
	}
	if err := client.counter.flush(context.Background()); err == nil {
		t.Fatal("counter flush error was hidden")
	}
	if _, err := client.Read.GetNowBlock(context.Background()); err != nil {
		t.Fatalf("RPC after failed counter flush: %v", err)
	}
	if got := client.RequestsToday(); got != 2 {
		t.Fatalf("in-memory requests = %d, want 2", got)
	}
}

func TestRequestCounterFinalFlush(t *testing.T) {
	client := testClient(t)
	flushed := make(chan int64, 1)
	client.counter.persist = func(_ context.Context, _, count int64, _ time.Time) error {
		flushed <- count
		return nil
	}
	client.counter.increment()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.RunRequestCounter(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request counter did not stop")
	}

	select {
	case count := <-flushed:
		if count != 1 {
			t.Fatalf("final request count = %d, want 1", count)
		}
	default:
		t.Fatal("final request count was not persisted")
	}
}

func TestQuotaProjectionUsesPersistedSevenDayTrend(t *testing.T) {
	client := testClient(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	client.counter.mu.Lock()
	client.counter.now = func() time.Time { return now }
	client.counter.rollUTC()
	for i, count := range []int64{40_000, 45_000, 50_000, 55_000, 60_000, 65_000, 70_000} {
		day := now.Truncate(24 * time.Hour).Add(time.Duration(i-7) * 24 * time.Hour).Unix()
		client.counter.counts[day] = count
	}
	client.counter.mu.Unlock()
	if got := client.QuotaProjectionRatio(); got != .75 {
		t.Fatalf("quota projection ratio = %f, want 0.75 (RL-006)", got)
	}
	var logs bytes.Buffer
	client.counter.logger = slog.New(slog.NewTextHandler(&logs, nil))
	client.counter.increment()
	if !strings.Contains(logs.String(), "seven-day quota projection crossed 60%") {
		t.Fatalf("RL-006 warning missing: %s", logs.String())
	}
}

func TestDailyCounterSurvivesClientRestart(t *testing.T) {
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Tron{Endpoints: []config.Endpoint{{URL: "https://node.example", Weight: 1}},
		SolidityURL: "https://solidity.example", RequestTimeout: 10 * time.Second, DailyRequestQuota: 100_000}
	client, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), database)
	if err != nil {
		t.Fatal(err)
	}
	client.counter.increment()
	if err := client.counter.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), database)
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.RequestsToday(); got != 1 {
		t.Fatalf("persisted daily requests = %d, want 1 (RL-006)", got)
	}
}
