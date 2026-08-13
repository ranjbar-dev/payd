package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

var (
	ErrPoolExhausted       = errors.New("address pool exhausted")
	ErrAddressNotFound     = errors.New("address not found")
	ErrExternalRefConflict = errors.New("external_ref conflicts with an existing order")
	ErrOrderNotFound       = errors.New("order not found")
	ErrOrderRequiresForce  = errors.New("order cancellation requires force")
	ErrOrderTerminal       = errors.New("order is terminal")
	ErrOrderLifetime       = errors.New("order lifetime exceeds maximum")
	ErrInvalidResolution   = errors.New("invalid order resolution")
	ErrPaymentNotFound     = errors.New("payment not found")
)

type ExternalRefConflictError struct{ Fields []string }

func (e *ExternalRefConflictError) Error() string { return ErrExternalRefConflict.Error() }
func (e *ExternalRefConflictError) Unwrap() error { return ErrExternalRefConflict }

type Order struct {
	ID             string
	ExternalRef    *string
	AddressID      int64
	Address        string
	Asset          string
	ExpectedRaw    string
	ReceivedRaw    string
	OverpaidRaw    string
	Status         string
	Resolution     *string
	ResolutionNote *string
	ResolvedAt     *int64
	PriceUSD       *string
	PriceAt        *int64
	Metadata       string
	Consumer       string
	ExpiresAt      int64
	CreatedAt      int64
	UpdatedAt      int64
}

type CreateOrderParams struct {
	ExternalRef *string
	Asset       string
	ExpectedRaw string
	Metadata    string
	Consumer    string
	ExpiresAt   int64
	PriceUSD    *string
	PriceAt     *int64
}

type FundedTerminal struct {
	Order
	Payers []string
}

type UnattributedPayment struct {
	ID             int64
	TxID           string
	LogIndex       int
	AddressID      *int64
	Asset          string
	AmountRaw      string
	FromAddress    string
	ToAddress      string
	BlockTimestamp int64
}

