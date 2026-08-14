package wallet

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcutil/base58"

	"payd/internal/config"
	"payd/internal/follower"
	"payd/internal/store"
)

const (
	BalanceReconcileInterval = 6 * time.Hour
	SafetyNetInterval        = 5 * time.Minute
)

type BalanceReader interface {
	GetAccount(context.Context, string) (json.RawMessage, error)
	GetTRC20Balance(context.Context, string, string) (json.RawMessage, error)
}

type SafetyReader interface {
	GetAccountHistory(context.Context, string, bool, url.Values) (json.RawMessage, error)
	GetTransactionInfoByID(context.Context, string) (json.RawMessage, error)
	GetBlockByNum(context.Context, int64) (json.RawMessage, error)
}

type SafetyDecoder interface {
	DecodeReconciled(context.Context, string, json.RawMessage, json.RawMessage, follower.Block) ([]store.PaymentRecord, error)
}

type Reconciler struct {
	reader BalanceReader
	store  *store.Store
	logger *slog.Logger

	mu      sync.RWMutex
	assets  map[string]config.Asset
	events  store.EventConfig
	history SafetyReader
	decoder SafetyDecoder
	match   func(*store.BlockWrite, store.PaymentRecord) error
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

func (r *Reconciler) EnableSafetyNet(reader SafetyReader, decoder SafetyDecoder, match func(*store.BlockWrite, store.PaymentRecord) error) error {
	if reader == nil || decoder == nil || match == nil {
		return errors.New("payment safety net requires history reader, decoder, and matcher")
	}
	r.mu.Lock()
	r.history, r.decoder, r.match = reader, decoder, match
	r.mu.Unlock()
	return nil
}

func (r *Reconciler) Run(ctx context.Context) {
	r.safetyAndReport(ctx, false)
	r.reconcileAndReport(ctx)
	active := time.NewTicker(SafetyNetInterval)
	full := time.NewTicker(BalanceReconcileInterval)
	defer active.Stop()
	defer full.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-active.C:
			r.safetyAndReport(ctx, false)
		case <-full.C:
			r.safetyAndReport(ctx, true)
			r.reconcileAndReport(ctx)
		}
	}
}

func (r *Reconciler) safetyAndReport(ctx context.Context, includeCooling bool) {
	err := r.SafetyNet(ctx, includeCooling)
	_ = r.store.RecordWorkerTick(ctx, "reconciler_safety_net", err, time.Now()) // OPS-008
	if err != nil && ctx.Err() == nil {
		r.logger.Error("reconcile payment history", "full", includeCooling, "error", err)
	}
}

func (r *Reconciler) reconcileAndReport(ctx context.Context) {
	err := r.Reconcile(ctx)
	_ = r.store.RecordWorkerTick(ctx, "reconciler_balances", err, time.Now()) // OPS-008
	if err != nil && ctx.Err() == nil {
		r.logger.Error("reconcile chain balances", "error", err)
	}
}

