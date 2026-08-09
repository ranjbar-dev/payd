package api

import "net/http"

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
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) listEnergyPurchases(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	purchases, err := s.store.ListEnergyPurchases(r.Context(), cursor, limit+1)
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
