// Package follower owns ordered block ingest and reorg recovery (W-001).
package follower

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"payd/internal/store"
)

const catchUpInterval = time.Second / 8

var (
	ErrHalted             = errors.New("chain follower halted")
	ErrReorgDepthExceeded = errors.New("reorg exceeds configured depth")
)

type ChainReader interface {
	GetNowBlock(context.Context) (json.RawMessage, error)
	GetBlockByNum(context.Context, int64) (json.RawMessage, error)
}

type Block struct {
	Height       int64
	ID           string
	ParentID     string
	Timestamp    int64
	Transactions []json.RawMessage
	Raw          json.RawMessage
}

// PrepareBlock completes decoding and any receipt RPC before returning a DB-only closure (CHN-006/006a).
type PrepareBlock func(context.Context, Block) (store.BlockApply, error)

type Worker struct {
	reader       ChainReader
	store        *store.Store
	prepare      PrepareBlock
	logger       *slog.Logger
	pollInterval time.Duration
	reorgDepth   int
	now          func() time.Time
	sleep        func(context.Context, time.Duration) error
	suspicion    *reorgSuspicion
	staleReads   atomic.Uint64
	suspected    atomic.Uint64
	confirmed    atomic.Uint64
	halted       atomic.Bool
}

type reorgSuspicion struct {
	height        int64
	replacementID string
	firstSeen     time.Time
}

func New(reader ChainReader, database *store.Store, prepare PrepareBlock, pollInterval time.Duration, reorgDepth int, logger *slog.Logger) (*Worker, error) {
	if reader == nil || database == nil {
		return nil, errors.New("follower requires chain reader and store")
	}
	if pollInterval != 3*time.Second {
		return nil, errors.New("follower poll interval must be 3s (CHN-001)")
	}
	if reorgDepth <= 0 {
		return nil, errors.New("follower reorg depth must be positive")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if prepare == nil {
		prepare = func(context.Context, Block) (store.BlockApply, error) { return nil, nil }
	}
	return &Worker{
		reader: reader, store: database, prepare: prepare, logger: logger,
		pollInterval: pollInterval, reorgDepth: reorgDepth, now: time.Now, sleep: sleepContext,
	}, nil
}

func (w *Worker) Run(ctx context.Context) {
	w.tickAndReport(ctx)
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tickAndReport(ctx)
		}
		if w.halted.Load() {
			<-ctx.Done()
			return
		}
	}
}

func (w *Worker) tickAndReport(ctx context.Context) {
	if err := w.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		if errors.Is(err, ErrReorgDepthExceeded) {
			w.logger.Error("CRITICAL: reorg exceeds configured depth; ingest halted", "error", err, "reorg_depth", w.reorgDepth) // CHN-015
			return
		}
		w.logger.Error("chain follower tick failed", "error", err)
	}
}

// Tick performs one getnowblock poll and catches the cursor up to that returned tip (CHN-001..007a).
func (w *Worker) Tick(ctx context.Context) error {
	if w.halted.Load() {
		return ErrHalted
	}
	rawTip, err := w.reader.GetNowBlock(ctx)
	if err != nil {
		return fmt.Errorf("get current block: %w", err)
	}
	tip, err := parseBlock(rawTip)
	if err != nil {
		return fmt.Errorf("decode current block: %w", err)
	}
	cursor, exists, err := w.store.Cursor(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return w.commit(ctx, tip)
	}
	if tip.Height < cursor.LastHeight {
		w.staleReads.Add(1) // CHN-007a
		return nil
	}
	if tip.Height == cursor.LastHeight {
		return nil // CHN-007
	}
	catchingUp := tip.Height-cursor.LastHeight > 100
	for {
		cursor, _, err = w.store.Cursor(ctx)
		if err != nil {
			return err
		}
		if cursor.LastHeight >= tip.Height {
			return nil
		}
		nextHeight := cursor.LastHeight + 1
		candidate := tip
		if nextHeight != tip.Height {
			if catchingUp {
				if err := w.sleep(ctx, catchUpInterval); err != nil {
					return err
				}
			}
			raw, err := w.reader.GetBlockByNum(ctx, nextHeight)
			if err != nil {
				return fmt.Errorf("fetch missing block %d: %w", nextHeight, err)
			}
			candidate, err = parseBlock(raw)
			if err != nil {
				return fmt.Errorf("decode missing block %d: %w", nextHeight, err)
			}
			if candidate.Height != nextHeight {
				return fmt.Errorf("requested block %d, received %d", nextHeight, candidate.Height)
			}
		}
		processed, err := w.processCandidate(ctx, candidate)
		if err != nil {
			return err
		}
		if !processed {
			return nil
		}
	}
}

func (w *Worker) processCandidate(ctx context.Context, block Block) (bool, error) {
	cursor, exists, err := w.store.Cursor(ctx)
	if err != nil {
		return false, err
	}
	if !exists {
		return true, w.commit(ctx, block)
	}
	if block.Height != cursor.LastHeight+1 {
		return false, fmt.Errorf("candidate block %d does not follow cursor %d", block.Height, cursor.LastHeight)
	}
	previous, found, err := w.store.Block(ctx, cursor.LastHeight)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("stored cursor block %d is missing", cursor.LastHeight)
	}
	if block.ParentID != previous.ID {
		replacement, confirmed, err := w.confirmMismatch(ctx, previous)
		if err != nil || !confirmed {
			return false, err
		}
		if err := w.recoverReorg(ctx, previous.Height, replacement); err != nil {
			return false, err
		}
		return true, nil
	}
	w.suspicion = nil
	return true, w.commit(ctx, block)
}

