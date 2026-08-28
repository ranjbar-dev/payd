package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"time"

	"payd/internal/energy"
	"payd/internal/store"
)

// burnExceedsCeiling compares the worst-case burn against the configured ceiling.
// The second return is false when either figure is absent or unparsable: an
// unknown comparison must render as unknown, never as "within limits".
func burnExceedsCeiling(burn, ceiling string) (bool, bool) {
	have, ok := new(big.Rat).SetString(burn)
	limit, limitOK := new(big.Rat).SetString(ceiling)
	if !ok || !limitOK {
		return false, false
	}
	return have.Cmp(limit) > 0, true
}

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
	s.mu.RLock()
	minEnergy, maxBurnTRX := s.resources.MinEnergy, s.energy.MaxBurnTRX
	s.mu.RUnlock()
	// WRES-012/WRES-015: the worst-case burn and its verdict against the configured
	// ceiling are computed here, from the fee that was actually read, because both
	// are money. A client deriving them would be doing decimal arithmetic on a cost
	// (INV-2) and duplicating a rule the engine already owns (ENR-017): a ceiling
	// set below the real burn silently disables the fallback of last resort, and
	// that is only visible if the two figures are compared by whoever holds both.
	burnSun := energy.BurnCostSun(minEnergy, params.EnergyFee)
	worstCaseBurn, err := store.FormatUnits(burnSun.String(), 6)
	if err != nil {
		s.logger.Error("format worst case burn", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	body := map[string]any{
		"getEnergyFee": params.EnergyFee, "getTransactionFee": params.TransactionFee,
		"fetched_at": params.FetchedAt, "stale": time.Since(time.Unix(params.FetchedAt, 0)) > 6*time.Hour,
		"worst_case_burn_trx": worstCaseBurn, "max_burn_trx": maxBurnTRX,
	}
	if exceeds, ok := burnExceedsCeiling(worstCaseBurn, maxBurnTRX); ok {
		body["burn_exceeds_ceiling"] = exceeds
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) chainStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.store.OperationalStatus(r.Context())
	if err != nil {
		s.logger.Error("load chain status", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	now := time.Now().UTC().Unix()
	lagSeconds := int64(0)
	if status.LastBlockTimestamp > 0 {
		lagSeconds = max(now-status.LastBlockTimestamp, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"last_height": status.LastHeight, "solidified_height": status.SolidifiedHeight,
		"lag_blocks": max(status.LastHeight-status.SolidifiedHeight, 0), "lag_seconds": lagSeconds,
		"reorg_suspected": status.ReorgSuspected, "last_block_timestamp": status.LastBlockTimestamp,
	})
}

func (s *Server) chainQuota(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	history, err := s.store.TronGridRequestHistory(r.Context(), now)
	if err != nil {
		s.logger.Error("load TronGrid quota history", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	s.mu.RLock()
	quota := s.tron.DailyRequestQuota
	s.mu.RUnlock()
	if quota <= 0 {
		writeError(w, http.StatusServiceUnavailable, "quota_unavailable", "daily request quota is not configured", nil)
		return
	}
	todayStart, requests := now.Truncate(24*time.Hour).Unix(), int64(0)
	items := make([]map[string]any, 0, len(history))
	for _, day := range history {
		if day.DayStart == todayStart {
			requests = day.Requests
		}
		items = append(items, map[string]any{"day_start": day.DayStart, "requests": day.Requests})
	}
	numerator := new(big.Int).Mul(big.NewInt(requests), big.NewInt(100))
	percent := new(big.Rat).SetFrac(numerator, big.NewInt(quota))
	writeJSON(w, http.StatusOK, map[string]any{
		"requests_today": requests, "daily_request_quota": quota,
		"percent_used": json.Number(decimalRat(percent)), "history": items,
	})
}
