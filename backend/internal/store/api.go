package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type OrderFilter struct {
	Status      string
	Asset       string
	ExternalRef string
	Consumer    string
	Address     string
	CreatedFrom *int64
	CreatedTo   *int64
	After       string
	Limit       int
}

type PaymentFilter struct {
	TxID      string
	Address   string
	OrderID   string
	Status    string
	Direction string
	Asset     string
	From      *int64
	To        *int64
	After     int64
	Limit     int
}

type Payment struct {
	ID             int64
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
	ConfirmedAt    *int64
	// UnattributedReason is set only for unattributed payments (ORD-020).
	UnattributedReason string
	// WithdrawalID is the withdrawal this outbound payment settles, matched on
	// txid. Empty for inbound rows and for outbound rows this service did not
	// broadcast.
	WithdrawalID string
}

// ListOrders provides API-025 keyset pagination; the ULID is the stable cursor.
func (s *Store) ListOrders(ctx context.Context, filter OrderFilter) ([]Order, error) {
	query, args := orderSelect+" WHERE 1=1", []any{}
	query, args = appendOrderFilters(query, args, filter)
	query += " ORDER BY id LIMIT ?"
	args = append(args, filter.Limit)
	rows, err := s.normal.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var orders []Order
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func appendOrderFilters(query string, args []any, filter OrderFilter) (string, []any) {
	if filter.Status != "" {
		query, args = query+" AND status = ?", append(args, filter.Status)
	}
	if filter.Asset != "" {
		query, args = query+" AND asset = ?", append(args, filter.Asset)
	}
	if filter.ExternalRef != "" {
		query, args = query+" AND external_ref = ?", append(args, filter.ExternalRef)
	}
	if filter.Consumer != "" {
		query, args = query+" AND consumer = ?", append(args, filter.Consumer)
	}
	if filter.Address != "" {
		query, args = query+" AND address = ?", append(args, filter.Address)
	}
	if filter.CreatedFrom != nil {
		query, args = query+" AND created_at >= ?", append(args, *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query, args = query+" AND created_at <= ?", append(args, *filter.CreatedTo)
	}
	if filter.After != "" {
		query, args = query+" AND id > ?", append(args, filter.After)
	}
	return query, args
}

func (s *Store) StreamOrders(ctx context.Context, filter OrderFilter, limit int, visit func(Order) error) error {
	query, args := appendOrderFilters(orderSelect+" WHERE 1=1", nil, filter)
	query, args = query+" ORDER BY id LIMIT ?", append(args, limit)
	rows, err := s.normal.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("stream orders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return err
		}
		if err := visit(order); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) OrderPayments(ctx context.Context, orderID string, after int64, limit int) ([]Payment, error) {
	return s.listPayments(ctx, "order_id = ?", []any{orderID}, after, limit)
}

func (s *Store) AddressPayments(ctx context.Context, addressID, after int64, limit int) ([]Payment, error) {
	return s.listPayments(ctx, "address_id = ?", []any{addressID}, after, limit)
}

func (s *Store) ListPayments(ctx context.Context, status string, after int64, limit int) ([]Payment, error) {
	return s.listPayments(ctx, "status = ?", []any{status}, after, limit)
}

func (s *Store) ListPaymentsFiltered(ctx context.Context, filter PaymentFilter) ([]Payment, error) {
	where, args := "1=1", []any{}
	add := func(column, value string) {
		if value != "" {
			where += " AND " + column + " = ?"
			args = append(args, value)
		}
	}
	add("txid", filter.TxID)
	if filter.Address != "" {
		where += " AND (from_address = ? OR to_address = ?)"
		args = append(args, filter.Address, filter.Address)
	}
	add("order_id", filter.OrderID)
	add("status", filter.Status)
	add("direction", filter.Direction)
	add("asset", filter.Asset)
	if filter.From != nil {
		where += " AND block_timestamp >= ?"
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		where += " AND block_timestamp <= ?"
		args = append(args, *filter.To)
	}
	return s.listPayments(ctx, where, args, filter.After, filter.Limit)
}

func (s *Store) listPayments(ctx context.Context, where string, args []any, after int64, limit int) ([]Payment, error) {
	query := `SELECT id, txid, log_index, direction, block_height, block_id, block_timestamp,
		from_address, to_address, address_id, order_id, asset, amount_raw, is_dust, status,
		detected_at, confirmed_at, unattributed_reason,
		COALESCE((SELECT w.id FROM withdrawals w
			WHERE w.txid = payments.txid AND payments.direction = 'out' LIMIT 1), '')
		FROM payments WHERE ` + where
	if after > 0 {
		query, args = query+" AND id > ?", append(args, after)
	}
	query += " ORDER BY id"
	if limit > 0 {
		query, args = query+" LIMIT ?", append(args, limit)
	}
	rows, err := s.normal.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var payments []Payment
	for rows.Next() {
		payment, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		payments = append(payments, payment)
	}
	return payments, rows.Err()
}

func scanPayment(row rowScanner) (Payment, error) {
	var payment Payment
	var addressID, confirmedAt sql.NullInt64
	var orderID, reason sql.NullString
	err := row.Scan(&payment.ID, &payment.TxID, &payment.LogIndex, &payment.Direction, &payment.BlockHeight,
		&payment.BlockID, &payment.BlockTimestamp, &payment.FromAddress, &payment.ToAddress, &addressID,
		&orderID, &payment.Asset, &payment.AmountRaw, &payment.IsDust, &payment.Status, &payment.DetectedAt, &confirmedAt,
		&reason, &payment.WithdrawalID)
	payment.UnattributedReason = reason.String
	if addressID.Valid {
		payment.AddressID = &addressID.Int64
	}
	if orderID.Valid {
		payment.OrderID = &orderID.String
	}
	if confirmedAt.Valid {
		payment.ConfirmedAt = &confirmedAt.Int64
	}
	return payment, err
}

// ListFundedTerminalOrders is the API-025 form of FundedTerminalOrders (ORD-005b).
func (s *Store) ListFundedTerminalOrders(ctx context.Context, after string, limit int) ([]FundedTerminal, error) {
	query := orderSelect + ` WHERE status IN ('expired_funded','cancelled_funded') AND resolution IS NULL`
	args := []any{}
	if after != "" {
		query, args = query+" AND id > ?", append(args, after)
	}
	query += " ORDER BY id LIMIT ?"
	args = append(args, limit)
	rows, err := s.normal.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list funded terminal orders: %w", err)
	}
	var orders []FundedTerminal
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		orders = append(orders, FundedTerminal{Order: order})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range orders {
		payers, err := s.paymentPayers(ctx, orders[index].ID)
		if err != nil {
			return nil, err
		}
		orders[index].Payers = payers
	}
	return orders, nil
}

func (s *Store) paymentPayers(ctx context.Context, orderID string) ([]string, error) {
	rows, err := s.normal.QueryContext(ctx, `SELECT DISTINCT from_address FROM payments
		WHERE order_id = ? AND direction = 'in' AND status <> 'orphaned' ORDER BY from_address`, orderID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var payers []string
	for rows.Next() {
		var payer string
		if err := rows.Scan(&payer); err != nil {
			return nil, err
		}
		payers = append(payers, payer)
	}
	return payers, rows.Err()
}

// UseTOTP makes API-022 replay prevention atomic and restart-safe.
func (s *Store) UseTOTP(ctx context.Context, code string, step int64, now time.Time) (bool, error) {
	result, err := s.normal.ExecContext(ctx, `INSERT INTO used_totp(code, step, used_at) VALUES (?, ?, ?)
		ON CONFLICT(code, step) DO NOTHING`, code, step, now.UTC().Unix())
	if err != nil {
		return false, fmt.Errorf("persist used TOTP: %w", err)
	}
	inserted, err := result.RowsAffected()
	return inserted == 1, err
}
