package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

var ErrBalanceDrift = errors.New("balance drift detected")

type WalletBalance struct {
	Asset        string
	ConfirmedRaw string
	PendingRaw   string
	ChainRaw     *string
	Drift        bool
}

type WalletAddress struct {
	ID                 int64
	HDIndex            uint32
	Address            string
	State              string
	EnergyLimit        int64
	EnergyUsed         int64
	BandwidthLimit     int64
	BandwidthUsed      int64
	NeedsResources     bool
	ResourcesCheckedAt *int64
	Balances           []WalletBalance
}

type ResourceReading struct {
	AddressID      int64
	EnergyLimit    int64
	EnergyUsed     int64
	BandwidthLimit int64
	BandwidthUsed  int64
	NeedsResources bool
}

type BalanceTarget struct {
	AddressID int64
	Address   string
	Asset     string
}

type ChainBalance struct {
	AddressID int64
	Address   string
	Asset     string
	Raw       string
}

// WalletAddresses is the common read model for monitoring and the wallet API.
func (s *Store) WalletAddresses(ctx context.Context, needsResourcesOnly bool) ([]WalletAddress, error) {
	query := `SELECT a.id, a.hd_index, a.address, a.state, a.energy_limit, a.energy_used,
        a.bandwidth_limit, a.bandwidth_used, a.needs_resources, a.resources_checked_at,
        b.asset, b.confirmed_raw, b.pending_raw, b.chain_raw, b.drift_detected
        FROM addresses a LEFT JOIN balances b ON b.address_id = a.id`
	args := []any{}
	if needsResourcesOnly {
		query += " WHERE a.needs_resources = 1"
	}
	return s.walletAddresses(ctx, query, args)
}

func (s *Store) WalletAddress(ctx context.Context, address string) (WalletAddress, error) {
	query := `SELECT a.id, a.hd_index, a.address, a.state, a.energy_limit, a.energy_used,
        a.bandwidth_limit, a.bandwidth_used, a.needs_resources, a.resources_checked_at,
        b.asset, b.confirmed_raw, b.pending_raw, b.chain_raw, b.drift_detected
        FROM addresses a LEFT JOIN balances b ON b.address_id = a.id WHERE a.address = ?`
	addresses, err := s.walletAddresses(ctx, query, []any{address})
	if err != nil {
		return WalletAddress{}, err
	}
	if len(addresses) == 0 {
		return WalletAddress{}, ErrAddressNotFound
	}
	return addresses[0], nil
}

// WalletAddressesWithConfirmedBalance returns withdrawal sources only; pending funds are never spendable (WDR-005).
func (s *Store) WalletAddressesWithConfirmedBalance(ctx context.Context) ([]WalletAddress, error) {
	addresses, err := s.WalletAddresses(ctx, false)
	if err != nil {
		return nil, err
	}
	funded := addresses[:0]
	for _, address := range addresses {
		for _, balance := range address.Balances {
			amount, ok := new(big.Int).SetString(balance.ConfirmedRaw, 10)
			if !ok {
				return nil, fmt.Errorf("invalid stored confirmed balance %q for address %d", balance.ConfirmedRaw, address.ID)
			}
			if amount.Sign() != 0 {
				funded = append(funded, address)
				break
			}
		}
	}
	return funded, nil
}

func (s *Store) walletAddresses(ctx context.Context, query string, args []any) ([]WalletAddress, error) {
	query += " ORDER BY a.id, b.asset"
	rows, err := s.normal.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list wallet addresses: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result []WalletAddress
	var current *WalletAddress
	for rows.Next() {
		var item WalletAddress
		var checked sql.NullInt64
		var asset, confirmed, pending, chain sql.NullString
		var drift sql.NullBool
		if err := rows.Scan(&item.ID, &item.HDIndex, &item.Address, &item.State, &item.EnergyLimit,
			&item.EnergyUsed, &item.BandwidthLimit, &item.BandwidthUsed, &item.NeedsResources,
			&checked, &asset, &confirmed, &pending, &chain, &drift); err != nil {
			return nil, fmt.Errorf("scan wallet address: %w", err)
		}
		if current == nil || current.ID != item.ID {
			if checked.Valid {
				item.ResourcesCheckedAt = &checked.Int64
			}
			result = append(result, item)
			current = &result[len(result)-1]
		}
		if asset.Valid {
			balance := WalletBalance{Asset: asset.String, ConfirmedRaw: confirmed.String, PendingRaw: pending.String, Drift: drift.Bool}
			if chain.Valid {
				balance.ChainRaw = &chain.String
			}
			current.Balances = append(current.Balances, balance)
		}
	}
	return result, rows.Err()
}

func (s *Store) UpdateAddressResources(ctx context.Context, reading ResourceReading, checkedAt time.Time) error {
	result, err := s.normal.ExecContext(ctx, `UPDATE addresses SET energy_limit = ?, energy_used = ?,
        bandwidth_limit = ?, bandwidth_used = ?, needs_resources = ?, resources_checked_at = ? WHERE id = ?`,
		reading.EnergyLimit, reading.EnergyUsed, reading.BandwidthLimit, reading.BandwidthUsed,
		reading.NeedsResources, checkedAt.UTC().Unix(), reading.AddressID)
	if err != nil {
		return fmt.Errorf("update address resources: %w", err)
	}
	changed, err := result.RowsAffected()
	if err == nil && changed != 1 {
		return ErrAddressNotFound
	}
	return err
}

