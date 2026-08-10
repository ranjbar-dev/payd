// Package price polls USD quotes and rejects unavailable or stale values.
package price

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"payd/internal/config"
	"payd/internal/store"
)

const (
	binanceTimeout = 10 * time.Second
	maxBackoff     = 5 * time.Minute
)

var ErrUnavailable = errors.New("asset price is unavailable or stale")

type Quote struct {
	USD       string
	FetchedAt int64
}

// Provider isolates callers from the external price source (PRC-003).
type Provider interface {
	Fetch(context.Context, []string) (map[string]string, error)
}

// Binance is the default Provider implementation (PRC-003).
type Binance struct {
	url    string
	client *http.Client
}

func NewBinance(endpoint string, client *http.Client) *Binance {
	if client == nil {
		client = &http.Client{Timeout: binanceTimeout}
	}
	return &Binance{url: endpoint, client: client}
}

// Fetch calls Binance's multi-symbol ticker endpoint (PRC-001).
func (b *Binance) Fetch(ctx context.Context, pairs []string) (map[string]string, error) {
	u, err := url.Parse(b.url)
	if err != nil {
		return nil, fmt.Errorf("parse Binance URL: %w", err)
	}
	symbols, err := json.Marshal(pairs)
	if err != nil {
		return nil, fmt.Errorf("encode Binance symbols: %w", err)
	}
	query := u.Query()
	query.Set("symbols", string(symbols))
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Binance request: %w", err)
	}
	response, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Binance prices: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch Binance prices: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read Binance prices: %w", err)
	}
	var payload []struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Binance prices: %w", err)
	}
	expected := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		expected[pair] = struct{}{}
	}
	prices := make(map[string]string, len(payload))
	for _, item := range payload {
		if _, ok := expected[item.Symbol]; !ok {
			return nil, fmt.Errorf("Binance returned unrequested pair %q", item.Symbol)
		}
		value, ok := new(big.Rat).SetString(item.Price)
		if !ok || value.Sign() <= 0 {
			return nil, fmt.Errorf("Binance returned invalid price for %s", item.Symbol)
		}
		prices[item.Symbol] = item.Price
	}
	for pair := range expected {
		if _, ok := prices[pair]; !ok {
			return nil, fmt.Errorf("Binance omitted pair %s", pair)
		}
	}
	return prices, nil
}

type Poller struct {
	provider   Provider
	store      *store.Store
	logger     *slog.Logger
	providerID string
	pairs      []string
	interval   time.Duration
	errorCount atomic.Uint64
}

// New constructs the W-005 poller with Binance as its default provider.
func New(cfg config.Price, database *store.Store, logger *slog.Logger) (*Poller, error) {
	if cfg.Provider != "binance" {
		return nil, fmt.Errorf("unsupported price provider %q", cfg.Provider)
	}
	return NewWithProvider(cfg, database, NewBinance(cfg.URL, nil), logger)
}

// NewWithProvider allows another source to be installed without changing price callers (PRC-003).
func NewWithProvider(cfg config.Price, database *store.Store, provider Provider, logger *slog.Logger) (*Poller, error) {
	if database == nil || provider == nil {
		return nil, errors.New("price poller requires store and provider")
	}
	if cfg.Provider == "" || cfg.Interval != time.Minute || cfg.StaleAfter <= 0 {
		return nil, errors.New("price provider, 60s interval, and positive stale_after are required")
	}
	pairs, err := validatePairs(cfg.Pairs)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{provider: provider, store: database, logger: logger, providerID: cfg.Provider, pairs: pairs, interval: cfg.Interval}, nil
}

