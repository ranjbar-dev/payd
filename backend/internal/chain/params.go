package chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"payd/internal/energy"
	"payd/internal/store"
)

const ParameterInterval = 6 * time.Hour

type ParameterWorker struct {
	read           *ReadClient
	store          *store.Store
	logger         *slog.Logger
	maxBurnTRX     string
	maxBurn        *big.Rat
	minEnergy      int64
	maxBurnMu      sync.RWMutex
	interval       time.Duration
	errorCount     atomic.Uint64
	ceilingHealthy atomic.Bool
}

func NewParameterWorker(read *ReadClient, database *store.Store, logger *slog.Logger, maxBurnTRX string, minEnergy int64) (*ParameterWorker, error) {
	if read == nil || database == nil {
		return nil, errors.New("chain parameter worker requires client and store")
	}
	if minEnergy < 0 {
		return nil, errors.New("resources.min_energy must be non-negative")
	}
	if logger == nil {
		logger = slog.Default()
	}
	var maxBurn *big.Rat
	if maxBurnTRX != "" {
		var ok bool
		maxBurn, ok = new(big.Rat).SetString(maxBurnTRX)
		if !ok || maxBurn.Sign() < 0 {
			return nil, errors.New("energy.max_burn_trx must be a non-negative decimal")
		}
	}
	worker := &ParameterWorker{read: read, store: database, logger: logger, maxBurnTRX: maxBurnTRX, maxBurn: maxBurn, minEnergy: minEnergy, interval: ParameterInterval}
	worker.ceilingHealthy.Store(maxBurn == nil)
	return worker, nil
}

// Run fetches immediately at startup and then every six hours (W-011, RES-020).
func (w *ParameterWorker) Run(ctx context.Context) {
	w.refreshAndReport(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.refreshAndReport(ctx)
		}
	}
}

func (w *ParameterWorker) refreshAndReport(ctx context.Context) {
	err := w.Refresh(ctx)
	_ = w.store.RecordWorkerTick(ctx, "chain_params", err, time.Now()) // OPS-008
	if err != nil {
		w.errorCount.Add(1) // RES-021
		w.logger.Error("refresh chain parameters", "error", err)
		if cached, cacheErr := w.store.LoadChainParameters(ctx); cacheErr == nil {
			w.validateBurnCeiling(cached.EnergyFee)
		}
	}
}

// Refresh completes network I/O before atomically updating the store (ARC-007, RES-020/021).
func (w *ParameterWorker) Refresh(ctx context.Context) error {
	body, err := w.read.GetChainParameters(ctx)
	if err != nil {
		return err
	}
	energyFee, transactionFee, err := parseChainParameters(body)
	if err != nil {
		return err
	}
	previous, err := w.store.UpsertChainParameters(ctx, energyFee, transactionFee, time.Now())
	if err != nil {
		return err
	}
	if previous != nil && *previous != energyFee {
		w.logger.Warn("getEnergyFee changed", "previous", *previous, "current", energyFee) // RES-023
	}
	w.validateBurnCeiling(energyFee) // ENR-017 at startup and after every fee change.
	return nil
}

func (w *ParameterWorker) ErrorCount() uint64 { return w.errorCount.Load() }

// BurnCeilingHealthy is consumed by the readiness endpoint added in P14 (ENR-017).
func (w *ParameterWorker) BurnCeilingHealthy() bool { return w.ceilingHealthy.Load() }

func (w *ParameterWorker) UpdateBurnCeiling(ctx context.Context, value string, minEnergy int64) error {
	if minEnergy < 0 {
		return errors.New("resources.min_energy must be non-negative")
	}
	var maximum *big.Rat
	if value != "" {
		var ok bool
		maximum, ok = new(big.Rat).SetString(value)
		if !ok || maximum.Sign() < 0 {
			return errors.New("energy.max_burn_trx must be a non-negative decimal")
		}
	}
	w.maxBurnMu.Lock()
	w.maxBurnTRX, w.maxBurn, w.minEnergy = value, maximum, minEnergy
	w.maxBurnMu.Unlock()
	params, err := w.store.LoadChainParameters(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		w.ceilingHealthy.Store(maximum == nil)
		return nil
	}
	if err != nil {
		return err
	}
	w.validateBurnCeiling(params.EnergyFee)
	return nil
}

func (w *ParameterWorker) validateBurnCeiling(energyFee int64) {
	w.maxBurnMu.RLock()
	maxBurn, maxBurnTRX, minEnergy := w.maxBurn, w.maxBurnTRX, w.minEnergy
	w.maxBurnMu.RUnlock()
	if maxBurn == nil {
		w.ceilingHealthy.Store(true)
		return
	}
	requiredSun := energy.BurnCostSun(minEnergy, energyFee) // ENR-017: validate the configured worst-case transfer.
	maxSun := new(big.Rat).Mul(maxBurn, big.NewRat(1_000_000, 1))
	healthy := maxSun.Cmp(new(big.Rat).SetInt(requiredSun)) >= 0
	w.ceilingHealthy.Store(healthy)
	if !healthy {
		w.logger.Warn("energy burn ceiling refuses a worst-case transfer", "max_burn_trx", maxBurnTRX, "min_energy", minEnergy, "required_burn_sun", requiredSun.String()) // ENR-017
	}
}

func parseChainParameters(body []byte) (int64, int64, error) {
	var response struct {
		Parameters []struct {
			Key   string `json:"key"`
			Value *int64 `json:"value"`
		} `json:"chainParameter"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, 0, fmt.Errorf("decode chain parameters: %w", err)
	}
	var energyFee, transactionFee *int64
	for _, parameter := range response.Parameters {
		switch parameter.Key {
		case "getEnergyFee":
			energyFee = parameter.Value
		case "getTransactionFee":
			transactionFee = parameter.Value
		}
	}
	if energyFee == nil || transactionFee == nil {
		return 0, 0, errors.New("chain parameters response omitted getEnergyFee or getTransactionFee")
	}
	return *energyFee, *transactionFee, nil
}