func (w *Worker) confirmMismatch(ctx context.Context, storedPrevious store.BlockRecord) (Block, bool, error) {
	raw, err := w.reader.GetBlockByNum(ctx, storedPrevious.Height)
	if err != nil {
		return Block{}, false, fmt.Errorf("confirm reorg at block %d: %w", storedPrevious.Height, err)
	}
	replacement, err := parseBlock(raw)
	if err != nil {
		return Block{}, false, fmt.Errorf("decode reorg check at block %d: %w", storedPrevious.Height, err)
	}
	if replacement.Height != storedPrevious.Height {
		return Block{}, false, fmt.Errorf("reorg check requested block %d, received %d", storedPrevious.Height, replacement.Height)
	}
	w.suspected.Add(1)
	if replacement.ID == storedPrevious.ID {
		w.suspicion = nil
		return Block{}, false, nil
	}
	now := w.now()
	if w.suspicion == nil || w.suspicion.height != replacement.Height || w.suspicion.replacementID != replacement.ID {
		w.suspicion = &reorgSuspicion{height: replacement.Height, replacementID: replacement.ID, firstSeen: now}
		return Block{}, false, nil
	}
	if now.Sub(w.suspicion.firstSeen) < w.pollInterval {
		return Block{}, false, nil
	}
	w.confirmed.Add(1)
	w.suspicion = nil
	return replacement, true, nil // CHN-011a: same replacement observed twice at least one poll apart.
}

func (w *Worker) recoverReorg(ctx context.Context, lastHeight int64, replacement Block) error {
	for height := lastHeight; ; height-- {
		storedBlock, found, err := w.store.Block(ctx, height)
		if err != nil {
			return err
		}
		if !found {
			return w.halt(fmt.Errorf("%w: retained block %d is unavailable", ErrReorgDepthExceeded, height))
		}
		if replacement.ID == storedBlock.ID {
			_, err := w.store.RewindChain(ctx, height)
			return err // CHN-012/013: forward replay resumes in Tick from this ancestor.
		}
		depth := lastHeight - height + 1
		if depth > int64(w.reorgDepth) || height == 0 {
			return w.halt(fmt.Errorf("%w: observed depth %d", ErrReorgDepthExceeded, depth))
		}
		raw, err := w.reader.GetBlockByNum(ctx, height-1)
		if err != nil {
			return fmt.Errorf("walk reorg at block %d: %w", height-1, err)
		}
		replacement, err = parseBlock(raw)
		if err != nil {
			return fmt.Errorf("decode reorg block %d: %w", height-1, err)
		}
		if replacement.Height != height-1 {
			return fmt.Errorf("reorg walk requested block %d, received %d", height-1, replacement.Height)
		}
	}
}

func (w *Worker) halt(err error) error {
	w.halted.Store(true)
	return err
}

func (w *Worker) commit(ctx context.Context, block Block) error {
	apply, err := w.prepare(ctx, block) // All matcher/receipt RPC completes before Store opens the write transaction (CHN-006a/ARC-007).
	if err != nil {
		return fmt.Errorf("prepare block %d: %w", block.Height, err)
	}
	record := store.BlockRecord{
		Height: block.Height, ID: block.ID, ParentID: block.ParentID, Timestamp: block.Timestamp,
		TxCount: len(block.Transactions), ProcessedAt: w.now().UTC().Unix(),
	}
	return w.store.CommitBlock(ctx, record, w.reorgDepth, apply)
}

func (w *Worker) StaleReads() uint64      { return w.staleReads.Load() }
func (w *Worker) ReorgSuspicions() uint64 { return w.suspected.Load() }
func (w *Worker) ReorgsConfirmed() uint64 { return w.confirmed.Load() }
func (w *Worker) Halted() bool            { return w.halted.Load() }

func parseBlock(raw json.RawMessage) (Block, error) {
	var response struct {
		ID     string `json:"blockID"`
		Header struct {
			Raw struct {
				Number     int64  `json:"number"`
				Timestamp  int64  `json:"timestamp"`
				ParentHash string `json:"parentHash"`
			} `json:"raw_data"`
		} `json:"block_header"`
		Transactions []json.RawMessage `json:"transactions"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return Block{}, err
	}
	if response.ID == "" || response.Header.Raw.Number < 0 || response.Header.Raw.Timestamp <= 0 {
		return Block{}, errors.New("block response is missing id, height, or timestamp")
	}
	return Block{
		Height: response.Header.Raw.Number, ID: response.ID, ParentID: response.Header.Raw.ParentHash,
		Timestamp: response.Header.Raw.Timestamp / 1000, Transactions: response.Transactions,
		Raw: append(json.RawMessage(nil), raw...),
	}, nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
