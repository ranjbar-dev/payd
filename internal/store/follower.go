package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type Cursor struct {
	LastHeight       int64
	SolidifiedHeight int64
}

type BlockRecord struct {
	Height      int64
	ID          string
	ParentID    string
	Timestamp   int64
	TxCount     int
	ProcessedAt int64
}

type OwnedAddress struct {
	ID      int64
	Address string
}

// BlockApply is prepared before CommitBlock starts its transaction. It may only use BlockWrite DB methods (CHN-006/006a).
type BlockApply func(*BlockWrite) error

type BlockWrite struct{ tx *sql.Tx }

type PaymentRecord struct {
	TxID           string
	LogIndex       int
	Direction      string
	BlockHeight    int64
	BlockID        string
	BlockTimestamp int64
	FromAddress    string
	ToAddress      string
	AddressID      *int64
	OrderID        *string
	Asset          string
	AmountRaw      string
	IsDust         bool
	Status         string
	DetectedAt     int64
}

type AttributionOrder struct {
	ID         string
	Asset      string
	Status     string
	CreatedAt  int64
	ReleasedAt *int64
}

type RewindResult struct {
	OrphanedPayments int64
	RevertedOrders   []string
}

func (s *Store) Cursor(ctx context.Context) (Cursor, bool, error) {
	var cursor Cursor
	err := s.normal.QueryRowContext(ctx, "SELECT last_height, solidified_height FROM crawler_state WHERE id = 1").Scan(&cursor.LastHeight, &cursor.SolidifiedHeight)
	if errors.Is(err, sql.ErrNoRows) {
		return Cursor{}, false, nil
	}
	if err != nil {
		return Cursor{}, false, fmt.Errorf("load crawler cursor: %w", err)
	}
	return cursor, true, nil
}

func (s *Store) Block(ctx context.Context, height int64) (BlockRecord, bool, error) {
	var block BlockRecord
	err := s.normal.QueryRowContext(ctx, `SELECT height, block_id, parent_id, timestamp, tx_count, processed_at
        FROM blocks WHERE height = ?`, height).Scan(&block.Height, &block.ID, &block.ParentID, &block.Timestamp, &block.TxCount, &block.ProcessedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return BlockRecord{}, false, nil
	}
	if err != nil {
		return BlockRecord{}, false, fmt.Errorf("load block %d: %w", height, err)
	}
	return block, true, nil
}

// SetReorgSuspicion blocks confirmation at and above the earliest unresolved divergence (CNF-002b).
func (s *Store) SetReorgSuspicion(ctx context.Context, height int64) error {
	_, err := s.normal.ExecContext(ctx, `UPDATE crawler_state SET reorg_suspected_from =
        CASE WHEN reorg_suspected_from IS NULL THEN ? ELSE MIN(reorg_suspected_from, ?) END
        WHERE id = 1`, height, height)
	if err != nil {
		return fmt.Errorf("store reorg suspicion at height %d: %w", height, err)
	}
	return nil
}

// ClearReorgSuspicion allows confirmation after the follower resolves or rejects the divergence (CNF-002b).
func (s *Store) ClearReorgSuspicion(ctx context.Context) error {
	if _, err := s.normal.ExecContext(ctx, "UPDATE crawler_state SET reorg_suspected_from = NULL WHERE id = 1"); err != nil {
		return fmt.Errorf("clear reorg suspicion: %w", err)
	}
	return nil
}

