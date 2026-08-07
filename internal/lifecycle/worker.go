// Package lifecycle owns clock-driven order and address transitions (W-010).
package lifecycle

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"payd/internal/store"
	"payd/internal/wallet"
)

type Worker struct {
	store    *store.Store
	pool     *wallet.Pool
	cooldown time.Duration
	logger   *slog.Logger
	mu       sync.RWMutex
	events   store.EventConfig
	now      func() time.Time
}

func (w *Worker) UpdateEvents(events store.EventConfig) {
	w.mu.Lock()
	w.events = events
	w.mu.Unlock()
}

func New(database *store.Store, pool *wallet.Pool, cooldown time.Duration, logger *slog.Logger, eventConfigs ...store.EventConfig) (*Worker, error) {
	if database == nil || pool == nil || cooldown <= 0 {
		return nil, errors.New("lifecycle worker requires store, wallet pool, and positive cooldown")
	}
	if logger == nil {
		logger = slog.Default()
	}
	var events store.EventConfig
	if len(eventConfigs) > 0 {
		events = eventConfigs[0]
	}
	return &Worker{store: database, pool: pool, cooldown: cooldown, logger: logger, events: events, now: time.Now}, nil
}

// Run has no network client by construction; every operation is local derivation or store I/O (LIF-005).
func (w *Worker) Run(ctx context.Context) {
	w.tick10(ctx)
	w.tick60(ctx)
	short := time.NewTicker(10 * time.Second)
	long := time.NewTicker(time.Minute)
	defer short.Stop()
	defer long.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-short.C:
			w.tick10(ctx)
		case <-long.C:
			w.tick60(ctx)
		}
	}
}

func (w *Worker) Tick10(ctx context.Context) error {
	now := w.now().UTC()
	w.mu.RLock()
	events := w.events
	w.mu.RUnlock()
	if _, err := w.store.ExpireOrders(ctx, now, w.cooldown, events); err != nil {
		return err
	}
	_, err := w.store.ReleaseCooled(ctx, now)
	return err
}

func (w *Worker) Tick60(ctx context.Context) error {
	now := w.now().UTC()
	if _, err := w.pool.TopUp(ctx, now); err != nil {
		return err
	}
	_, err := w.store.PruneUsedTOTP(ctx, now)
	return err
}

func (w *Worker) tick10(ctx context.Context) {
	if err := w.Tick10(ctx); err != nil && !errors.Is(err, context.Canceled) {
		w.logger.Error("lifecycle 10s tick failed", "error", err)
	}
}

func (w *Worker) tick60(ctx context.Context) {
	if err := w.Tick60(ctx); err != nil && !errors.Is(err, context.Canceled) {
		w.logger.Error("lifecycle 60s tick failed", "error", err)
	}
}
