package api

import (
	"database/sql"
	"errors"
	"math/big"
	"net/http"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/price"
	"payd/internal/store"
)

func (s *Server) createWithdrawal(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FromAddress string `json:"from_address"`
		ToAddress   string `json:"to_address"`
		Asset       string `json:"asset"`
		Amount      string `json:"amount"`
		TOTP        string `json:"totp"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is invalid", nil)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key is required", nil)
		return
	}
	existing, exists, err := s.store.WithdrawalByIdempotency(r.Context(), key)
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	decimals, configured := s.assetDecimals(request.Asset)
	raw, parseErr := store.ParseUnits(request.Amount, decimals)
	if exists { // WDR-001a: this branch completes before TOTP is inspected or consumed.
		if !configured || parseErr != nil || existing.FromAddress != request.FromAddress || existing.ToAddress != request.ToAddress ||
			existing.Asset != request.Asset || existing.AmountRaw != raw.String() {
			writeError(w, http.StatusConflict, "idempotency_key_reuse", "Idempotency-Key belongs to a different withdrawal request", nil)
			return
		}
		writeJSON(w, http.StatusOK, s.withdrawalJSON(existing))
		return
	}
	s.mu.RLock()
	cfg, priceCfg := s.withdrawal, s.price
	s.mu.RUnlock()
	if !cfg.Enabled {
		writeError(w, http.StatusServiceUnavailable, "withdrawals_disabled", "withdrawals are disabled", nil)
		return
	}
	if !configured {
		writeError(w, http.StatusBadRequest, "invalid_asset", "asset is not configured", nil)
		return
	}
	if !hdwallet.IsValidAddress(hdwallet.TRX, request.ToAddress) || raw == nil || parseErr != nil || raw.Sign() <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_withdrawal", "destination and positive amount are required", nil)
		return
	}
	if cfg.RequireTOTP {
		if err := s.ValidateTOTP(r.Context(), request.TOTP, time.Now()); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_totp", "TOTP is invalid or already used", nil)
			return
		}
	}
	quote, err := price.Current(r.Context(), s.store, priceCfg, request.Asset, time.Now())
	if err != nil {
		if errors.Is(err, price.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "price_unavailable", "asset price is unavailable or stale", nil)
			return
		}
		s.writeWithdrawalError(w, err)
		return
	}
	usd, ok := exactUSD(raw, decimals, quote.USD)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "stored price is invalid", nil)
		return
	}
	state := requestStateFrom(r.Context())
	created, fresh, err := s.store.CreateWithdrawal(r.Context(), store.CreateWithdrawal{IdempotencyKey: key,
		FromAddress: request.FromAddress, ToAddress: request.ToAddress, Asset: request.Asset, AmountRaw: raw.String(),
		AmountUSD: usd, DailyLimitUSD: cfg.DailyLimitUSD, RequestedBy: state.keyName, IP: r.RemoteAddr, Now: time.Now()})
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	status := http.StatusOK
	if fresh {
		status = http.StatusCreated
	}
	writeJSON(w, status, s.withdrawalJSON(created))
}

func (s *Server) getWithdrawal(w http.ResponseWriter, r *http.Request) {
	withdrawal, err := s.store.Withdrawal(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.withdrawalJSON(withdrawal))
}

func (s *Server) listWithdrawals(w http.ResponseWriter, r *http.Request) {
	limit, _, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	withdrawals, err := s.store.ListWithdrawals(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(withdrawals))
	for _, withdrawal := range withdrawals {
		items = append(items, s.withdrawalJSON(withdrawal))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) withdrawalLimits(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	limit := s.withdrawal.DailyLimitUSD
	s.mu.RUnlock()
	used, err := s.store.WithdrawalUSDUsed(r.Context(), time.Now())
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	remaining, _ := new(big.Rat).SetString(limit)
	remaining.Sub(remaining, used)
	if remaining.Sign() < 0 {
		remaining.SetInt64(0)
	}
	writeJSON(w, http.StatusOK, map[string]any{"daily_limit_usd": limit, "used_usd": decimalRat(used), "remaining_usd": decimalRat(remaining)})
}

func (s *Server) withdrawalJSON(withdrawal store.Withdrawal) map[string]any {
	decimals, _ := s.assetDecimals(withdrawal.Asset)
	amount, _ := store.FormatUnits(withdrawal.AmountRaw, decimals)
	networkFee, resourceFee, bandwidthCost, totalCost := withdrawalCosts(withdrawal)
	return map[string]any{"id": withdrawal.ID, "from_address": withdrawal.FromAddress, "to_address": withdrawal.ToAddress,
		"asset": withdrawal.Asset, "amount": amount, "amount_raw": withdrawal.AmountRaw, "amount_usd": withdrawal.AmountUSD,
		"status": withdrawal.Status, "txid": withdrawal.TxID, "failure_reason": withdrawal.FailureReason,
		"resolved_by": withdrawal.ResolvedBy, "broadcast_response": withdrawal.BroadcastResponse,
		"fee_raw": withdrawal.FeeRaw, "network_fee_trx": networkFee, "energy_used": withdrawal.EnergyUsed,
		"energy_source": withdrawal.EnergySource, "energy_cost_trx": zeroIfEmpty(withdrawal.EnergyCostTRX),
		"bandwidth_source": withdrawal.BandwidthSource, "bandwidth_cost_trx": bandwidthCost,
		"resource_fee_trx": resourceFee, "total_cost_trx": totalCost, "last_lookup_error": withdrawal.LastLookupError,
		"created_at": withdrawal.CreatedAt, "broadcast_at": withdrawal.BroadcastAt, "confirmed_at": withdrawal.ConfirmedAt}
}

// WDR-025: withdrawalCosts keeps on-chain fees in base units until the final format.
// Burned energy/bandwidth is already part of fee_raw; rented energy is not.
func withdrawalCosts(withdrawal store.Withdrawal) (network, resource, bandwidth, total string) {
	parseRaw := func(value string) *big.Int {
		amount, ok := new(big.Int).SetString(value, 10)
		if !ok || amount.Sign() < 0 {
			return new(big.Int)
		}
		return amount
	}
	networkRaw := parseRaw(withdrawal.FeeRaw)
	energyGrantRaw := parseRaw(withdrawal.EnergyGrantFeeRaw)
	bandwidthRaw := parseRaw(withdrawal.BandwidthGrantFeeRaw)
	resourceRaw := new(big.Int).Add(new(big.Int).Set(energyGrantRaw), bandwidthRaw)
	totalValue := new(big.Rat).SetFrac(new(big.Int).Add(new(big.Int).Set(networkRaw), resourceRaw), big.NewInt(1_000_000))
	if withdrawal.EnergySource == "rented" {
		if rented, ok := new(big.Rat).SetString(withdrawal.EnergyCostTRX); ok && rented.Sign() >= 0 {
			totalValue.Add(totalValue, rented)
		}
	}
	network, _ = store.FormatUnits(networkRaw.String(), 6)
	resource, _ = store.FormatUnits(resourceRaw.String(), 6)
	bandwidth, _ = store.FormatUnits(bandwidthRaw.String(), 6)
	return network, resource, bandwidth, decimalRat(totalValue)
}

func zeroIfEmpty(value string) string {
	if value == "" {
		return "0"
	}
	return value
}

func (s *Server) writeWithdrawalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrWithdrawalNotFound):
		writeError(w, http.StatusNotFound, "not_found", "withdrawal was not found", nil)
	case errors.Is(err, store.ErrIdempotencyReuse):
		writeError(w, http.StatusConflict, "idempotency_key_reuse", err.Error(), nil)
	case errors.Is(err, store.ErrBalanceDrift):
		writeError(w, http.StatusConflict, "balance_drift", err.Error(), nil)
	case errors.Is(err, store.ErrDailyLimit):
		writeError(w, http.StatusConflict, "daily_limit_exceeded", err.Error(), nil)
	case errors.Is(err, store.ErrInsufficientFunds):
		writeError(w, http.StatusConflict, "insufficient_confirmed_balance", err.Error(), nil)
	case errors.Is(err, store.ErrSourceUnavailable), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusBadRequest, "invalid_source", "source address is unavailable", nil)
	default:
		s.logger.Error("withdrawal API failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}