// OwnedAddresses returns every address derived by this service for bidirectional screening (DET-002b).
func (s *Store) OwnedAddresses(ctx context.Context) ([]OwnedAddress, error) {
	rows, err := s.normal.QueryContext(ctx, "SELECT id, address FROM addresses")
	if err != nil {
		return nil, fmt.Errorf("load owned addresses: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var addresses []OwnedAddress
	for rows.Next() {
		var address OwnedAddress
		if err := rows.Scan(&address.ID, &address.Address); err != nil {
			return nil, fmt.Errorf("scan owned address: %w", err)
		}
		addresses = append(addresses, address)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate owned addresses: %w", err)
	}
	return addresses, nil
}

// CommitBlock stores the block, prepared payment writes, and cursor in one transaction (CHN-006).
func (s *Store) CommitBlock(ctx context.Context, block BlockRecord, reorgDepth int, apply BlockApply) error {
	if reorgDepth <= 0 {
		return errors.New("reorg depth must be positive")
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin block %d: %w", block.Height, err)
	}
	defer func() { _ = tx.Rollback() }()

	var previous int64
	err = tx.QueryRowContext(ctx, "SELECT last_height FROM crawler_state WHERE id = 1").Scan(&previous)
	newCursor := errors.Is(err, sql.ErrNoRows)
	if err != nil && !newCursor {
		return fmt.Errorf("load cursor for block %d: %w", block.Height, err)
	}
	if !newCursor && block.Height != previous+1 {
		return fmt.Errorf("block %d is not the next height after %d (CHN-005)", block.Height, previous)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO blocks(height, block_id, parent_id, timestamp, tx_count, processed_at)
        VALUES (?, ?, ?, ?, ?, ?)`, block.Height, block.ID, block.ParentID, block.Timestamp, block.TxCount, block.ProcessedAt); err != nil {
		return fmt.Errorf("insert block %d: %w", block.Height, err)
	}
	if apply != nil {
		if err := apply(&BlockWrite{tx: tx}); err != nil {
			return fmt.Errorf("apply block %d: %w", block.Height, err)
		}
	}
	now := time.Now().UTC().Unix()
	if newCursor {
		_, err = tx.ExecContext(ctx, `INSERT INTO crawler_state(id, last_height, solidified_height, updated_at)
            VALUES (1, ?, 0, ?)`, block.Height, now)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE crawler_state SET last_height = ?, updated_at = ?
            WHERE id = 1 AND last_height = ?`, block.Height, now, previous)
		if err == nil {
			var changed int64
			changed, err = result.RowsAffected()
			if err == nil && changed != 1 {
				err = errors.New("crawler cursor changed concurrently")
			}
		}
	}
	if err != nil {
		return fmt.Errorf("advance cursor to block %d: %w", block.Height, err)
	}
	// Keep the common ancestor needed to recover a reorg exactly reorgDepth blocks deep (CHN-010/015).
	if _, err := tx.ExecContext(ctx, "DELETE FROM blocks WHERE height < ?", block.Height-int64(reorgDepth)); err != nil {
		return fmt.Errorf("trim retained blocks: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit block %d: %w", block.Height, err)
	}
	return nil
}

// UpsertPayment implements CHN-016 for the prepared matcher closure used by later phases.
func (w *BlockWrite) UpsertPayment(payment PaymentRecord) (bool, error) {
	status := payment.Status
	if status == "" {
		status = "seen"
	}
	var oldStatus string
	var oldOrder sql.NullString
	err := w.tx.QueryRow(`SELECT status, order_id FROM payments WHERE txid = ? AND log_index = ?`, payment.TxID, payment.LogIndex).Scan(&oldStatus, &oldOrder)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("read existing payment: %w", err)
	}
	_, err = w.tx.Exec(`INSERT INTO payments(
        txid, log_index, direction, block_height, block_id, block_timestamp,
        from_address, to_address, address_id, order_id, asset, amount_raw,
        is_dust, status, detected_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT (txid, log_index) DO UPDATE SET
          block_height = excluded.block_height,
          block_id = excluded.block_id,
          block_timestamp = excluded.block_timestamp,
          status = CASE WHEN payments.status = 'orphaned' THEN excluded.status ELSE payments.status END,
          detected_at = COALESCE(payments.detected_at, excluded.detected_at)`,
		payment.TxID, payment.LogIndex, payment.Direction, payment.BlockHeight, payment.BlockID, payment.BlockTimestamp,
		payment.FromAddress, payment.ToAddress, payment.AddressID, payment.OrderID, payment.Asset, payment.AmountRaw,
		payment.IsDust, status, payment.DetectedAt)
	if err != nil {
		return false, fmt.Errorf("upsert payment %s/%d: %w", payment.TxID, payment.LogIndex, err)
	}
	orderID := payment.OrderID
	if oldOrder.Valid {
		orderID = &oldOrder.String
	}
	if orderID != nil {
		if _, err := recalculateOrderWithTransition(w.tx, *orderID, !payment.IsDust); err != nil {
			return false, err
		}
	}
	if payment.AddressID != nil {
		if err := recalculateBalance(w.tx, *payment.AddressID, payment.Asset); err != nil {
			return false, err
		}
	}
	return oldStatus == "orphaned", nil
}

