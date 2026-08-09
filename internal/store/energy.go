package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type EnergyPurchase struct {
	ID, Provider, ProviderOrderID, WithdrawalID, ReceiverAddress string
	ResourceType, QuotedTRX, ActualTRX, Status                   string
	DelegationTxID, FailureReason                                string
	AddressID, Amount, DurationSeconds, CreatedAt                int64
	DelegatedAt                                                  *int64
}

const energyPurchaseSelect = `SELECT id,provider,COALESCE(provider_order_id,''),COALESCE(withdrawal_id,''),address_id,
	receiver_address,resource_type,energy_amount,duration_seconds,quoted_trx,COALESCE(actual_trx,''),status,
	COALESCE(delegation_txid,''),COALESCE(failure_reason,''),created_at,delegated_at FROM energy_purchases`

func scanEnergyPurchase(row rowScanner) (EnergyPurchase, error) {
	var purchase EnergyPurchase
	var delegated sql.NullInt64
	err := row.Scan(&purchase.ID, &purchase.Provider, &purchase.ProviderOrderID, &purchase.WithdrawalID,
		&purchase.AddressID, &purchase.ReceiverAddress, &purchase.ResourceType, &purchase.Amount,
		&purchase.DurationSeconds, &purchase.QuotedTRX, &purchase.ActualTRX, &purchase.Status,
		&purchase.DelegationTxID, &purchase.FailureReason, &purchase.CreatedAt, &delegated)
	if delegated.Valid {
		purchase.DelegatedAt = &delegated.Int64
	}
	return purchase, err
}

func (s *Store) EnergyPurchaseForWithdrawal(ctx context.Context, withdrawalID string) (EnergyPurchase, bool, error) {
	purchase, err := scanEnergyPurchase(s.normal.QueryRowContext(ctx, energyPurchaseSelect+" WHERE withdrawal_id=? ORDER BY created_at DESC,id DESC LIMIT 1", withdrawalID))
	if errors.Is(err, sql.ErrNoRows) {
		return EnergyPurchase{}, false, nil
	}
	return purchase, err == nil, err
}

// CreateEnergyPurchase records the quote before the prepaid purchase call (ENR-009).
func (s *Store) CreateEnergyPurchase(ctx context.Context, provider, withdrawalID string, addressID int64, receiver, resourceType string, amount int64, duration time.Duration, quotedTRX string, now time.Time) (EnergyPurchase, error) {
	if value, ok := new(big.Rat).SetString(quotedTRX); !ok || value.Sign() < 0 {
		return EnergyPurchase{}, errors.New("invalid quoted energy price")
	}
	id, err := newULID(now)
	if err != nil {
		return EnergyPurchase{}, err
	}
	_, err = s.normal.ExecContext(ctx, `INSERT INTO energy_purchases
		(id,provider,withdrawal_id,address_id,receiver_address,resource_type,energy_amount,duration_seconds,quoted_trx,status,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,'quoted',?)`, id, provider, withdrawalID, addressID, receiver, resourceType,
		amount, int64(duration/time.Second), quotedTRX, now.UTC().Unix())
	if err != nil {
		return EnergyPurchase{}, fmt.Errorf("create energy purchase: %w", err)
	}
	return scanEnergyPurchase(s.normal.QueryRowContext(ctx, energyPurchaseSelect+" WHERE id=?", id))
}

// MarkEnergyPurchaseAttempt durably spends the single Purchase attempt before external I/O.
func (s *Store) MarkEnergyPurchaseAttempt(ctx context.Context, id, externalOrderID string) error {
	result, err := s.full.ExecContext(ctx, `UPDATE energy_purchases SET status='purchased',provider_order_id=?
		WHERE id=? AND status='quoted' AND provider_order_id IS NULL`, externalOrderID, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return errors.New("energy purchase attempt was already spent (WDR-000)")
	}
	return nil
}

func (s *Store) RejectEnergyQuote(ctx context.Context, id, reason string) error {
	_, err := s.normal.ExecContext(ctx, `UPDATE energy_purchases SET status='failed',failure_reason=? WHERE id=? AND status='quoted'`, reason, id)
	return err
}

func (s *Store) RecordEnergyPurchaseOrder(ctx context.Context, id, providerOrderID, actualTRX, delegationTxID string) error {
	_, err := s.normal.ExecContext(ctx, `UPDATE energy_purchases SET provider_order_id=COALESCE(NULLIF(?,''),provider_order_id),
		actual_trx=COALESCE(NULLIF(?,''),actual_trx),delegation_txid=COALESCE(NULLIF(?,''),delegation_txid)
		WHERE id=? AND status='purchased'`, providerOrderID, actualTRX, delegationTxID, id)
	return err
}

