package store

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"time"
)

type TronGridDailyRequests struct {
	DayStart int64
	Requests int64
}

// RecordTronGridRequest persists every attempted request against its UTC day
// so restarts cannot erase RL-006's historical trend.
func (s *Store) RecordTronGridRequest(ctx context.Context, at time.Time) (int64, error) {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	var requests int64
	err := s.normal.QueryRowContext(ctx, `INSERT INTO trongrid_daily_requests(day_start,requests,updated_at)
		VALUES (?,1,?) ON CONFLICT(day_start) DO UPDATE SET requests=requests+1,updated_at=excluded.updated_at
		RETURNING requests`, day, at.UTC().Unix()).Scan(&requests)
	return requests, err
}

func (s *Store) TronGridRequestHistory(ctx context.Context, now time.Time) ([]TronGridDailyRequests, error) {
	today := now.UTC().Truncate(24 * time.Hour)
	rows, err := s.normal.QueryContext(ctx, `SELECT day_start,requests FROM trongrid_daily_requests
		WHERE day_start>=? AND day_start<=? ORDER BY day_start`, today.Add(-7*24*time.Hour).Unix(), today.Unix())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var history []TronGridDailyRequests
	for rows.Next() {
		var day TronGridDailyRequests
		if err := rows.Scan(&day.DayStart, &day.Requests); err != nil {
			return nil, err
		}
		history = append(history, day)
	}
	return history, rows.Err()
}

type OperationalStatus struct {
	LastHeight           int64
	SolidifiedHeight     int64
	SolidifiedUpdatedAt  int64
	OldestPriceFetchedAt int64
	HasPrice             bool
}

type OperationalMetrics struct {
	Orders             map[string]uint64 `json:"orders"`
	Payments           map[string]uint64 `json:"payments"`
	OrphanedUnresolved uint64            `json:"orphaned_unresolved"`
	IPNDead            map[string]uint64 `json:"ipn_dead"`
	IPNQueue           map[string]uint64 `json:"ipn_queue"`
	Withdrawals        map[string]uint64 `json:"withdrawals"`
	NeedsOperator      uint64            `json:"needs_operator"`
	AddressesNeeding   uint64            `json:"addresses_needing_resources"`
	AddressesFunded    uint64            `json:"addresses_funded"`
	BalanceDrift       uint64            `json:"balance_drift"`
	EnergyPurchases    map[string]uint64 `json:"energy_purchases"`
	EnergyCosts        map[string]string `json:"energy_costs"`
	EnergyFeeSun       int64             `json:"energy_fee_sun"`
	ProviderBalanceTRX string            `json:"provider_balance_trx"`
}

// OperationalStatus supplies readiness state without exposing a database
// handle outside internal/store (ARC-005, OPS-001).
func (s *Store) OperationalStatus(ctx context.Context) (OperationalStatus, error) {
	var status OperationalStatus
	if err := s.normal.QueryRowContext(ctx, `SELECT last_height, solidified_height,
		solidified_updated_at FROM crawler_state WHERE id=1`).
		Scan(&status.LastHeight, &status.SolidifiedHeight, &status.SolidifiedUpdatedAt); err != nil {
		return OperationalStatus{}, err
	}
	var oldest sql.NullInt64
	if err := s.normal.QueryRowContext(ctx, "SELECT MIN(fetched_at) FROM prices").Scan(&oldest); err != nil {
		return OperationalStatus{}, err
	}
	status.HasPrice, status.OldestPriceFetchedAt = oldest.Valid, oldest.Int64
	return status, nil
}

// Writable opens and rolls back a harmless write transaction. A read-only or
// full filesystem therefore fails readiness before a real state change does.
func (s *Store) Writable(ctx context.Context) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, "UPDATE crawler_state SET updated_at=updated_at WHERE id=1")
	return err
}

