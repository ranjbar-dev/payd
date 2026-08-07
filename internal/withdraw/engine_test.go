package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	"google.golang.org/protobuf/proto"

	"payd/internal/chain"
	"payd/internal/config"
	"payd/internal/store"
)

const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

type fakeReader struct {
	transaction json.RawMessage
}

func (f *fakeReader) GetNowBlock(context.Context) (json.RawMessage, error) {
	return referenceBlock(), nil
}
func (f *fakeReader) GetTransactionByID(context.Context, string) (json.RawMessage, error) {
	return f.transaction, nil
}
func (f *fakeReader) GetAccountResource(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{"EnergyLimit":100000,"EnergyUsed":0,"NetLimit":1000,"NetUsed":0}`), nil
}

type fakeSolidity struct{ body json.RawMessage }

func (f fakeSolidity) GetTransactionInfoByID(context.Context, string) (json.RawMessage, error) {
	return f.body, nil
}

type fakeBroadcast struct {
	response       chain.Response
	err            error
	calls          int
	panicAfterSend bool
}

func (f *fakeBroadcast) Send(context.Context, json.RawMessage) (chain.Response, error) {
	f.calls++
	if f.panicAfterSend {
		panic("injected process death after broadcast request")
	}
	return f.response, f.err
}

type countingSigner struct {
	wallet *hdwallet.HDWallet
	calls  int
}

func (s *countingSigner) SignTransaction(chain hdwallet.Chain, index uint32, input proto.Message) (proto.Message, error) {
	s.calls++
	return s.wallet.SignTransaction(chain, index, input)
}

// TST-014: DUP_TRANSACTION_ERROR is evidence of an attempted transaction, never a terminal rejection.
func TestDuplicateBroadcastResolvesConfirmed(t *testing.T) {
	engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	broadcast.response = chain.Response{StatusCode: 200, Body: json.RawMessage(`{"result":false,"code":"DUP_TRANSACTION_ERROR"}`)}
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w := onlyWithdrawal(t, database)
	if w.Status != "broadcast" {
		t.Fatalf("status after duplicate = %s", w.Status)
	}
	reader.transaction = json.RawMessage(`{"txID":"present"}`)
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w = onlyWithdrawal(t, database)
	if w.Status != "confirmed" || broadcast.calls != 1 || signer.calls != 1 {
		t.Fatalf("resolved = status %s, broadcasts %d, signs %d", w.Status, broadcast.calls, signer.calls)
	}
}

// TST-016: recovery keys off the FULL-committed txid and never signs or broadcasts again.
func TestCrashAfterBroadcastRequestResolvesWithoutResigning(t *testing.T) {
	engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	broadcast.panicAfterSend = true
	func() {
		defer func() { _ = recover() }()
		_ = engine.Tick(context.Background())
	}()
	w := onlyWithdrawal(t, database)
	if w.TxID == "" || w.BroadcastAttemptedAt == nil || broadcast.calls != 1 {
		t.Fatalf("durability marker = %#v, calls=%d", w, broadcast.calls)
	}
	reader.transaction = json.RawMessage(`{"txID":"present"}`)
	broadcast.panicAfterSend = false
	if err := engine.recover(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	w = onlyWithdrawal(t, database)
	if w.Status != "confirmed" || broadcast.calls != 1 || signer.calls != 1 {
		t.Fatalf("restart = status %s, broadcasts %d, signs %d", w.Status, broadcast.calls, signer.calls)
	}
}

// TST-021: every broadcast-layer outcome consumes the one lifetime attempt.
func TestExactlyOneBroadcastAcrossOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		response chain.Response
		err      error
	}{
		{"accepted", chain.Response{StatusCode: 200, Body: json.RawMessage(`{"result":true}`)}, nil},
		{"duplicate", chain.Response{StatusCode: 200, Body: json.RawMessage(`{"result":false,"code":"DUP_TRANSACTION_ERROR"}`)}, nil},
		{"server-500", chain.Response{StatusCode: 500, Body: json.RawMessage(`{"error":"busy"}`)}, errors.New("500")},
		{"timeout", chain.Response{}, context.DeadlineExceeded},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
			defer cleanup()
			broadcast.response, broadcast.err = test.response, test.err
			if err := engine.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			reader.transaction = json.RawMessage(`{"txID":"present"}`)
			if err := engine.recover(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if err := engine.recover(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if w := onlyWithdrawal(t, database); w.Status != "confirmed" || broadcast.calls != 1 || signer.calls != 1 {
				t.Fatalf("status=%s broadcasts=%d signs=%d", w.Status, broadcast.calls, signer.calls)
			}
		})
	}
}

func withdrawalFixture(t *testing.T) (*Engine, *store.Store, *countingSigner, *fakeReader, *fakeBroadcast, func()) {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	wallet, err := hdwallet.FromMnemonic(testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeWallet(ctx, wallet, 0, 3, 1000); err != nil {
		t.Fatal(err)
	}
	addresses, err := database.WalletAddresses(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	source, destination, contract := addresses[0], addresses[1], addresses[2]
	addressID := source.ID
	if err := database.CommitBlock(ctx, store.BlockRecord{Height: 1, ID: "block-1", ParentID: "block-0", Timestamp: 1,
		ProcessedAt: 1, Reference: mustReference(t)}, 10, func(write *store.BlockWrite) error {
		_, err := write.UpsertPayment(store.PaymentRecord{TxID: "funding", LogIndex: 0, Direction: "in", BlockHeight: 1,
			BlockID: "block-1", BlockTimestamp: 1, FromAddress: destination.Address, ToAddress: source.Address,
			AddressID: &addressID, Asset: "USDT", AmountRaw: "5000000", Status: "confirmed", DetectedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Assets: []config.Asset{{Symbol: "USDT", Kind: "trc20", Contract: contract.Address, Decimals: 6}},
		Resources: config.Resources{MinEnergy: 1, MinBandwidth: 1}, Withdrawal: config.Withdrawal{Enabled: true, DailyLimitUSD: "100", FeeLimitTRX: 100, Expiration: time.Minute}}
	if _, _, err := database.CreateWithdrawal(ctx, store.CreateWithdrawal{IdempotencyKey: "one", FromAddress: source.Address,
		ToAddress: destination.Address, Asset: "USDT", AmountRaw: "1000000", AmountUSD: "1", DailyLimitUSD: "100",
		RequestedBy: "test", IP: "127.0.0.1", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	reader := &fakeReader{transaction: json.RawMessage(`{}`)}
	broadcast := &fakeBroadcast{}
	signer := &countingSigner{wallet: wallet}
	engine, err := New(reader, fakeSolidity{body: json.RawMessage(`{"id":"solid","fee":10,"blockNumber":2,"blockTimeStamp":2000,"receipt":{"energy_usage_total":3},"result":"SUCCESS"}`)},
		broadcast, database, signer, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return engine, database, signer, reader, broadcast, func() { wallet.Destroy(); _ = database.Close() }
}

func onlyWithdrawal(t *testing.T, database *store.Store) store.Withdrawal {
	t.Helper()
	items, err := database.ListWithdrawals(context.Background(), "", 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("withdrawals=%d err=%v", len(items), err)
	}
	return items[0]
}

func referenceBlock() json.RawMessage {
	return json.RawMessage(`{"blockID":"0000000000000001d698d4192c56cb6be724a558448e2684802de4d6cd8690dc","block_header":{"raw_data":{"timestamp":1700000000000,"txTrieRoot":"0000000000000000000000000000000000000000000000000000000000000000","parentHash":"0000000000000000d698d4192c56cb6be724a558448e2684802de4d6cd8690dc","number":1,"witness_address":"41f24cbabb5cf3e639e1869a5cc6be49614f8e6a14","version":9}}}`)
}

func mustReference(t *testing.T) store.ReferenceBlock {
	t.Helper()
	reference, err := parseReferenceBlock(referenceBlock())
	if err != nil {
		t.Fatal(err)
	}
	return reference
}
