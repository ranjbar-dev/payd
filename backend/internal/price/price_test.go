package price

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"payd/internal/config"
	"payd/internal/store"
)

type providerFunc func(context.Context, []string) (map[string]string, error)

func (f providerFunc) Fetch(ctx context.Context, pairs []string) (map[string]string, error) {
	return f(ctx, pairs)
}

func TestBinanceFetchUsesMultiSymbolTicker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v3/ticker/price" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.URL.Query().Get("symbols"); got != `["TRXUSDT"]` {
			t.Errorf("symbols = %q", got)
		}
		_, _ = io.WriteString(response, `[{"symbol":"TRXUSDT","price":"0.33810000"}]`)
	}))
	defer server.Close()

	prices, err := NewBinance(server.URL+"/api/v3/ticker/price", server.Client()).Fetch(context.Background(), []string{"TRXUSDT"})
	if err != nil || prices["TRXUSDT"] != "0.33810000" {
		t.Fatalf("prices = %v, err = %v", prices, err)
	}
}

func TestPollerPreservesLastGoodPriceAndCountsFailures(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	calls := 0
	provider := providerFunc(func(_ context.Context, pairs []string) (map[string]string, error) {
		calls++
		if len(pairs) != 1 || pairs[0] != "TRXUSDT" {
			t.Fatalf("pairs = %v", pairs)
		}
		if calls > 1 {
			return nil, errors.New("Binance unavailable")
		}
		return map[string]string{"TRXUSDT": "0.25"}, nil
	})
	cfg := config.Price{Provider: "binance", Interval: time.Minute, Pairs: []string{"TRXUSDT"}, StaleAfter: 5 * time.Minute}
	poller, err := NewWithProvider(cfg, database, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	value, fetchedAt, err := database.AssetPrice(ctx, "TRX")
	if err != nil || value != "0.25" {
		t.Fatalf("stored price = %q at %d, err = %v", value, fetchedAt, err)
	}
	if err := poller.Refresh(ctx); err == nil || poller.ErrorCount() != 1 {
		t.Fatalf("failed refresh err = %v, error count = %d", err, poller.ErrorCount())
	}
	got, gotAt, err := database.AssetPrice(ctx, "TRX")
	if err != nil || got != value || gotAt != fetchedAt {
		t.Fatalf("failed fetch changed price to %q at %d, err = %v", got, gotAt, err)
	}

	if _, err := Current(ctx, database, cfg, "TRX", time.Unix(fetchedAt, 0).Add(cfg.StaleAfter)); err != nil {
		t.Fatalf("price at exact stale boundary rejected: %v", err)
	}
	if _, err := Current(ctx, database, cfg, "TRX", time.Unix(fetchedAt, 0).Add(cfg.StaleAfter+time.Second)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale price error = %v", err)
	}
}

func TestStablecoinsBypassPricesUnlessPairConfigured(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Unix(1_750_000_000, 0)
	cfg := config.Price{Pairs: []string{"TRXUSDT"}, StaleAfter: 5 * time.Minute}
	for _, symbol := range []string{"USDT", "USDC"} {
		quote, err := Current(ctx, database, cfg, symbol, now)
		if err != nil || quote.USD != "1.00" || quote.FetchedAt != now.Unix() {
			t.Fatalf("%s quote = %+v, err = %v", symbol, quote, err)
		}
	}
	cfg.Pairs = append(cfg.Pairs, "USDCUSDT")
	if _, err := Current(ctx, database, cfg, "USDC", now); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("explicit USDC pair did not require fetched quote: %v", err)
	}
}

func TestRetryDelayCapsAtFiveMinutes(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for index, expected := range want {
		if got := retryDelay(time.Minute, index+1); got != expected {
			t.Fatalf("failure %d delay = %v, want %v", index+1, got, expected)
		}
	}
}
