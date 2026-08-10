package decode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"payd/internal/config"
	"payd/internal/follower"
	"payd/internal/store"
)

type recordedFixtures struct {
	Cases []recordedCase `json:"cases"`
}

func TestConfiguredDustThresholdUsesBaseUnits(t *testing.T) {
	assets, err := compileAssets([]config.Asset{{Symbol: "USDT", Kind: "trc20", Decimals: 6,
		Contract: "TEkxiTehnzSmSe2XqrBj4w32RUN966rdz8", MinDeposit: "0.5"}})
	if err != nil {
		t.Fatal(err)
	}
	if !belowMinimum("400000", assets.minRaw["USDT"]) || belowMinimum("500000", assets.minRaw["USDT"]) {
		t.Fatal("0.4 USDT must be dust and 0.5 USDT must not")
	}
}

type recordedCase struct {
	Name            string          `json:"name"`
	Tags            []string        `json:"tags"`
	Height          int64           `json:"height"`
	OwnedAddressHex string          `json:"owned_address_hex"`
	Block           json.RawMessage `json:"block"`
	Receipts        json.RawMessage `json:"receipts"`
}

type fakeReceipts struct {
	raw   json.RawMessage
	err   error
	calls int
}

func (f *fakeReceipts) GetTransactionInfoByBlockNum(context.Context, int64) (json.RawMessage, error) {
	f.calls++
	return f.raw, f.err
}

