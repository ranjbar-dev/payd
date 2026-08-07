// Package withdraw implements the crash-safe W-008 withdrawal state machine.
package withdraw

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	tronpb "github.com/ranjbar-dev/hd-wallet/txproto/tron"
	"google.golang.org/protobuf/proto"

	"payd/internal/chain"
	"payd/internal/config"
	"payd/internal/store"
)

const (
	tickInterval = 2 * time.Second
	lookupEvery  = 15 * time.Second
)

type Reader interface {
	GetNowBlock(context.Context) (json.RawMessage, error)
	GetTransactionByID(context.Context, string) (json.RawMessage, error)
	GetAccountResource(context.Context, string) (json.RawMessage, error)
}

type SolidityReader interface {
	GetTransactionInfoByID(context.Context, string) (json.RawMessage, error)
}

type Broadcaster interface {
	Send(context.Context, json.RawMessage) (chain.Response, error)
}

type Signer interface {
	SignTransaction(hdwallet.Chain, uint32, proto.Message) (proto.Message, error)
}

type Engine struct {
	reader      Reader
	solidity    SolidityReader
	broadcaster Broadcaster
	store       *store.Store
	signer      Signer
	logger      *slog.Logger
	now         func() time.Time

	mu           sync.RWMutex
	config       config.Config
	events       store.EventConfig
	afterAttempt func()
}

