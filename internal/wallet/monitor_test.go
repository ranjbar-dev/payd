package wallet

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/config"
	"payd/internal/store"
)

const monitorTestMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

type fakeWalletReader struct {
	resourceCalls []string
	chainRaw      string
	chainCalls    int
}

func (f *fakeWalletReader) GetAccountResource(_ context.Context, address string) (json.RawMessage, error) {
	f.resourceCalls = append(f.resourceCalls, address)
	return json.RawMessage(`{"EnergyLimit":150,"EnergyUsed":25,"freeNetLimit":600,"freeNetUsed":100}`), nil
}

func (f *fakeWalletReader) GetAccount(context.Context, string) (json.RawMessage, error) {
	return nil, errors.New("unexpected native balance read")
}

func (f *fakeWalletReader) GetTRC20Balance(context.Context, string, string) (json.RawMessage, error) {
	f.chainCalls++
	return json.RawMessage(`{"constant_result":["` + f.chainRaw + `"]}`), nil
}

func TestMonitorTiersAndBalanceDrift(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	hd, err := hdwallet.FromMnemonic(monitorTestMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hd.Destroy)
	if err := database.InitializeWallet(ctx, hd, 0, 3, 1000); err != nil {
		t.Fatal(err)
	}
	addresses, err := database.WalletAddresses(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	amounts := []string{"20", "5"}
	if err := database.CommitBlock(ctx, store.BlockRecord{Height: 1, ID: "B1", ParentID: "B0", Timestamp: 1}, 64, func(write *store.BlockWrite) error {
		for index, amount := range amounts {
			addressID := addresses[index].ID
			_, err := write.UpsertPayment(store.PaymentRecord{
				TxID: "tx-" + amount, Direction: "in", BlockHeight: 1, BlockID: "B1", BlockTimestamp: 1,
				FromAddress: "payer", ToAddress: addresses[index].Address, AddressID: &addressID,
				Asset: "USDT", AmountRaw: amount, Status: "confirmed", DetectedAt: 1,
			})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Assets: []config.Asset{{Symbol: "USDT", Kind: "trc20", Contract: "token", Decimals: 0}},
		Price:  config.Price{Pairs: []string{"TRXUSDT"}, StaleAfter: time.Minute},
		Resources: config.Resources{MinEnergy: 100, MinBandwidth: 345, CheckInterval: time.Minute,
			SlowCheckInterval: 6 * time.Hour, PollThresholdUSD: "10", MaxPolledAddresses: 1},
		IPN: config.IPN{Consumers: []config.Consumer{{Name: "ops", URL: "https://example.test", Enabled: true, ReceivesGlobal: true}}},
	}
	reader := &fakeWalletReader{chainRaw: "15"}
	monitor, err := NewMonitor(reader, database, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := monitor.Poll(ctx, true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reader.resourceCalls, []string{addresses[0].Address}) {
		t.Fatalf("fast calls = %v", reader.resourceCalls)
	}
	reader.resourceCalls = nil
	if err := monitor.Poll(ctx, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reader.resourceCalls, []string{addresses[1].Address}) {
		t.Fatalf("slow calls = %v; zero balance address must not be polled", reader.resourceCalls)
	}

	reconciler, err := NewReconciler(reader, database, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Reconcile(ctx); err != nil || reader.chainCalls != 2 {
		t.Fatalf("RL-003 repeated reconciliation calls = %d, %v", reader.chainCalls, err)
	}
	if _, err := database.BalanceForWithdrawal(ctx, addresses[0].Address, "USDT"); !errors.Is(err, store.ErrBalanceDrift) {
		t.Fatalf("drifting balance validation = %v", err)
	}
	if count, err := database.OutboxCount(ctx, "balance.drift_detected"); err != nil || count != 2 {
		t.Fatalf("drift events = %d, %v", count, err)
	}
	if err := database.ClearBalanceDrift(ctx, addresses[0].Address, "operator", "127.0.0.1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if balance, err := database.BalanceForWithdrawal(ctx, addresses[0].Address, "USDT"); err != nil || balance.ConfirmedRaw != "20" {
		t.Fatalf("cleared withdrawal balance = %+v, %v", balance, err)
	}
}
