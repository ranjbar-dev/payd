package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBAL001AttributionRecomputesAndDriftClearIsAudited(t *testing.T) {
	ctx := context.Background()
	database := testOrderStore(t)
	order, _, err := database.CreateOrder(ctx, testDerive, 1, CreateOrderParams{
		Asset: "USDT", ExpectedRaw: "10", Consumer: "shop", ExpiresAt: 999,
	}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	addressID := order.AddressID
	if err := database.CommitBlock(ctx, BlockRecord{Height: 1, ID: "B1", ParentID: "B0", Timestamp: 1}, 64,
		func(write *BlockWrite) error {
			_, err := write.UpsertPayment(PaymentRecord{TxID: "manual", Direction: "in", BlockHeight: 1,
				BlockID: "B1", BlockTimestamp: 1, FromAddress: "payer", ToAddress: order.Address,
				AddressID: &addressID, Asset: "USDT", AmountRaw: "5", Status: "unattributed", DetectedAt: 1})
			return err
		}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.normal.Exec("UPDATE balances SET pending_raw = '999' WHERE address_id = ? AND asset = 'USDT'", addressID); err != nil {
		t.Fatal(err)
	}
	if err := database.AttributePayment(ctx, 1, order.ID, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	var pending string
	if err := database.normal.QueryRow("SELECT pending_raw FROM balances WHERE address_id = ? AND asset = 'USDT'", addressID).Scan(&pending); err != nil || pending != "5" {
		t.Fatalf("attribution balance = %q, %v", pending, err)
	}
	if _, err := database.normal.Exec("UPDATE balances SET drift_detected = 1 WHERE address_id = ? AND asset = 'USDT'", addressID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.BalanceForWithdrawal(ctx, order.Address, "USDT"); !errors.Is(err, ErrBalanceDrift) {
		t.Fatalf("drifting withdrawal balance = %v", err)
	}
	if err := database.ClearBalanceDrift(ctx, order.Address, "operator", "127.0.0.1", time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}
	var audits int
	if err := database.normal.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action = 'balance.clear_drift'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("clear-drift audits = %d, %v", audits, err)
	}
}
