package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestOrderTerminalStatesAndPoolCooldown(t *testing.T) {
	ctx := context.Background()
	database := testOrderStore(t)
	now := time.Unix(100, 0)
	order, created, err := database.CreateOrder(ctx, testDerive, 3, CreateOrderParams{
		Asset: "USDT", ExpectedRaw: "25000000", Consumer: "shop", ExpiresAt: 101,
	}, now)
	if err != nil || !created {
		t.Fatalf("create order: created=%v err=%v", created, err)
	}
	events := EventConfig{
		DefaultConsumer: "shop",
		Consumers:       map[string]EventConsumer{"shop": {Name: "shop", URL: "http://sink", Enabled: true}},
		Decimals:        map[string]int{"USDT": 6},
	}
	expired, err := database.ExpireOrders(ctx, time.Unix(102, 0), 10*time.Second, events)
	if err != nil || expired != 1 {
		t.Fatalf("expire orders = %d, %v", expired, err)
	}
	got, err := database.Order(ctx, order.ID)
	if err != nil || got.Status != "expired" {
		t.Fatalf("expired order status = %s, %v", got.Status, err)
	}
	var state, assignedOrder string
	var assignedAt, releasedAt, coolingUntil sql.NullInt64
	if err := database.normal.QueryRow(`SELECT state, assigned_order_id, assigned_at, released_at, cooling_until
        FROM addresses WHERE id = ?`, order.AddressID).Scan(&state, &assignedOrder, &assignedAt, &releasedAt, &coolingUntil); err != nil {
		t.Fatal(err)
	}
	if state != "cooling" || assignedOrder != order.ID || !assignedAt.Valid || releasedAt.Int64 != 102 || coolingUntil.Int64 != 112 {
		t.Fatalf("cooling address = state:%s order:%s assigned:%v released:%v until:%v", state, assignedOrder, assignedAt, releasedAt, coolingUntil)
	}
	var eventsWritten int
	if err := database.normal.QueryRow("SELECT COUNT(*) FROM ipn_outbox WHERE event_type = 'order.expired'").Scan(&eventsWritten); err != nil || eventsWritten != 1 {
		t.Fatalf("expiry events = %d, %v", eventsWritten, err)
	}
	if released, err := database.ReleaseCooled(ctx, time.Unix(111, 0)); err != nil || released != 0 {
		t.Fatalf("early cooldown release = %d, %v", released, err)
	}
	if released, err := database.ReleaseCooled(ctx, time.Unix(112, 0)); err != nil || released != 1 {
		t.Fatalf("cooldown release = %d, %v", released, err)
	}
	var nullableOrder sql.NullString
	if err := database.normal.QueryRow(`SELECT state, assigned_order_id, assigned_at, released_at
        FROM addresses WHERE id = ?`, order.AddressID).Scan(&state, &nullableOrder, &assignedAt, &releasedAt); err != nil {
		t.Fatal(err)
	}
	if state != "free" || nullableOrder.Valid || assignedAt.Valid || releasedAt.Valid {
		t.Fatalf("released address = state:%s order:%v assigned:%v released:%v", state, nullableOrder, assignedAt, releasedAt)
	}

	funded, _, err := database.CreateOrder(ctx, testDerive, 3, CreateOrderParams{
		Asset: "USDT", ExpectedRaw: "10", Consumer: "shop", ExpiresAt: 114,
	}, time.Unix(113, 0))
	if err != nil {
		t.Fatal(err)
	}
	fundedID, fundedAddressID := funded.ID, funded.AddressID
	payment := PaymentRecord{TxID: "funded-expiry", Direction: "in", BlockHeight: 1, BlockID: "B1", BlockTimestamp: 113,
		FromAddress: "TFunder", ToAddress: funded.Address, AddressID: &fundedAddressID, OrderID: &fundedID,
		Asset: "USDT", AmountRaw: "5", DetectedAt: 113}
	if err := database.CommitBlock(ctx, BlockRecord{Height: 1, ID: "B1", ParentID: "0", Timestamp: 113, ProcessedAt: 113}, 64,
		func(write *BlockWrite) error { _, err := write.UpsertPayment(payment); return err }); err != nil {
		t.Fatal(err)
	}
	if expired, err := database.ExpireOrders(ctx, time.Unix(115, 0), 10*time.Second, events); err != nil || expired != 1 {
		t.Fatalf("funded expiry = %d, %v", expired, err)
	}
	if got, err := database.Order(ctx, funded.ID); err != nil || got.Status != "expired_funded" {
		t.Fatalf("funded expiry status = %s, %v", got.Status, err)
	}
}