func New(reader Reader, solidity SolidityReader, broadcaster Broadcaster, database *store.Store, signer Signer, cfg config.Config, logger *slog.Logger) (*Engine, error) {
	if reader == nil || solidity == nil || broadcaster == nil || database == nil || signer == nil {
		return nil, errors.New("withdrawal engine requires chain clients, store, and signer")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{reader: reader, solidity: solidity, broadcaster: broadcaster, store: database, signer: signer,
		logger: logger, now: time.Now, config: cfg, events: store.NewEventConfig(cfg.IPN, cfg.Assets)}, nil
}

func (e *Engine) UpdateConfig(cfg config.Config) {
	e.mu.Lock()
	e.config, e.events = cfg, store.NewEventConfig(cfg.IPN, cfg.Assets)
	e.mu.Unlock()
}

func (e *Engine) Run(ctx context.Context) {
	// WDR-019a: forced startup reconciliation completes before the first claim.
	if err := e.recover(ctx, true); err != nil && ctx.Err() == nil {
		e.logger.Error("withdrawal startup recovery", "error", err)
	}
	for ctx.Err() == nil {
		if err := e.Tick(ctx); err != nil && ctx.Err() == nil {
			e.logger.Error("withdrawal tick", "error", err)
		}
		timer := time.NewTimer(tickInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (e *Engine) Tick(ctx context.Context) error {
	if err := e.recover(ctx, false); err != nil {
		return err
	}
	e.mu.RLock()
	enabled := e.config.Withdrawal.Enabled
	e.mu.RUnlock()
	if !enabled {
		return nil
	}
	w, found, err := e.store.NextRequestedWithdrawal(ctx)
	if err != nil || !found {
		return err
	}
	w, claimed, err := e.store.ClaimWithdrawal(ctx, w.ID)
	if err != nil || !claimed {
		return err
	}
	return e.acquireAndSend(ctx, w)
}

func (e *Engine) recover(ctx context.Context, force bool) error {
	now := e.now()
	e.mu.RLock()
	energyTimeout := e.config.Energy.PollTimeout
	e.mu.RUnlock()
	candidates, err := e.store.WithdrawalRecoveryCandidates(ctx, now, energyTimeout, force)
	if err != nil {
		return err
	}
	for _, w := range candidates {
		if w.TxID == "" {
			if w.Status == "signing" { // WDR-018b: never infer that pre-txid signing was harmless.
				if err := e.store.WithdrawalNeedsOperator(ctx, w.ID, "signing state has no persisted txid", now, e.eventConfig()); err != nil {
					return err
				}
				continue
			}
			if w.Status == "awaiting_resources" {
				if err := e.acquireAndSend(ctx, w); err != nil {
					return err
				}
			}
			continue
		}
		if !force {
			if w.Status == "signing" && w.BroadcastAttemptedAt != nil && now.Unix()-*w.BroadcastAttemptedAt < 60 {
				continue
			}
			if w.LastLookupAt != nil && now.Unix()-*w.LastLookupAt < int64(lookupEvery/time.Second) {
				continue
			}
		}
		if err := e.resolve(ctx, w, ""); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) acquireAndSend(ctx context.Context, w store.Withdrawal) error {
	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()
	body, err := e.reader.GetAccountResource(ctx, w.FromAddress)
	if err != nil {
		return err
	}
	energy, bandwidth, err := availableResources(body)
	if err != nil {
		return err
	}
	asset, found := configuredAsset(cfg.Assets, w.Asset)
	if !found {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "withdrawal asset is no longer configured", e.now(), e.eventConfig())
	}
	energySource := ""
	if asset.Kind == "trc20" {
		if w.Status == "awaiting_energy" {
			if err := e.store.ReconcileWithdrawalEnergy(ctx, w.ID, energy >= cfg.Resources.MinEnergy, e.now()); err != nil {
				return err
			}
		}
		if energy < cfg.Resources.MinEnergy {
			_, err = e.store.SetWithdrawalResourceState(ctx, w.ID, w.Status, "awaiting_energy", "", "") // WDR-009c; P12 performs sourcing.
			return err
		}
		energySource = "existing"
	}
	bandwidthSource := "existing"
	if bandwidth < cfg.Resources.MinBandwidth {
		params, loadErr := e.store.LoadChainParameters(ctx)
		balance, balanceErr := e.store.BalanceForWithdrawal(ctx, w.FromAddress, "TRX")
		trx, ok := new(big.Int).SetString(balance.ConfirmedRaw, 10)
		burn := new(big.Int).Mul(big.NewInt(cfg.Resources.MinBandwidth), big.NewInt(params.TransactionFee))
		if ok && asset.Kind == "native" {
			amount, valid := new(big.Int).SetString(w.AmountRaw, 10)
			if !valid {
				ok = false
			} else {
				trx.Sub(trx, amount)
			}
		}
		if loadErr != nil || balanceErr != nil || !ok || trx.Cmp(burn) < 0 {
			return nil // RES-006: remain awaiting_resources until P12 can acquire bandwidth.
		}
		bandwidthSource = "burn"
	}
	changed, err := e.store.SetWithdrawalResourceState(ctx, w.ID, w.Status, "signing", energySource, bandwidthSource)
	if err != nil || !changed {
		return err
	}
	w.Status, w.EnergySource, w.BandwidthSource = "signing", energySource, bandwidthSource
	return e.signAndBroadcast(ctx, w, asset, cfg)
}

func (e *Engine) signAndBroadcast(ctx context.Context, w store.Withdrawal, asset config.Asset, cfg config.Config) error {
	reference, found := e.store.LatestReferenceBlock()
	if found {
		if cursor, ok, err := e.store.Cursor(ctx); err != nil {
			return err
		} else if ok && cursor.LastHeight-reference.Height > 10 {
			found = false
		}
	}
	if !found {
		raw, err := e.reader.GetNowBlock(ctx)
		if err != nil {
			return err
		}
		reference, err = parseReferenceBlock(raw)
		if err != nil {
			return err
		}
	}
	input, err := signingInput(w, asset, reference, cfg.Withdrawal)
	if err != nil {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, err.Error(), e.now(), e.eventConfig())
	}
	out, err := e.signer.SignTransaction(hdwallet.TRX, w.HDIndex, input)
	if err != nil {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "signing failed: "+err.Error(), e.now(), e.eventConfig())
	}
	txid, err := hdwallet.TransactionID(out)
	if err != nil {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "txid extraction failed: "+err.Error(), e.now(), e.eventConfig())
	}
	payload, err := hdwallet.BroadcastPayload(hdwallet.TRX, out)
	if err != nil {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "broadcast payload failed: "+err.Error(), e.now(), e.eventConfig())
	}
	now := e.now()
	expires := reference.TimestampMS/1000 + int64(cfg.Withdrawal.Expiration/time.Second)
	if err := e.store.PersistWithdrawalAttempt(ctx, w.ID, txid, now, expires); err != nil { // WDR-015 happens before Send.
		return err
	}
	if e.afterAttempt != nil {
		e.afterAttempt()
	}
	response, sendErr := e.broadcaster.Send(ctx, json.RawMessage(payload)) // WDR-014a: this call appears once in the state machine.
	rawResponse := string(response.Body)
	if rawResponse == "" && sendErr != nil {
		rawResponse = sendErr.Error()
	}
	w.TxID, w.ExpirationAt = txid, &expires
	classification, code := classifyBroadcast(response, sendErr)
	if classification != "deterministic" {
		return e.store.RecordWithdrawalBroadcast(ctx, w.ID, rawResponse, now) // WDR-017(a,c): ambiguity means broadcast.
	}
	if err := e.store.RecordWithdrawalBroadcast(ctx, w.ID, rawResponse, now); err != nil {
		return err
	}
	return e.resolve(ctx, w, code)
}