// CreateOrder atomically inserts the order and conditionally claims one free pool address (POOL-001/002/006, ORD-001/008a).
func (s *Store) CreateOrder(ctx context.Context, derive func(uint32) (string, error), poolMax int, params CreateOrderParams, now time.Time) (Order, bool, error) {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, false, fmt.Errorf("begin order creation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	addressID, hdIndex, address, err := freeAddress(tx)
	poolExhausted := false
	if errors.Is(err, sql.ErrNoRows) {
		addressID, hdIndex, address, err = insertNextAddress(tx, derive, poolMax, now)
		if errors.Is(err, ErrPoolExhausted) {
			poolExhausted = true
			// ORD-008a/LIF-003: continue through INSERT so an external_ref replay resolves; this in-use address is only a rollback placeholder and must never reach commit.
			err = tx.QueryRow(`SELECT id, hd_index, address FROM addresses
                WHERE hd_index BETWEEN 0 AND 999 ORDER BY hd_index LIMIT 1`).Scan(&addressID, &hdIndex, &address)
			if errors.Is(err, sql.ErrNoRows) {
				return Order{}, false, ErrPoolExhausted
			}
		}
	}
	if err != nil {
		return Order{}, false, err
	}

	created := now.UTC().Unix()
	metadata := params.Metadata
	if metadata == "" {
		metadata = "{}"
	}
	orderID, err := newULID(now)
	if err != nil {
		return Order{}, false, err
	}
	order := Order{
		ID: orderID, ExternalRef: params.ExternalRef, AddressID: addressID, Address: address,
		Asset: params.Asset, ExpectedRaw: params.ExpectedRaw, ReceivedRaw: "0", OverpaidRaw: "0", Status: "pending",
		PriceUSD: params.PriceUSD, PriceAt: params.PriceAt, Metadata: metadata, Consumer: params.Consumer,
		ExpiresAt: params.ExpiresAt, CreatedAt: created, UpdatedAt: created,
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO orders(
        id, external_ref, address_id, address, asset, expected_raw, price_usd, price_at,
        metadata, consumer, expires_at, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		order.ID, order.ExternalRef, order.AddressID, order.Address, order.Asset, order.ExpectedRaw,
		order.PriceUSD, order.PriceAt, order.Metadata, order.Consumer, order.ExpiresAt, order.CreatedAt, order.UpdatedAt)
	if err != nil {
		return Order{}, false, fmt.Errorf("insert order: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return Order{}, false, fmt.Errorf("check order insert: %w", err)
	}
	if inserted == 0 {
		if params.ExternalRef == nil {
			return Order{}, false, errors.New("order insert affected no rows")
		}
		existing, err := orderByExternalRef(tx, *params.ExternalRef)
		if err != nil {
			return Order{}, false, err
		}
		var conflicts []string
		if existing.Asset != params.Asset {
			conflicts = append(conflicts, "asset")
		}
		if existing.ExpectedRaw != params.ExpectedRaw {
			conflicts = append(conflicts, "expected_raw")
		}
		if existing.Consumer != params.Consumer {
			conflicts = append(conflicts, "consumer")
		}
		if len(conflicts) > 0 {
			return Order{}, false, &ExternalRefConflictError{Fields: conflicts}
		}
		return existing, false, nil
	}
	if poolExhausted {
		return Order{}, false, ErrPoolExhausted
	}

	// POOL-006: this guarded UPDATE, not the preceding SELECT, is the exclusive claim.
	result, err = tx.ExecContext(ctx, `UPDATE addresses
        SET state = 'assigned', assigned_order_id = ?, assigned_at = ?, released_at = NULL, cooling_until = NULL
        WHERE id = ? AND hd_index = ? AND state = 'free'`, order.ID, created, addressID, hdIndex)
	if err != nil {
		return Order{}, false, fmt.Errorf("assign address %d: %w", hdIndex, err)
	}
	claimed, err := result.RowsAffected()
	if err != nil || claimed != 1 {
		if err != nil {
			return Order{}, false, fmt.Errorf("check address assignment: %w", err)
		}
		return Order{}, false, errors.New("free address was claimed concurrently")
	}
	if err := tx.Commit(); err != nil {
		return Order{}, false, fmt.Errorf("commit order creation: %w", err)
	}
	return order, true, nil
}

func freeAddress(tx *sql.Tx) (int64, uint32, string, error) {
	var id int64
	var index uint32
	var address string
	err := tx.QueryRow(`SELECT id, hd_index, address FROM addresses
        WHERE state = 'free' AND hd_index BETWEEN 0 AND 999 ORDER BY hd_index LIMIT 1`).Scan(&id, &index, &address)
	return id, index, address, err
}

func insertNextAddress(tx *sql.Tx, derive func(uint32) (string, error), poolMax int, now time.Time) (int64, uint32, string, error) {
	indexes, count, err := poolIndexes(tx)
	if err != nil {
		return 0, 0, "", err
	}
	if count >= poolMax {
		return 0, 0, "", ErrPoolExhausted
	}
	index := nextPoolIndex(indexes)
	if index >= 1000 {
		return 0, 0, "", ErrPoolExhausted
	}
	address, err := derive(index)
	if err != nil {
		return 0, 0, "", fmt.Errorf("derive pool address %d: %w", index, err)
	}
	result, err := tx.Exec(`INSERT INTO addresses(hd_index, address, state, created_at) VALUES (?, ?, 'free', ?)`, index, address, now.UTC().Unix())
	if err != nil {
		return 0, 0, "", fmt.Errorf("insert pool address %d: %w", index, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, 0, "", fmt.Errorf("read pool address id: %w", err)
	}
	return id, index, address, nil
}

// TopUpPool replenishes free addresses to target only when free falls below minFree (POOL-003, LIF-003).
func (s *Store) TopUpPool(ctx context.Context, derive func(uint32) (string, error), minFree, target, poolMax int, now time.Time) (int, error) {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin pool top-up: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var free int
	if err := tx.QueryRow("SELECT COUNT(*) FROM addresses WHERE state = 'free' AND hd_index BETWEEN 0 AND 999").Scan(&free); err != nil {
		return 0, fmt.Errorf("count free addresses: %w", err)
	}
	if free >= minFree {
		return 0, nil
	}
	indexes, count, err := poolIndexes(tx)
	if err != nil {
		return 0, err
	}
	toAdd := min(target-free, poolMax-count)
	added := 0
	for added < toAdd {
		index := nextPoolIndex(indexes)
		if index >= 1000 {
			break
		}
		address, err := derive(index)
		if err != nil {
			return 0, fmt.Errorf("derive pool address %d: %w", index, err)
		}
		if _, err := tx.Exec(`INSERT INTO addresses(hd_index, address, state, created_at) VALUES (?, ?, 'free', ?)`, index, address, now.UTC().Unix()); err != nil {
			return 0, fmt.Errorf("insert pool address %d: %w", index, err)
		}
		indexes[index] = struct{}{}
		added++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit pool top-up: %w", err)
	}
	return added, nil
}

func poolIndexes(tx *sql.Tx) (map[uint32]struct{}, int, error) {
	rows, err := tx.Query("SELECT hd_index FROM addresses WHERE hd_index BETWEEN 0 AND 999 ORDER BY hd_index")
	if err != nil {
		return nil, 0, fmt.Errorf("load pool indexes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	indexes := make(map[uint32]struct{})
	for rows.Next() {
		var index uint32
		if err := rows.Scan(&index); err != nil {
			return nil, 0, fmt.Errorf("scan pool index: %w", err)
		}
		indexes[index] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return indexes, len(indexes), nil
}

func nextPoolIndex(indexes map[uint32]struct{}) uint32 {
	for index := uint32(0); index < 1000; index++ {
		if _, used := indexes[index]; !used {
			return index
		}
	}
	return 1000
}

// ReleaseCooled returns elapsed cooling addresses to the pool and clears the assignment window (POOL-005, LIF-002).
func (s *Store) ReleaseCooled(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.normal.ExecContext(ctx, `UPDATE addresses SET state = 'free', assigned_order_id = NULL,
        assigned_at = NULL, released_at = NULL, cooling_until = NULL
        WHERE state = 'cooling' AND cooling_until <= ?`, now.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("release cooled addresses: %w", err)
	}
	return result.RowsAffected()
}

// DisableAddress permanently removes an owned address from rotation and records the operator action (POOL-007/008).
func (s *Store) DisableAddress(ctx context.Context, address, actor, ip string, now time.Time) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin disable address: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, "UPDATE addresses SET state = 'disabled' WHERE address = ?", address)
	if err != nil {
		return fmt.Errorf("disable address: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrAddressNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor,action,subject,detail,ip,created_at)
		VALUES (?,'wallet.disable',?,NULL,?,?)`, actor, address, ip, now.UTC().Unix()); err != nil {
		return fmt.Errorf("audit address disable: %w", err)
	}
	return tx.Commit()
}

func (s *Store) Order(ctx context.Context, id string) (Order, error) {
	order, err := scanOrder(s.normal.QueryRowContext(ctx, orderSelect+" WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	return order, err
}

// ExtendOrder implements API-029 with a hard lifetime measured from created_at.
func (s *Store) ExtendOrder(ctx context.Context, id string, ttl, maxLifetime time.Duration, now time.Time) (Order, error) {
	if ttl <= 0 || maxLifetime <= 0 {
		return Order{}, ErrOrderLifetime
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback() }()
	order, err := scanOrder(tx.QueryRowContext(ctx, orderSelect+" WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, err
	}
	switch order.Status {
	case "confirmed", "expired", "expired_funded", "cancelled", "cancelled_funded":
		return Order{}, ErrOrderTerminal
	}
	seconds := int64(ttl / time.Second)
	if seconds <= 0 || order.ExpiresAt > int64(^uint64(0)>>1)-seconds {
		return Order{}, ErrOrderLifetime
	}
	expiresAt := order.ExpiresAt + seconds
	maximum := order.CreatedAt + int64(maxLifetime/time.Second)
	if expiresAt > maximum {
		return Order{}, ErrOrderLifetime
	}
	updatedAt := now.UTC().Unix()
	result, err := tx.ExecContext(ctx, `UPDATE orders SET expires_at=?,updated_at=? WHERE id=? AND updated_at=?`, expiresAt, updatedAt, id, order.UpdatedAt)
	if err != nil {
		return Order{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		if err != nil {
			return Order{}, err
		}
		return Order{}, errors.New("order changed concurrently")
	}
	order.ExpiresAt, order.UpdatedAt = expiresAt, updatedAt
	if err := tx.Commit(); err != nil {
		return Order{}, err
	}
	return order, nil
}

// CancelOrder enforces force for funded/non-pending states and preserves funded terminal orders (ORD-005a/011, POOL-004).
func (s *Store) CancelOrder(ctx context.Context, id string, force bool, cooldown time.Duration, now time.Time) (Order, error) {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, fmt.Errorf("begin order cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	order, err := scanOrder(tx.QueryRow(orderSelect+" WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, err
	}
	if order.Status != "pending" && !force {
		if order.Status == "partial" || order.Status == "paid" || order.Status == "confirmed" {
			return Order{}, ErrOrderRequiresForce
		}
		return Order{}, ErrOrderTerminal
	}
	if order.Status == "expired" || order.Status == "expired_funded" || order.Status == "cancelled" || order.Status == "cancelled_funded" {
		return Order{}, ErrOrderTerminal
	}
	status := "cancelled"
	if amountPositive(order.ReceivedRaw) {
		status = "cancelled_funded"
	}
	stamp := now.UTC().Unix()
	if _, err := tx.Exec("UPDATE orders SET status = ?, updated_at = ? WHERE id = ?", status, stamp, id); err != nil {
		return Order{}, fmt.Errorf("cancel order: %w", err)
	}
	if _, err := tx.Exec(`UPDATE addresses SET state = 'cooling', released_at = ?, cooling_until = ?
        WHERE id = ? AND state = 'assigned'`, stamp, now.Add(cooldown).UTC().Unix(), order.AddressID); err != nil {
		return Order{}, fmt.Errorf("cool cancelled order address: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Order{}, fmt.Errorf("commit order cancellation: %w", err)
	}
	order.Status, order.UpdatedAt = status, stamp
	return order, nil
}

// ResolveFundedOrder records the operator disposition and audit entry (ORD-005d).
func (s *Store) ResolveFundedOrder(ctx context.Context, id, resolution, note, actor string, now time.Time) error {
	if resolution != "refunded" && resolution != "written_off" && resolution != "reattributed" {
		return ErrInvalidResolution
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin funded order resolution: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now.UTC().Unix()
	result, err := tx.Exec(`UPDATE orders SET resolution = ?, resolution_note = ?, resolved_at = ?, updated_at = ?
        WHERE id = ? AND status IN ('expired_funded','cancelled_funded') AND resolution IS NULL`, resolution, note, stamp, stamp, id)
	if err != nil {
		return fmt.Errorf("resolve funded order: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return ErrOrderNotFound
	}
	detail, _ := json.Marshal(map[string]string{"resolution": resolution, "note": note})
	if _, err := tx.Exec(`INSERT INTO audit_log(actor, action, subject, detail, created_at)
        VALUES (?, 'order.resolve', ?, ?, ?)`, actor, id, string(detail), stamp); err != nil {
		return fmt.Errorf("audit funded order resolution: %w", err)
	}
	return tx.Commit()
}

// FundedTerminalOrders includes distinct payer addresses for actionable refunds (ORD-005b).
func (s *Store) FundedTerminalOrders(ctx context.Context) ([]FundedTerminal, error) {
	rows, err := s.normal.QueryContext(ctx, orderSelect+` WHERE status IN ('expired_funded','cancelled_funded')
        AND resolution IS NULL ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list funded terminal orders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var orders []FundedTerminal
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, FundedTerminal{Order: order})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range orders {
		payerRows, err := s.normal.QueryContext(ctx, `SELECT DISTINCT from_address FROM payments
			WHERE order_id = ? AND direction = 'in' AND status <> 'orphaned' ORDER BY from_address`, orders[index].ID)
		if err != nil {
			return nil, err
		}
		var payers []string
		for payerRows.Next() {
			var payer string
			if err := payerRows.Scan(&payer); err != nil {
				_ = payerRows.Close()
				return nil, err
			}
			payers = append(payers, payer)
		}
		if err := payerRows.Close(); err != nil {
			return nil, err
		}
		orders[index].Payers = payers
	}
	return orders, nil
}

// UnattributedPayments is the store surface for ORD-021.
func (s *Store) UnattributedPayments(ctx context.Context) ([]UnattributedPayment, error) {
	rows, err := s.normal.QueryContext(ctx, `SELECT id, txid, log_index, address_id, asset, amount_raw,
        from_address, to_address, block_timestamp FROM payments WHERE status = 'unattributed' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list unattributed payments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var payments []UnattributedPayment
	for rows.Next() {
		var payment UnattributedPayment
		var addressID sql.NullInt64
		if err := rows.Scan(&payment.ID, &payment.TxID, &payment.LogIndex, &addressID, &payment.Asset,
			&payment.AmountRaw, &payment.FromAddress, &payment.ToAddress, &payment.BlockTimestamp); err != nil {
			return nil, err
		}
		if addressID.Valid {
			payment.AddressID = &addressID.Int64
		}
		payments = append(payments, payment)
	}
	return payments, rows.Err()
}

// AttributePayment manually binds an unattributed payment and recomputes the order (ORD-024).
func (s *Store) AttributePayment(ctx context.Context, paymentID int64, orderID string, now time.Time) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin manual attribution: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRow("SELECT 1 FROM orders WHERE id = ?", orderID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return ErrOrderNotFound
	} else if err != nil {
		return fmt.Errorf("load attribution order: %w", err)
	}
	var addressID sql.NullInt64
	var asset string
	if err := tx.QueryRow("SELECT address_id, asset FROM payments WHERE id = ? AND status = 'unattributed'", paymentID).Scan(&addressID, &asset); errors.Is(err, sql.ErrNoRows) {
		return ErrPaymentNotFound
	} else if err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE payments SET order_id = ?, status = CASE WHEN confirmed_at IS NULL THEN 'seen' ELSE 'confirmed' END
        WHERE id = ? AND status = 'unattributed'`, orderID, paymentID)
	if err != nil {
		return fmt.Errorf("attribute payment: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return ErrPaymentNotFound
	}
	if _, err := recalculateOrder(tx, orderID); err != nil {
		return err
	}
	if addressID.Valid {
		if err := recalculateBalance(tx, addressID.Int64, asset); err != nil { // BAL-001
			return err
		}
	}
	if _, err := tx.Exec("UPDATE orders SET updated_at = ? WHERE id = ?", now.UTC().Unix(), orderID); err != nil {
		return err
	}
	return tx.Commit()
}

const orderSelect = `SELECT id, external_ref, address_id, address, asset, expected_raw, received_raw,
    overpaid_raw, status, resolution, resolution_note, resolved_at, price_usd, price_at,
    COALESCE(metadata, '{}'), COALESCE(consumer, ''), expires_at, created_at, updated_at FROM orders`

type rowScanner interface{ Scan(...any) error }

func scanOrder(row rowScanner) (Order, error) {
	var order Order
	var externalRef, resolution, resolutionNote, priceUSD sql.NullString
	var resolvedAt, priceAt sql.NullInt64
	err := row.Scan(&order.ID, &externalRef, &order.AddressID, &order.Address, &order.Asset, &order.ExpectedRaw,
		&order.ReceivedRaw, &order.OverpaidRaw, &order.Status, &resolution, &resolutionNote, &resolvedAt,
		&priceUSD, &priceAt, &order.Metadata, &order.Consumer, &order.ExpiresAt, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return Order{}, err
	}
	if externalRef.Valid {
		order.ExternalRef = &externalRef.String
	}
	if resolution.Valid {
		order.Resolution = &resolution.String
	}
	if resolutionNote.Valid {
		order.ResolutionNote = &resolutionNote.String
	}
	if resolvedAt.Valid {
		order.ResolvedAt = &resolvedAt.Int64
	}
	if priceUSD.Valid {
		order.PriceUSD = &priceUSD.String
	}
	if priceAt.Valid {
		order.PriceAt = &priceAt.Int64
	}
	return order, nil
}

func orderByExternalRef(tx *sql.Tx, externalRef string) (Order, error) {
	return scanOrder(tx.QueryRow(orderSelect+" WHERE external_ref = ?", externalRef))
}

func amountPositive(value string) bool {
	amount, ok := new(big.Int).SetString(value, 10)
	return ok && amount.Sign() > 0
}

func newULID(now time.Time) (string, error) {
	var raw [16]byte
	milliseconds := uint64(now.UTC().UnixMilli())
	for index := 5; index >= 0; index-- {
		raw[index] = byte(milliseconds)
		milliseconds >>= 8
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", fmt.Errorf("generate ULID entropy: %w", err)
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	value := new(big.Int).SetBytes(raw[:])
	mask := big.NewInt(31)
	encoded := make([]byte, 26)
	for index := len(encoded) - 1; index >= 0; index-- {
		encoded[index] = alphabet[new(big.Int).And(value, mask).Int64()]
		value.Rsh(value, 5)
	}
	return string(encoded), nil
}
