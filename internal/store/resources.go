package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ResourceGrant struct {
	ID, WithdrawalID, ReceiverAddress, ResourceType, Source, AmountSun string
	TxID, Status, BroadcastResponse, FailureReason                     string
	AddressID, CreatedAt                                               int64
	LookupFailures                                                     int64
	LastLookupError                                                    string
	BroadcastAttemptedAt, ExpirationAt, ConfirmedAt                    *int64
}

const resourceGrantSelect = `SELECT id,COALESCE(withdrawal_id,''),address_id,COALESCE(receiver_address,''),resource_type,
	source,amount_sun,COALESCE(txid,''),status,created_at,broadcast_attempted_at,expiration_at,
	COALESCE(broadcast_response,''),COALESCE(failure_reason,''),lookup_failures,COALESCE(last_lookup_error,''),confirmed_at FROM resource_grants`

func scanResourceGrant(row rowScanner) (ResourceGrant, error) {
	var grant ResourceGrant
	var attempted, expiration, confirmed sql.NullInt64
	err := row.Scan(&grant.ID, &grant.WithdrawalID, &grant.AddressID, &grant.ReceiverAddress,
		&grant.ResourceType, &grant.Source, &grant.AmountSun, &grant.TxID, &grant.Status,
		&grant.CreatedAt, &attempted, &expiration, &grant.BroadcastResponse, &grant.FailureReason,
		&grant.LookupFailures, &grant.LastLookupError, &confirmed)
	if attempted.Valid {
		grant.BroadcastAttemptedAt = &attempted.Int64
	}
	if expiration.Valid {
		grant.ExpirationAt = &expiration.Int64
	}
	if confirmed.Valid {
		grant.ConfirmedAt = &confirmed.Int64
	}
	return grant, err
}

func (s *Store) ResourceWallet(ctx context.Context, index uint32) (int64, string, error) {
	var id int64
	var address string
	err := s.normal.QueryRowContext(ctx, "SELECT id,address FROM addresses WHERE hd_index=? AND state='disabled'", index).Scan(&id, &address)
	return id, address, err
}

func (s *Store) ResourceGrantForWithdrawal(ctx context.Context, withdrawalID, resourceType string) (ResourceGrant, bool, error) {
	grant, err := scanResourceGrant(s.normal.QueryRowContext(ctx, resourceGrantSelect+" WHERE withdrawal_id=? AND resource_type=?", withdrawalID, resourceType))
	if errors.Is(err, sql.ErrNoRows) {
		return ResourceGrant{}, false, nil
	}
	return grant, err == nil, err
}

func (s *Store) CreateResourceGrant(ctx context.Context, withdrawalID string, addressID int64, receiver, resourceType, source, amountSun string, now time.Time) (ResourceGrant, error) {
	id, err := newULID(now)
	if err != nil {
		return ResourceGrant{}, err
	}
	_, err = s.normal.ExecContext(ctx, `INSERT INTO resource_grants
        (id,withdrawal_id,address_id,receiver_address,resource_type,source,amount_sun,status,created_at)
        VALUES (?,?,?,?,?,?,?,'requested',?) ON CONFLICT(withdrawal_id,resource_type) WHERE withdrawal_id IS NOT NULL DO NOTHING`,
		id, withdrawalID, addressID, receiver, resourceType, source, amountSun, now.UTC().Unix())
	if err != nil {
		return ResourceGrant{}, fmt.Errorf("create resource grant: %w", err)
	}
	grant, found, err := s.ResourceGrantForWithdrawal(ctx, withdrawalID, resourceType)
	if err != nil || !found {
		return ResourceGrant{}, errors.Join(errors.New("resource grant was not persisted"), err)
	}
	return grant, nil
}

// PersistResourceGrantAttempt is the RES-008/013 durability boundary: FULL commit precedes the one broadcast.
func (s *Store) PersistResourceGrantAttempt(ctx context.Context, id, txid string, attemptedAt time.Time, expirationAt int64) error {
	tx, err := s.full.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE resource_grants SET txid=?,broadcast_attempted_at=?,expiration_at=?,status='attempted'
        WHERE id=? AND status='requested' AND txid IS NULL AND broadcast_attempted_at IS NULL`,
		txid, attemptedAt.UTC().Unix(), expirationAt, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return errors.New("resource broadcast attempt was already spent (WDR-000)")
	}
	return tx.Commit()
}

func (s *Store) RecordResourceGrantBroadcast(ctx context.Context, id, response string) error {
	_, err := s.full.ExecContext(ctx, `UPDATE resource_grants SET status='broadcast',broadcast_response=?
        WHERE id=? AND txid IS NOT NULL AND broadcast_attempted_at IS NOT NULL AND status='attempted'`, response, id)
	return err
}

func (s *Store) FailResourceGrant(ctx context.Context, id, response, reason string) error {
	_, err := s.normal.ExecContext(ctx, `UPDATE resource_grants SET status='failed',broadcast_response=COALESCE(NULLIF(?,''),broadcast_response),failure_reason=?
        WHERE id=? AND status NOT IN ('confirmed','failed')`, response, reason, id)
	return err
}

func (s *Store) ConfirmResourceGrant(ctx context.Context, id string, now time.Time) error {
	_, err := s.normal.ExecContext(ctx, `UPDATE resource_grants SET status='confirmed',confirmed_at=?
		WHERE id=? AND status NOT IN ('confirmed','failed')`, now.UTC().Unix(), id)
	return err
}

func (s *Store) RecordResourceGrantLookup(ctx context.Context, id string, lookupErr error) (int64, error) {
	if lookupErr == nil {
		_, err := s.normal.ExecContext(ctx, "UPDATE resource_grants SET lookup_failures=0,last_lookup_error=NULL WHERE id=?", id)
		return 0, err
	}
	var failures int64
	err := s.normal.QueryRowContext(ctx, `UPDATE resource_grants SET lookup_failures=lookup_failures+1,last_lookup_error=?
		WHERE id=? RETURNING lookup_failures`, lookupErr.Error(), id).Scan(&failures)
	return failures, err
}

func (s *Store) ResourceGrantNeedsOperator(ctx context.Context, id, reason string) error {
	_, err := s.normal.ExecContext(ctx, `UPDATE resource_grants SET status='needs_operator',failure_reason=?
		WHERE id=? AND status NOT IN ('confirmed','failed','needs_operator')`, reason, id)
	return err
}