func (s *Store) CompleteEnergyPurchase(ctx context.Context, id, actualTRX, delegationTxID string, now time.Time) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE energy_purchases SET status='delegated',
		actual_trx=COALESCE(NULLIF(?,''),actual_trx,quoted_trx),delegation_txid=COALESCE(NULLIF(?,''),delegation_txid),delegated_at=?
		WHERE id=? AND status='purchased'`, actualTRX, delegationTxID, now.UTC().Unix(), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		// WDR-009d/WDR-025: keep the withdrawal's visible cost in sync with the auditable purchase row.
		if _, err := tx.ExecContext(ctx, `UPDATE withdrawals SET energy_cost_trx=(
			SELECT COALESCE(actual_trx,quoted_trx) FROM energy_purchases WHERE id=?)
			WHERE id=(SELECT withdrawal_id FROM energy_purchases WHERE id=?)`, id, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) FailEnergyPurchase(ctx context.Context, id, reason string, now time.Time, events EventConfig) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE energy_purchases SET status='failed',failure_reason=?
		WHERE id=? AND status IN ('quoted','purchased')`, reason, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		var withdrawalID string
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(withdrawal_id,'') FROM energy_purchases WHERE id=?", id).Scan(&withdrawalID); err != nil {
			return err
		}
		if err := enqueueGlobalEvent(tx, events, "energy:"+id, "energy.purchase_failed", map[string]any{
			"purchase_id": id, "withdrawal_id": withdrawalID, "reason": reason,
		}, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if changed == 1 {
		s.notifyOutbox()
	}
	return nil
}

func (s *Store) RecordEnergyProviderFailure(ctx context.Context, provider, reason string) (int, error) {
	var failures int
	err := s.normal.QueryRowContext(ctx, `INSERT INTO energy_provider_state(provider,last_error,consecutive_failures)
		VALUES (?,?,1) ON CONFLICT(provider) DO UPDATE SET last_error=excluded.last_error,
		consecutive_failures=energy_provider_state.consecutive_failures+1 RETURNING consecutive_failures`, provider, reason).Scan(&failures)
	return failures, err
}

func (s *Store) ResetEnergyProviderFailures(ctx context.Context, provider string) error {
	_, err := s.normal.ExecContext(ctx, `INSERT INTO energy_provider_state(provider,consecutive_failures) VALUES (?,0)
		ON CONFLICT(provider) DO UPDATE SET consecutive_failures=0,last_error=NULL`, provider)
	return err
}

// RecordEnergyProviderBalance stores the 15-minute check and atomically emits the low-balance alert (ENR-010/011).
func (s *Store) RecordEnergyProviderBalance(ctx context.Context, provider, balance, warnBelow string, now time.Time, events EventConfig) error {
	value, valueOK := new(big.Rat).SetString(balance)
	warn, warnOK := new(big.Rat).SetString(warnBelow)
	if !valueOK || !warnOK || value.Sign() < 0 || warn.Sign() < 0 {
		return errors.New("invalid provider balance or warning threshold")
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO energy_provider_state(provider,balance_trx,last_checked_at,last_error)
		VALUES (?,?,?,NULL) ON CONFLICT(provider) DO UPDATE SET balance_trx=excluded.balance_trx,
		last_checked_at=excluded.last_checked_at,last_error=NULL`, provider, balance, now.UTC().Unix()); err != nil {
		return err
	}
	low := value.Cmp(warn) < 0
	if low {
		if err := enqueueGlobalEvent(tx, events, "global", "energy.balance_low", map[string]any{
			"provider": provider, "balance_trx": balance, "warn_below_trx": warnBelow,
		}, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if low {
		s.notifyOutbox()
	}
	return nil
}

func (s *Store) RecordEnergyProviderBalanceError(ctx context.Context, provider string, checkedAt time.Time, balanceErr error) error {
	_, err := s.normal.ExecContext(ctx, `INSERT INTO energy_provider_state(provider,last_checked_at,last_error)
		VALUES (?,?,?) ON CONFLICT(provider) DO UPDATE SET last_checked_at=excluded.last_checked_at,last_error=excluded.last_error`,
		provider, checkedAt.UTC().Unix(), balanceErr.Error())
	return err
}
