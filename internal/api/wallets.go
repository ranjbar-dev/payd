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
	s.writeWalletPage(w, r, false)
}

func (s *Server) walletsWithBalance(w http.ResponseWriter, r *http.Request) {
	s.writeWalletPage(w, r, true)
}

func (s *Server) writeWalletPage(w http.ResponseWriter, r *http.Request, withConfirmedBalance bool) {
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
	var addresses []store.WalletAddress
	if withConfirmedBalance {
		addresses, err = s.store.WalletAddressesWithConfirmedBalancePage(r.Context(), after, limit+1)
	} else {
		addresses, err = s.store.WalletAddressPage(r.Context(), false, after, limit+1)
	}
	if err != nil {
		s.logger.Error("list wallets", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	next := ""
	if len(addresses) > limit {
		next = encodeCursor(strconv.FormatInt(addresses[limit-1].ID, 10))
		addresses = addresses[:limit]
	}
	view, err := s.loadWalletView(r.Context())
	if err != nil {
		s.logger.Error("load wallet view", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	items := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		item, err := s.walletJSON(address, view)
		if err != nil {
			s.logger.Error("format wallet", "address", address.Address, "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"wallets": items, "next_cursor": next})
}

func (s *Server) getWallet(w http.ResponseWriter, r *http.Request) {
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
	view, err := s.loadWalletView(r.Context())
	if err != nil {
		s.logger.Error("load wallet view", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	item, err := s.walletJSON(address, view)
	if err != nil {
		s.logger.Error("format wallet", "address", address.Address, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	payments, err := s.store.AddressPayments(r.Context(), address.ID, after, limit+1)
	if err != nil {
		s.logger.Error("list wallet payments", "address", address.Address, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	next := ""
	if len(payments) > limit {
		next = encodeCursor(strconv.FormatInt(payments[limit-1].ID, 10))
	}
	item["payments"] = s.paymentJSON(payments[:min(len(payments), limit)])
	item["next_cursor"] = next
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
	view, err := s.loadWalletView(r.Context())
	if err != nil {
		s.logger.Error("load wallet resource view", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	items := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		item, err := s.walletJSON(address, view)
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
	var request struct {
		Asset    string `json:"asset"`
		ChainRaw string `json:"chain_raw"`
		TOTP     string `json:"totp"` // Accepted only to reject the retired credential transport (API-022a).
	}
	if err := decodeJSON(w, r, &request, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is invalid", nil)
		return
	}
	if request.TOTP != "" {
		writeError(w, http.StatusBadRequest, "totp_in_body", "send the TOTP code in the X-TOTP header, not the request body", nil)
		return
	}
	chain, ok := new(big.Int).SetString(request.ChainRaw, 10)
	if request.Asset == "" || !ok || chain.Sign() < 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "asset and a non-negative base-unit chain_raw are required", nil)
		return
	}
	if err := s.ValidateTOTP(r.Context(), r.Header.Get("X-TOTP"), time.Now()); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_totp", "TOTP is invalid or already used", nil)
		return
	}
	state := requestStateFrom(r.Context())
	if err := s.store.ClearBalanceDrift(r.Context(), r.PathValue("address"), request.Asset, chain.String(), state.keyName, r.RemoteAddr, time.Now()); err != nil {
		if errors.Is(err, store.ErrAddressNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "wallet address was not found", nil)
		} else if errors.Is(err, store.ErrBalanceDriftChanged) {
			writeError(w, http.StatusConflict, "drift_ack_mismatch", "asset drift is absent or its chain balance changed", map[string]any{"asset": request.Asset})
		} else {
			s.logger.Error("clear balance drift", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": request.Asset, "chain_raw": chain.String(), "drift_detected": false})
}

type walletView struct {
	assets    map[string]config.Asset
	price     config.Price
	resources config.Resources
	prices    []store.Price
	params    store.ChainParameters
	hasParams bool
	now       time.Time
}

func (s *Server) loadWalletView(ctx context.Context) (walletView, error) {
	s.mu.RLock()
	assets := make(map[string]config.Asset, len(s.assets))
	for symbol, asset := range s.assets {
		assets[symbol] = asset
	}
	priceConfig, resources := s.price, s.resources
	s.mu.RUnlock()
	prices, err := s.store.Prices(ctx)
	if err != nil {
		return walletView{}, err
	}
	params, err := s.store.LoadChainParameters(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return walletView{}, err
	}
	return walletView{assets: assets, price: priceConfig, resources: resources, prices: prices,
		params: params, hasParams: err == nil, now: time.Now()}, nil
}

func (s *Server) walletJSON(address store.WalletAddress, view walletView) (map[string]any, error) {
	energyAvailable := max(address.EnergyLimit-address.EnergyUsed, 0)
	bandwidthAvailable := max(address.BandwidthLimit-address.BandwidthUsed, 0)
	energySufficient := energyAvailable >= view.resources.MinEnergy
	bandwidthSufficient := bandwidthAvailable >= view.resources.MinBandwidth
	balances := make([]map[string]any, 0, len(address.Balances))
	trxRaw := new(big.Int)
	drift := false
	for _, balance := range address.Balances {
		asset, ok := view.assets[balance.Asset]
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
		if balance.ChainRaw != nil {
			item["chain_raw"] = *balance.ChainRaw
		}
		if quote, err := price.CurrentFromPrices(view.prices, view.price, balance.Asset, view.now); err == nil {
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
	bandwidthBurnSufficient := false
	if view.hasParams {
		cost := new(big.Int).Mul(big.NewInt(view.resources.MinBandwidth), big.NewInt(view.params.TransactionFee))
		bandwidthBurnSufficient = trxRaw.Cmp(cost) >= 0
	}
	trxForBandwidth, err := store.FormatUnits(trxRaw.String(), 6)
	if err != nil {
		return nil, err
	}
	canWithdraw := make(map[string]bool, len(view.assets))
	for symbol, asset := range view.assets {
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
		"energy":                 map[string]any{"available": energyAvailable, "limit": address.EnergyLimit, "required": view.resources.MinEnergy, "sufficient": energySufficient},
		"bandwidth":              map[string]any{"available": bandwidthAvailable, "limit": address.BandwidthLimit, "required": view.resources.MinBandwidth, "sufficient": bandwidthSufficient},
		"trx_for_bandwidth_burn": trxForBandwidth, "can_withdraw": canWithdraw,
		"blocked_by": blocked, "drift_detected": drift, "needs_resources": address.NeedsResources,
	}
	if address.ResourcesCheckedAt != nil {
		item["checked_at"] = *address.ResourcesCheckedAt
	}
	if view.hasParams {
		burnSun := energy.BurnCostSun(view.resources.MinEnergy, view.params.EnergyFee)
		burn, err := store.FormatUnits(burnSun.String(), 6)
		if err != nil {
			return nil, err
		}
		item["estimated_burn_trx"], item["energy_fee_sun"] = burn, view.params.EnergyFee
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
