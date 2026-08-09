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
	ErrWithdrawalNotFound = errors.New("withdrawal not found")
	ErrIdempotencyReuse   = errors.New("idempotency key reused with different fields")
	ErrDailyLimit         = errors.New("withdrawal daily limit exceeded")
	ErrInsufficientFunds  = errors.New("insufficient confirmed balance")
	ErrSourceUnavailable  = errors.New("withdrawal source is not available")
	ErrWithdrawalState    = errors.New("withdrawal is not awaiting operator resolution")
)

type Withdrawal struct {
	ID, IdempotencyKey, FromAddress, ToAddress, Asset, AmountRaw         string
	AmountUSD, Status, TxID, BroadcastResponse                           string
	FeeRaw, EnergySource, EnergyCostTRX, BandwidthSource                 string
	EnergyGrantFeeRaw, BandwidthGrantFeeRaw                              string
	FailureReason, ResolvedBy, RequestedBy, RequestedIP, LastLookupError string
	AddressID, CreatedAt                                                 int64
	HDIndex                                                              uint32
	EnergyUsed, LookupFailures                                           int64
	BroadcastAttemptedAt, ExpirationAt, LastLookupAt                     *int64
	BroadcastAt, ConfirmedAt                                             *int64
}

type CreateWithdrawal struct {
	IdempotencyKey, FromAddress, ToAddress, Asset, AmountRaw string
	AmountUSD, DailyLimitUSD, RequestedBy, IP                string
	Now                                                      time.Time
}

const withdrawalSelect = `SELECT w.id, w.idempotency_key, w.address_id, a.hd_index, w.from_address,
    w.to_address, w.asset, w.amount_raw, COALESCE(w.amount_usd,''), w.status, COALESCE(w.txid,''),
    w.broadcast_attempted_at, COALESCE(w.broadcast_response,''), COALESCE(w.fee_raw,''),
    COALESCE(w.energy_used,0), COALESCE(w.energy_source,''), COALESCE(w.energy_cost_trx,''),
    COALESCE(w.bandwidth_source,''), COALESCE(w.failure_reason,''), COALESCE(w.resolved_by,''),
    w.requested_by, w.created_at, w.broadcast_at, w.confirmed_at, w.expiration_at,
	w.last_lookup_at, w.lookup_failures, COALESCE(w.last_lookup_error,''), COALESCE(w.requested_ip,''),
	COALESCE((SELECT fee_raw FROM resource_grants WHERE withdrawal_id=w.id AND resource_type='ENERGY'),''),
	COALESCE((SELECT fee_raw FROM resource_grants WHERE withdrawal_id=w.id AND resource_type='BANDWIDTH'),'')
    FROM withdrawals w JOIN addresses a ON a.id = w.address_id`

func scanWithdrawal(row rowScanner) (Withdrawal, error) {
	var w Withdrawal
	var attempted, broadcastAt, confirmedAt, expirationAt, lookupAt sql.NullInt64
	err := row.Scan(&w.ID, &w.IdempotencyKey, &w.AddressID, &w.HDIndex, &w.FromAddress, &w.ToAddress,
		&w.Asset, &w.AmountRaw, &w.AmountUSD, &w.Status, &w.TxID, &attempted, &w.BroadcastResponse,
		&w.FeeRaw, &w.EnergyUsed, &w.EnergySource, &w.EnergyCostTRX, &w.BandwidthSource,
		&w.FailureReason, &w.ResolvedBy, &w.RequestedBy, &w.CreatedAt, &broadcastAt, &confirmedAt,
		&expirationAt, &lookupAt, &w.LookupFailures, &w.LastLookupError, &w.RequestedIP,
		&w.EnergyGrantFeeRaw, &w.BandwidthGrantFeeRaw)
	setNullable := func(value sql.NullInt64, target **int64) {
		if value.Valid {
			v := value.Int64
			*target = &v
		}
	}
	setNullable(attempted, &w.BroadcastAttemptedAt)
	setNullable(broadcastAt, &w.BroadcastAt)
	setNullable(confirmedAt, &w.ConfirmedAt)
	setNullable(expirationAt, &w.ExpirationAt)
	setNullable(lookupAt, &w.LastLookupAt)
	return w, err
}

