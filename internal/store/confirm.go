package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type ConfirmationResult struct {
	PaymentsConfirmed int
	PaymentsOrphaned  int
	OrdersConfirmed   int
}

type confirmationPayment struct {
	id        int64
	orderID   sql.NullString
	addressID sql.NullInt64
	asset     string
	status    string
}

// ApplySolidifiedHeight atomically stores the solidity head and applies CNF-002/003 promotions.
func (s *Store) ApplySolidifiedHeight(ctx context.Context, height int64, confirmationsRequired int, cooldown time.Duration, events EventConfig, now time.Time) (ConfirmationResult, error) {
	if height < 0 || confirmationsRequired <= 0 {
		return ConfirmationResult{}, errors.New("solidified height must be non-negative and confirmations_required positive")
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return ConfirmationResult{}, fmt.Errorf("begin confirmation update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// CNF-002a: lagging solidity backends cannot move this cursor backwards.
	updated, err := tx.ExecContext(ctx, `UPDATE crawler_state
        SET solidified_height = MAX(solidified_height, ?) WHERE id = 1`, height)
	if err != nil {
		return ConfirmationResult{}, fmt.Errorf("store solidified height: %w", err)
	}
	rowsAffected, err := updated.RowsAffected()
	if err != nil {
		return ConfirmationResult{}, err
	}
	if rowsAffected == 0 {
		return ConfirmationResult{}, tx.Commit()
	}

	var lastHeight, solidifiedHeight int64
	var suspectedFrom sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT last_height, solidified_height, reorg_suspected_from
        FROM crawler_state WHERE id = 1`).Scan(&lastHeight, &solidifiedHeight, &suspectedFrom); err != nil {
		return ConfirmationResult{}, fmt.Errorf("load confirmation cursor: %w", err)
	}

	orphaned, err := mismatchedPayments(tx, solidifiedHeight)
	if err != nil {
		return ConfirmationResult{}, err
	}
	promoted, err := promotablePayments(tx, lastHeight, solidifiedHeight, confirmationsRequired, suspectedFrom)
	if err != nil {
		return ConfirmationResult{}, err
	}

	stamp := now.UTC().Unix()
	balances := make(map[balanceKey]struct{}, len(orphaned)+len(promoted))
	orphanedOrders := make(map[string]struct{})
	for _, payment := range orphaned {
		if _, err := tx.ExecContext(ctx, "UPDATE payments SET status = 'orphaned', confirmed_at = NULL WHERE id = ?", payment.id); err != nil {
			return ConfirmationResult{}, fmt.Errorf("orphan mismatched payment: %w", err)
		}
		collectConfirmationEffects(payment, balances, orphanedOrders)
	}
	for _, payment := range promoted {
		status := payment.status
		if status == "seen" {
			status = "confirmed"
		}
		if _, err := tx.ExecContext(ctx, "UPDATE payments SET status = ?, confirmed_at = ? WHERE id = ?", status, stamp, payment.id); err != nil {
			return ConfirmationResult{}, fmt.Errorf("confirm payment: %w", err)
		}
		collectConfirmationEffects(payment, balances, nil)
	}
	for key := range balances {
		if err := recalculateBalance(tx, key.addressID, key.asset); err != nil {
			return ConfirmationResult{}, err
		}
	}
	for orderID := range orphanedOrders {
		reverted, err := recalculateOrder(tx, orderID)
		if err != nil {
			return ConfirmationResult{}, err
		}
		if reverted {
			var status string
			if err := tx.QueryRow("SELECT status FROM orders WHERE id = ?", orderID).Scan(&status); err != nil {
				return ConfirmationResult{}, err
			}
			if err := enqueueOrderEvent(tx, events, orderID, "order.reverted", map[string]any{
				"order_id": orderID, "status": status,
			}, now); err != nil {
				return ConfirmationResult{}, fmt.Errorf("enqueue order reversion: %w", err)
			}
		}
	}

	confirmedOrders, err := promotePaidOrders(tx, lastHeight, cooldown, events, now)
	if err != nil {
		return ConfirmationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ConfirmationResult{}, fmt.Errorf("commit confirmation update: %w", err)
	}
	s.notifyOutbox()
	return ConfirmationResult{
		PaymentsConfirmed: len(promoted), PaymentsOrphaned: len(orphaned), OrdersConfirmed: confirmedOrders,
	}, nil
}

func mismatchedPayments(tx *sql.Tx, solidifiedHeight int64) ([]confirmationPayment, error) {
	rows, err := tx.Query(`SELECT p.id, p.order_id, p.address_id, p.asset, p.status
        FROM payments p JOIN blocks b ON b.height = p.block_height
        WHERE (p.status = 'seen' OR (p.status = 'unattributed' AND p.confirmed_at IS NULL))
          AND p.block_height <= ? AND p.block_id <> b.block_id`, solidifiedHeight)
	if err != nil {
		return nil, fmt.Errorf("find mismatched payments: %w", err)
	}
	return scanConfirmationPayments(rows)
}

func promotablePayments(tx *sql.Tx, lastHeight, solidifiedHeight int64, confirmationsRequired int, suspectedFrom sql.NullInt64) ([]confirmationPayment, error) {
	var suspicion any
	if suspectedFrom.Valid {
		suspicion = suspectedFrom.Int64
	}
	rows, err := tx.Query(`WITH RECURSIVE canonical(height, block_id, parent_id) AS (
          SELECT height, block_id, parent_id FROM blocks WHERE height = ?
          UNION ALL
          SELECT b.height, b.block_id, b.parent_id FROM blocks b
          JOIN canonical child ON b.height = child.height - 1 AND child.parent_id = b.block_id
        )
        SELECT p.id, p.order_id, p.address_id, p.asset, p.status
        FROM payments p JOIN canonical b ON b.height = p.block_height
        WHERE (p.status = 'seen' OR (p.status = 'unattributed' AND p.confirmed_at IS NULL))
          AND p.block_height <= ?                         -- CNF-002(a): solidified height
          AND p.block_id = b.block_id                    -- CNF-002(b): block identity
          AND ? - p.block_height >= ?                    -- CNF-002(d): configured depth
          AND (? IS NULL OR p.block_height < ?)`, // CNF-002b: unresolved reorg range
		lastHeight, solidifiedHeight, lastHeight, confirmationsRequired, suspicion, suspicion)
	if err != nil {
		return nil, fmt.Errorf("find promotable payments: %w", err)
	}
	// Membership in canonical proves CNF-002(c): exact parent links connect the payment block to the stored tip.
	return scanConfirmationPayments(rows)
}

func scanConfirmationPayments(rows *sql.Rows) ([]confirmationPayment, error) {
	defer func() { _ = rows.Close() }()
	var payments []confirmationPayment
	for rows.Next() {
		var payment confirmationPayment
		if err := rows.Scan(&payment.id, &payment.orderID, &payment.addressID, &payment.asset, &payment.status); err != nil {
			return nil, err
		}
		payments = append(payments, payment)
	}
	return payments, rows.Err()
}

type balanceKey struct {
	addressID int64
	asset     string
}

func collectConfirmationEffects(payment confirmationPayment, balances map[balanceKey]struct{}, orders map[string]struct{}) {
	if payment.addressID.Valid {
		balances[balanceKey{addressID: payment.addressID.Int64, asset: payment.asset}] = struct{}{}
	}
	if orders != nil && payment.orderID.Valid {
		orders[payment.orderID.String] = struct{}{}
	}
}

func promotePaidOrders(tx *sql.Tx, lastHeight int64, cooldown time.Duration, events EventConfig, now time.Time) (int, error) {
	rows, err := tx.Query(`SELECT o.id, o.address_id, o.asset, COALESCE(o.metadata, '{}')
        FROM orders o JOIN addresses a ON a.id = o.address_id
        WHERE o.status = 'paid'
          AND EXISTS (SELECT 1 FROM payments p WHERE p.order_id = o.id AND p.status <> 'orphaned'
            AND p.direction = 'in' AND p.asset = o.asset AND p.block_timestamp >= o.created_at
            AND (a.released_at IS NULL OR p.block_timestamp < a.released_at))
          AND NOT EXISTS (SELECT 1 FROM payments p WHERE p.order_id = o.id AND p.status <> 'orphaned'
            AND p.status <> 'confirmed' AND p.direction = 'in' AND p.asset = o.asset
            AND p.block_timestamp >= o.created_at
            AND (a.released_at IS NULL OR p.block_timestamp < a.released_at))`)
	if err != nil {
		return 0, fmt.Errorf("find confirmable orders: %w", err)
	}
	type paidOrder struct {
		id, asset, metadata string
		addressID           int64
	}
	var orders []paidOrder
	for rows.Next() {
		var order paidOrder
		if err := rows.Scan(&order.id, &order.addressID, &order.asset, &order.metadata); err != nil {
			_ = rows.Close()
			return 0, err
		}
		orders = append(orders, order)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	stamp := now.UTC().Unix()
	for _, order := range orders {
		if _, err := tx.Exec("UPDATE orders SET status = 'confirmed', updated_at = ? WHERE id = ?", stamp, order.id); err != nil {
			return 0, fmt.Errorf("confirm order %s: %w", order.id, err)
		}
		if _, err := tx.Exec(`UPDATE addresses SET state = 'cooling', released_at = ?, cooling_until = ?
            WHERE id = ? AND state = 'assigned'`, stamp, now.Add(cooldown).UTC().Unix(), order.addressID); err != nil {
			return 0, fmt.Errorf("cool confirmed order address: %w", err)
		}
		payments, minimum, err := confirmedPaymentPayloads(tx, order.id, order.asset, events.Decimals[order.asset], lastHeight)
		if err != nil {
			return 0, err
		}
		if err := enqueueOrderEvent(tx, events, order.id, "order.confirmed", map[string]any{
			"order_id": order.id, "status": "confirmed", "confirmations": minimum,
			"payments": payments, "metadata": json.RawMessage(order.metadata),
		}, now); err != nil {
			return 0, fmt.Errorf("enqueue order confirmation: %w", err)
		}
	}
	return len(orders), nil
}

func confirmedPaymentPayloads(tx *sql.Tx, orderID, asset string, decimals int, lastHeight int64) ([]map[string]any, int64, error) {
	rows, err := tx.Query(`SELECT txid, log_index, from_address, amount_raw, block_height, status
        FROM payments WHERE order_id = ? AND asset = ? AND direction = 'in' AND status = 'confirmed'
        ORDER BY id`, orderID, asset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var payloads []map[string]any
	minimum := int64(-1)
	for rows.Next() {
		var txID, from, amountRaw, status string
		var logIndex int
		var blockHeight int64
		if err := rows.Scan(&txID, &logIndex, &from, &amountRaw, &blockHeight, &status); err != nil {
			return nil, 0, err
		}
		amount, err := FormatUnits(amountRaw, decimals)
		if err != nil {
			return nil, 0, err
		}
		depth := ConfirmationDepth(lastHeight, blockHeight)
		if minimum < 0 || depth < minimum {
			minimum = depth
		}
		payloads = append(payloads, map[string]any{
			"txid": txID, "log_index": logIndex, "from": from, "amount": amount,
			"block_height": blockHeight, "status": status, "confirmations": depth,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return payloads, max(minimum, 0), nil
}

// ConfirmationDepth is the sole IPN confirmations computation (CNF-007).
func ConfirmationDepth(lastHeight, blockHeight int64) int64 {
	return max(lastHeight-blockHeight, 0)
}
