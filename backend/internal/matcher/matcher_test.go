package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/config"
	"payd/internal/store"
	"payd/internal/wallet"
)

const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

type fakeWriter struct {
	exists, found, reactivated            bool
	existsErr, attributionErr, upsertErr  error
	metadataErr, orderEventErr, statusErr error
	globalEventErr                        error
	order                                 store.AttributionOrder
	status                                string
}

func (f *fakeWriter) PaymentExists(string, int) (bool, error) { return f.exists, f.existsErr }
func (f *fakeWriter) AttributionOrder(int64) (store.AttributionOrder, bool, error) {
	return f.order, f.found, f.attributionErr
}
func (f *fakeWriter) UpsertPayment(store.PaymentRecord) (bool, error) {
	return f.reactivated, f.upsertErr
}
func (f *fakeWriter) EnqueueGlobalEvent(store.EventConfig, string, string, map[string]any, time.Time) error {
	return f.globalEventErr
}
func (f *fakeWriter) EnqueueOrderEvent(store.EventConfig, string, string, map[string]any, time.Time) error {
	return f.orderEventErr
}
func (f *fakeWriter) OrderMetadata(string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), f.metadataErr
}
func (f *fakeWriter) OrderStatus(string) (string, error) { return f.status, f.statusErr }

func TestMatcherTrustBoundaryErrorsAndNonOrderPaths(t *testing.T) {
	sentinel := errors.New("sentinel")
	events := store.EventConfig{Decimals: map[string]int{"USDT": 6}}
	matcher := New()
	matcher.UpdateEvents(events)
	addressID := int64(1)
	payment := store.PaymentRecord{TxID: "tx", Direction: "in", AddressID: &addressID, Asset: "USDT", AmountRaw: "1", BlockTimestamp: 2, DetectedAt: 3}
	active := store.AttributionOrder{ID: "order", Asset: "USDT", Status: "pending", CreatedAt: 1}

	checks := []struct {
		name    string
		writer  *fakeWriter
		payment store.PaymentRecord
		wantErr bool
	}{
		{"payment lookup", &fakeWriter{existsErr: sentinel}, payment, true},
		{"attribution lookup", &fakeWriter{attributionErr: sentinel}, payment, true},
		{"payment write", &fakeWriter{found: true, order: active, upsertErr: sentinel}, payment, true},
		{"invalid amount", &fakeWriter{}, store.PaymentRecord{TxID: "out", Direction: "out", Asset: "USDT", AmountRaw: "-1"}, true},
		{"metadata", &fakeWriter{found: true, order: active, metadataErr: sentinel}, payment, true},
		{"order event", &fakeWriter{found: true, order: active, orderEventErr: sentinel}, payment, true},
		{"status", &fakeWriter{found: true, order: active, statusErr: sentinel}, payment, true},
		{"unchanged status", &fakeWriter{found: true, order: active, status: "pending"}, payment, false},
		{"non-transition status", &fakeWriter{found: true, order: active, status: "confirmed"}, payment, false},
		{"outbound", &fakeWriter{}, store.PaymentRecord{TxID: "out", Direction: "out", Asset: "USDT", AmountRaw: "1"}, false},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := matcher.Match(check.writer, check.payment)
			if (err != nil) != check.wantErr {
				t.Fatalf("Match error = %v, wantErr %v", err, check.wantErr)
			}
		})
	}
}

func TestWrongAssetPaymentStaysUnattributed(t *testing.T) { // TST-017 / ORD-002a
	database, order := newOrder(t, "USDT", "25000000", time.Unix(1_750_000_000, 0))
	payment := paymentFor(order, "wrong-asset", "TRX", "25000000", order.CreatedAt+1, order.CreatedAt+2, false)
	commitPayment(t, database, 1, "B1", "0", payment)

	got, err := database.Order(context.Background(), order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending" || got.ReceivedRaw != "0" {
		t.Fatalf("wrong-asset order = status %s, received %s", got.Status, got.ReceivedRaw)
	}
	unattributed, err := database.UnattributedPayments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(unattributed) != 1 || unattributed[0].Asset != "TRX" || unattributed[0].TxID != "wrong-asset" {
		t.Fatalf("unattributed payments = %#v", unattributed)
	}
	if events, err := database.OutboxCount(context.Background(), "payment.unattributed"); err != nil || events != 1 {
		t.Fatalf("payment.unattributed events = %d, %v", events, err)
	}
	if balance, err := database.Balance(context.Background(), order.AddressID, "TRX"); err != nil || balance.PendingRaw != "25000000" {
		t.Fatalf("unattributed balance = %#v, %v", balance, err)
	}
}

