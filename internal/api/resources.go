package api

import (
	"errors"
	"net/http"

	"payd/internal/store"
)

func (s *Server) delegateWallet(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ResourceType string `json:"resource_type"`
		Amount       int64  `json:"amount"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil ||
		(request.ResourceType != "ENERGY" && request.ResourceType != "BANDWIDTH") || request.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "resource_type must be ENERGY or BANDWIDTH and amount must be a positive integer", nil)
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