func (e *Engine) resolve(ctx context.Context, w store.Withdrawal, deterministicCode string) error {
	now := e.now()
	body, err := e.reader.GetTransactionByID(ctx, w.TxID)
	if err != nil {
		if deterministicCode != "" {
			return e.store.WithdrawalNeedsOperator(ctx, w.ID, err.Error(), now, e.eventConfig()) // WDR-022a
		}
		return e.store.RecordWithdrawalLookup(ctx, w.ID, false, err, now, e.eventConfig())
	}
	found, err := transactionFound(body)
	if err != nil {
		return e.store.RecordWithdrawalLookup(ctx, w.ID, false, err, now, e.eventConfig())
	}
	if !found {
		if deterministicCode != "" {
			return e.store.FailWithdrawalAbsent(ctx, w.ID, deterministicCode, now, e.eventConfig())
		}
		if w.ExpirationAt != nil && now.Unix() >= *w.ExpirationAt {
			return e.store.FailWithdrawalAbsent(ctx, w.ID, "transaction absent after expiration", now, e.eventConfig())
		}
		return e.store.RecordWithdrawalLookup(ctx, w.ID, false, nil, now, e.eventConfig())
	}
	if err := e.store.RecordWithdrawalLookup(ctx, w.ID, true, nil, now, e.eventConfig()); err != nil {
		return err
	}
	receiptBody, err := e.solidity.GetTransactionInfoByID(ctx, w.TxID)
	if err != nil {
		return nil
	}
	receipt, solid, err := parseReceipt(receiptBody)
	if err != nil || !solid {
		return err
	}
	if receipt.Result != "" && receipt.Result != "SUCCESS" {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "solidified transaction result: "+receipt.Result, now, e.eventConfig())
	}
	return e.store.ConfirmWithdrawal(ctx, w.ID, receipt.FeeRaw, receipt.EnergyUsed, receipt.BlockHeight, receipt.BlockTimestamp, e.eventConfig(), now)
}

func (e *Engine) eventConfig() store.EventConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.events
}

func configuredAsset(assets []config.Asset, symbol string) (config.Asset, bool) {
	for _, asset := range assets {
		if asset.Symbol == symbol {
			return asset, true
		}
	}
	return config.Asset{}, false
}

func availableResources(body []byte) (int64, int64, error) {
	var r struct{ EnergyLimit, EnergyUsed, NetLimit, NetUsed, FreeNetLimit, FreeNetUsed int64 }
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, 0, err
	}
	return r.EnergyLimit - r.EnergyUsed, r.NetLimit - r.NetUsed + r.FreeNetLimit - r.FreeNetUsed, nil
}