// Reconcile completes every chain read before opening the write transaction (ARC-007, RL-003).
func (r *Reconciler) Reconcile(ctx context.Context) error {
	now := time.Now()
	targets, err := r.store.BalanceTargets(ctx, now.Add(-BalanceReconcileInterval))
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

// SafetyNet covers TRX and TRC-20, inbound and outbound, and consumes every
// fingerprint page for assigned addresses (plus cooling on the 6-hour pass).
func (r *Reconciler) SafetyNet(ctx context.Context, includeCooling bool) error {
	r.mu.RLock()
	history, decoder, match := r.history, r.decoder, r.match
	r.mu.RUnlock()
	if history == nil {
		return nil
	}
	targets, err := r.store.ReconcileTargets(ctx, includeCooling)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{})
	for _, target := range targets {
		minimum := max(target.NewestPaymentAt-600, 0) * 1000 // DET-010b: chain timestamp minus ten minutes.
		for _, trc20 := range []bool{true, false} {
			for _, inbound := range []bool{true, false} {
				if err := r.historyPages(ctx, history, decoder, match, target, trc20, inbound, minimum, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *Reconciler) historyPages(ctx context.Context, history SafetyReader, decoder SafetyDecoder,
	match func(*store.BlockWrite, store.PaymentRecord) error, target store.ReconcileTarget, trc20, inbound bool,
	minimum int64, seen map[string]struct{}) error {
	query := url.Values{"limit": {"200"}, "min_timestamp": {fmt.Sprint(minimum)}}
	if inbound {
		query.Set("only_to", "true")
	}
	lastFingerprint := ""
	for {
		body, err := history.GetAccountHistory(ctx, target.Address, trc20, query)
		if err != nil {
			return err
		}
		var page struct {
			Data []json.RawMessage `json:"data"`
			Meta struct {
				Fingerprint string `json:"fingerprint"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("decode account history for %s: %w", target.Address, err)
		}
		for _, raw := range page.Data {
			txID, outgoing, err := historyItem(raw, trc20, target.Address)
			if err != nil {
				return err
			}
			if txID == "" || !inbound && !outgoing {
				continue
			}
			if _, duplicate := seen[txID]; duplicate {
				continue
			}
			seen[txID] = struct{}{}
			info, err := history.GetTransactionInfoByID(ctx, txID) // DET-010a
			if err != nil {
				return err
			}
			block, err := reconciledBlock(ctx, history, info)
			if err != nil {
				return err
			}
			var txRaw json.RawMessage
			if !trc20 {
				txRaw = raw
			}
			payments, err := decoder.DecodeReconciled(ctx, txID, txRaw, info, block)
			if err != nil {
				return err
			}
			inserted, err := r.store.ApplyReconciledPayments(ctx, payments, match)
			if err != nil {
				return err
			}
			if inserted > 0 {
				r.logger.Warn("payment safety net recovered follower miss", "txid", txID, "payments", inserted) // DET-013
			}
		}
		fingerprint := page.Meta.Fingerprint
		if fingerprint == "" {
			return nil
		}
		if fingerprint == lastFingerprint {
			return errors.New("TronGrid history repeated fingerprint")
		}
		lastFingerprint = fingerprint
		query.Set("fingerprint", fingerprint) // DET-010/011: iterate until exhausted.
	}
}

func historyItem(raw json.RawMessage, trc20 bool, address string) (string, bool, error) {
	if trc20 {
		var item struct {
			ID   string `json:"transaction_id"`
			From string `json:"from"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return "", false, err
		}
		return item.ID, sameTRONAddress(item.From, address), nil
	}
	var item struct {
		ID      string `json:"txID"`
		RawData struct {
			Contracts []struct {
				Type      string `json:"type"`
				Parameter struct {
					Value struct {
						Owner string `json:"owner_address"`
					} `json:"value"`
				} `json:"parameter"`
			} `json:"contract"`
		} `json:"raw_data"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return "", false, err
	}
	outgoing := len(item.RawData.Contracts) > 0 && item.RawData.Contracts[0].Type == "TransferContract" &&
		sameTRONAddress(item.RawData.Contracts[0].Parameter.Value.Owner, address)
	return item.ID, outgoing, nil
}

func sameTRONAddress(left, right string) bool {
	toBase58 := func(value string) string {
		payload, version, err := base58.CheckDecode(value)
		if err == nil && version == 0x41 && len(payload) == 20 {
			return base58.CheckEncode(payload, version)
		}
		hexValue := strings.TrimPrefix(strings.ToLower(value), "0x")
		decoded, err := hex.DecodeString(hexValue)
		if err == nil && len(decoded) == 21 && decoded[0] == 0x41 {
			return base58.CheckEncode(decoded[1:], decoded[0])
		}
		return value
	}
	return toBase58(left) == toBase58(right)
}

func reconciledBlock(ctx context.Context, history SafetyReader, info json.RawMessage) (follower.Block, error) {
	var transactionInfo struct {
		BlockNumber    int64 `json:"blockNumber"`
		BlockTimestamp int64 `json:"blockTimeStamp"`
	}
	if err := json.Unmarshal(info, &transactionInfo); err != nil || transactionInfo.BlockNumber <= 0 || transactionInfo.BlockTimestamp <= 0 {
		return follower.Block{}, errors.New("transaction info omitted block number or timestamp")
	}
	raw, err := history.GetBlockByNum(ctx, transactionInfo.BlockNumber)
	if err != nil {
		return follower.Block{}, err
	}
	var header struct {
		ID     string `json:"blockID"`
		Header struct {
			Raw struct {
				Timestamp int64 `json:"timestamp"`
			} `json:"raw_data"`
		} `json:"block_header"`
	}
	if err := json.Unmarshal(raw, &header); err != nil || header.ID == "" {
		return follower.Block{}, errors.New("block response omitted blockID")
	}
	return follower.Block{Height: transactionInfo.BlockNumber, ID: header.ID, Timestamp: transactionInfo.BlockTimestamp / 1000}, nil
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
