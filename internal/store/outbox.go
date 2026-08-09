package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/config"
)

type EventConsumer struct {
	Name           string
	URL            string
	Enabled        bool
	ReceivesGlobal bool
}

// IPNEvent is an immutable outbox snapshot claimed for delivery.
type IPNEvent struct {
	ID          string
	OrderID     string
	SequenceKey string
	Consumer    string
	TargetURL   string
	EventType   string
	Payload     json.RawMessage
	Attempts    int
	CreatedAt   int64
}

type DeadIPN struct {
	ID, OrderID, Consumer, EventType, LastError string
	Attempts, LastStatusCode                    int
	CreatedAt                                   int64
}

type EventConfig struct {
	DefaultConsumer string
	Consumers       map[string]EventConsumer
	Decimals        map[string]int
}

func NewEventConfig(ipn config.IPN, assets []config.Asset) EventConfig {
	events := EventConfig{DefaultConsumer: ipn.DefaultConsumer, Consumers: make(map[string]EventConsumer), Decimals: make(map[string]int)}
	for _, consumer := range ipn.Consumers {
		events.Consumers[consumer.Name] = EventConsumer{
			Name: consumer.Name, URL: consumer.URL, Enabled: consumer.Enabled, ReceivesGlobal: consumer.ReceivesGlobal,
		}
	}
	for _, asset := range assets {
		events.Decimals[asset.Symbol] = asset.Decimals
	}
	return events
}

func (s *Store) OutboxCount(ctx context.Context, eventType string) (int, error) {
	var count int
	err := s.normal.QueryRowContext(ctx, "SELECT COUNT(*) FROM ipn_outbox WHERE event_type = ?", eventType).Scan(&count)
	return count, err
}

