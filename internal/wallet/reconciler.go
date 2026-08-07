package wallet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"payd/internal/config"
	"payd/internal/store"
)

const balanceReconcileInterval = 6 * time.Hour

type BalanceReader interface {
	GetAccount(context.Context, string) (json.RawMessage, error)
	GetTRC20Balance(context.Context, string, string) (json.RawMessage, error)
}

type Reconciler struct {
	reader BalanceReader
	store  *store.Store
	logger *slog.Logger

	mu     sync.RWMutex
	assets map[string]config.Asset
	events store.EventConfig
}

func NewReconciler(reader BalanceReader, database *store.Store, cfg config.Config, logger *slog.Logger) (*Reconciler, error) {
	if reader == nil || database == nil {
		return nil, errors.New("balance reconciler requires reader and store")
	}
	if logger == nil {
		logger = slog.Default()
	}
	r := &Reconciler{reader: reader, store: database, logger: logger}
	r.UpdateConfig(cfg)
	return r, nil
}

func (r *Reconciler) UpdateConfig(cfg config.Config) {
	assets := make(map[string]config.Asset, len(cfg.Assets))
	for _, asset := range cfg.Assets {
		assets[asset.Symbol] = asset
	}
	r.mu.Lock()
	r.assets, r.events = assets, store.NewEventConfig(cfg.IPN, cfg.Assets)
	r.mu.Unlock()
}

func (r *Reconciler) Run(ctx context.Context) {
	r.reconcileAndReport(ctx)
	ticker := time.NewTicker(balanceReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcileAndReport(ctx)
		}
	}
}

func (r *Reconciler) reconcileAndReport(ctx context.Context) {
	if err := r.Reconcile(ctx); err != nil && ctx.Err() == nil {
		r.logger.Error("reconcile chain balances", "error", err)
	}
}

// Reconcile completes every chain read before opening the write transaction (ARC-007, RL-003).
func (r *Reconciler) Reconcile(ctx context.Context) error {
	now := time.Now()
	targets, err := r.store.BalanceTargets(ctx, now.Add(-balanceReconcileInterval))
	if err != nil {
		return err
	}
	r.mu.RLock()
	assets := r.assets
	events := r.events
	r.mu.RUnlock()
	readings := make([]store.ChainBalance, 0, len(targets))
	for _, target := range targets {
		asset, ok := assets[target.Asset]
		if !ok {
			return fmt.Errorf("balance uses unconfigured asset %s", target.Asset)
		}
		var body json.RawMessage
		if asset.Kind == "native" {
			body, err = r.reader.GetAccount(ctx, target.Address)
		} else {
			body, err = r.reader.GetTRC20Balance(ctx, target.Address, asset.Contract)
		}
		if err != nil {
			return fmt.Errorf("read %s balance for %s: %w", target.Asset, target.Address, err)
		}
		raw, err := parseChainBalance(body, asset.Kind)
		if err != nil {
			return fmt.Errorf("decode %s balance for %s: %w", target.Asset, target.Address, err)
		}
		readings = append(readings, store.ChainBalance{AddressID: target.AddressID, Address: target.Address, Asset: target.Asset, Raw: raw})
	}
	return r.store.ReconcileChainBalances(ctx, readings, events, now)
}

func parseChainBalance(body []byte, kind string) (string, error) {
	amount := new(big.Int)
	if kind == "native" {
		var response struct {
			Balance json.Number `json:"balance"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return "", err
		}
		if response.Balance == "" {
			return "0", nil
		}
		if _, ok := amount.SetString(string(response.Balance), 10); !ok {
			return "", errors.New("native balance is not an integer")
		}
	} else {
		var response struct {
			ConstantResult []string `json:"constant_result"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return "", err
		}
		if len(response.ConstantResult) != 1 {
			return "", errors.New("balanceOf response omitted constant_result")
		}
		if _, ok := amount.SetString(response.ConstantResult[0], 16); !ok {
			return "", errors.New("balanceOf result is not hexadecimal")
		}
	}
	if amount.Sign() < 0 {
		return "", errors.New("chain balance is negative")
	}
	return amount.String(), nil
}
