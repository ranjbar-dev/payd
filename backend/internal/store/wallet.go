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

var (
	ErrBalanceDrift        = errors.New("balance drift detected")
	ErrBalanceDriftChanged = errors.New("balance drift acknowledgement does not match")
)

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
	// CoolingUntil is set while the address is in cooldown (POOL-004); AssignedOrderID
	// survives until POOL-005 returns the address to the pool, so the two together say
	// which order still holds an address and for how much longer.
	CoolingUntil    *int64
	AssignedOrderID *string
	Balances        []WalletBalance
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
	return s.walletAddressPage(ctx, WalletFilter{NeedsResources: needsResourcesOnly}, 0, 0)
}

// WalletFilter narrows a wallet page server-side. Filtering these in the client
// would apply to the loaded cursor page only and misrepresent the pool's real
// composition, which is the same defect DAT-020 forbids for every other list.
type WalletFilter struct {
	NeedsResources bool
	ConfirmedOnly  bool
	// State is one of free, assigned, cooling, disabled. Empty means every state.
	State string
	// Asset restricts to addresses holding a balance row for that asset, whether
	// confirmed or pending — an address with pending funds in an asset is still an
	// address that holds it.
	Asset string
	// DriftOnly restricts to addresses where at least one asset disagrees with the
	// chain. Those cannot be withdrawn from at all (WDR-002a).
	DriftOnly bool
}

// WalletAddressPage bounds API reads by address rather than joined balance rows (API-025).
func (s *Store) WalletAddressPage(ctx context.Context, needsResourcesOnly bool, after int64, limit int) ([]WalletAddress, error) {
	return s.walletAddressPage(ctx, WalletFilter{NeedsResources: needsResourcesOnly}, after, limit)
}

// WalletAddressPageFiltered is WalletAddressPage with the API-014 filters applied.
func (s *Store) WalletAddressPageFiltered(ctx context.Context, filter WalletFilter, after int64, limit int) ([]WalletAddress, error) {
	return s.walletAddressPage(ctx, filter, after, limit)
}

func (s *Store) walletAddressPage(ctx context.Context, filter WalletFilter, after int64, limit int) ([]WalletAddress, error) {
	selector := "SELECT a.id FROM addresses a WHERE a.id > ?"
	args := []any{after}
	if filter.NeedsResources {
		selector += " AND a.needs_resources = 1"
	}
	if filter.ConfirmedOnly {
		selector += " AND EXISTS (SELECT 1 FROM balances funded WHERE funded.address_id = a.id AND funded.confirmed_raw <> '0')"
	}
	if filter.State != "" {
		selector += " AND a.state = ?"
		args = append(args, filter.State)
	}
	if filter.Asset != "" {
		selector += " AND EXISTS (SELECT 1 FROM balances held WHERE held.address_id = a.id AND held.asset = ?)"
		args = append(args, filter.Asset)
	}
	if filter.DriftOnly {
		selector += " AND EXISTS (SELECT 1 FROM balances drifted WHERE drifted.address_id = a.id AND drifted.drift_detected = 1)"
	}
	selector += " ORDER BY a.id"
	if limit > 0 {
		selector += " LIMIT ?"
		args = append(args, limit)
	}
	query := `SELECT a.id, a.hd_index, a.address, a.state, a.energy_limit, a.energy_used,
        a.bandwidth_limit, a.bandwidth_used, a.needs_resources, a.resources_checked_at,
        a.cooling_until, a.assigned_order_id,
        b.asset, b.confirmed_raw, b.pending_raw, b.chain_raw, b.drift_detected
		FROM addresses a JOIN (` + selector + `) page ON page.id = a.id
		LEFT JOIN balances b ON b.address_id = a.id`
	return s.walletAddresses(ctx, query, args)
}

func (s *Store) WalletAddress(ctx context.Context, address string) (WalletAddress, error) {
	query := `SELECT a.id, a.hd_index, a.address, a.state, a.energy_limit, a.energy_used,
        a.bandwidth_limit, a.bandwidth_used, a.needs_resources, a.resources_checked_at,
        a.cooling_until, a.assigned_order_id,
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
	return s.walletAddressPage(ctx, WalletFilter{ConfirmedOnly: true}, 0, 0)
}

// WalletAddressesWithConfirmedBalancePage returns a bounded withdrawal-source page (WDR-005, API-025).
func (s *Store) WalletAddressesWithConfirmedBalancePage(ctx context.Context, after int64, limit int) ([]WalletAddress, error) {
	return s.walletAddressPage(ctx, WalletFilter{ConfirmedOnly: true}, after, limit)
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
		var checked, cooling sql.NullInt64
		var asset, confirmed, pending, chain, assignedOrder sql.NullString
		var drift sql.NullBool
		if err := rows.Scan(&item.ID, &item.HDIndex, &item.Address, &item.State, &item.EnergyLimit,
			&item.EnergyUsed, &item.BandwidthLimit, &item.BandwidthUsed, &item.NeedsResources,
			&checked, &cooling, &assignedOrder, &asset, &confirmed, &pending, &chain, &drift); err != nil {
			return nil, fmt.Errorf("scan wallet address: %w", err)
		}
		if current == nil || current.ID != item.ID {
			if cooling.Valid {
				item.CoolingUntil = &cooling.Int64
			}
			if assignedOrder.Valid {
				item.AssignedOrderID = &assignedOrder.String
			}
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

func (s *Store) ClearBalanceDrift(ctx context.Context, address, asset, expectedChainRaw, actor, ip string, now time.Time) error {
	chain, ok := new(big.Int).SetString(expectedChainRaw, 10)
	if !ok || chain.Sign() < 0 {
		return ErrBalanceDriftChanged
	}
	expectedChainRaw = chain.String()
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
	// BAL-002: acknowledge one exact reconciled asset value; another reconcile cannot be cleared by a stale review.
	result, err := tx.ExecContext(ctx, `UPDATE balances SET drift_detected = 0
		WHERE address_id = ? AND asset = ? AND chain_raw = ? AND drift_detected = 1`, addressID, asset, expectedChainRaw)
	if err != nil {
		return fmt.Errorf("clear balance drift: %w", err)
	}
	cleared, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if cleared != 1 {
		return ErrBalanceDriftChanged
	}
	detail, _ := json.Marshal(map[string]string{"asset": asset, "chain_raw": expectedChainRaw})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor, action, subject, detail, ip, created_at)
        VALUES (?, 'balance.clear_drift', ?, ?, ?, ?)`, actor, address, string(detail), ip, now.UTC().Unix()); err != nil {
		return fmt.Errorf("audit balance drift clear: %w", err)
	}
	return tx.Commit()
}
