package wallet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"sync"
	"time"

	"payd/internal/config"
	"payd/internal/price"
	"payd/internal/store"
)

type ResourceReader interface {
	GetAccountResource(context.Context, string) (json.RawMessage, error)
}

type Monitor struct {
	reader ResourceReader
	store  *store.Store
	logger *slog.Logger

	mu     sync.RWMutex
	config config.Config
}

func NewMonitor(reader ResourceReader, database *store.Store, cfg config.Config, logger *slog.Logger) (*Monitor, error) {
	if reader == nil || database == nil {
		return nil, errors.New("wallet monitor requires reader and store")
	}
	if cfg.Resources.CheckInterval <= 0 || cfg.Resources.SlowCheckInterval <= 0 || cfg.Resources.MaxPolledAddresses <= 0 || cfg.Resources.MaxPolledAddresses > 50 {
		return nil, errors.New("wallet monitor requires positive polling intervals and cap")
	}
	threshold, ok := new(big.Rat).SetString(cfg.Resources.PollThresholdUSD)
	if !ok || threshold.Sign() < 0 {
		return nil, errors.New("wallet monitor requires a non-negative USD threshold")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Monitor{reader: reader, store: database, config: cfg, logger: logger}, nil
}

func (m *Monitor) UpdateConfig(cfg config.Config) {
	m.mu.Lock()
	m.config.Assets, m.config.Price, m.config.Resources = cfg.Assets, cfg.Price, cfg.Resources
	m.mu.Unlock()
}

// Run polls both tiers independently so SIGHUP cadence changes take effect on the next cycle.
func (m *Monitor) Run(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); m.runTier(ctx, true) }()
	go func() { defer workers.Done(); m.runTier(ctx, false) }()
	workers.Wait()
}

func (m *Monitor) runTier(ctx context.Context, fast bool) {
	for {
		if err := m.Poll(ctx, fast); err != nil && ctx.Err() == nil {
			m.logger.Error("poll wallet resources", "tier", map[bool]string{true: "fast", false: "slow"}[fast], "error", err)
		}
		m.mu.RLock()
		delay := m.config.Resources.SlowCheckInterval
		if fast {
			delay = m.config.Resources.CheckInterval
		}
		m.mu.RUnlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Poll selects one tier, performs each RPC, then persists its result (ARC-007, RES-001..004).
func (m *Monitor) Poll(ctx context.Context, fast bool) error {
	addresses, err := m.tier(ctx, fast)
	if err != nil {
		return err
	}
	m.mu.RLock()
	resources := m.config.Resources
	m.mu.RUnlock()
	for _, address := range addresses {
		body, err := m.reader.GetAccountResource(ctx, address.Address)
		if err != nil {
			return fmt.Errorf("get resources for %s: %w", address.Address, err)
		}
		reading, err := parseResources(body)
		if err != nil {
			return fmt.Errorf("decode resources for %s: %w", address.Address, err)
		}
		reading.AddressID = address.ID
		reading.NeedsResources = reading.EnergyLimit-reading.EnergyUsed < resources.MinEnergy ||
			reading.BandwidthLimit-reading.BandwidthUsed < resources.MinBandwidth
		if err := m.store.UpdateAddressResources(ctx, reading, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) tier(ctx context.Context, fast bool) ([]store.WalletAddress, error) {
	addresses, err := m.store.WalletAddresses(ctx, false)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()
	threshold, _ := new(big.Rat).SetString(cfg.Resources.PollThresholdUSD)
	type valued struct {
		address store.WalletAddress
		usd     *big.Rat
	}
	values := make([]valued, 0, len(addresses))
	for _, address := range addresses {
		total := new(big.Rat)
		funded := false
		for _, balance := range address.Balances {
			raw, ok := new(big.Int).SetString(balance.ConfirmedRaw, 10)
			if !ok {
				return nil, fmt.Errorf("invalid confirmed balance %q", balance.ConfirmedRaw)
			}
			if raw.Sign() <= 0 {
				continue
			}
			asset, ok := configuredAsset(cfg.Assets, balance.Asset)
			if !ok {
				return nil, fmt.Errorf("balance uses unconfigured asset %s", balance.Asset)
			}
			quote, err := price.Current(ctx, m.store, cfg.Price, balance.Asset, time.Now())
			if err != nil {
				return nil, err
			}
			usd, _ := new(big.Rat).SetString(quote.USD)
			scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(asset.Decimals)), nil)
			total.Add(total, new(big.Rat).Mul(new(big.Rat).SetFrac(raw, scale), usd))
			funded = true
		}
		if funded { // RES-002: zero-balance addresses never enter either tier.
			values = append(values, valued{address: address, usd: total})
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if compared := values[i].usd.Cmp(values[j].usd); compared != 0 {
			return compared > 0
		}
		return values[i].address.ID < values[j].address.ID
	})
	fastIDs := make(map[int64]bool, cfg.Resources.MaxPolledAddresses)
	for _, value := range values {
		if len(fastIDs) == cfg.Resources.MaxPolledAddresses || value.usd.Cmp(threshold) <= 0 {
			break
		}
		fastIDs[value.address.ID] = true
	}
	selected := make([]store.WalletAddress, 0, len(values))
	for _, value := range values {
		if fastIDs[value.address.ID] == fast {
			selected = append(selected, value.address)
		}
	}
	return selected, nil
}

func configuredAsset(assets []config.Asset, symbol string) (config.Asset, bool) {
	for _, asset := range assets {
		if asset.Symbol == symbol {
			return asset, true
		}
	}
	return config.Asset{}, false
}

func parseResources(body []byte) (store.ResourceReading, error) {
	var response struct {
		EnergyLimit  int64 `json:"EnergyLimit"`
		EnergyUsed   int64 `json:"EnergyUsed"`
		NetLimit     int64 `json:"NetLimit"`
		NetUsed      int64 `json:"NetUsed"`
		FreeNetLimit int64 `json:"freeNetLimit"`
		FreeNetUsed  int64 `json:"freeNetUsed"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return store.ResourceReading{}, err
	}
	values := []int64{response.EnergyLimit, response.EnergyUsed, response.NetLimit, response.NetUsed, response.FreeNetLimit, response.FreeNetUsed}
	for _, value := range values {
		if value < 0 {
			return store.ResourceReading{}, errors.New("resource response contains a negative value")
		}
	}
	return store.ResourceReading{
		EnergyLimit: response.EnergyLimit, EnergyUsed: response.EnergyUsed,
		BandwidthLimit: response.NetLimit + response.FreeNetLimit,
		BandwidthUsed:  response.NetUsed + response.FreeNetUsed,
	}, nil
}
