// Package decode performs the follower's two-tier TRX/TRC-20 payment decoding.
package decode

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcutil/base58"

	"payd/internal/config"
	"payd/internal/follower"
	"payd/internal/store"
)

const (
	transferSelector = "a9059cbb"
	transferTopic    = "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
)

type ReceiptReader interface {
	GetTransactionInfoByBlockNum(context.Context, int64) (json.RawMessage, error)
}

type Decoder struct {
	reader ReceiptReader
	owned  func(context.Context) ([]store.OwnedAddress, error)
	mu     sync.RWMutex
	assets assetSet
	now    func() time.Time
}

type assetSet struct {
	native  string
	byToken map[string]string
}

type candidate struct {
	txID   string
	native *store.PaymentRecord
}

type transaction struct {
	ID      string `json:"txID"`
	RawData struct {
		Contracts []struct {
			Type      string `json:"type"`
			Parameter struct {
				Value struct {
					Owner    string          `json:"owner_address"`
					To       string          `json:"to_address"`
					Amount   json.RawMessage `json:"amount"`
					Contract string          `json:"contract_address"`
					Data     string          `json:"data"`
				} `json:"value"`
			} `json:"parameter"`
		} `json:"contract"`
	} `json:"raw_data"`
	Ret []struct {
		Result string `json:"contractRet"`
	} `json:"ret"`
}

type receipt struct {
	ID      string `json:"id"`
	Result  string `json:"result"`
	Receipt struct {
		Result string `json:"result"`
	} `json:"receipt"`
	Logs []struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
	} `json:"log"`
}

func New(reader ReceiptReader, database *store.Store, assets []config.Asset) (*Decoder, error) {
	if reader == nil || database == nil {
		return nil, errors.New("decoder requires receipt reader and store")
	}
	set, err := compileAssets(assets)
	if err != nil {
		return nil, err
	}
	return &Decoder{reader: reader, owned: database.OwnedAddresses, assets: set, now: time.Now}, nil
}

// UpdateAssets applies CFG-006 asset reloads without racing the follower.
func (d *Decoder) UpdateAssets(assets []config.Asset) error {
	set, err := compileAssets(assets)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.assets = set
	d.mu.Unlock()
	return nil
}

// Prepare finishes the receipt RPC and decoding before returning a DB-only closure (DET-005a, CHN-006a, ARC-007).
func (d *Decoder) Prepare(ctx context.Context, block follower.Block) (store.BlockApply, error) {
	d.mu.RLock()
	assets := d.assets
	d.mu.RUnlock()
	addresses, err := d.owned(ctx)
	if err != nil {
		return nil, err
	}
	owned := make(map[string]int64, len(addresses))
	for _, address := range addresses {
		key, _, err := tronAddress(address.Address)
		if err != nil {
			return nil, fmt.Errorf("decode owned address %d: %w", address.ID, err)
		}
		owned[key] = address.ID
	}

	candidates, err := screen(block, owned, assets)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	rawReceipts, err := d.reader.GetTransactionInfoByBlockNum(ctx, block.Height) // DET-005: exactly one receipt fetch per hit block.
	if err != nil {
		return nil, fmt.Errorf("fetch receipts for block %d: %w", block.Height, err)
	}
	payments, err := credit(block, candidates, rawReceipts, owned, assets, d.now().UTC().Unix())
	if err != nil {
		return nil, err
	}
	return func(write *store.BlockWrite) error {
		for _, payment := range payments {
			if _, err := write.UpsertPayment(payment); err != nil {
				return err
			}
		}
		return nil
	}, nil
}