func (s *Store) ListDeadIPN(ctx context.Context, consumer, after string, limit int) ([]DeadIPN, error) {
	query := `SELECT id,COALESCE(order_id,''),consumer,event_type,attempts,COALESCE(last_error,''),
		COALESCE(last_status_code,0),created_at FROM ipn_outbox WHERE status='dead'`
	args := []any{}
	if consumer != "" {
		query, args = query+" AND consumer=?", append(args, consumer)
	}
	if after != "" {
		query, args = query+" AND id>?", append(args, after)
	}
	query, args = query+" ORDER BY id LIMIT ?", append(args, limit)
	rows, err := s.normal.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list dead IPN events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var events []DeadIPN
	for rows.Next() {
		var event DeadIPN
		if err := rows.Scan(&event.ID, &event.OrderID, &event.Consumer, &event.EventType, &event.Attempts,
			&event.LastError, &event.LastStatusCode, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) IPNStatus(ctx context.Context, id string) (string, bool, error) {
	var status string
	err := s.normal.QueryRowContext(ctx, "SELECT status FROM ipn_outbox WHERE id=?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return status, err == nil, err
}

// OutboxReady wakes W-004 after a producer commits an event.
func (s *Store) OutboxReady() <-chan struct{} { return s.outboxReady }

func (s *Store) notifyOutbox() {
	select {
	case s.outboxReady <- struct{}{}:
	default:
	}
}

// EnqueueGlobalEvent writes a standalone global notification atomically. State-changing callers must use their existing transaction instead (IPN-001/003/004).
func (s *Store) EnqueueGlobalEvent(ctx context.Context, events EventConfig, sequenceKey, eventType string, payload map[string]any, now time.Time) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin global event: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := enqueueGlobalEvent(tx, events, sequenceKey, eventType, payload, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit global event: %w", err)
	}
	s.notifyOutbox()
	return nil
}

// ClaimIPN atomically claims the oldest eligible head from any enabled pair (IPN-011..013).
func (s *Store) ClaimIPN(ctx context.Context, now time.Time, enabledConsumers []string) (IPNEvent, bool, error) {
	if len(enabledConsumers) == 0 {
		return IPNEvent{}, false, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(enabledConsumers)), ",")
	args := make([]any, 0, len(enabledConsumers)+1)
	args = append(args, now.UTC().Unix())
	for _, consumer := range enabledConsumers {
		args = append(args, consumer)
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return IPNEvent{}, false, fmt.Errorf("begin IPN claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	query := `SELECT q.rowid, q.id, COALESCE(q.order_id, ''), q.sequence_key, q.consumer,
        q.target_url, q.event_type, q.payload, q.attempts, q.created_at
        FROM ipn_outbox q
        WHERE q.status = 'pending' AND q.next_attempt_at <= ? AND q.consumer IN (` + placeholders + `)
          AND NOT EXISTS (
            SELECT 1 FROM ipn_outbox previous
            WHERE previous.sequence_key = q.sequence_key AND previous.consumer = q.consumer
              AND (previous.created_at < q.created_at
                OR (previous.created_at = q.created_at AND previous.rowid < q.rowid))
              AND previous.status <> 'delivered'
          )
        ORDER BY q.next_attempt_at, q.created_at, q.rowid LIMIT 1`
	var event IPNEvent
	var rowID int64
	var payload string
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&rowID, &event.ID, &event.OrderID, &event.SequenceKey,
		&event.Consumer, &event.TargetURL, &event.EventType, &payload, &event.Attempts, &event.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IPNEvent{}, false, nil
		}
		return IPNEvent{}, false, fmt.Errorf("select IPN claim: %w", err)
	}
	result, err := tx.ExecContext(ctx, "UPDATE ipn_outbox SET status = 'failed', attempts = attempts + 1 WHERE rowid = ? AND status = 'pending'", rowID)
	if err != nil {
		return IPNEvent{}, false, fmt.Errorf("claim IPN %s: %w", event.ID, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return IPNEvent{}, false, err
	}
	if changed != 1 {
		return IPNEvent{}, false, tx.Commit()
	}
	if err := tx.Commit(); err != nil {
		return IPNEvent{}, false, fmt.Errorf("commit IPN claim: %w", err)
	}
	event.Payload = json.RawMessage(payload)
	event.Attempts++
	return event, true, nil
}

// RecoverIPNClaims makes crash-interrupted deliveries eligible again; duplicate delivery keeps the same event ID (IPN-022).
func (s *Store) RecoverIPNClaims(ctx context.Context) error {
	_, err := s.normal.ExecContext(ctx, "UPDATE ipn_outbox SET status = 'pending' WHERE status = 'failed'")
	return err
}

func (s *Store) DeliverIPN(ctx context.Context, id string, statusCode int, now time.Time) error {
	_, err := s.normal.ExecContext(ctx, `UPDATE ipn_outbox SET status = 'delivered', delivered_at = ?,
        last_error = NULL, last_status_code = ? WHERE id = ? AND status = 'failed'`, now.UTC().Unix(), statusCode, id)
	if err == nil {
		s.notifyOutbox()
	}
	return err
}

func (s *Store) FailIPN(ctx context.Context, id, message string, statusCode *int, dead bool, nextAttempt time.Time) error {
	status := "pending"
	if dead {
		status = "dead"
	}
	var code any
	if statusCode != nil {
		code = *statusCode
	}
	_, err := s.normal.ExecContext(ctx, `UPDATE ipn_outbox SET status = ?, next_attempt_at = ?,
        last_error = ?, last_status_code = ? WHERE id = ? AND status = 'failed'`,
		status, nextAttempt.UTC().Unix(), message, code, id)
	if err == nil && !dead {
		s.notifyOutbox()
	}
	return err
}

// RetryIPN resets one dead row for explicit manual redelivery (IPN-010).
func (s *Store) RetryIPN(ctx context.Context, id string, now time.Time) (bool, error) {
	result, err := s.normal.ExecContext(ctx, `UPDATE ipn_outbox SET status = 'pending', attempts = 0,
        next_attempt_at = ?, last_error = NULL, last_status_code = NULL, delivered_at = NULL
        WHERE id = ? AND status = 'dead'`, now.UTC().Unix(), id)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err == nil && changed == 1 {
		s.notifyOutbox()
	}
	return changed == 1, err
}

// IPNCurrentStatus reads only the mutable status field added at send time (IPN-021a).
func (s *Store) IPNCurrentStatus(ctx context.Context, event IPNEvent) (string, error) {
	var status string
	if event.OrderID != "" {
		err := s.normal.QueryRowContext(ctx, "SELECT status FROM orders WHERE id = ?", event.OrderID).Scan(&status)
		return status, err
	}
	if id, ok := strings.CutPrefix(event.SequenceKey, "withdrawal:"); ok {
		err := s.normal.QueryRowContext(ctx, "SELECT status FROM withdrawals WHERE id = ?", id).Scan(&status)
		if err == nil || !errors.Is(err, sql.ErrNoRows) {
			return status, err
		}
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &snapshot); err != nil {
		return "", err
	}
	if raw, ok := snapshot["status"]; ok {
		_ = json.Unmarshal(raw, &status)
	}
	return status, nil
}

func (w *BlockWrite) PaymentExists(txID string, logIndex int) (bool, error) {
	var exists int
	err := w.tx.QueryRow("SELECT 1 FROM payments WHERE txid = ? AND log_index = ?", txID, logIndex).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (w *BlockWrite) OrderStatus(id string) (string, error) {
	var status string
	err := w.tx.QueryRow("SELECT status FROM orders WHERE id = ?", id).Scan(&status)
	return status, err
}

func (w *BlockWrite) OrderMetadata(id string) (json.RawMessage, error) {
	var metadata string
	err := w.tx.QueryRow("SELECT COALESCE(metadata, '{}') FROM orders WHERE id = ?", id).Scan(&metadata)
	return json.RawMessage(metadata), err
}

func (w *BlockWrite) EnqueueOrderEvent(events EventConfig, orderID, eventType string, payload map[string]any, now time.Time) error {
	return enqueueOrderEvent(w.tx, events, orderID, eventType, payload, now)
}

func (w *BlockWrite) EnqueueGlobalEvent(events EventConfig, sequenceKey, eventType string, payload map[string]any, now time.Time) error {
	return enqueueGlobalEvent(w.tx, events, sequenceKey, eventType, payload, now)
}

func enqueueOrderEvent(tx *sql.Tx, events EventConfig, orderID, eventType string, payload map[string]any, now time.Time) error {
	var consumer string
	if err := tx.QueryRow("SELECT COALESCE(consumer, '') FROM orders WHERE id = ?", orderID).Scan(&consumer); err != nil {
		return err
	}
	if consumer == "" {
		consumer = events.DefaultConsumer
	}
	target, found := events.Consumers[consumer]
	status, lastError := "pending", any(nil)
	if !found || !target.Enabled {
		status, lastError = "dead", "consumer removed" // IPN-002a
	}
	return insertOutbox(tx, orderID, "order:"+orderID, consumer, target.URL, eventType, payload, status, lastError, now)
}

func enqueueGlobalEvent(tx *sql.Tx, events EventConfig, sequenceKey, eventType string, payload map[string]any, now time.Time) error {
	for _, target := range events.Consumers {
		if !target.Enabled || !target.ReceivesGlobal {
			continue
		}
		if err := insertOutbox(tx, "", sequenceKey, target.Name, target.URL, eventType, payload, "pending", nil, now); err != nil {
			return err
		}
	}
	return nil
}

func insertOutbox(tx *sql.Tx, orderID, sequenceKey, consumer, targetURL, eventType string, payload map[string]any, status string, lastError any, now time.Time) error {
	if sequenceKey == "" {
		return errors.New("IPN sequence_key must not be empty (IPN-011)")
	}
	id, err := newULID(now)
	if err != nil {
		return err
	}
	snapshot := make(map[string]any, len(payload)+4)
	for key, value := range payload {
		snapshot[key] = value
	}
	delete(snapshot, "current_status")
	delete(snapshot, "snapshot_age_seconds")
	snapshot["event_id"], snapshot["event_type"] = id, eventType
	snapshot["occurred_at"], snapshot["consumer"] = now.UTC().Unix(), consumer
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	var nullableOrder any
	if orderID != "" {
		nullableOrder = orderID
	}
	_, err = tx.Exec(`INSERT INTO ipn_outbox(id, order_id, sequence_key, consumer, target_url, event_type,
        payload, status, next_attempt_at, last_error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nullableOrder, sequenceKey, consumer, targetURL, eventType, string(raw), status, now.UTC().Unix(), lastError, now.UTC().Unix())
	return err
}

func FormatUnits(raw string, decimals int) (string, error) {
	amount, ok := new(big.Int).SetString(raw, 10)
	if !ok || amount.Sign() < 0 || decimals < 0 || decimals > 255 {
		return "", fmt.Errorf("invalid base-unit amount %q", raw)
	}
	return hdwallet.FormatUnits(amount, uint8(decimals)), nil // IPN-020
}

func ParseUnits(value string, decimals int) (*big.Int, error) {
	if decimals < 0 || decimals > 255 {
		return nil, errors.New("invalid asset decimals")
	}
	if value == "" {
		return new(big.Int), nil
	}
	whole, fraction, _ := strings.Cut(value, ".")
	if len(fraction) > decimals {
		return nil, errors.New("has more fractional digits than asset decimals")
	}
	digits := whole + fraction + strings.Repeat("0", decimals-len(fraction))
	amount, ok := new(big.Int).SetString(digits, 10)
	if !ok || amount.Sign() < 0 {
		return nil, errors.New("invalid decimal amount")
	}
	return amount, nil
}