func (s *Store) WithdrawalByIdempotency(ctx context.Context, key string) (Withdrawal, bool, error) {
	w, err := scanWithdrawal(s.normal.QueryRowContext(ctx, withdrawalSelect+" WHERE w.idempotency_key = ?", key))
	if errors.Is(err, sql.ErrNoRows) {
		return Withdrawal{}, false, nil
	}
	return w, err == nil, err
}

func (s *Store) Withdrawal(ctx context.Context, id string) (Withdrawal, error) {
	w, err := scanWithdrawal(s.normal.QueryRowContext(ctx, withdrawalSelect+" WHERE w.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Withdrawal{}, ErrWithdrawalNotFound
	}
	return w, err
}

// ResolveWithdrawal records an operator's terminal decision only; it never mutates txid or broadcast fields (API-031, WDR-018).
func (s *Store) ResolveWithdrawal(ctx context.Context, id, outcome, reason, actor, ip string, now time.Time) (Withdrawal, error) {
	if outcome != "confirmed" && outcome != "failed" {
		return Withdrawal{}, ErrWithdrawalState
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return Withdrawal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE withdrawals SET status=?,failure_reason=?,resolved_by='operator',
		status_updated_at=?,confirmed_at=CASE WHEN ?='confirmed' THEN ? ELSE confirmed_at END
		WHERE id=? AND status='needs_operator'`, outcome, reason, now.UTC().Unix(), outcome, now.UTC().Unix(), id)
	if err != nil {
		return Withdrawal{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Withdrawal{}, err
	}
	if changed != 1 {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM withdrawals WHERE id=?", id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return Withdrawal{}, ErrWithdrawalNotFound
		} else if err != nil {
			return Withdrawal{}, err
		}
		return Withdrawal{}, ErrWithdrawalState
	}
	detail, _ := json.Marshal(map[string]string{"outcome": outcome, "failure_reason": reason})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor,action,subject,detail,ip,created_at)
		VALUES (?,'withdrawal.resolved',?,?,?,?)`, actor, id, string(detail), ip, now.UTC().Unix()); err != nil {
		return Withdrawal{}, err
	}
	w, err := scanWithdrawal(tx.QueryRowContext(ctx, withdrawalSelect+" WHERE w.id=?", id))
	if err != nil {
		return Withdrawal{}, err
	}
	if err := tx.Commit(); err != nil {
		return Withdrawal{}, err
	}
	return w, nil
}

// CreateWithdrawal enforces WDR-002a/005/006/006a in one serialized write transaction.
func (s *Store) CreateWithdrawal(ctx context.Context, request CreateWithdrawal) (Withdrawal, bool, error) {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return Withdrawal{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if existing, err := scanWithdrawal(tx.QueryRowContext(ctx, withdrawalSelect+" WHERE w.idempotency_key = ?", request.IdempotencyKey)); err == nil {
		if existing.FromAddress != request.FromAddress || existing.ToAddress != request.ToAddress || existing.Asset != request.Asset || existing.AmountRaw != request.AmountRaw {
			return Withdrawal{}, false, ErrIdempotencyReuse
		}
		return existing, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Withdrawal{}, false, err
	}
	var addressID int64
	var state, confirmed string
	var drift bool
	if err := tx.QueryRowContext(ctx, `SELECT a.id, a.state, b.confirmed_raw, b.drift_detected
        FROM addresses a JOIN balances b ON b.address_id=a.id AND b.asset=? WHERE a.address=?`, request.Asset, request.FromAddress).
		Scan(&addressID, &state, &confirmed, &drift); errors.Is(err, sql.ErrNoRows) {
		return Withdrawal{}, false, ErrSourceUnavailable
	} else if err != nil {
		return Withdrawal{}, false, err
	}
	if state == "disabled" {
		return Withdrawal{}, false, ErrSourceUnavailable
	}
	if drift {
		return Withdrawal{}, false, ErrBalanceDrift
	}
	want, ok := new(big.Int).SetString(request.AmountRaw, 10)
	have, haveOK := new(big.Int).SetString(confirmed, 10)
	if !ok || !haveOK || want.Sign() <= 0 || want.Cmp(have) > 0 {
		return Withdrawal{}, false, ErrInsufficientFunds
	}
	amountUSD, amountOK := new(big.Rat).SetString(request.AmountUSD)
	limit, limitOK := new(big.Rat).SetString(request.DailyLimitUSD)
	if !amountOK || !limitOK || amountUSD.Sign() <= 0 || limit.Sign() <= 0 {
		return Withdrawal{}, false, ErrDailyLimit
	}
	id, err := newULID(request.Now)
	if err != nil {
		return Withdrawal{}, false, err
	}
	// WDR-006a: the row is created by a conditional INSERT inside the same transaction as the exact decimal sum.
	result, err := tx.ExecContext(ctx, `INSERT INTO withdrawals(id,idempotency_key,address_id,from_address,to_address,asset,
		amount_raw,amount_usd,status,requested_by,requested_ip,created_at,status_updated_at)
		SELECT ?,?,?,?,?,?,?,?,'requested',?,?,?,? WHERE payd_decimal_sum_within(COALESCE((
			SELECT json_group_array(amount_usd) FROM withdrawals WHERE created_at>=?
			AND status IN ('requested','awaiting_resources','awaiting_energy','signing','broadcast','confirmed')
		),'[]'),?,?)=1`, id, request.IdempotencyKey, addressID, request.FromAddress,
		request.ToAddress, request.Asset, request.AmountRaw, request.AmountUSD, request.RequestedBy, request.IP,
		request.Now.UTC().Unix(), request.Now.UTC().Unix(), request.Now.UTC().Truncate(24*time.Hour).Unix(), request.AmountUSD, request.DailyLimitUSD)
	if err != nil {
		return Withdrawal{}, false, err
	}
	inserted, _ := result.RowsAffected()
	if inserted != 1 {
		return Withdrawal{}, false, ErrDailyLimit
	}
	detail, _ := json.Marshal(map[string]any{"asset": request.Asset, "amount_raw": request.AmountRaw, "to_address": request.ToAddress})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor,action,subject,detail,ip,created_at)
        VALUES (?,'withdrawal.requested',?,?,?,?)`, request.RequestedBy, id, string(detail), request.IP, request.Now.UTC().Unix()); err != nil {
		return Withdrawal{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Withdrawal{}, false, err
	}
	w, err := s.Withdrawal(ctx, id)
	return w, true, err
}

func (s *Store) ListWithdrawals(ctx context.Context, status string, limit int) ([]Withdrawal, error) {
	query, args := withdrawalSelect, []any{}
	if status != "" {
		query, args = query+" WHERE w.status = ?", append(args, status)
	}
	query += " ORDER BY w.created_at DESC, w.id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.normal.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []Withdrawal
	for rows.Next() {
		w, err := scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (s *Store) StreamWithdrawals(ctx context.Context, status string, limit int, visit func(Withdrawal) error) error {
	query, args := withdrawalSelect, []any{}
	if status != "" {
		query, args = query+" WHERE w.status=?", append(args, status)
	}
	query, args = query+" ORDER BY w.created_at DESC,w.id DESC LIMIT ?", append(args, limit)
	rows, err := s.normal.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		withdrawal, err := scanWithdrawal(rows)
		if err != nil {
			return err
		}
		if err := visit(withdrawal); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) WithdrawalUSDUsed(ctx context.Context, now time.Time) (*big.Rat, error) {
	rows, err := s.normal.QueryContext(ctx, `SELECT amount_usd FROM withdrawals WHERE created_at >= ?
        AND status IN ('requested','awaiting_resources','awaiting_energy','signing','broadcast','confirmed')`, now.UTC().Truncate(24*time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	total := new(big.Rat)
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, err
		}
		value, ok := new(big.Rat).SetString(text)
		if !ok {
			return nil, fmt.Errorf("invalid stored withdrawal USD value %q", text)
		}
		total.Add(total, value)
	}
	return total, rows.Err()
}

// ClaimWithdrawal is the single conditional WDR-008 exclusivity update and rechecks WDR-005 under the claim transaction.
func (s *Store) ClaimWithdrawal(ctx context.Context, id string) (Withdrawal, bool, error) {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return Withdrawal{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	w, err := scanWithdrawal(tx.QueryRowContext(ctx, withdrawalSelect+" WHERE w.id = ?", id))
	if err != nil {
		return Withdrawal{}, false, err
	}
	var confirmed string
	if err := tx.QueryRowContext(ctx, "SELECT confirmed_raw FROM balances WHERE address_id=? AND asset=? AND drift_detected=0", w.AddressID, w.Asset).Scan(&confirmed); err != nil {
		return Withdrawal{}, false, err
	}
	have, ok := new(big.Int).SetString(confirmed, 10)
	want, wantOK := new(big.Int).SetString(w.AmountRaw, 10)
	var heldRows *sql.Rows
	heldRows, err = tx.QueryContext(ctx, `SELECT amount_raw FROM withdrawals WHERE address_id=? AND asset=? AND id<>?
        AND status IN ('awaiting_resources','awaiting_energy','signing','broadcast')`, w.AddressID, w.Asset, w.ID)
	if err != nil {
		return Withdrawal{}, false, err
	}
	for heldRows.Next() {
		var raw string
		if err := heldRows.Scan(&raw); err != nil {
			_ = heldRows.Close()
			return Withdrawal{}, false, err
		}
		amount, valid := new(big.Int).SetString(raw, 10)
		if !valid {
			_ = heldRows.Close()
			return Withdrawal{}, false, fmt.Errorf("invalid held amount %q", raw)
		}
		have.Sub(have, amount)
	}
	_ = heldRows.Close()
	funded := ok && wantOK && want.Sign() > 0 && have.Cmp(want) >= 0
	if !funded {
		if _, err := tx.ExecContext(ctx, `UPDATE withdrawals SET status='rejected',failure_reason='insufficient confirmed balance',status_updated_at=?
			WHERE id=? AND status='requested'`, time.Now().UTC().Unix(), w.ID); err != nil {
			return Withdrawal{}, false, err
		}
		if err := s.auditWithdrawal(tx, w.ID, "withdrawal.rejected", "insufficient confirmed balance", time.Now()); err != nil {
			return Withdrawal{}, false, err
		}
		return Withdrawal{}, false, tx.Commit()
	}
	result, err := tx.ExecContext(ctx, `UPDATE withdrawals SET status='awaiting_resources',status_updated_at=? WHERE id=? AND status='requested' AND ?
        AND NOT EXISTS (SELECT 1 FROM withdrawals WHERE address_id=? AND id<>? AND status IN ('awaiting_resources','awaiting_energy','signing','broadcast'))`,
		time.Now().UTC().Unix(), w.ID, funded, w.AddressID, w.ID)
	if err != nil {
		return Withdrawal{}, false, err
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		if err := s.auditWithdrawal(tx, w.ID, "withdrawal.approved", "balance and address claim passed", time.Now()); err != nil {
			return Withdrawal{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Withdrawal{}, false, err
	}
	if changed == 1 {
		w.Status = "awaiting_resources"
	}
	return w, changed == 1, nil
}

func (s *Store) NextRequestedWithdrawal(ctx context.Context) (Withdrawal, bool, error) {
	w, err := scanWithdrawal(s.normal.QueryRowContext(ctx, withdrawalSelect+" WHERE w.status='requested' AND w.txid IS NULL ORDER BY w.created_at,w.id LIMIT 1"))
	if errors.Is(err, sql.ErrNoRows) {
		return Withdrawal{}, false, nil
	}
	return w, err == nil, err
}

func (s *Store) AwaitingResourceWithdrawals(ctx context.Context) ([]Withdrawal, error) {
	rows, err := s.normal.QueryContext(ctx, withdrawalSelect+` WHERE w.status IN ('awaiting_resources','awaiting_energy')
		AND w.txid IS NULL ORDER BY w.created_at,w.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []Withdrawal
	for rows.Next() {
		withdrawal, err := scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, withdrawal)
	}
	return result, rows.Err()
}

