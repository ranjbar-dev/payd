package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"payd/internal/config"
	"payd/internal/energy"
	"payd/internal/price"
	"payd/internal/store"
)

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
		"address": address.Address, "hd_index": address.HDIndex, "balances": balances,
		"energy":                 map[string]any{"available": energyAvailable, "limit": address.EnergyLimit, "required": resources.MinEnergy, "sufficient": energySufficient},
		"bandwidth":              map[string]any{"available": bandwidthAvailable, "limit": address.BandwidthLimit, "required": resources.MinBandwidth, "sufficient": bandwidthSufficient},
		"trx_for_bandwidth_burn": trxForBandwidth, "can_withdraw": canWithdraw,
		"blocked_by": blocked, "drift_detected": drift,
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