func (s *Store) OperationalMetrics(ctx context.Context) (OperationalMetrics, error) {
	metrics := OperationalMetrics{
		Orders: make(map[string]uint64), Payments: make(map[string]uint64),
		IPNDead: make(map[string]uint64), IPNQueue: make(map[string]uint64),
		Withdrawals: make(map[string]uint64), EnergyPurchases: make(map[string]uint64),
		EnergyCosts: make(map[string]string),
	}
	if err := groupedCounts(ctx, s.normal, "SELECT status, COUNT(*) FROM orders GROUP BY status", metrics.Orders); err != nil {
		return metrics, err
	}
	if err := groupedCounts(ctx, s.normal, "SELECT status, COUNT(*) FROM payments GROUP BY status", metrics.Payments); err != nil {
		return metrics, err
	}
	metrics.OrphanedUnresolved = metrics.Payments["orphaned"]
	if err := groupedCounts(ctx, s.normal, "SELECT consumer, COUNT(*) FROM ipn_outbox WHERE status='dead' GROUP BY consumer", metrics.IPNDead); err != nil {
		return metrics, err
	}
	if err := groupedCounts(ctx, s.normal, "SELECT consumer, COUNT(*) FROM ipn_outbox WHERE status IN ('pending','failed') GROUP BY consumer", metrics.IPNQueue); err != nil {
		return metrics, err
	}
	if err := groupedCounts(ctx, s.normal, "SELECT status, COUNT(*) FROM withdrawals GROUP BY status", metrics.Withdrawals); err != nil {
		return metrics, err
	}
	metrics.NeedsOperator = metrics.Withdrawals["needs_operator"]
	if err := s.normal.QueryRowContext(ctx, "SELECT COUNT(*) FROM addresses WHERE needs_resources=1").Scan(&metrics.AddressesNeeding); err != nil {
		return metrics, err
	}
	if err := s.normal.QueryRowContext(ctx, "SELECT COUNT(DISTINCT address_id) FROM balances WHERE drift_detected=1").Scan(&metrics.BalanceDrift); err != nil {
		return metrics, err
	}
	if err := groupedCounts(ctx, s.normal, "SELECT status, COUNT(*) FROM energy_purchases GROUP BY status", metrics.EnergyPurchases); err != nil {
		return metrics, err
	}
	if err := s.fundedAddressCount(ctx, &metrics); err != nil {
		return metrics, err
	}
	if err := s.energyCostTotals(ctx, &metrics); err != nil {
		return metrics, err
	}
	_ = s.normal.QueryRowContext(ctx, "SELECT value FROM chain_params WHERE name='getEnergyFee'").Scan(&metrics.EnergyFeeSun)
	_ = s.normal.QueryRowContext(ctx, "SELECT balance_trx FROM energy_provider_state ORDER BY provider LIMIT 1").Scan(&metrics.ProviderBalanceTRX)
	return metrics, nil
}

func groupedCounts(ctx context.Context, database *sql.DB, query string, destination map[string]uint64) error {
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var label string
		var count uint64
		if err := rows.Scan(&label, &count); err != nil {
			return err
		}
		destination[label] = count
	}
	return rows.Err()
}

func (s *Store) fundedAddressCount(ctx context.Context, metrics *OperationalMetrics) error {
	rows, err := s.normal.QueryContext(ctx, "SELECT address_id, confirmed_raw, pending_raw FROM balances ORDER BY address_id")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	funded := make(map[int64]struct{})
	for rows.Next() {
		var addressID int64
		var confirmed, pending string
		if err := rows.Scan(&addressID, &confirmed, &pending); err != nil {
			return err
		}
		confirmedRaw, confirmedOK := new(big.Int).SetString(confirmed, 10)
		pendingRaw, pendingOK := new(big.Int).SetString(pending, 10)
		if !confirmedOK || !pendingOK {
			return fmt.Errorf("invalid stored balance for address %d", addressID)
		}
		if confirmedRaw.Sign() != 0 || pendingRaw.Sign() != 0 {
			funded[addressID] = struct{}{}
		}
	}
	metrics.AddressesFunded = uint64(len(funded))
	return rows.Err()
}

func (s *Store) energyCostTotals(ctx context.Context, metrics *OperationalMetrics) error {
	rows, err := s.normal.QueryContext(ctx, `SELECT energy_source, energy_cost_trx FROM withdrawals
		WHERE energy_source IS NOT NULL AND energy_cost_trx IS NOT NULL`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	totals := make(map[string]*big.Rat)
	for rows.Next() {
		var source, text string
		if err := rows.Scan(&source, &text); err != nil {
			return err
		}
		value, ok := new(big.Rat).SetString(text)
		if !ok {
			return fmt.Errorf("invalid stored energy cost %q", text)
		}
		if totals[source] == nil {
			totals[source] = new(big.Rat)
		}
		totals[source].Add(totals[source], value)
	}
	for source, total := range totals {
		metrics.EnergyCosts[source] = rationalDecimal(total)
	}
	return rows.Err()
}
