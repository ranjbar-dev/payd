package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestReorgOrphansRecalculatesAndReincludesPayment(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.normal.Exec(`INSERT INTO addresses(id, hd_index, address, state, created_at)
        VALUES (1, 1, 'TTestAddress', 'assigned', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.normal.Exec(`INSERT INTO orders(
        id, address_id, address, asset, expected_raw, status, expires_at, created_at, updated_at)
        VALUES ('order-1', 1, 'TTestAddress', 'USDT', '25', 'pending', 9999999999, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.normal.Exec("UPDATE addresses SET assigned_order_id = 'order-1', assigned_at = 1 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBlock(ctx, BlockRecord{Height: 1, ID: "A", ParentID: "0", Timestamp: 1, ProcessedAt: 1}, 64, nil); err != nil {
		t.Fatal(err)
	}
	orderID := "order-1"
	addressID := int64(1)
	payment := PaymentRecord{
		TxID: "tx-1", Direction: "in", BlockHeight: 2, BlockID: "B", BlockTimestamp: 2,
		FromAddress: "TPayer", ToAddress: "TTestAddress", AddressID: &addressID, OrderID: &orderID,
		Asset: "USDT", AmountRaw: "25", Status: "seen", DetectedAt: 2,
	}
	if err := database.CommitBlock(ctx, BlockRecord{Height: 2, ID: "B", ParentID: "A", Timestamp: 2, ProcessedAt: 2}, 64, func(write *BlockWrite) error {
		_, err := write.UpsertPayment(payment)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertOrderAndPayment(t, database, "paid", "25", "seen", 2)

	result, err := database.RewindChain(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.OrphanedPayments != 1 || len(result.RevertedOrders) != 1 || result.RevertedOrders[0] != orderID {
		t.Fatalf("rewind result = %+v", result)
	}
	assertOrderAndPayment(t, database, "pending", "0", "orphaned", 1) // TST-003

	payment.BlockID = "B2"
	applyErr := errors.New("stub matcher failed")
	err = database.CommitBlock(ctx, BlockRecord{Height: 2, ID: "B2", ParentID: "A", Timestamp: 3, ProcessedAt: 3}, 64, func(write *BlockWrite) error {
		if _, err := write.UpsertPayment(payment); err != nil {
			return err
		}
		return applyErr
	})
	if !errors.Is(err, applyErr) {
		t.Fatalf("failed matcher error = %v", err)
	}
	assertOrderAndPayment(t, database, "pending", "0", "orphaned", 1)

	if err := database.CommitBlock(ctx, BlockRecord{Height: 2, ID: "B2", ParentID: "A", Timestamp: 3, ProcessedAt: 3}, 64, func(write *BlockWrite) error {
		reactivated, err := write.UpsertPayment(payment)
		if err == nil && !reactivated {
			t.Error("orphaned payment was not reported reactivated")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertOrderAndPayment(t, database, "paid", "25", "seen", 2) // TST-003a / CHN-016
}

func assertOrderAndPayment(t *testing.T, database *Store, orderStatus, received, paymentStatus string, cursor int64) {
	t.Helper()
	var gotOrderStatus, gotReceived, gotPaymentStatus string
	if err := database.normal.QueryRow("SELECT status, received_raw FROM orders WHERE id = 'order-1'").Scan(&gotOrderStatus, &gotReceived); err != nil {
		t.Fatal(err)
	}
	if err := database.normal.QueryRow("SELECT status FROM payments WHERE txid = 'tx-1'").Scan(&gotPaymentStatus); err != nil {
		t.Fatal(err)
	}
	state, exists, err := database.Cursor(context.Background())
	if err != nil || !exists {
		t.Fatalf("cursor exists=%v, err=%v", exists, err)
	}
	if gotOrderStatus != orderStatus || gotReceived != received || gotPaymentStatus != paymentStatus || state.LastHeight != cursor {
		t.Fatalf("order=(%s,%s), payment=%s, cursor=%d; want (%s,%s), %s, %d",
			gotOrderStatus, gotReceived, gotPaymentStatus, state.LastHeight, orderStatus, received, paymentStatus, cursor)
	}
}
