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
		if request.URL.Path != "/wallet/broadcasttransaction" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if got := request.Header.Get("TRON-PRO-API-KEY"); got != "secret" {
			t.Fatalf("API key header = %q", got)
		}
		return nil, errors.New("simulated connection reset")
	})

	if _, err := client.Broadcast.Send(context.Background(), []byte(`{"txID":"abc"}`)); err == nil {
		t.Fatal("broadcast network error was hidden")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("broadcast attempts = %d, want exactly 1 (CHN-024a/WDR-014a)", got)
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
	now := time.Date(2026, 8, 7, 23, 59, 59, 0, time.UTC)
	client.counter.now = func() time.Time { return now }
	if err := client.counter.increment(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := client.RequestsToday(); got != 1 {
		t.Fatalf("first UTC day count = %d", got)
	}
	now = now.Add(time.Second)
	if got := client.RequestsToday(); got != 0 {
		t.Fatalf("new UTC day count = %d, want 0", got)
	}
	if got := client.SoftCap(); got != 70_000 {
		t.Fatalf("soft cap = %d, want 70000", got)
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
	if err := client.counter.increment(context.Background()); err != nil {
		t.Fatal(err)
	}
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
	if err := client.counter.increment(context.Background()); err != nil {
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