// WithdrawalRecoveryCandidates implements WDR-018/018a: txid wins over the status column.
func (s *Store) WithdrawalRecoveryCandidates(ctx context.Context, now time.Time, energyTimeout time.Duration, force bool) ([]Withdrawal, error) {
	rows, err := s.normal.QueryContext(ctx, withdrawalSelect+` WHERE
        (w.txid IS NOT NULL AND w.status<>'confirmed' AND (w.status NOT IN ('failed','needs_operator') OR ?))
        OR (w.status='signing' AND w.txid IS NULL)
        OR (w.status='awaiting_resources' AND COALESCE(w.status_updated_at,w.created_at) <= ?)
        OR (w.status='awaiting_energy' AND COALESCE(w.status_updated_at,w.created_at) <= ?)
        ORDER BY w.created_at,w.id`, force, now.Add(-5*time.Minute).UTC().Unix(), now.Add(-energyTimeout).UTC().Unix())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []Withdrawal
	for rows.Next() {
		w, err := scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (s *Store) SetWithdrawalResourceState(ctx context.Context, id, from, to, energySource, bandwidthSource string) (bool, error) {
	result, err := s.normal.ExecContext(ctx, `UPDATE withdrawals SET status=?,status_updated_at=?, energy_source=COALESCE(NULLIF(?,''),energy_source),
		bandwidth_source=COALESCE(NULLIF(?,''),bandwidth_source) WHERE id=? AND status=?`, to, time.Now().UTC().Unix(), energySource, bandwidthSource, id, from)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// ReconcileWithdrawalEnergy closes stale purchased rows before startup recovery permits signing (WDR-019).
func (s *Store) ReconcileWithdrawalEnergy(ctx context.Context, id string, resourcesVisible bool, now time.Time) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT COALESCE(actual_trx,quoted_trx) FROM energy_purchases
        WHERE withdrawal_id=? AND status='purchased'`, id)
	if err != nil {
		return err
	}
	cost := new(big.Rat)
	count := 0
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			_ = rows.Close()
			return err
		}
		value, ok := new(big.Rat).SetString(text)
		if !ok {
			_ = rows.Close()
			return fmt.Errorf("invalid energy purchase cost %q", text)
		}
		cost.Add(cost, value)
		count++
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if count == 0 {
		return tx.Commit()
	}
	status := "expired"
	if resourcesVisible {
		status = "delegated"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE energy_purchases SET status=?,actual_trx=COALESCE(actual_trx,quoted_trx),
        failure_reason=CASE WHEN ?='expired' THEN 'resource not visible at recovery timeout' ELSE failure_reason END,
        delegated_at=CASE WHEN ?='delegated' THEN COALESCE(delegated_at,?) ELSE delegated_at END
        WHERE withdrawal_id=? AND status='purchased'`, status, status, status, now.UTC().Unix(), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE withdrawals SET energy_cost_trx=? WHERE id=?`, rationalDecimal(cost), id); err != nil {
		return err
	}
	return tx.Commit()
}

func rationalDecimal(value *big.Rat) string {
	for digits := 0; digits <= 64; digits++ {
		text := value.FloatString(digits)
		if parsed, ok := new(big.Rat).SetString(text); ok && parsed.Cmp(value) == 0 {
			return text
		}
	}
	return value.FloatString(64)
}

// PersistWithdrawalAttempt is the WDR-015 durability boundary. It is deliberately the only pre-broadcast FULL write.
func (s *Store) PersistWithdrawalAttempt(ctx context.Context, id, txid string, attemptedAt time.Time, expirationAt int64) error {
	tx, err := s.full.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE withdrawals SET txid=?, broadcast_attempted_at=?, expiration_at=?
        WHERE id=? AND status='signing' AND txid IS NULL AND broadcast_attempted_at IS NULL`, txid, attemptedAt.UTC().Unix(), expirationAt, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return errors.New("withdrawal broadcast attempt was already spent (WDR-014a/017a)")
	}
	return tx.Commit()
}

func (s *Store) RecordWithdrawalBroadcast(ctx context.Context, id, response string, now time.Time) error {
	tx, err := s.full.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE withdrawals SET status='broadcast',status_updated_at=?, broadcast_response=?, broadcast_at=COALESCE(broadcast_at,?),
        lookup_failures=0,last_lookup_error=NULL WHERE id=? AND txid IS NOT NULL AND broadcast_attempted_at IS NOT NULL
		AND status NOT IN ('confirmed','failed')`, now.UTC().Unix(), response, now.UTC().Unix(), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		if err := s.auditWithdrawal(tx, id, "withdrawal.broadcast", response, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) RecordWithdrawalLookup(ctx context.Context, id string, found bool, lookupErr error, now time.Time, events EventConfig) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if lookupErr == nil {
		status := "status"
		if found {
			status = "'broadcast'"
		}
		_, err = tx.ExecContext(ctx, `UPDATE withdrawals SET status=`+status+`,status_updated_at=CASE WHEN `+status+`='broadcast' THEN ? ELSE status_updated_at END,
			last_lookup_at=?,lookup_failures=0,last_lookup_error=NULL WHERE id=?`, now.UTC().Unix(), now.UTC().Unix(), id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE withdrawals SET last_lookup_at=?,lookup_failures=lookup_failures+1,last_lookup_error=? WHERE id=?`, now.UTC().Unix(), lookupErr.Error(), id)
		if err == nil {
			var failures int
			if err = tx.QueryRowContext(ctx, "SELECT lookup_failures FROM withdrawals WHERE id=?", id).Scan(&failures); err == nil && failures >= 10 {
				err = s.setNeedsOperator(tx, id, lookupErr.Error(), now, events)
			}
		}
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyOutbox()
	return nil
}