// screen is Tier 1: it decides only whether a receipt is needed. TRC-20 calldata never becomes a payment (DET-001..004a).
func screen(block follower.Block, owned map[string]int64, assets assetSet) ([]candidate, error) {
	var candidates []candidate
	for _, raw := range block.Transactions {
		var tx transaction
		if err := json.Unmarshal(raw, &tx); err != nil {
			return nil, fmt.Errorf("decode transaction in block %d: %w", block.Height, err)
		}
		if tx.ID == "" || len(tx.RawData.Contracts) == 0 || len(tx.Ret) == 0 || tx.Ret[0].Result != "SUCCESS" {
			continue // DET-004
		}
		contract := tx.RawData.Contracts[0] // DET-001
		switch contract.Type {
		case "TransferContract":
			if assets.native == "" {
				continue
			}
			fromKey, from, err := tronAddress(contract.Parameter.Value.Owner)
			if err != nil {
				return nil, fmt.Errorf("transaction %s owner: %w", tx.ID, err)
			}
			toKey, to, err := tronAddress(contract.Parameter.Value.To)
			if err != nil {
				return nil, fmt.Errorf("transaction %s destination: %w", tx.ID, err)
			}
			direction, addressID, hit := direction(fromKey, toKey, owned)
			if !hit {
				continue
			}
			amount, err := decimalAmount(contract.Parameter.Value.Amount)
			if err != nil {
				return nil, fmt.Errorf("transaction %s amount: %w", tx.ID, err)
			}
			candidates = append(candidates, candidate{txID: tx.ID, native: &store.PaymentRecord{
				TxID: tx.ID, LogIndex: 0, Direction: direction, BlockHeight: block.Height, BlockID: block.ID,
				BlockTimestamp: block.Timestamp, FromAddress: from, ToAddress: to, AddressID: &addressID,
				Asset: assets.native, AmountRaw: amount,
			}}) // DET-002/002a/002b
		case "TriggerSmartContract":
			contractKey, _, err := tronAddress(contract.Parameter.Value.Contract)
			if err != nil {
				return nil, fmt.Errorf("transaction %s token contract: %w", tx.ID, err)
			}
			if _, configured := assets.byToken[contractKey]; !configured {
				continue
			}
			data := strings.ToLower(strings.TrimPrefix(contract.Parameter.Value.Data, "0x"))
			if len(data) < 8+64+64 || data[:8] != transferSelector {
				continue
			}
			fromKey, _, err := tronAddress(contract.Parameter.Value.Owner)
			if err != nil {
				return nil, fmt.Errorf("transaction %s owner: %w", tx.ID, err)
			}
			toKey, _, err := tronAddress(data[8 : 8+64])
			if err != nil {
				return nil, fmt.Errorf("transaction %s calldata destination: %w", tx.ID, err)
			}
			if _, err := hexAmount(data[8+64 : 8+64+64]); err != nil {
				return nil, fmt.Errorf("transaction %s calldata amount: %w", tx.ID, err)
			}
			if _, _, hit := direction(fromKey, toKey, owned); hit {
				candidates = append(candidates, candidate{txID: tx.ID}) // DET-003: amount is intentionally not retained.
			}
		}
	}
	return candidates, nil
}

