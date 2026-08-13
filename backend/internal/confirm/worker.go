// Package confirm tracks the solidity head and promotes irreversible payments.
package confirm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"payd/internal/store"
)

const PollInterval = 20 * time.Second

type SolidityReader interface {
	GetNowBlock(context.Context) (json.RawMessage, error)
}

type Worker struct {
	reader                SolidityReader
	store                 *store.Store
	confirmationsRequired int
	cooldown              time.Duration
	logger                *slog.Logger
	now                   func() time.Time
	eventsMu              sync.RWMutex
	events                store.EventConfig
}

func New(reader SolidityReader, database *store.Store, confirmationsRequired int, cooldown time.Duration, logger *slog.Logger, events store.EventConfig) (*Worker, error) {
	if reader == nil || database == nil {
		return nil, errors.New("confirmation tracker requires solidity reader and store")
	}
	if confirmationsRequired <= 0 || cooldown <= 0 {
		return nil, errors.New("confirmation tracker requires positive confirmations and cooldown")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		reader: reader, store: database, confirmationsRequired: confirmationsRequired,
		cooldown: cooldown, logger: logger, now: time.Now, events: events,
	}, nil
}

func (w *Worker) Run(ctx context.Context) {
	w.tickAndReport(ctx)
	ticker := time.NewTicker(PollInterval) // CNF-001 / W-003
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tickAndReport(ctx)
		}
	}
}

func (w *Worker) tickAndReport(ctx context.Context) {
	_, err := w.Tick(ctx)
	_ = w.store.RecordWorkerTick(ctx, "confirm", err, time.Now()) // OPS-008
	if err != nil && !errors.Is(err, context.Canceled) {
		w.logger.Error("confirmation tracker tick failed", "error", err)
	}
}

// Tick performs the one solidity-head RPC before opening the store transaction (CNF-001/004, ARC-007).
func (w *Worker) Tick(ctx context.Context) (store.ConfirmationResult, error) {
	raw, err := w.reader.GetNowBlock(ctx)
	if err != nil {
		return store.ConfirmationResult{}, fmt.Errorf("get solidity head: %w", err)
	}
	height, err := solidifiedHeight(raw)
	if err != nil {
		return store.ConfirmationResult{}, err
	}
	w.eventsMu.RLock()
	events := w.events
	w.eventsMu.RUnlock()
	return w.store.ApplySolidifiedHeight(ctx, height, w.confirmationsRequired, w.cooldown, events, w.now())
}

func (w *Worker) UpdateEvents(events store.EventConfig) {
	w.eventsMu.Lock()
	w.events = events
	w.eventsMu.Unlock()
}

// Confirmations computes the CNF-007 payload field and clamps pre-tip blocks to zero.
func Confirmations(lastHeight, blockHeight int64) int64 {
	return store.ConfirmationDepth(lastHeight, blockHeight)
}

func solidifiedHeight(raw json.RawMessage) (int64, error) {
	var response struct {
		Header struct {
			Raw struct {
				Number *int64 `json:"number"`
			} `json:"raw_data"`
		} `json:"block_header"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return 0, fmt.Errorf("decode solidity head: %w", err)
	}
	if response.Header.Raw.Number == nil || *response.Header.Raw.Number < 0 {
		return 0, errors.New("solidity head is missing a valid height")
	}
	return *response.Header.Raw.Number, nil
}
