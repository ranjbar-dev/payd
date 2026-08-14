package api

import (
	"math/big"
	"net/http"
	"slices"
)

func (s *Server) energyStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	provider := s.energy.Provider
	s.mu.RUnlock()
	status, err := s.store.EnergyProviderStatus(r.Context(), provider)
	if err != nil {
		s.logger.Error("load energy status", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	s.mu.RLock()
	delegator := s.delegator
	s.mu.RUnlock()
	if delegator != nil {
		if balance, _ := delegator.ProviderBalanceMetric(); balance != "" {
			status.BalanceTRX = balance
		}
	}
	s.mu.RLock()
	warnTRX := s.energy.BalanceWarnTRX
	s.mu.RUnlock()
	// WRES-002: the comparison is made here because the figures are money. A client
	// that compared them would be doing decimal arithmetic on a balance, which is
	// exactly what the dashboard's INV-2 forbids — and a float comparison on a
	// provider balance is how a low balance goes unnoticed.
	body := map[string]any{"provider": status.Provider, "balance_trx": status.BalanceTRX,
		"last_checked_at": status.LastCheckedAt, "last_error": status.LastError,
		"consecutive_failures": status.ConsecutiveFailures, "purchases": status.Purchases,
		"balance_warn_trx": warnTRX, "balance_low": balanceBelow(status.BalanceTRX, warnTRX)}
	writeJSON(w, http.StatusOK, body)
}

// balanceBelow reports whether balance is strictly below the warning threshold.
// Both are decimal strings; an unparsable or absent figure is never reported as
// low, because a missing balance is an unknown state rather than a safe one.
func balanceBelow(balance, threshold string) bool {
	have, ok := new(big.Rat).SetString(balance)
	want, wantOK := new(big.Rat).SetString(threshold)
	if !ok || !wantOK {
		return false
	}
	return have.Cmp(want) < 0
}

func (s *Server) listEnergyPurchases(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !slices.Contains([]string{"quoted", "purchased", "delegated", "expired", "failed"}, status) {
		writeError(w, http.StatusBadRequest, "invalid_status", "status must be quoted, purchased, delegated, expired or failed", nil)
		return
	}
	purchases, err := s.store.ListEnergyPurchases(r.Context(), status, cursor, limit+1)
	if err != nil {
		s.logger.Error("list energy purchases", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	next := ""
	if len(purchases) > limit {
		next = encodeCursor(purchases[limit-1].ID)
	}
	items := make([]map[string]any, 0, min(len(purchases), limit))
	for _, purchase := range purchases[:min(len(purchases), limit)] {
		items = append(items, map[string]any{
			"id": purchase.ID, "provider": purchase.Provider, "provider_order_id": purchase.ProviderOrderID,
			"withdrawal_id": purchase.WithdrawalID, "receiver_address": purchase.ReceiverAddress,
			"resource_type": purchase.ResourceType, "amount": purchase.Amount,
			"duration_seconds": purchase.DurationSeconds, "quoted_trx": purchase.QuotedTRX,
			"actual_trx": purchase.ActualTRX, "status": purchase.Status,
			"failure_reason": purchase.FailureReason, "delegation_txid": purchase.DelegationTxID,
			"created_at": purchase.CreatedAt, "delegated_at": purchase.DelegatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"purchases": items, "next_cursor": next})
}
