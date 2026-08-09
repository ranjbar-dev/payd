package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"payd/internal/config"
	"payd/internal/energy"
	"payd/internal/price"
	"payd/internal/store"
)

func (s *Server) listWallets(w http.ResponseWriter, r *http.Request) {
	addresses, err := s.store.WalletAddresses(r.Context(), false)
	if err != nil {
		s.logger.Error("list wallets", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	s.writeWalletPage(w, r, addresses)
}

func (s *Server) walletsWithBalance(w http.ResponseWriter, r *http.Request) {
	addresses, err := s.store.WalletAddressesWithConfirmedBalance(r.Context())
	if err != nil {
		s.logger.Error("list wallets with balance", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	s.writeWalletPage(w, r, addresses)
}

func (s *Server) writeWalletPage(w http.ResponseWriter, r *http.Request, addresses []store.WalletAddress) {
	limit, cursor, err := pagination(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	after := int64(0)
	if cursor != "" {
		after, _ = strconv.ParseInt(cursor, 10, 64)
		if after < 0 {
			writeError(w, http.StatusBadRequest, "invalid_pagination", "cursor is invalid", nil)
			return
		}
	}
	start := 0
	for start < len(addresses) && addresses[start].ID <= after {
		start++
	}
	end := min(start+limit, len(addresses))
	items := make([]map[string]any, 0, end-start)
	for _, address := range addresses[start:end] {
		item, err := s.walletJSON(r.Context(), address)
		if err != nil {
			s.logger.Error("format wallet", "address", address.Address, "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		items = append(items, item)
	}
	next := ""
	if end < len(addresses) && len(items) > 0 {
		next = encodeCursor(strconv.FormatInt(addresses[end-1].ID, 10))
	}
	writeJSON(w, http.StatusOK, map[string]any{"wallets": items, "next_cursor": next})
}

func (s *Server) getWallet(w http.ResponseWriter, r *http.Request) {
	address, err := s.store.WalletAddress(r.Context(), r.PathValue("address"))
	if errors.Is(err, store.ErrAddressNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "wallet address was not found", nil)
		return
	}
	if err != nil {
		s.logger.Error("get wallet", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	item, err := s.walletJSON(r.Context(), address)
	if err != nil {
		s.logger.Error("format wallet", "address", address.Address, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	payments, err := s.store.AddressPayments(r.Context(), address.ID)
	if err != nil {
		s.logger.Error("list wallet payments", "address", address.Address, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	item["payments"] = s.paymentJSON(payments)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) disableWallet(w http.ResponseWriter, r *http.Request) {
	state := requestStateFrom(r.Context())
	address := r.PathValue("address")
	if err := s.store.DisableAddress(r.Context(), address, state.keyName, r.RemoteAddr, time.Now()); err != nil {
		if errors.Is(err, store.ErrAddressNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "wallet address was not found", nil)
		} else {
			s.logger.Error("disable wallet", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"address": address, "state": "disabled"})
}

func (s *Server) walletsNeedingResources(w http.ResponseWriter, r *http.Request) {
	addresses, err := s.store.WalletAddresses(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	items := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		item, err := s.walletJSON(r.Context(), address)
		if err != nil {
			s.logger.Error("format wallet resources", "address", address.Address, "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": items, "total": len(items)})
}

func (s *Server) clearBalanceDrift(w http.ResponseWriter, r *http.Request) {
	state := requestStateFrom(r.Context())
	if err := s.store.ClearBalanceDrift(r.Context(), r.PathValue("address"), state.keyName, r.RemoteAddr, time.Now()); err != nil {
		if errors.Is(err, store.ErrAddressNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "wallet address was not found", nil)
		} else {
			s.logger.Error("clear balance drift", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drift_detected": false})
}

func (s *Server) walletJSON(ctx context.Context, address store.WalletAddress) (map[string]any, error) {
	s.mu.RLock()
	assets := make(map[string]config.Asset, len(s.assets))
	for symbol, asset := range s.assets {
		assets[symbol] = asset
	}
	priceConfig, resources := s.price, s.resources
	s.mu.RUnlock()
	energyAvailable := max(address.EnergyLimit-address.EnergyUsed, 0)
	bandwidthAvailable := max(address.BandwidthLimit-address.BandwidthUsed, 0)
	energySufficient := energyAvailable >= resources.MinEnergy
	bandwidthSufficient := bandwidthAvailable >= resources.MinBandwidth
	balances := make([]map[string]any, 0, len(address.Balances))
	trxRaw := new(big.Int)
	drift := false
	for _, balance := range address.Balances {
		asset, ok := assets[balance.Asset]
		if !ok {
			return nil, fmt.Errorf("unknown wallet asset %s", balance.Asset)
		}
		confirmed, err := store.FormatUnits(balance.ConfirmedRaw, asset.Decimals)
		if err != nil {
			return nil, err
		}
		pending, err := store.FormatUnits(balance.PendingRaw, asset.Decimals)
		if err != nil {
			return nil, err
		}
		item := map[string]any{"asset": balance.Asset, "confirmed": confirmed, "pending": pending, "drift_detected": balance.Drift}
		if quote, err := price.Current(ctx, s.store, priceConfig, balance.Asset, time.Now()); err == nil {
			if usd, ok := amountUSD(balance.ConfirmedRaw, asset.Decimals, quote.USD); ok {
				item["usd"] = usd
			}
		}
		balances = append(balances, item)
		if balance.Asset == "TRX" {
			trxRaw.SetString(balance.ConfirmedRaw, 10)
		}
		drift = drift || balance.Drift
	}
	params, paramsErr := s.store.LoadChainParameters(ctx)
	if paramsErr != nil && !errors.Is(paramsErr, sql.ErrNoRows) {
		return nil, paramsErr
	}
	bandwidthBurnSufficient := false
	if paramsErr == nil {
		cost := new(big.Int).Mul(big.NewInt(resources.MinBandwidth), big.NewInt(params.TransactionFee))
		bandwidthBurnSufficient = trxRaw.Cmp(cost) >= 0
	}
	trxForBandwidth, err := store.FormatUnits(trxRaw.String(), 6)
	if err != nil {
		return nil, err
	}
	canWithdraw := make(map[string]bool, len(assets))
	for symbol, asset := range assets {
		// RES-016 / API-013: energy alone never makes either asset kind withdrawable.
		canWithdraw[symbol] = !drift && (bandwidthSufficient || bandwidthBurnSufficient) && (asset.Kind == "native" || energySufficient)
	}
	blocked := make([]string, 0, 2)
	if !bandwidthSufficient && !bandwidthBurnSufficient {
		blocked = append(blocked, "bandwidth")
	}
	if !energySufficient {
		blocked = append(blocked, "energy")
	}
	item := map[string]any{
		"address": address.Address, "hd_index": address.HDIndex, "state": address.State, "balances": balances,
		"energy":                 map[string]any{"available": energyAvailable, "limit": address.EnergyLimit, "required": resources.MinEnergy, "sufficient": energySufficient},
		"bandwidth":              map[string]any{"available": bandwidthAvailable, "limit": address.BandwidthLimit, "required": resources.MinBandwidth, "sufficient": bandwidthSufficient},
		"trx_for_bandwidth_burn": trxForBandwidth, "can_withdraw": canWithdraw,
		"blocked_by": blocked, "drift_detected": drift, "needs_resources": address.NeedsResources,
	}
	if address.ResourcesCheckedAt != nil {
		item["checked_at"] = *address.ResourcesCheckedAt
	}
	if paramsErr == nil {
		burnSun := energy.BurnCostSun(resources.MinEnergy, params.EnergyFee)
		burn, err := store.FormatUnits(burnSun.String(), 6)
		if err != nil {
			return nil, err
		}
		item["estimated_burn_trx"], item["energy_fee_sun"] = burn, params.EnergyFee
	}
	return item, nil
}

// ValidateWithdrawalSource maps BAL-002 into the HTTP conflict used by P11's handler.
func (s *Server) ValidateWithdrawalSource(ctx context.Context, address, asset string) (store.Balance, int, string, error) {
	balance, err := s.store.BalanceForWithdrawal(ctx, address, asset)
	if errors.Is(err, store.ErrBalanceDrift) {
		return store.Balance{}, http.StatusConflict, "balance_drift", err
	}
	return balance, 0, "", err
}
