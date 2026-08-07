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

	"payd/internal/config"
)

type EventConsumer struct {
	Name           string
	URL            string
	Enabled        bool
	ReceivesGlobal bool
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
	id, err := newULID(now)
	if err != nil {
		return err
	}
	snapshot := make(map[string]any, len(payload)+3)
	for key, value := range payload {
		snapshot[key] = value
	}
	snapshot["event_type"], snapshot["occurred_at"], snapshot["consumer"] = eventType, now.UTC().Unix(), consumer
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
	if !ok || amount.Sign() < 0 {
		return "", fmt.Errorf("invalid base-unit amount %q", raw)
	}
	if decimals == 0 {
		return amount.String(), nil
	}
	digits := amount.String()
	if len(digits) <= decimals {
		digits = strings.Repeat("0", decimals-len(digits)+1) + digits
	}
	point := len(digits) - decimals
	return digits[:point] + "." + digits[point:], nil
}
