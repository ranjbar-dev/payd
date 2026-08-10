package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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
	if events, err := database.OutboxCount(ctx, "order.reverted"); err != nil || events != 1 {
		t.Fatalf("order.reverted events=%d err=%v", events, err)
	}

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

func TestRecalculateBalanceOwnedTransfersUsesIndexesWithoutOverlap(t *testing.T) {
	ctx := context.Background()
	database := testOrderStore(t)
	if _, err := database.normal.Exec(`INSERT INTO addresses(id,hd_index,address,state,created_at) VALUES
		(1,1,'TSource','assigned',1),(2,2,'TRecipient','assigned',1)`); err != nil {
		t.Fatal(err)
	}
	sourceID := int64(1)
	if err := database.CommitBlock(ctx, BlockRecord{Height: 1, ID: "B1", ParentID: "B0", Timestamp: 1}, 64, func(write *BlockWrite) error {
		for _, payment := range []PaymentRecord{
			{TxID: "fund", Direction: "in", BlockHeight: 1, BlockID: "B1", BlockTimestamp: 1, FromAddress: "TPayer", ToAddress: "TSource", AddressID: &sourceID, Asset: "TRX", AmountRaw: "100", Status: "confirmed", DetectedAt: 1},
			{TxID: "owned", Direction: "out", BlockHeight: 1, BlockID: "B1", BlockTimestamp: 1, FromAddress: "TSource", ToAddress: "TRecipient", AddressID: &sourceID, Asset: "TRX", AmountRaw: "30", Status: "confirmed", DetectedAt: 1},
			{TxID: "self", Direction: "out", BlockHeight: 1, BlockID: "B1", BlockTimestamp: 1, FromAddress: "TSource", ToAddress: "TSource", AddressID: &sourceID, Asset: "TRX", AmountRaw: "5", Status: "confirmed", DetectedAt: 1},
			{TxID: "pending", Direction: "out", BlockHeight: 1, BlockID: "B1", BlockTimestamp: 1, FromAddress: "TSource", ToAddress: "TRecipient", AddressID: &sourceID, Asset: "TRX", AmountRaw: "7", Status: "seen", DetectedAt: 1},
		} {
			if _, err := write.UpsertPayment(payment); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		id                 int64
		confirmed, pending string
	}{{1, "65", "-7"}, {2, "30", "7"}} {
		balance, err := database.Balance(ctx, want.id, "TRX")
		if err != nil || balance.ConfirmedRaw != want.confirmed || balance.PendingRaw != want.pending {
			t.Fatalf("balance %d = (%s,%s), want (%s,%s), err=%v", want.id, balance.ConfirmedRaw, balance.PendingRaw, want.confirmed, want.pending, err)
		}
	}

	rows, err := database.normal.QueryContext(ctx, "EXPLAIN QUERY PLAN "+balancePaymentsQuery, sourceID, "TRX", sourceID, "TRX", sourceID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{"idx_payments_addr_asset", "idx_payments_to_address"} {
		if !strings.Contains(plan.String(), index) {
			t.Fatalf("balance query plan does not use %s: %s", index, plan.String())
		}
	}
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
