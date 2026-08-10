package confirm

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"payd/internal/store"
)

type fakeSolidity struct {
	height int64
	calls  int
}

func (f *fakeSolidity) GetNowBlock(context.Context) (json.RawMessage, error) {
	f.calls++
	return json.Marshal(map[string]any{
		"block_header": map[string]any{"raw_data": map[string]any{"number": f.height}},
	})
}

type paymentChain struct {
	database  *store.Store
	order     store.Order
	reader    *fakeSolidity
	addressID int64
}

func newPaymentChain(t *testing.T, paymentBlockID string) paymentChain {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	order, _, err := database.CreateOrder(ctx, func(index uint32) (string, error) {
		return fmt.Sprintf("TAddress%d", index), nil
	}, 1, store.CreateOrderParams{Asset: "USDT", ExpectedRaw: "25", ExpiresAt: 999}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	orderID, addressID := order.ID, order.AddressID
	payment := store.PaymentRecord{
		TxID: "payment-tx", Direction: "in", BlockHeight: 1, BlockID: paymentBlockID, BlockTimestamp: 2,
		FromAddress: "TPayer", ToAddress: order.Address, AddressID: &addressID, OrderID: &orderID,
		Asset: "USDT", AmountRaw: "25", Status: "seen", DetectedAt: 2,
	}
	if err := database.CommitBlock(ctx, store.BlockRecord{
		Height: 1, ID: "B1", ParentID: "genesis", Timestamp: 2, ProcessedAt: 2,
	}, 64, func(write *store.BlockWrite) error {
		_, err := write.UpsertPayment(payment)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for height := int64(2); height <= 21; height++ {
		if err := database.CommitBlock(ctx, store.BlockRecord{
			Height: height, ID: fmt.Sprintf("B%d", height), ParentID: fmt.Sprintf("B%d", height-1),
			Timestamp: height + 1, ProcessedAt: height + 1,
		}, 64, nil); err != nil {
			t.Fatal(err)
		}
	}
	return paymentChain{database: database, order: order, reader: &fakeSolidity{height: 21}, addressID: addressID}
}

func TestOrphanedBlockAtSolidifiedHeightDoesNotConfirm(t *testing.T) {
	ctx := context.Background()
	chain := newPaymentChain(t, "orphaned-B1")
	worker, err := New(chain.reader, chain.database, 19, time.Hour, nil, store.EventConfig{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if chain.reader.calls != 1 || result.PaymentsConfirmed != 0 || result.PaymentsOrphaned != 1 {
		t.Fatalf("calls=%d result=%+v", chain.reader.calls, result)
	}
	gotOrder, err := chain.database.Order(ctx, chain.order.ID)
	if err != nil || gotOrder.Status != "pending" {
		t.Fatalf("order status=%q err=%v, want pending after orphaning", gotOrder.Status, err)
	}
	balance, err := chain.database.Balance(ctx, chain.addressID, "USDT")
	if err != nil || balance.PendingRaw != "0" || balance.ConfirmedRaw != "0" {
		t.Fatalf("balance=%+v err=%v, orphaned payment was credited", balance, err)
	}

	chain.reader.height = 10
	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	cursor, _, err := chain.database.Cursor(ctx)
	if err != nil || cursor.SolidifiedHeight != 21 {
		t.Fatalf("solidified height=%d err=%v, want monotonic 21", cursor.SolidifiedHeight, err)
	}
	if Confirmations(10, 12) != 0 || Confirmations(21, 1) != 20 {
		t.Fatal("CNF-007 confirmation depth is not clamped last_height - block_height")
	}
}

func TestPromotionWaitsForReorgResolutionThenConfirmsOrder(t *testing.T) {
	ctx := context.Background()
	chain := newPaymentChain(t, "B1")
	if err := chain.database.SetReorgSuspicion(ctx, 1); err != nil {
		t.Fatal(err)
	}
	worker, err := New(chain.reader, chain.database, 19, time.Hour, nil, store.EventConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := worker.Tick(ctx); err != nil || result.PaymentsConfirmed != 0 {
		t.Fatalf("promotion during suspicion result=%+v err=%v", result, err)
	}
	if order, err := chain.database.Order(ctx, chain.order.ID); err != nil || order.Status != "paid" {
		t.Fatalf("order during suspicion status=%q err=%v", order.Status, err)
	}

	if err := chain.database.ClearReorgSuspicion(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := worker.Tick(ctx)
	if err != nil || result.PaymentsConfirmed != 1 || result.OrdersConfirmed != 1 {
		t.Fatalf("resolved promotion result=%+v err=%v", result, err)
	}
	order, err := chain.database.Order(ctx, chain.order.ID)
	if err != nil || order.Status != "confirmed" {
		t.Fatalf("confirmed order status=%q err=%v", order.Status, err)
	}
	balance, err := chain.database.Balance(ctx, chain.addressID, "USDT")
	if err != nil || balance.PendingRaw != "0" || balance.ConfirmedRaw != "25" {
		t.Fatalf("confirmed balance=%+v err=%v", balance, err)
	}
	if events, err := chain.database.OutboxCount(ctx, "order.confirmed"); err != nil || events != 1 {
		t.Fatalf("order.confirmed events=%d err=%v", events, err)
	}
}

func TestPaymentPromotesAfterBlockRetentionPrunesItsBlock(t *testing.T) {
	ctx := context.Background()
	chain := newPaymentChain(t, "B1")
	for height := int64(22); height <= 100; height++ {
		if err := chain.database.CommitBlock(ctx, store.BlockRecord{
			Height: height, ID: fmt.Sprintf("B%d", height), ParentID: fmt.Sprintf("B%d", height-1),
			Timestamp: height + 1, ProcessedAt: height + 1,
		}, 64, nil); err != nil {
			t.Fatal(err)
		}
	}

	chain.reader.height = 100
	worker, err := New(chain.reader, chain.database, 19, time.Hour, nil, store.EventConfig{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.Tick(ctx) // CNF-002: solidity still promotes below the retained block floor.
	if err != nil || result.PaymentsConfirmed != 1 || result.OrdersConfirmed != 1 {
		t.Fatalf("delayed promotion result=%+v err=%v", result, err)
	}
	order, err := chain.database.Order(ctx, chain.order.ID)
	if err != nil || order.Status != "confirmed" {
		t.Fatalf("confirmed order status=%q err=%v", order.Status, err)
	}
}