func TestFundedCancellationResolutionAndIdempotency(t *testing.T) {
	ctx := context.Background()
	database := testOrderStore(t)
	ref := "invoice-1"
	params := CreateOrderParams{ExternalRef: &ref, Asset: "USDT", ExpectedRaw: "10", Consumer: "shop", ExpiresAt: 999}
	order, created, err := database.CreateOrder(ctx, testDerive, 2, params, time.Unix(1, 0))
	if err != nil || !created {
		t.Fatal(err)
	}
	replay, created, err := database.CreateOrder(ctx, testDerive, 2, params, time.Unix(2, 0))
	if err != nil || created || replay.ID != order.ID {
		t.Fatalf("external_ref replay = %#v created=%v err=%v", replay, created, err)
	}
	conflict := params
	conflict.ExpectedRaw = "11"
	if _, _, err := database.CreateOrder(ctx, testDerive, 2, conflict, time.Unix(2, 0)); !errors.Is(err, ErrExternalRefConflict) {
		t.Fatalf("external_ref mismatch error = %v", err)
	}

	orderID, addressID := order.ID, order.AddressID
	payment := PaymentRecord{TxID: "partial", Direction: "in", BlockHeight: 1, BlockID: "B1", BlockTimestamp: 2,
		FromAddress: "TPayer", ToAddress: order.Address, AddressID: &addressID, OrderID: &orderID,
		Asset: "USDT", AmountRaw: "5", DetectedAt: 2}
	if err := database.CommitBlock(ctx, BlockRecord{Height: 1, ID: "B1", ParentID: "0", Timestamp: 2, ProcessedAt: 2}, 64,
		func(write *BlockWrite) error { _, err := write.UpsertPayment(payment); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CancelOrder(ctx, order.ID, false, time.Hour, time.Unix(3, 0)); !errors.Is(err, ErrOrderRequiresForce) {
		t.Fatalf("partial cancel error = %v", err)
	}
	cancelled, err := database.CancelOrder(ctx, order.ID, true, time.Hour, time.Unix(3, 0))
	if err != nil || cancelled.Status != "cancelled_funded" {
		t.Fatalf("forced cancellation = %#v, %v", cancelled, err)
	}
	funded, err := database.FundedTerminalOrders(ctx)
	if err != nil || len(funded) != 1 || len(funded[0].Payers) != 1 || funded[0].Payers[0] != "TPayer" {
		t.Fatalf("funded terminal orders = %#v, %v", funded, err)
	}
	if err := database.ResolveFundedOrder(ctx, order.ID, "refunded", "tx refund", "operator", time.Unix(4, 0)); err != nil {
		t.Fatal(err)
	}
	if funded, err := database.FundedTerminalOrders(ctx); err != nil || len(funded) != 0 {
		t.Fatalf("resolved funded terminal orders = %#v, %v", funded, err)
	}
	var audits int
	if err := database.normal.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action = 'order.resolve'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("resolution audits = %d, %v", audits, err)
	}
}

func TestPoolTopUpAndTOTPPrune(t *testing.T) {
	ctx := context.Background()
	database := testOrderStore(t)
	if _, _, err := database.CreateOrder(ctx, testDerive, 3, CreateOrderParams{
		Asset: "USDT", ExpectedRaw: "1", Consumer: "shop", ExpiresAt: 999,
	}, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	added, err := database.TopUpPool(ctx, testDerive, 1, 3, 3, time.Unix(2, 0))
	if err != nil || added != 2 {
		t.Fatalf("pool top-up = %d, %v", added, err)
	}
	if _, err := database.normal.Exec("INSERT INTO used_totp(code, step, used_at) VALUES ('old', 1, 1), ('new', 2, 400)"); err != nil {
		t.Fatal(err)
	}
	pruned, err := database.PruneUsedTOTP(ctx, time.Unix(500, 0))
	if err != nil || pruned != 1 {
		t.Fatalf("TOTP prune = %d, %v", pruned, err)
	}
}

func testOrderStore(t *testing.T) *Store {
	t.Helper()
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func testDerive(index uint32) (string, error) { return fmt.Sprintf("TAddress%03d", index), nil }
