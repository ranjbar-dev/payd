package api

import (
	"database/sql"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
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
		TOTP        string `json:"totp"` // Accepted solely so the retired field can be rejected by name.
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
	// API-022: the code travels in X-TOTP so it never sits in a body that a proxy or client
	// may log or replay. A body-supplied code is refused rather than ignored — silently
	// dropping it would let a caller believe it had presented a second factor when it had not.
	if request.TOTP != "" {
		writeError(w, http.StatusBadRequest, "totp_in_body", "send the TOTP code in the X-TOTP header, not the request body", nil)
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
		if err := s.ValidateTOTP(r.Context(), r.Header.Get("X-TOTP"), time.Now()); err != nil {
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
		var details map[string]any
		if cfg.RequireTOTP {
			// API-022: synchronous rejection happens after the single-use code was consumed.
			details = map[string]any{"totp_consumed": true}
		}
		s.writeWithdrawalErrorDetails(w, err, details)
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
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	var beforeCreatedAt int64
	var beforeID string
	if cursor != "" {
		createdAt, id, found := strings.Cut(cursor, "\x00")
		beforeCreatedAt, err = strconv.ParseInt(createdAt, 10, 64)
		if !found || err != nil || id == "" || strings.ContainsRune(id, '\x00') {
			writeError(w, http.StatusBadRequest, "invalid_pagination", "cursor is invalid", nil)
			return
		}
		beforeID = id
	}
	withdrawals, err := s.store.ListWithdrawals(r.Context(), r.URL.Query().Get("status"), beforeCreatedAt, beforeID, limit+1)
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	items := make([]map[string]any, 0, min(len(withdrawals), limit))
	for _, withdrawal := range withdrawals[:min(len(withdrawals), limit)] {
		items = append(items, s.withdrawalJSON(withdrawal))
	}
	next := ""
	if len(withdrawals) > limit {
		last := withdrawals[limit-1]
		next = encodeCursor(strconv.FormatInt(last.CreatedAt, 10) + "\x00" + last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
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

func (s *Server) resolveWithdrawal(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Outcome       string `json:"outcome"`
		FailureReason string `json:"failure_reason"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil ||
		(request.Outcome != "confirmed" && request.Outcome != "failed") ||
		(request.Outcome == "failed" && request.FailureReason == "") {
		writeError(w, http.StatusBadRequest, "invalid_request", "outcome must be confirmed or failed; failed requires failure_reason", nil)
		return
	}
	if err := s.ValidateTOTP(r.Context(), r.Header.Get("X-TOTP"), time.Now()); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_totp", "TOTP is invalid or already used", nil)
		return
	}
	state := requestStateFrom(r.Context())
	resolved, err := s.store.ResolveWithdrawal(r.Context(), r.PathValue("id"), request.Outcome, request.FailureReason,
		state.keyName, r.RemoteAddr, time.Now())
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.withdrawalJSON(resolved))
}

func (s *Server) estimateWithdrawal(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FromAddress string `json:"from_address"`
		ToAddress   string `json:"to_address"`
		Asset       string `json:"asset"`
		Amount      string `json:"amount"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is invalid", nil)
		return
	}
	decimals, configured := s.assetDecimals(request.Asset)
	raw, parseErr := store.ParseUnits(request.Amount, decimals)
	if !configured || parseErr != nil || raw == nil || raw.Sign() <= 0 ||
		!hdwallet.IsValidAddress(hdwallet.TRX, request.ToAddress) {
		writeError(w, http.StatusBadRequest, "invalid_withdrawal", "configured asset, destination, and positive amount are required", nil)
		return
	}
	balance, err := s.store.BalanceForWithdrawal(r.Context(), request.FromAddress, request.Asset)
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	s.mu.RLock()
	delegator, withdrawalCfg, priceCfg := s.delegator, s.withdrawal, s.price
	asset := s.assets[request.Asset]
	s.mu.RUnlock()
	if delegator == nil {
		writeError(w, http.StatusServiceUnavailable, "estimator_unavailable", "resource estimator is unavailable", nil)
		return
	}
	resources, err := delegator.EstimateResources(r.Context(), request.FromAddress, asset.Kind)
	if err != nil {
		s.logger.Error("estimate withdrawal resources", "error", err)
		writeError(w, http.StatusBadGateway, "resource_estimate_failed", "live resource estimate failed", nil)
		return
	}
	confirmed, ok := new(big.Int).SetString(balance.ConfirmedRaw, 10)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "stored balance is invalid", nil)
		return
	}
	required := new(big.Int).Set(raw)
	projectedCost := new(big.Int)
	if resources.TRXCost != "" {
		cost, costErr := store.ParseUnits(resources.TRXCost, 6)
		if costErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "resource estimate is invalid", nil)
			return
		}
		projectedCost.Set(cost)
		if asset.Kind == "native" {
			required.Add(required, cost)
		}
	}
	balanceSufficient := confirmed.Cmp(required) >= 0
	// A TRC-20 transfer spends two balances on the source address: the asset itself and the TRX
	// that pays for energy. They are reported separately because the remedies differ — deposit
	// more USDT versus top the address up with TRX — and one shared flag sent operators to the
	// wrong one, telling them the balance was short while the asset balance sat 5x the request.
	resourceFundsSufficient := true
	if asset.Kind != "native" && projectedCost.Sign() > 0 {
		trxBalance, balanceErr := s.store.BalanceForWithdrawal(r.Context(), request.FromAddress, "TRX")
		if errors.Is(balanceErr, sql.ErrNoRows) {
			resourceFundsSufficient = false
		} else if balanceErr != nil {
			s.writeWithdrawalError(w, balanceErr)
			return
		} else if available, valid := new(big.Int).SetString(trxBalance.ConfirmedRaw, 10); !valid || available.Cmp(projectedCost) < 0 {
			resourceFundsSufficient = false
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
	amountUSDValue, ok := exactUSD(raw, decimals, quote.USD)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "stored price is invalid", nil)
		return
	}
	used, err := s.store.WithdrawalUSDUsed(r.Context(), time.Now())
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	amountUSD, amountOK := new(big.Rat).SetString(amountUSDValue)
	dailyLimit, limitOK := new(big.Rat).SetString(withdrawalCfg.DailyLimitUSD)
	if !amountOK || !limitOK {
		writeError(w, http.StatusInternalServerError, "internal_error", "withdrawal limit configuration is invalid", nil)
		return
	}
	dailyCapBlocked := new(big.Rat).Add(new(big.Rat).Set(used), amountUSD).Cmp(dailyLimit) > 0
	blockedBy := make([]string, 0, 4)
	if !withdrawalCfg.Enabled {
		blockedBy = append(blockedBy, "withdrawals_disabled")
	}
	if !balanceSufficient {
		blockedBy = append(blockedBy, "confirmed_balance")
	}
	if !resourceFundsSufficient {
		blockedBy = append(blockedBy, "trx_for_resources")
	}
	if dailyCapBlocked {
		blockedBy = append(blockedBy, "daily_usd_cap")
	}
	if resources.BlockedBy != "" {
		blockedBy = append(blockedBy, resources.BlockedBy)
	}
	// UI-060/WWD-070: echo what THIS SERVICE understood the request to be, so a
	// confirmation screen can restate the transfer from the response instead of from
	// the operator's own inputs. `amount` is re-formatted from the parsed base units
	// rather than copied from the request, so an amount the parser normalised is
	// visible before anyone types a TOTP code — the operator confirms the transfer
	// that would actually go out, not the one they believe they typed.
	echoedAmount, formatErr := store.FormatUnits(raw.String(), decimals)
	if formatErr != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "parsed amount is invalid", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from_address": request.FromAddress,
		"to_address":   request.ToAddress,
		"asset":        request.Asset,
		"amount":       echoedAmount,
		"amount_raw":   raw.String(),
		"amount_usd":   amountUSDValue,
		// can_proceed is the single field a caller should gate on: the per-condition flags each
		// answer only their own question, so checking one in isolation misses the others.
		"can_proceed":                  len(blockedBy) == 0,
		"confirmed_balance_sufficient": balanceSufficient,
		"trx_for_resources_sufficient": resourceFundsSufficient,
		"projected_energy_source":      resources.EnergySource,
		"projected_trx_cost":           zeroIfEmpty(resources.TRXCost),
		"daily_cap_blocked":            dailyCapBlocked,
		"blocked_by":                   blockedBy,
	})
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
		"created_at": withdrawal.CreatedAt,
		// WWD-012: when the row entered its current status, so time-in-state is
		// readable. `awaiting_energy` is bounded by the energy poll timeout, and one
		// sitting there for ten minutes is a fault — which cannot be seen against
		// created_at alone.
		"status_updated_at": withdrawal.StatusUpdatedAt,
		"broadcast_at":      withdrawal.BroadcastAt, "confirmed_at": withdrawal.ConfirmedAt}
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
	s.writeWithdrawalErrorDetails(w, err, nil)
}

func (s *Server) writeWithdrawalErrorDetails(w http.ResponseWriter, err error, details map[string]any) {
	switch {
	case errors.Is(err, store.ErrWithdrawalNotFound):
		writeError(w, http.StatusNotFound, "not_found", "withdrawal was not found", nil)
	case errors.Is(err, store.ErrIdempotencyReuse):
		writeError(w, http.StatusConflict, "idempotency_key_reuse", err.Error(), details)
	case errors.Is(err, store.ErrBalanceDrift):
		writeError(w, http.StatusConflict, "balance_drift", err.Error(), details)
	case errors.Is(err, store.ErrDailyLimit):
		writeError(w, http.StatusConflict, "daily_limit_exceeded", err.Error(), details)
	case errors.Is(err, store.ErrInsufficientFunds):
		writeError(w, http.StatusConflict, "insufficient_confirmed_balance", err.Error(), details)
	case errors.Is(err, store.ErrSourceUnavailable), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusBadRequest, "invalid_source", "source address is unavailable", nil)
	case errors.Is(err, store.ErrWithdrawalState):
		writeError(w, http.StatusConflict, "invalid_state", err.Error(), details)
	default:
		s.logger.Error("withdrawal API failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}