func (s *Store) BalanceTargets(ctx context.Context, dueBefore time.Time) ([]BalanceTarget, error) {
	rows, err := s.normal.QueryContext(ctx, `SELECT b.address_id, a.address, b.asset
		FROM balances b JOIN addresses a ON a.id = b.address_id
		WHERE b.last_verified_at IS NULL OR b.last_verified_at <= ? ORDER BY b.address_id, b.asset`, dueBefore.UTC().Unix())
	if err != nil {
		return nil, fmt.Errorf("list balance targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var targets []BalanceTarget
	for rows.Next() {
		var target BalanceTarget
		if err := rows.Scan(&target.AddressID, &target.Address, &target.Asset); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

// ReconcileChainBalances is the sole chain_raw writer (BAL-001, RL-003).
func (s *Store) ReconcileChainBalances(ctx context.Context, readings []ChainBalance, events EventConfig, now time.Time) error {
	for index := range readings {
		amount, ok := new(big.Int).SetString(readings[index].Raw, 10)
		if !ok || amount.Sign() < 0 {
			return fmt.Errorf("invalid chain balance %q", readings[index].Raw)
		}
		readings[index].Raw = amount.String()
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin balance reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, reading := range readings {
		var confirmed string
		var drift bool
		if err := tx.QueryRowContext(ctx, `SELECT confirmed_raw, drift_detected FROM balances
            WHERE address_id = ? AND asset = ?`, reading.AddressID, reading.Asset).Scan(&confirmed, &drift); err != nil {
			return fmt.Errorf("load reconciled balance: %w", err)
		}
		ledger, ok := new(big.Int).SetString(confirmed, 10)
		if !ok {
			return fmt.Errorf("invalid ledger balance %q", confirmed)
		}
		chain, _ := new(big.Int).SetString(reading.Raw, 10)
		mismatch := ledger.Cmp(chain) != 0
		if _, err := tx.ExecContext(ctx, `UPDATE balances SET chain_raw = ?, last_verified_at = ?,
            drift_detected = CASE WHEN drift_detected = 1 OR ? THEN 1 ELSE 0 END
            WHERE address_id = ? AND asset = ?`, reading.Raw, now.UTC().Unix(), mismatch, reading.AddressID, reading.Asset); err != nil {
			return fmt.Errorf("store reconciled balance: %w", err)
		}
		if mismatch && !drift {
			decimals := events.Decimals[reading.Asset]
			ledgerAmount, err := FormatUnits(ledger.String(), decimals)
			if err != nil {
				return err
			}
			chainAmount, err := FormatUnits(chain.String(), decimals)
			if err != nil {
				return err
			}
			if err := enqueueGlobalEvent(tx, events, fmt.Sprintf("address:%d", reading.AddressID), "balance.drift_detected", map[string]any{
				"address": reading.Address, "asset": reading.Asset, "ledger": ledgerAmount, "chain": chainAmount,
			}, now); err != nil {
				return fmt.Errorf("enqueue balance drift: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit balance reconciliation: %w", err)
	}
	s.notifyOutbox()
	return nil
}

// BalanceForWithdrawal is the BAL-002 validation boundary used by P11.
func (s *Store) BalanceForWithdrawal(ctx context.Context, address, asset string) (Balance, error) {
	var addressID int64
	var balance Balance
	var chain sql.NullString
	err := s.normal.QueryRowContext(ctx, `SELECT a.id, b.confirmed_raw, b.pending_raw, b.chain_raw, b.drift_detected
        FROM addresses a JOIN balances b ON b.address_id = a.id WHERE a.address = ? AND b.asset = ?`, address, asset).
		Scan(&addressID, &balance.ConfirmedRaw, &balance.PendingRaw, &chain, &balance.Drift)
	if err != nil {
		return Balance{}, err
	}
	if balance.Drift {
		return Balance{}, ErrBalanceDrift
	}
	if chain.Valid {
		balance.ChainRaw = &chain.String
	}
	return balance, nil
}

func (s *Store) ClearBalanceDrift(ctx context.Context, address, actor, ip string, now time.Time) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clear balance drift: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var addressID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM addresses WHERE address = ?", address).Scan(&addressID); errors.Is(err, sql.ErrNoRows) {
		return ErrAddressNotFound
	} else if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE balances SET drift_detected = 0 WHERE address_id = ? AND drift_detected = 1", addressID)
	if err != nil {
		return fmt.Errorf("clear balance drift: %w", err)
	}
	cleared, err := result.RowsAffected()
	if err != nil {
		return err
	}
	detail, _ := json.Marshal(map[string]int64{"balances_cleared": cleared})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor, action, subject, detail, ip, created_at)
        VALUES (?, 'balance.clear_drift', ?, ?, ?, ?)`, actor, address, string(detail), ip, now.UTC().Unix()); err != nil {
		return fmt.Errorf("audit balance drift clear: %w", err)
	}
	return tx.Commit()
}