func parseReferenceBlock(raw []byte) (store.ReferenceBlock, error) {
	var response struct {
		ID     string `json:"blockID"`
		Header struct {
			Raw struct {
				Number         int64  `json:"number"`
				Timestamp      int64  `json:"timestamp"`
				TxTrieRoot     string `json:"txTrieRoot"`
				ParentHash     string `json:"parentHash"`
				WitnessAddress string `json:"witness_address"`
				Version        int32  `json:"version"`
			} `json:"raw_data"`
		} `json:"block_header"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return store.ReferenceBlock{}, err
	}
	r := response.Header.Raw
	if response.ID == "" || r.Timestamp <= 0 {
		return store.ReferenceBlock{}, errors.New("reference block is incomplete")
	}
	return store.ReferenceBlock{Height: r.Number, TimestampMS: r.Timestamp, ID: response.ID, TxTrieRoot: r.TxTrieRoot,
		ParentHash: r.ParentHash, WitnessAddress: r.WitnessAddress, Version: r.Version}, nil
}

func signingInput(w store.Withdrawal, asset config.Asset, reference store.ReferenceBlock, cfg config.Withdrawal) (*tronpb.SigningInput, error) {
	amount, ok := new(big.Int).SetString(w.AmountRaw, 10)
	if !ok || amount.Sign() <= 0 {
		return nil, errors.New("invalid withdrawal amount")
	}
	decode := func(name, value string) ([]byte, error) {
		b, err := hex.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("invalid reference block %s: %w", name, err)
		}
		return b, nil
	}
	txRoot, err := decode("tx trie root", reference.TxTrieRoot)
	if err != nil {
		return nil, err
	}
	parent, err := decode("parent hash", reference.ParentHash)
	if err != nil {
		return nil, err
	}
	witness, err := decode("witness address", reference.WitnessAddress)
	if err != nil {
		return nil, err
	}
	// WDR-010/010a: TAPOS, timestamp, and expiration share one authoritative reference block; the host clock is not transaction input.
	tx := &tronpb.Transaction{Timestamp: reference.TimestampMS, Expiration: reference.TimestampMS + cfg.Expiration.Milliseconds(),
		FeeLimit: cfg.FeeLimitTRX * 1_000_000, BlockHeader: &tronpb.BlockHeader{Timestamp: reference.TimestampMS,
			TxTrieRoot: txRoot, ParentHash: parent, Number: reference.Height, WitnessAddress: witness, Version: reference.Version}}
	if asset.Kind == "native" { // WDR-013: native and TRC-20 transfers use their distinct signer contracts.
		if !amount.IsInt64() {
			return nil, errors.New("TRX withdrawal exceeds signer integer range")
		}
		tx.ContractOneof = &tronpb.Transaction_Transfer{Transfer: &tronpb.TransferContract{OwnerAddress: w.FromAddress, ToAddress: w.ToAddress, Amount: amount.Int64()}}
	} else {
		tx.ContractOneof = &tronpb.Transaction_TransferTrc20{TransferTrc20: &tronpb.TransferTRC20Contract{
			OwnerAddress: w.FromAddress, ContractAddress: asset.Contract, ToAddress: w.ToAddress, Amount: amount.Bytes()}}
	}
	return &tronpb.SigningInput{Transaction: tx}, nil
}

func classifyBroadcast(response chain.Response, sendErr error) (string, string) {
	var body struct {
		Result bool   `json:"result"`
		Code   string `json:"code"`
	}
	if json.Unmarshal(response.Body, &body) == nil && body.Result && sendErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		return "accepted", ""
	}
	code := strings.ToUpper(body.Code)
	switch code {
	case "SIGERROR", "TAPOS_ERROR", "TRANSACTION_EXPIRATION_ERROR", "CONTRACT_VALIDATE_ERROR", "BANDWITH_ERROR":
		return "deterministic", code
	default:
		if sendErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
			return "ambiguous", code
		}
		return "ambiguous", code
	}
}

func transactionFound(body []byte) (bool, error) {
	var result map[string]json.RawMessage
	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}
	_, txid := result["txID"]
	_, id := result["id"]
	return txid || id, nil
}

type receipt struct {
	FeeRaw                                  string
	EnergyUsed, BlockHeight, BlockTimestamp int64
	Result                                  string
}

func parseReceipt(body []byte) (receipt, bool, error) {
	var response struct {
		ID             string      `json:"id"`
		Fee            json.Number `json:"fee"`
		BlockNumber    int64       `json:"blockNumber"`
		BlockTimeStamp int64       `json:"blockTimeStamp"`
		Result         string      `json:"result"`
		Receipt        struct {
			EnergyUsageTotal int64 `json:"energy_usage_total"`
		} `json:"receipt"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		return receipt{}, false, err
	}
	if response.ID == "" {
		return receipt{}, false, nil
	}
	fee := response.Fee.String()
	if fee == "" {
		fee = "0"
	}
	return receipt{FeeRaw: fee, EnergyUsed: response.Receipt.EnergyUsageTotal, BlockHeight: response.BlockNumber,
		BlockTimestamp: response.BlockTimeStamp / 1000, Result: response.Result}, true, nil
}
