package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func (s *Server) chainParameters(w http.ResponseWriter, r *http.Request) {
	params, err := s.store.LoadChainParameters(r.Context())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusServiceUnavailable, "chain_params_unavailable", "chain parameters are unavailable", nil)
		return
	}
	if err != nil {
		s.logger.Error("load chain parameters", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"getEnergyFee": params.EnergyFee, "getTransactionFee": params.TransactionFee,
		"fetched_at": params.FetchedAt, "stale": time.Since(time.Unix(params.FetchedAt, 0)) > 6*time.Hour,
	})
}