func TestAttributionUsesBlockTimestampNotDetectedAt(t *testing.T) { // TST-019 / ORD-002b
	database, order := newOrder(t, "USDT", "25000000", time.Unix(1_750_000_000, 0))
	payment := paymentFor(order, "late-detection", "USDT", "25000000",
		order.CreatedAt+1, order.ExpiresAt+60, false)
	commitPayment(t, database, 1, "B1", "0", payment)

	got, err := database.Order(context.Background(), order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "paid" || got.ReceivedRaw != "25000000" {
		t.Fatalf("late-detected order = status %s, received %s", got.Status, got.ReceivedRaw)
	}
}

func TestDustTopUpCompletesPartialAtExpiryRecheck(t *testing.T) { // TST-020 / DET-007 / ORD-005c
	database, order := newOrder(t, "USDT", "25000000", time.Unix(1_750_000_000, 0))
	first := paymentFor(order, "partial", "USDT", "24600000", order.CreatedAt+1, order.CreatedAt+2, false)
	commitPayment(t, database, 1, "B1", "0", first)
	got, err := database.Order(context.Background(), order.ID)
	if err != nil || got.Status != "partial" {
		t.Fatalf("first payment status = %s, err = %v", got.Status, err)
	}

	dust := paymentFor(order, "dust", "USDT", "400000", order.CreatedAt+2, order.CreatedAt+3, true)
	commitPayment(t, database, 2, "B2", "B1", dust)
	got, err = database.Order(context.Background(), order.ID)
	if err != nil || got.Status != "partial" || got.ReceivedRaw != "25000000" {
		t.Fatalf("dust payment order = status %s, received %s, err = %v", got.Status, got.ReceivedRaw, err)
	}

	expired, err := database.ExpireOrders(context.Background(), time.Unix(order.ExpiresAt+1, 0), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err = database.Order(context.Background(), order.ID)
	if err != nil || expired != 0 || got.Status != "paid" {
		t.Fatalf("expiry recheck = expired %d, status %s, err %v", expired, got.Status, err)
	}
}

func TestExpiredOrderDoesNotReopenForLatePayment(t *testing.T) { // ORD-006/020
	database, order := newOrder(t, "USDT", "1", time.Unix(1_750_000_000, 0))
	if _, err := database.ExpireOrders(context.Background(), time.Unix(order.ExpiresAt+1, 0), time.Hour); err != nil {
		t.Fatal(err)
	}
	payment := paymentFor(order, "late-payment", "USDT", "1", order.ExpiresAt+1, order.ExpiresAt+2, false)
	commitPayment(t, database, 1, "B1", "0", payment)
	got, err := database.Order(context.Background(), order.ID)
	if err != nil || got.Status != "expired" || got.ReceivedRaw != "0" {
		t.Fatalf("expired order after late payment = status %s, received %s, err %v", got.Status, got.ReceivedRaw, err)
	}
	if payments, err := database.UnattributedPayments(context.Background()); err != nil || len(payments) != 1 {
		t.Fatalf("late unattributed payments = %#v, %v", payments, err)
	}
}

func newOrder(t *testing.T, asset, expected string, now time.Time) (*store.Store, store.Order) {
	t.Helper()
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
	t.Cleanup(hd.Destroy)
	if err := database.InitializeWallet(ctx, hd, 0, 1, 1000); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Wallet: config.Wallet{PoolInitialSize: 1, PoolMinFree: 1, PoolMaxSize: 1, Cooldown: time.Hour},
		Assets: []config.Asset{{Symbol: "TRX", Kind: "native", Decimals: 6, Verified: true},
			{Symbol: "USDT", Kind: "trc20", Decimals: 6, MinDeposit: "0.5", Verified: true}},
		Orders: config.Orders{DefaultTTL: 30 * time.Minute},
		IPN:    config.IPN{DefaultConsumer: "shop", Consumers: []config.Consumer{{Name: "shop", Enabled: true}}},
		Price:  config.Price{StaleAfter: 5 * time.Minute},
	}
	pool, err := wallet.NewPool(database, hd, cfg)
	if err != nil {
		t.Fatal(err)
	}
	order, created, err := pool.CreateOrder(ctx, wallet.CreateOrderRequest{Asset: asset, ExpectedRaw: expected}, now)
	if err != nil || !created {
		t.Fatalf("create order: created=%v err=%v", created, err)
	}
	return database, order
}

func paymentFor(order store.Order, txID, asset, amount string, blockTimestamp, detectedAt int64, dust bool) store.PaymentRecord {
	addressID := order.AddressID
	return store.PaymentRecord{
		TxID: txID, Direction: "in", BlockHeight: 1, BlockID: "B1", BlockTimestamp: blockTimestamp,
		FromAddress: "TPayer", ToAddress: order.Address, AddressID: &addressID, Asset: asset,
		AmountRaw: amount, IsDust: dust, DetectedAt: detectedAt,
	}
}

func commitPayment(t *testing.T, database *store.Store, height int64, id, parent string, payment store.PaymentRecord) {
	t.Helper()
	payment.BlockHeight, payment.BlockID = height, id
	match := New(store.EventConfig{
		DefaultConsumer: "shop",
		Consumers: map[string]store.EventConsumer{
			"shop": {Name: "shop", URL: "http://sink", Enabled: true, ReceivesGlobal: true},
		},
		Decimals: map[string]int{"TRX": 6, "USDT": 6},
	})
	err := database.CommitBlock(context.Background(), store.BlockRecord{
		Height: height, ID: id, ParentID: parent, Timestamp: payment.BlockTimestamp, ProcessedAt: payment.DetectedAt,
	}, 64, func(write *store.BlockWrite) error { return match.Match(write, payment) })
	if err != nil {
		t.Fatal(err)
	}
}