// Run fetches immediately, every configured interval after success, and backs off after failures (W-005, PRC-001/007).
func (p *Poller) Run(ctx context.Context) {
	failures := 0
	for {
		err := p.Refresh(ctx)
		_ = p.store.RecordWorkerTick(ctx, "price", err, time.Now()) // OPS-008
		if err != nil {
			failures++
			p.logger.Error("refresh prices", "error", err, "retry_in", retryDelay(p.interval, failures))
		} else {
			failures = 0
		}
		delay := p.interval
		if failures > 0 {
			delay = retryDelay(p.interval, failures)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Refresh performs all network I/O before opening the store transaction (ARC-007, PRC-001/004).
func (p *Poller) Refresh(ctx context.Context) error {
	prices, err := p.provider.Fetch(ctx, p.pairs)
	if err != nil {
		p.errorCount.Add(1)
		return err
	}
	now := time.Now().UTC().Unix()
	records := make([]store.Price, 0, len(p.pairs))
	for _, pair := range p.pairs {
		value, ok := prices[pair]
		if !ok {
			p.errorCount.Add(1)
			return fmt.Errorf("price provider omitted pair %s", pair)
		}
		decimal, ok := new(big.Rat).SetString(value)
		if !ok || decimal.Sign() <= 0 {
			p.errorCount.Add(1)
			return fmt.Errorf("price provider returned invalid price for %s", pair)
		}
		records = append(records, store.Price{Symbol: strings.TrimSuffix(pair, "USDT"), PriceUSD: value, Source: p.providerID, FetchedAt: now})
	}
	if err := p.store.UpsertPrices(ctx, records); err != nil {
		return err
	}
	return nil
}

func (p *Poller) ErrorCount() uint64 { return p.errorCount.Load() }

// Current returns a usable quote or ErrUnavailable. Order and withdrawal creation call this gate (PRC-002/005).
func Current(ctx context.Context, database *store.Store, cfg config.Price, symbol string, now time.Time) (Quote, error) {
	if (symbol == "USDT" || symbol == "USDC") && !pairConfigured(cfg.Pairs, symbol+"USDT") {
		return Quote{USD: "1.00", FetchedAt: now.UTC().Unix()}, nil
	}
	value, fetchedAt, err := database.AssetPrice(ctx, symbol)
	if errors.Is(err, sql.ErrNoRows) {
		return Quote{}, fmt.Errorf("%w: %s", ErrUnavailable, symbol)
	}
	if err != nil {
		return Quote{}, err
	}
	return currentQuote(cfg, symbol, value, fetchedAt, now)
}

// CurrentFromPrices applies the PRC-002/005 gates to one request-scoped price snapshot.
func CurrentFromPrices(prices []store.Price, cfg config.Price, symbol string, now time.Time) (Quote, error) {
	if (symbol == "USDT" || symbol == "USDC") && !pairConfigured(cfg.Pairs, symbol+"USDT") {
		return Quote{USD: "1.00", FetchedAt: now.UTC().Unix()}, nil
	}
	for _, item := range prices {
		if item.Symbol == symbol {
			return currentQuote(cfg, symbol, item.PriceUSD, item.FetchedAt, now)
		}
	}
	return Quote{}, fmt.Errorf("%w: %s", ErrUnavailable, symbol)
}

func currentQuote(cfg config.Price, symbol, value string, fetchedAt int64, now time.Time) (Quote, error) {
	if IsStale(fetchedAt, now, cfg.StaleAfter) {
		return Quote{}, fmt.Errorf("%w: %s", ErrUnavailable, symbol)
	}
	return Quote{USD: value, FetchedAt: fetchedAt}, nil
}

// IsStale is the shared PRC-005 boundary check.
func IsStale(fetchedAt int64, now time.Time, staleAfter time.Duration) bool {
	return staleAfter <= 0 || now.UTC().Sub(time.Unix(fetchedAt, 0)) > staleAfter
}

func validatePairs(pairs []string) ([]string, error) {
	if len(pairs) == 0 {
		return nil, errors.New("price pairs must not be empty")
	}
	seen := make(map[string]struct{}, len(pairs))
	validated := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		if !strings.HasSuffix(pair, "USDT") || len(pair) == len("USDT") {
			return nil, fmt.Errorf("price pair %q must be a *USDT pair", pair)
		}
		if _, exists := seen[pair]; exists {
			return nil, fmt.Errorf("duplicate price pair %q", pair)
		}
		seen[pair] = struct{}{}
		validated = append(validated, pair)
	}
	return validated, nil
}

func pairConfigured(pairs []string, wanted string) bool {
	for _, pair := range pairs {
		if pair == wanted {
			return true
		}
	}
	return false
}

func retryDelay(interval time.Duration, failures int) time.Duration {
	if failures >= 4 {
		return maxBackoff
	}
	delay := interval * time.Duration(1<<max(failures-1, 0))
	return min(delay, maxBackoff)
}