// TST-002 exercises every required recorded fixture through the two decoder tiers.
func TestRecordedBlockFixtures(t *testing.T) {
	fixtures := loadFixtures(t)
	native := fixtureWithTag(t, fixtures, "trx_transfer")
	nativeBlock := fixtureBlock(t, native)

	t.Run("TRX outbound and failed or reverted call", func(t *testing.T) {
		ownedKey, _, err := tronAddress(native.OwnedAddressHex)
		if err != nil {
			t.Fatal(err)
		}
		got, err := screen(nativeBlock, map[string]int64{ownedKey: 7}, assetSet{native: "TRX", byToken: map[string]string{}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].native == nil {
			t.Fatalf("screened candidates = %#v, want the one successful TRX transfer", got)
		}
		payment := got[0].native
		if payment.Direction != "out" || payment.LogIndex != 0 || payment.AmountRaw != "1" || payment.AddressID == nil || *payment.AddressID != 7 {
			t.Fatalf("outbound TRX payment = %#v", payment)
		}
	})

	t.Run("TRX inbound direction", func(t *testing.T) {
		var tx transaction
		if err := json.Unmarshal(nativeBlock.Transactions[0], &tx); err != nil {
			t.Fatal(err)
		}
		toKey, _, err := tronAddress(tx.RawData.Contracts[0].Parameter.Value.To)
		if err != nil {
			t.Fatal(err)
		}
		got, err := screen(nativeBlock, map[string]int64{toKey: 8}, assetSet{native: "TRX", byToken: map[string]string{}})
		if err != nil || len(got) != 1 || got[0].native.Direction != "in" {
			t.Fatalf("inbound TRX screen = %#v, %v", got, err)
		}
	})

	t.Run("TRC20 receipt replaces calldata", func(t *testing.T) {
		item := fixtureWithTag(t, fixtures, "trc20_transfer")
		block := fixtureBlock(t, item)
		// Make Tier 1 request one unit; the recorded Tier 2 event delivers 200,000,000.
		block.Transactions[0] = json.RawMessage(strings.Replace(string(block.Transactions[0]), "000000000bebc200", "0000000000000001", 1))
		contractKey, _, err := tronAddress("41eca9bc828a3005b9a3b909f2cc5c2a54794de05f")
		if err != nil {
			t.Fatal(err)
		}
		toKey, _, err := tronAddress("41d03bdd7c4eea8210fdfb7010e31a4bda89ca36b2")
		if err != nil {
			t.Fatal(err)
		}
		assets := assetSet{byToken: map[string]string{contractKey: "TEST"}}
		candidates, err := screen(block, map[string]int64{toKey: 9}, assets)
		if err != nil || len(candidates) != 1 || candidates[0].native != nil {
			t.Fatalf("TRC20 candidates = %#v, %v", candidates, err)
		}
		payments, err := credit(block, candidates, item.Receipts, map[string]int64{toKey: 9}, assets, 123)
		if err != nil {
			t.Fatal(err)
		}
		if len(payments) != 1 || payments[0].AmountRaw != "200000000" || payments[0].Direction != "in" || payments[0].LogIndex != 0 {
			t.Fatalf("Tier 2 payments = %#v", payments)
		}
		fromKey, _, err := tronAddress("41d4532dc709c5411a8ef6d1300ed6e7a7c7e088a3")
		if err != nil {
			t.Fatal(err)
		}
		outbound, err := screen(block, map[string]int64{fromKey: 10}, assets)
		if err != nil {
			t.Fatal(err)
		}
		payments, err = credit(block, outbound, item.Receipts, map[string]int64{fromKey: 10}, assets, 123)
		if err != nil || len(payments) != 1 || payments[0].Direction != "out" {
			t.Fatalf("outbound TRC20 payments = %#v, %v", payments, err)
		}
	})

	t.Run("multiple Transfer logs use transaction-local indexes", func(t *testing.T) {
		item := fixtureWithTag(t, fixtures, "multiple_transfer_logs")
		block := fixtureBlock(t, item)
		var infos []receipt
		if err := json.Unmarshal(item.Receipts, &infos); err != nil {
			t.Fatal(err)
		}
		contractKey, _, err := tronAddress(infos[0].Logs[0].Address)
		if err != nil {
			t.Fatal(err)
		}
		first, _, err := tronAddress(infos[0].Logs[0].Topics[2])
		if err != nil {
			t.Fatal(err)
		}
		second, _, err := tronAddress(infos[0].Logs[1].Topics[2])
		if err != nil {
			t.Fatal(err)
		}
		payments, err := credit(block, []candidate{{txID: infos[0].ID}}, item.Receipts,
			map[string]int64{first: 10, second: 11}, assetSet{byToken: map[string]string{contractKey: "USDT"}}, 123)
		if err != nil {
			t.Fatal(err)
		}
		if len(payments) != 2 || payments[0].LogIndex != 0 || payments[1].LogIndex != 1 ||
			payments[0].AmountRaw != "1500000" || payments[1].AmountRaw != "220458000000" {
			t.Fatalf("multi-log payments = %#v", payments)
		}
	})

	t.Run("failed receipt cannot credit a transfer", func(t *testing.T) {
		item := fixtureWithTag(t, fixtures, "reverted_transfer")
		block := fixtureBlock(t, item)
		var infos []receipt
		if err := json.Unmarshal(item.Receipts, &infos); err != nil {
			t.Fatal(err)
		}
		payments, err := credit(block, []candidate{{txID: infos[1].ID}}, item.Receipts, map[string]int64{}, assetSet{byToken: map[string]string{}}, 123)
		if err != nil || len(payments) != 0 {
			t.Fatalf("failed receipt payments = %#v, %v", payments, err)
		}
	})

	t.Run("empty block skips receipt fetch", func(t *testing.T) {
		item := fixtureWithTag(t, fixtures, "empty_block")
		reader := &fakeReceipts{err: errors.New("must not be called")}
		decoder := &Decoder{
			reader: reader,
			owned:  func(context.Context) ([]store.OwnedAddress, error) { return nil, nil },
			assets: assetSet{native: "TRX", byToken: map[string]string{}}, now: time.Now,
		}
		apply, err := decoder.Prepare(context.Background(), fixtureBlock(t, item))
		if err != nil || apply != nil || reader.calls != 0 {
			t.Fatalf("empty block prepare = apply:%v calls:%d err:%v", apply != nil, reader.calls, err)
		}
	})

	for _, tag := range []string{"trx_transfer", "trc20_transfer", "failed_contract_call", "reverted_transfer", "multiple_transfer_logs", "outbound_transfer", "empty_block"} {
		fixtureWithTag(t, fixtures, tag)
	}
}

// DET-010a: safety-net inserts use the authoritative transaction receipt's
// transaction-local log indexes, never an assumed zero.
func TestDecodeReconciledUsesReceiptLogIndexes(t *testing.T) {
	item := fixtureWithTag(t, loadFixtures(t), "multiple_transfer_logs")
	block := fixtureBlock(t, item)
	var infos []receipt
	if err := json.Unmarshal(item.Receipts, &infos); err != nil {
		t.Fatal(err)
	}
	contractKey, _, err := tronAddress(infos[0].Logs[0].Address)
	if err != nil {
		t.Fatal(err)
	}
	firstKey, first, err := tronAddress(infos[0].Logs[0].Topics[2])
	if err != nil {
		t.Fatal(err)
	}
	secondKey, second, err := tronAddress(infos[0].Logs[1].Topics[2])
	if err != nil {
		t.Fatal(err)
	}
	decoder := &Decoder{
		owned: func(context.Context) ([]store.OwnedAddress, error) {
			return []store.OwnedAddress{{ID: 1, Address: first}, {ID: 2, Address: second}}, nil
		},
		assets: assetSet{byToken: map[string]string{contractKey: "USDT"}}, now: func() time.Time { return time.Unix(20, 0) },
	}
	payments, err := decoder.DecodeReconciled(context.Background(), infos[0].ID, nil, mustJSON(t, infos[0]), block)
	if err != nil {
		t.Fatal(err)
	}
	if len(payments) != 2 || payments[0].LogIndex != 0 || payments[1].LogIndex != 1 ||
		*payments[0].AddressID != 1 || *payments[1].AddressID != 2 {
		t.Fatalf("reconciled payments = %#v (keys %s %s)", payments, firstKey, secondKey)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// DET-005a: a receipt error escapes Prepare, so follower.commit never opens CommitBlock and cannot advance the cursor.
func TestReceiptFailureDoesNotCommitBlockOrCursor(t *testing.T) {
	item := fixtureWithTag(t, loadFixtures(t), "trx_transfer")
	block := fixtureBlock(t, item)
	database, err := store.Open(context.Background(), t.TempDir()+"/payd.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	reader := &fakeReceipts{err: errors.New("simulated network error")}
	decoder := &Decoder{
		reader: reader,
		owned: func(context.Context) ([]store.OwnedAddress, error) {
			return []store.OwnedAddress{{ID: 1, Address: item.OwnedAddressHex}}, nil
		},
		assets: assetSet{native: "TRX", byToken: map[string]string{}}, now: time.Now,
	}
	chain := &fakeChain{tip: item.Block}
	worker, err := follower.New(chain, database, decoder.Prepare, 3*time.Second, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Tick(context.Background()); err == nil || !strings.Contains(err.Error(), "simulated network error") {
		t.Fatalf("Tick error = %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("receipt calls = %d, want 1", reader.calls)
	}
	if cursor, exists, err := database.Cursor(context.Background()); err != nil || exists {
		t.Fatalf("cursor after receipt failure = %#v, exists=%v, err=%v", cursor, exists, err)
	}
	if _, exists, err := database.Block(context.Background(), block.Height); err != nil || exists {
		t.Fatalf("block after receipt failure: exists=%v err=%v", exists, err)
	}
}

// DET-002a/004a: a configured token's non-indexed Transfer log is not
// attributable, but it must not prevent valid logs or the block cursor advancing.
func TestMalformedTransferLogDoesNotStallFollower(t *testing.T) {
	item := fixtureWithTag(t, loadFixtures(t), "trc20_transfer")
	block := fixtureBlock(t, item)
	var infos []receipt
	if err := json.Unmarshal(item.Receipts, &infos); err != nil {
		t.Fatal(err)
	}
	valid := infos[0].Logs[0]
	infos[0].Logs = append(infos[0].Logs, valid)
	infos[0].Logs[0].Topics = []string{valid.Topics[0]}

	contractKey, _, err := tronAddress(valid.Address)
	if err != nil {
		t.Fatal(err)
	}
	toKey, to, err := tronAddress(valid.Topics[2])
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(context.Background(), t.TempDir()+"/payd.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	reader := &fakeReceipts{raw: mustJSON(t, infos)}
	var payments []store.PaymentRecord
	decoder := &Decoder{
		reader: reader,
		owned: func(context.Context) ([]store.OwnedAddress, error) {
			return []store.OwnedAddress{{ID: 1, Address: to}}, nil
		},
		match: func(_ *store.BlockWrite, payment store.PaymentRecord) error {
			payments = append(payments, payment)
			return nil
		},
		assets: assetSet{byToken: map[string]string{contractKey: "USDT"}},
		now:    func() time.Time { return time.Unix(123, 0) },
	}
	worker, err := follower.New(&fakeChain{tip: item.Block}, database, decoder.Prepare, 3*time.Second, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(payments) != 1 || payments[0].LogIndex != 1 || payments[0].AddressID == nil || *payments[0].AddressID != 1 {
		t.Fatalf("payments = %#v, want only the valid log at index 1 for owned key %s", payments, toKey)
	}
	if cursor, exists, err := database.Cursor(context.Background()); err != nil || !exists || cursor.LastHeight != block.Height {
		t.Fatalf("cursor after malformed log = %#v, exists=%v, err=%v", cursor, exists, err)
	}
}

type fakeChain struct{ tip json.RawMessage }

func (f *fakeChain) GetNowBlock(context.Context) (json.RawMessage, error) { return f.tip, nil }
func (f *fakeChain) GetBlockByNum(context.Context, int64) (json.RawMessage, error) {
	return f.tip, nil
}

func loadFixtures(t *testing.T) recordedFixtures {
	t.Helper()
	raw, err := os.ReadFile("../follower/testdata/recorded_blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures recordedFixtures
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func fixtureWithTag(t *testing.T, fixtures recordedFixtures, tag string) recordedCase {
	t.Helper()
	for _, item := range fixtures.Cases {
		if slices.Contains(item.Tags, tag) {
			return item
		}
	}
	t.Fatalf("fixture tag %q not found", tag)
	return recordedCase{}
}

func fixtureBlock(t *testing.T, item recordedCase) follower.Block {
	t.Helper()
	var raw struct {
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
	if err := json.Unmarshal(item.Block, &raw); err != nil {
		t.Fatal(err)
	}
	return follower.Block{Height: raw.Header.Raw.Number, ID: raw.ID, ParentID: raw.Header.Raw.ParentHash,
		Timestamp: raw.Header.Raw.Timestamp / 1000, Transactions: raw.Transactions, Raw: item.Block}
}