func (s *Store) WithdrawalNeedsOperator(ctx context.Context, id, reason string, now time.Time, events EventConfig) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.setNeedsOperator(tx, id, reason, now, events); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyOutbox()
	return nil
}

func (s *Store) setNeedsOperator(tx *sql.Tx, id, reason string, now time.Time, events EventConfig) error {
	result, err := tx.Exec(`UPDATE withdrawals SET status='needs_operator',status_updated_at=?,failure_reason=?,last_lookup_error=COALESCE(last_lookup_error,?)
		WHERE id=? AND status NOT IN ('confirmed','failed','needs_operator')`, now.UTC().Unix(), reason, reason, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil
	}
	var txid string
	_ = tx.QueryRow("SELECT COALESCE(txid,'') FROM withdrawals WHERE id=?", id).Scan(&txid)
	if err := enqueueGlobalEvent(tx, events, "withdrawal:"+id, "withdrawal.needs_operator", map[string]any{
		"withdrawal_id": id, "status": "needs_operator", "txid": txid, "last_lookup_error": reason,
	}, now); err != nil {
		return err
	}
	return s.auditWithdrawal(tx, id, "withdrawal.needs_operator", reason, now)
}

func (s *Store) FailWithdrawalAbsent(ctx context.Context, id, reason string, now time.Time, events EventConfig) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE withdrawals SET status='failed',status_updated_at=?,failure_reason=?,resolved_by='chain_absence'
		WHERE id=? AND txid IS NOT NULL AND status NOT IN ('confirmed','failed')`, now.UTC().Unix(), reason, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		if err := enqueueGlobalEvent(tx, events, "withdrawal:"+id, "withdrawal.failed", map[string]any{
			"withdrawal_id": id, "status": "failed", "reason": reason,
		}, now); err != nil {
			return err
		}
		if err := s.auditWithdrawal(tx, id, "withdrawal.failed", reason, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyOutbox()
	return nil
}

// FailWithdrawalResources applies RES-009 before any withdrawal transaction exists.
func (s *Store) FailWithdrawalResources(ctx context.Context, id, reason string, now time.Time, events EventConfig) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE withdrawals SET status='failed',status_updated_at=?,failure_reason=?,resolved_by='resource_acquisition'
		WHERE id=? AND txid IS NULL AND status IN ('awaiting_resources','awaiting_energy')`, now.UTC().Unix(), reason, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		if err := enqueueGlobalEvent(tx, events, "withdrawal:"+id, "withdrawal.failed", map[string]any{
			"withdrawal_id": id, "status": "failed", "reason": reason,
		}, now); err != nil {
			return err
		}
		if err := s.auditWithdrawal(tx, id, "withdrawal.failed", reason, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyOutbox()
	return nil
}