// AttributionOrder returns the address's retained assignment; matcher applies the asset, state, and chain-time checks (ORD-002/020).
func (w *BlockWrite) AttributionOrder(addressID int64) (AttributionOrder, bool, error) {
	var order AttributionOrder
	var released sql.NullInt64
	err := w.tx.QueryRow(`SELECT o.id, o.asset, o.status, o.created_at, a.released_at
        FROM addresses a JOIN orders o ON o.id = a.assigned_order_id WHERE a.id = ?`, addressID).
		Scan(&order.ID, &order.Asset, &order.Status, &order.CreatedAt, &released)
	if errors.Is(err, sql.ErrNoRows) {
		return AttributionOrder{}, false, nil
	}
	if err != nil {
		return AttributionOrder{}, false, fmt.Errorf("load attribution order: %w", err)
	}
	if released.Valid {
		order.ReleasedAt = &released.Int64
	}
	return order, true, nil
}

// RewindChain orphans payments, recalculates affected orders, deletes orphaned blocks, and rewinds the cursor atomically (CHN-012/013).
func (s *Store) RewindChain(ctx context.Context, ancestorHeight int64) (RewindResult, error) {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return RewindResult{}, fmt.Errorf("begin chain rewind: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var lastHeight int64
	if err := tx.QueryRowContext(ctx, "SELECT last_height FROM crawler_state WHERE id = 1").Scan(&lastHeight); err != nil {
		return RewindResult{}, fmt.Errorf("load cursor for rewind: %w", err)
	}
	if ancestorHeight >= lastHeight {
		return RewindResult{}, fmt.Errorf("rewind ancestor %d must be below cursor %d", ancestorHeight, lastHeight)
	}
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM blocks WHERE height = ?", ancestorHeight).Scan(&exists); err != nil {
		return RewindResult{}, fmt.Errorf("load rewind ancestor %d: %w", ancestorHeight, err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT order_id FROM payments
        WHERE block_height > ? AND order_id IS NOT NULL`, ancestorHeight)
	if err != nil {
		return RewindResult{}, fmt.Errorf("find reorg-affected orders: %w", err)
	}
	var orderIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return RewindResult{}, fmt.Errorf("scan reorg-affected order: %w", err)
		}
		orderIDs = append(orderIDs, id)
	}
	if err := rows.Close(); err != nil {
		return RewindResult{}, err
	}
	balanceRows, err := tx.Query(`SELECT DISTINCT address_id, asset FROM payments
        WHERE block_height > ? AND address_id IS NOT NULL`, ancestorHeight)
	if err != nil {
		return RewindResult{}, fmt.Errorf("find reorg-affected balances: %w", err)
	}
	type balanceKey struct {
		addressID int64
		asset     string
	}
	var balances []balanceKey
	for balanceRows.Next() {
		var key balanceKey
		if err := balanceRows.Scan(&key.addressID, &key.asset); err != nil {
			_ = balanceRows.Close()
			return RewindResult{}, err
		}
		balances = append(balances, key)
	}
	if err := balanceRows.Close(); err != nil {
		return RewindResult{}, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE payments SET status = 'orphaned' WHERE block_height > ?", ancestorHeight)
	if err != nil {
		return RewindResult{}, fmt.Errorf("orphan reorg payments: %w", err)
	}
	orphaned, err := result.RowsAffected()
	if err != nil {
		return RewindResult{}, fmt.Errorf("count orphaned payments: %w", err)
	}
	var reverted []string
	for _, orderID := range orderIDs {
		wasReverted, err := recalculateOrder(tx, orderID)
		if err != nil {
			return RewindResult{}, err
		}
		if wasReverted {
			reverted = append(reverted, orderID)
		}
	}
	for _, key := range balances {
		if err := recalculateBalance(tx, key.addressID, key.asset); err != nil {
			return RewindResult{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM blocks WHERE height > ?", ancestorHeight); err != nil {
		return RewindResult{}, fmt.Errorf("delete orphaned blocks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE crawler_state SET last_height = ?, updated_at = ? WHERE id = 1`, ancestorHeight, time.Now().UTC().Unix()); err != nil {
		return RewindResult{}, fmt.Errorf("rewind crawler cursor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RewindResult{}, fmt.Errorf("commit chain rewind: %w", err)
	}
	return RewindResult{OrphanedPayments: orphaned, RevertedOrders: reverted}, nil
}

func recalculateBalance(tx *sql.Tx, addressID int64, asset string) error {
	rows, err := tx.Query(`SELECT amount_raw, direction, status, confirmed_at FROM payments
        WHERE address_id = ? AND asset = ? AND status <> 'orphaned'`, addressID, asset)
	if err != nil {
		return fmt.Errorf("load balance payments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	pending := new(big.Int)
	confirmed := new(big.Int)
	for rows.Next() {
		var amountRaw, direction, status string
		var confirmedAt sql.NullInt64
		if err := rows.Scan(&amountRaw, &direction, &status, &confirmedAt); err != nil {
			return err
		}
		amount, ok := new(big.Int).SetString(amountRaw, 10)
		if !ok || amount.Sign() < 0 {
			return fmt.Errorf("invalid balance amount %q", amountRaw)
		}
		if direction == "out" {
			amount.Neg(amount)
		}
		switch {
		case status == "confirmed" || status == "unattributed" && confirmedAt.Valid:
			confirmed.Add(confirmed, amount)
		case status == "seen" || status == "unattributed" && !confirmedAt.Valid:
			pending.Add(pending, amount)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO balances(address_id, asset, confirmed_raw, pending_raw)
        VALUES (?, ?, ?, ?) ON CONFLICT(address_id, asset) DO UPDATE SET
        confirmed_raw = excluded.confirmed_raw, pending_raw = excluded.pending_raw`,
		addressID, asset, confirmed.String(), pending.String())
	if err != nil {
		return fmt.Errorf("store balance: %w", err)
	}
	return nil
}

func recalculateOrder(tx *sql.Tx, orderID string) (bool, error) {
	return recalculateOrderWithTransition(tx, orderID, true)
}

func recalculateOrderWithTransition(tx *sql.Tx, orderID string, allowTransition bool) (bool, error) {
	var expectedRaw, asset, oldStatus string
	var addressID, createdAt int64
	var releasedAt sql.NullInt64
	err := tx.QueryRow(`SELECT o.expected_raw, o.asset, o.status, o.address_id, o.created_at, a.released_at
        FROM orders o JOIN addresses a ON a.id = o.address_id WHERE o.id = ?`, orderID).
		Scan(&expectedRaw, &asset, &oldStatus, &addressID, &createdAt, &releasedAt)
	if err != nil {
		return false, fmt.Errorf("load order %s for recalculation: %w", orderID, err)
	}
	rows, err := tx.Query(`SELECT amount_raw FROM payments WHERE order_id = ? AND address_id = ?
        AND status <> 'orphaned' AND direction = 'in' AND asset = ?
        AND block_timestamp >= ? AND (? IS NULL OR block_timestamp < ?)`,
		orderID, addressID, asset, createdAt, releasedAt, releasedAt)
	if err != nil {
		return false, fmt.Errorf("load payments for order %s: %w", orderID, err)
	}
	received := new(big.Int)
	for rows.Next() {
		var amountRaw string
		if err := rows.Scan(&amountRaw); err != nil {
			_ = rows.Close()
			return false, fmt.Errorf("scan payment amount for order %s: %w", orderID, err)
		}
		amount, ok := new(big.Int).SetString(amountRaw, 10)
		if !ok || amount.Sign() < 0 {
			_ = rows.Close()
			return false, fmt.Errorf("invalid payment amount %q for order %s", amountRaw, orderID)
		}
		received.Add(received, amount)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	expected, ok := new(big.Int).SetString(expectedRaw, 10)
	if !ok || expected.Sign() < 0 {
		return false, fmt.Errorf("invalid expected amount %q for order %s", expectedRaw, orderID)
	}
	overpaid := new(big.Int).Sub(new(big.Int).Set(received), expected)
	if overpaid.Sign() < 0 {
		overpaid.SetInt64(0)
	}
	newStatus := oldStatus
	if allowTransition && (oldStatus == "pending" || oldStatus == "partial" || oldStatus == "paid" || oldStatus == "confirmed") {
		switch {
		case received.Cmp(expected) >= 0:
			newStatus = "paid"
		case received.Sign() == 0:
			newStatus = "pending"
		default:
			newStatus = "partial"
		}
	}
	if _, err := tx.Exec(`UPDATE orders SET received_raw = ?, overpaid_raw = ?, status = ?, updated_at = ? WHERE id = ?`,
		received.String(), overpaid.String(), newStatus, time.Now().UTC().Unix(), orderID); err != nil {
		return false, fmt.Errorf("recalculate order %s: %w", orderID, err)
	}
	return (oldStatus == "paid" || oldStatus == "confirmed") && (newStatus == "partial" || newStatus == "pending"), nil
}