// credit is Tier 2. Receipt Transfer logs replace every TRC-20 Tier 1 field (DET-004a/006).
func credit(block follower.Block, candidates []candidate, raw json.RawMessage, owned map[string]int64, assets assetSet, detectedAt int64) ([]store.PaymentRecord, error) {
	var receipts []receipt
	if err := json.Unmarshal(raw, &receipts); err != nil {
		return nil, fmt.Errorf("decode receipts for block %d: %w", block.Height, err)
	}
	byID := make(map[string]receipt, len(receipts))
	for _, item := range receipts {
		byID[item.ID] = item
	}
	var payments []store.PaymentRecord
	for _, item := range candidates {
		if item.native != nil {
			item.native.DetectedAt = detectedAt
			payments = append(payments, *item.native)
			continue
		}
		info, found := byID[item.txID]
		if !found || info.Result == "FAILED" || (info.Receipt.Result != "" && info.Receipt.Result != "SUCCESS") {
			continue
		}
		for logIndex, log := range info.Logs { // DET-002a: the index is the position in this transaction's unfiltered log array.
			if len(log.Topics) == 0 || strings.ToLower(strings.TrimPrefix(log.Topics[0], "0x")) != transferTopic {
				continue
			}
			contractKey, _, err := tronAddress(log.Address)
			if err != nil {
				return nil, fmt.Errorf("transaction %s log %d contract: %w", item.txID, logIndex, err)
			}
			asset, configured := assets.byToken[contractKey]
			if !configured {
				continue
			}
			if len(log.Topics) < 3 {
				return nil, fmt.Errorf("transaction %s log %d Transfer event has fewer than three topics", item.txID, logIndex)
			}
			fromKey, from, err := tronAddress(log.Topics[1])
			if err != nil {
				return nil, fmt.Errorf("transaction %s log %d source: %w", item.txID, logIndex, err)
			}
			toKey, to, err := tronAddress(log.Topics[2])
			if err != nil {
				return nil, fmt.Errorf("transaction %s log %d destination: %w", item.txID, logIndex, err)
			}
			direction, addressID, hit := direction(fromKey, toKey, owned)
			if !hit {
				continue
			}
			amount, err := hexAmount(log.Data)
			if err != nil {
				return nil, fmt.Errorf("transaction %s log %d amount: %w", item.txID, logIndex, err)
			}
			payments = append(payments, store.PaymentRecord{
				TxID: item.txID, LogIndex: logIndex, Direction: direction, BlockHeight: block.Height, BlockID: block.ID,
				BlockTimestamp: block.Timestamp, FromAddress: from, ToAddress: to, AddressID: &addressID,
				Asset: asset, AmountRaw: amount, DetectedAt: detectedAt,
			})
		}
	}
	return payments, nil
}

func compileAssets(assets []config.Asset) (assetSet, error) {
	set := assetSet{byToken: make(map[string]string)}
	for _, asset := range assets {
		switch asset.Kind {
		case "native":
			set.native = asset.Symbol
		case "trc20":
			key, _, err := tronAddress(asset.Contract)
			if err != nil {
				return assetSet{}, fmt.Errorf("asset %s contract: %w", asset.Symbol, err)
			}
			set.byToken[key] = asset.Symbol
		}
	}
	return set, nil
}

func direction(from, to string, owned map[string]int64) (string, int64, bool) {
	if id, ok := owned[from]; ok {
		return "out", id, true // DET-002b; one DB-004 identity also covers an owned-to-owned transfer.
	}
	if id, ok := owned[to]; ok {
		return "in", id, true
	}
	return "", 0, false
}

func decimalAmount(raw json.RawMessage) (string, error) {
	value := strings.Trim(string(raw), `"`)
	amount, ok := new(big.Int).SetString(value, 10)
	if !ok || amount.Sign() < 0 {
		return "", fmt.Errorf("invalid non-negative base-unit amount %q", value)
	}
	return amount.String(), nil
}

func hexAmount(value string) (string, error) {
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	if value == "" {
		return "", errors.New("empty event amount")
	}
	amount, ok := new(big.Int).SetString(value, 16)
	if !ok {
		return "", fmt.Errorf("invalid hex amount %q", value)
	}
	return amount.String(), nil
}

// tronAddress accepts Base58Check, 21-byte Tron hex, 20-byte receipt hex, or a 32-byte ABI word.
func tronAddress(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "T") {
		payload, version, err := base58.CheckDecode(value)
		if err != nil || version != 0x41 || len(payload) != 20 {
			return "", "", fmt.Errorf("invalid Tron Base58Check address %q", value)
		}
		key := "41" + hex.EncodeToString(payload)
		return key, value, nil
	}
	hexValue := strings.TrimPrefix(strings.ToLower(value), "0x")
	switch len(hexValue) {
	case 64:
		hexValue = "41" + hexValue[24:]
	case 40:
		hexValue = "41" + hexValue
	case 42:
	default:
		return "", "", fmt.Errorf("invalid Tron hex address length %d", len(hexValue))
	}
	raw, err := hex.DecodeString(hexValue)
	if err != nil || len(raw) != 21 || raw[0] != 0x41 {
		return "", "", fmt.Errorf("invalid Tron hex address %q", value)
	}
	return hexValue, base58.CheckEncode(raw[1:], raw[0]), nil
}