func (s *Store) ConfirmWithdrawal(ctx context.Context, id, feeRaw string, energyUsed, blockHeight, blockTimestamp int64, events EventConfig, now time.Time) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var w Withdrawal
	w, err = scanWithdrawal(tx.QueryRowContext(ctx, withdrawalSelect+" WHERE w.id=?", id))
	if err != nil {
		return err
	}
	energyCost := ""
	if w.EnergySource == "burned" {
		energyCost, err = FormatUnits(feeRaw, 6)
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE withdrawals SET status='confirmed',status_updated_at=?,fee_raw=?,energy_used=?,confirmed_at=?,
		energy_cost_trx=COALESCE(NULLIF(?,''),energy_cost_trx),
		lookup_failures=0,last_lookup_error=NULL WHERE id=? AND txid IS NOT NULL AND status<>'confirmed'`,
		now.UTC().Unix(), feeRaw, energyUsed, now.UTC().Unix(), energyCost, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil
	}
	if fee, ok := new(big.Int).SetString(feeRaw, 10); ok && fee.Sign() > 0 {
		// WDR-023a: a synthetic negative log index cannot collide with decoded transfer events.
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO payments(txid,log_index,direction,block_height,block_id,block_timestamp,
            from_address,to_address,address_id,asset,amount_raw,is_dust,status,detected_at,confirmed_at)
            VALUES (?,-1,'out',?,'',?,?,'network_fee',?,'TRX',?,0,'confirmed',?,?)`, w.TxID, blockHeight, blockTimestamp,
			w.FromAddress, w.AddressID, fee.String(), now.UTC().Unix(), now.UTC().Unix())
		if err != nil {
			return err
		}
		if err := recalculateBalance(tx, w.AddressID, "TRX"); err != nil {
			return err
		}
	}
	if err := enqueueGlobalEvent(tx, events, "withdrawal:"+id, "withdrawal.confirmed", map[string]any{
		"withdrawal_id": id, "status": "confirmed", "txid": w.TxID, "fee_raw": feeRaw, "energy_used": energyUsed,
	}, now); err != nil {
		return err
	}
	if err := s.auditWithdrawal(tx, id, "withdrawal.confirmed", w.TxID, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyOutbox()
	return nil
}

func (s *Store) auditWithdrawal(tx *sql.Tx, id, action, detail string, now time.Time) error {
	_, err := tx.Exec(`INSERT INTO audit_log(actor,action,subject,detail,ip,created_at)
		SELECT requested_by,?,?,?,requested_ip,? FROM withdrawals WHERE id=?`, action, id, detail, now.UTC().Unix(), id)
	return err
}
