// Package matcher attributes decoded payments to the address's active order.
package matcher

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"payd/internal/store"
)

type Matcher struct {
	mu     sync.RWMutex
	events store.EventConfig
}

type PaymentWriter interface {
	PaymentExists(string, int) (bool, error)
	AttributionOrder(int64) (store.AttributionOrder, bool, error)
	UpsertPayment(store.PaymentRecord) (bool, error)
	EnqueueGlobalEvent(store.EventConfig, string, string, map[string]any, time.Time) error
	EnqueueOrderEvent(store.EventConfig, string, string, map[string]any, time.Time) error
	OrderMetadata(string) (json.RawMessage, error)
	OrderStatus(string) (string, error)
}

func New(events ...store.EventConfig) *Matcher {
	var configured store.EventConfig
	if len(events) > 0 {
		configured = events[0]
	}
	return &Matcher{events: configured}
}

func (m *Matcher) UpdateEvents(events store.EventConfig) {
	m.mu.Lock()
	m.events = events
	m.mu.Unlock()
}

// Match applies ORD-002/002a/002b and writes the payment in the follower's existing block transaction.
func (m *Matcher) Match(write PaymentWriter, payment store.PaymentRecord) error {
	m.mu.RLock()
	events := m.events
	m.mu.RUnlock()
	existed, err := write.PaymentExists(payment.TxID, payment.LogIndex)
	if err != nil {
		return err
	}
	var orderID, oldStatus string
	if payment.Direction == "in" && payment.AddressID != nil {
		order, found, err := write.AttributionOrder(*payment.AddressID)
		if err != nil {
			return err
		}
		active := found && (order.Status == "pending" || order.Status == "partial" || order.Status == "paid")
		assetMatches := found && order.Asset == payment.Asset // ORD-002a / F-1
		insideWindow := found && payment.BlockTimestamp >= order.CreatedAt &&
			(order.ReleasedAt == nil || payment.BlockTimestamp < *order.ReleasedAt) // ORD-002b / F-7: chain time, never detected_at.
		if active && assetMatches && insideWindow {
			payment.OrderID = &order.ID
			orderID, oldStatus = order.ID, order.Status
		} else {
			payment.OrderID = nil
			payment.Status = "unattributed" // ORD-020
		}
	}
	reactivated, err := write.UpsertPayment(payment)
	if err != nil || existed && !reactivated || payment.IsDust {
		return err // DET-007: dust changes totals and balances, but emits no IPN by itself.
	}
	amount, err := store.FormatUnits(payment.AmountRaw, events.Decimals[payment.Asset])
	if err != nil {
		return err
	}
	payload := map[string]any{"payment": map[string]any{
		"txid": payment.TxID, "log_index": payment.LogIndex, "asset": payment.Asset, "amount": amount,
		"from": payment.FromAddress, "to": payment.ToAddress, "block_height": payment.BlockHeight,
	}}
	now := time.Unix(payment.DetectedAt, 0).UTC()
	if orderID == "" {
		if payment.Status == "unattributed" && payment.AddressID != nil {
			return write.EnqueueGlobalEvent(events, fmt.Sprintf("address:%d", *payment.AddressID), "payment.unattributed", payload, now) // ORD-023
		}
		return nil
	}
	payload["order_id"] = orderID
	metadata, err := write.OrderMetadata(orderID)
	if err != nil {
		return err
	}
	payload["metadata"] = metadata // ORD-010
	if err := write.EnqueueOrderEvent(events, orderID, "order.payment_seen", payload, now); err != nil {
		return err
	}
	newStatus, err := write.OrderStatus(orderID)
	if err != nil || newStatus == oldStatus {
		return err
	}
	if newStatus == "partial" || newStatus == "paid" {
		return write.EnqueueOrderEvent(events, orderID, "order."+newStatus, payload, now)
	}
	return nil
}
