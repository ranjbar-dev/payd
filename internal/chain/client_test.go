package chain

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"payd/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func testClient(t *testing.T) *Client {
	t.Helper()
	client, err := New(config.Tron{
		Endpoints:         []config.Endpoint{{URL: "https://node.example", APIKey: "secret", Weight: 1}},
		SolidityURL:       "https://solidity.example",
		RequestTimeout:    10 * time.Second,
		DailyRequestQuota: 100_000,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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

func TestThrottlingBacksOffAndFailsOver(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, err := New(config.Tron{
				Endpoints: []config.Endpoint{
					{URL: "https://one.example", APIKey: "one", Weight: 1},
					{URL: "https://two.example", APIKey: "two", Weight: 1},
				},
				SolidityURL:       "https://solidity.example",
				RequestTimeout:    10 * time.Second,
				DailyRequestQuota: 100_000,
			}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	client.counter.increment()
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
