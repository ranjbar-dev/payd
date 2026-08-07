package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ExpireOrders rechecks all payments including dust, expires remaining underfunded orders, and starts cooldown atomically (LIF-001, ORD-005a/005c, POOL-004).
func (s *Store) ExpireOrders(ctx context.Context, now time.Time, cooldown time.Duration, eventConfigs ...EventConfig) (int, error) {
	var events EventConfig
	if len(eventConfigs) > 0 {
		events = eventConfigs[0]
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin order expiry: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM orders
        WHERE status IN ('pending','partial') AND expires_at <= ? ORDER BY expires_at`, now.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("load expiring orders: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	stamp := now.UTC().Unix()
	expired := 0
	for _, id := range ids {
		if _, err := recalculateOrder(tx, id); err != nil { // ORD-005c includes is_dust rows.
			return 0, err
		}
		var addressID int64
		var received, status, asset, metadata string
		if err := tx.QueryRow("SELECT address_id, received_raw, status, asset, COALESCE(metadata, '{}') FROM orders WHERE id = ?", id).
			Scan(&addressID, &received, &status, &asset, &metadata); err != nil {
			return 0, err
		}
		if status == "paid" {
			continue
		}
		status = "expired"
		if amountPositive(received) {
			status = "expired_funded"
		}
		if _, err := tx.Exec("UPDATE orders SET status = ?, updated_at = ? WHERE id = ?", status, stamp, id); err != nil {
			return 0, fmt.Errorf("expire order %s: %w", id, err)
		}
		if _, err := tx.Exec(`UPDATE addresses SET state = 'cooling', released_at = ?, cooling_until = ?
            WHERE id = ? AND state = 'assigned'`, stamp, now.Add(cooldown).UTC().Unix(), addressID); err != nil {
			return 0, fmt.Errorf("cool expired order address: %w", err)
		}
		receivedFormatted, err := FormatUnits(received, events.Decimals[asset])
		if err != nil {
			return 0, err
		}
		payerRows, err := tx.Query(`SELECT DISTINCT from_address FROM payments
            WHERE order_id = ? AND direction = 'in' AND status <> 'orphaned' ORDER BY from_address`, id)
		if err != nil {
			return 0, err
		}
		var payers []string
		for payerRows.Next() {
			var payer string
			if err := payerRows.Scan(&payer); err != nil {
				_ = payerRows.Close()
				return 0, err
			}
			payers = append(payers, payer)
		}
		if err := payerRows.Close(); err != nil {
			return 0, err
		}
		if err := enqueueOrderEvent(tx, events, id, "order.expired", map[string]any{
			"order_id": id, "status": status, "received": receivedFormatted,
			"refundable": status == "expired_funded", "from_addresses": payers, "metadata": json.RawMessage(metadata),
		}, now); err != nil {
			return 0, fmt.Errorf("enqueue order expiry: %w", err)
		}
		expired++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit order expiry: %w", err)
	}
	return expired, nil
}

// PruneUsedTOTP removes replay-prevention rows strictly older than five minutes (LIF-004, DB-007).
func (s *Store) PruneUsedTOTP(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.normal.ExecContext(ctx, "DELETE FROM used_totp WHERE used_at < ?", now.Add(-5*time.Minute).UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("prune used TOTP: %w", err)
	}
	return result.RowsAffected()
}
