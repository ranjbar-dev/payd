package api

import (
	"database/sql"
	"errors"
	"math/big"
	"net/http"
	"time"

	"payd/internal/store"
)

func (s *Server) delegateWallet(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ResourceType string `json:"resource_type"`
		Amount       int64  `json:"amount"`
		TOTP         string `json:"totp"` // Accepted only to reject the retired credential transport (API-022a).
	}
	if err := decodeJSON(w, r, &request, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is invalid", nil)
		return
	}
	if request.TOTP != "" {
		writeError(w, http.StatusBadRequest, "totp_in_body", "send the TOTP code in the X-TOTP header, not the request body", nil)
		return
	}
	if (request.ResourceType != "ENERGY" && request.ResourceType != "BANDWIDTH") || request.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "resource_type must be ENERGY or BANDWIDTH and amount must be a positive integer", nil)
		return
	}
	// API-022 / RES-013: consume the second factor before entering the single-attempt broadcast path.
	if err := s.ValidateTOTP(r.Context(), r.Header.Get("X-TOTP"), time.Now()); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_totp", "TOTP is invalid or already used", nil)
		return
	}
	s.mu.RLock()
	delegator := s.delegator
	s.mu.RUnlock()
	if delegator == nil {
		writeError(w, http.StatusServiceUnavailable, "resource_unavailable", "resource delegation is unavailable", nil)
		return
	}
	state := requestStateFrom(r.Context())
	grant, err := delegator.DelegateResources(r.Context(), r.PathValue("address"), request.ResourceType, request.Amount, state.keyName, r.RemoteAddr)
	if errors.Is(err, store.ErrAddressNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "wallet address was not found", nil)
		return
	}
	if err != nil {
		s.logger.Error("delegate wallet resource", "address", r.PathValue("address"), "resource_type", request.ResourceType, "error", err)
		writeError(w, http.StatusServiceUnavailable, "resource_unavailable", "resource delegation could not be started", nil)
		return
	}
	stake, err := store.FormatUnits(grant.AmountSun, 6)
	if err != nil {
		s.logger.Error("format delegation stake", "grant_id", grant.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id": grant.ID, "address": grant.ReceiverAddress, "resource_type": grant.ResourceType,
		"amount": request.Amount, "stake_trx": stake, "status": grant.Status, "txid": grant.TxID,
	})
}

func (s *Server) listResourceGrants(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	filter := store.ResourceGrantFilter{WithdrawalID: r.URL.Query().Get("withdrawal_id"), Status: r.URL.Query().Get("status"),
		ResourceType: r.URL.Query().Get("resource_type"), After: cursor, Limit: limit + 1}
	grants, err := s.store.ListResourceGrants(r.Context(), filter)
	if err != nil {
		s.logger.Error("list resource grants", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	items := make([]map[string]any, 0, min(len(grants), limit))
	for _, grant := range grants[:min(len(grants), limit)] {
		items = append(items, resourceGrantJSON(grant))
	}
	next := ""
	if len(grants) > limit {
		next = encodeCursor(grants[limit-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": items, "next_cursor": next})
}

func (s *Server) resourceWallet(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	index := s.resources.ResourceWalletIndex
	s.mu.RUnlock()
	status, err := s.store.ResourceWalletStatus(r.Context(), index)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusServiceUnavailable, "resource_wallet_unavailable", "resource wallet is unavailable", nil)
		return
	}
	if err != nil {
		s.logger.Error("load resource wallet", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	trxRaw := new(big.Int)
	for _, balance := range status.Wallet.Balances {
		if balance.Asset == "TRX" {
			if parsed, ok := new(big.Int).SetString(balance.ConfirmedRaw, 10); ok {
				trxRaw.Set(parsed)
			}
		}
	}
	trx, _ := store.FormatUnits(trxRaw.String(), 6)
	outstanding := make(map[string]any, len(status.Outstanding))
	for resourceType, delegation := range status.Outstanding {
		stake, err := store.FormatUnits(delegation.AmountSun, 6)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		outstanding[resourceType] = map[string]any{"count": delegation.Count, "stake_trx": stake}
	}
	writeJSON(w, http.StatusOK, map[string]any{"address": status.Wallet.Address, "trx_balance": trx,
		"energy":                  map[string]any{"limit": status.Wallet.EnergyLimit, "available": max(status.Wallet.EnergyLimit-status.Wallet.EnergyUsed, 0)},
		"bandwidth":               map[string]any{"limit": status.Wallet.BandwidthLimit, "available": max(status.Wallet.BandwidthLimit-status.Wallet.BandwidthUsed, 0)},
		"outstanding_delegations": outstanding})
}

func resourceGrantJSON(grant store.ResourceGrant) map[string]any {
	amount, _ := store.FormatUnits(grant.AmountSun, 6)
	fee, _ := store.FormatUnits(zeroIfEmpty(grant.FeeRaw), 6)
	return map[string]any{"id": grant.ID, "withdrawal_id": grant.WithdrawalID, "receiver_address": grant.ReceiverAddress,
		"resource_type": grant.ResourceType, "source": grant.Source, "amount_sun": grant.AmountSun, "stake_trx": amount,
		"txid": grant.TxID, "status": grant.Status, "broadcast_response": grant.BroadcastResponse,
		"failure_reason": grant.FailureReason, "fee_trx": fee, "lookup_failures": grant.LookupFailures,
		"last_lookup_error": grant.LastLookupError, "created_at": grant.CreatedAt,
		"broadcast_attempted_at": grant.BroadcastAttemptedAt, "expiration_at": grant.ExpirationAt, "confirmed_at": grant.ConfirmedAt}
}
