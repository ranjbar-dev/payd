package lifecycle

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/config"
	"payd/internal/store"
	"payd/internal/wallet"
)

func TestQuietChainTickExpiresOrder(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	hd, err := hdwallet.FromMnemonic("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hd.Destroy)
	if err := database.InitializeWallet(ctx, hd, 0, 1, 1000); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Wallet: config.Wallet{PoolInitialSize: 1, PoolMinFree: 1, PoolMaxSize: 2, Cooldown: time.Hour},
		Assets: []config.Asset{{Symbol: "USDT", Kind: "trc20", Decimals: 6, Verified: true}},
		Orders: config.Orders{DefaultTTL: time.Minute},
		IPN:    config.IPN{DefaultConsumer: "shop", Consumers: []config.Consumer{{Name: "shop", URL: "http://sink", Enabled: true}}},
		Price:  config.Price{StaleAfter: time.Minute},
	}
	pool, err := wallet.NewPool(database, hd, cfg)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Unix(1_750_000_000, 0)
	order, _, err := pool.CreateOrder(ctx, wallet.CreateOrderRequest{Asset: "USDT", ExpectedRaw: "1"}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(database, pool, cfg.Wallet.Cooldown, nil, store.NewEventConfig(cfg.IPN, cfg.Assets))
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return createdAt.Add(2 * time.Minute) }
	if err := worker.Tick10(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := database.Order(ctx, order.ID)
	if err != nil || got.Status != "expired" {
		t.Fatalf("quiet-chain expiry status = %s, err = %v", got.Status, err)
	}
	if err := worker.Tick60(ctx); err != nil {
		t.Fatal(err)
	}
}
