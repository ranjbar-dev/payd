package wallet

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/config"
	"payd/internal/store"
)

const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

// TST-005 / POOL-006: concurrent creations either claim distinct addresses or fail; they can never share one.
func TestConcurrentOrderCreationNeverDoubleAssignsAddress(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	hd, err := hdwallet.FromMnemonic(testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	defer hd.Destroy()
	if err := database.InitializeWallet(ctx, hd, 0, 2, 1000); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(2)
	pool, err := NewPool(database, hd, cfg)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan store.Order, 2)
	errorsOut := make(chan error, 2)
	var wait sync.WaitGroup
	for index := range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			ref := "concurrent-" + string(rune('a'+index))
			order, created, err := pool.CreateOrder(ctx, CreateOrderRequest{
				ExternalRef: &ref, Asset: "USDT", ExpectedRaw: "25000000",
			}, time.Unix(1_750_000_000, 0))
			if err != nil {
				errorsOut <- err
				return
			}
			if !created {
				errorsOut <- errors.New("new order reported as replay")
				return
			}
			results <- order
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsOut)
	for err := range errorsOut {
		t.Fatal(err)
	}
	var orders []store.Order
	for order := range results {
		orders = append(orders, order)
	}
	if len(orders) != 2 || orders[0].AddressID == orders[1].AddressID || orders[0].Address == orders[1].Address {
		t.Fatalf("concurrent orders = %#v", orders)
	}
	replay, created, err := pool.CreateOrder(ctx, CreateOrderRequest{
		ExternalRef: orders[0].ExternalRef, Asset: "USDT", ExpectedRaw: "25000000",
	}, time.Now())
	if err != nil || created || replay.ID != orders[0].ID {
		t.Fatalf("full-pool idempotent replay = %#v created=%v err=%v", replay, created, err)
	}
	if _, _, err := pool.CreateOrder(ctx, CreateOrderRequest{Asset: "USDT", ExpectedRaw: "1"}, time.Now()); !errors.Is(err, store.ErrPoolExhausted) {
		t.Fatalf("third order error = %v, want pool exhausted", err)
	}
}

func testConfig(poolSize int) config.Config {
	return config.Config{
		Wallet: config.Wallet{Account: 0, PoolInitialSize: poolSize, PoolMinFree: 1, PoolMaxSize: poolSize, Cooldown: time.Hour},
		Assets: []config.Asset{{Symbol: "TRX", Kind: "native", Decimals: 6, Verified: true},
			{Symbol: "USDT", Kind: "trc20", Decimals: 6, MinDeposit: "0.5", Verified: true}},
		Orders: config.Orders{DefaultTTL: 30 * time.Minute},
		IPN:    config.IPN{DefaultConsumer: "shop", Consumers: []config.Consumer{{Name: "shop", Enabled: true}}},
		Price:  config.Price{StaleAfter: 5 * time.Minute},
	}
}
