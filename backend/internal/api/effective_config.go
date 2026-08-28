package api

import (
	"net/http"
	"sort"
)

func (s *Server) effectiveConfig(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	assets := make([]map[string]any, 0, len(s.assets))
	for _, asset := range s.assets {
		assets = append(assets, map[string]any{"symbol": asset.Symbol, "kind": asset.Kind, "contract": asset.Contract,
			"decimals": asset.Decimals, "min_deposit": asset.MinDeposit, "verified": asset.Verified})
	}
	consumers := make([]string, 0, len(s.ipn.Consumers))
	for _, consumer := range s.ipn.Consumers {
		consumers = append(consumers, consumer.Name)
	}
	withdrawalRequireTOTP, dailyLimit := s.withdrawal.RequireTOTP, s.withdrawal.DailyLimitUSD
	confirmations, reorgDepth := s.tron.ConfirmationsRequired, s.tron.ReorgDepth
	defaultTTL := s.orders.DefaultTTL
	energyEnabled := s.energy.Enabled
	// Operator-display thresholds: every one is a number, a duration, or a decimal
	// string, so none is structurally capable of holding a credential (API-043,
	// CFG-011). A dashboard that cannot read these cannot say how close a figure
	// is to the limit that will reject it, and would have to hardcode a second
	// copy of the operator's configuration to try.
	maxBurnTRX, balanceWarnTRX := s.energy.MaxBurnTRX, s.energy.BalanceWarnTRX
	priceStaleAfter := s.price.StaleAfter
	poolMinFree, poolMaxSize, cooldown := s.wallet.PoolMinFree, s.wallet.PoolMaxSize, s.cooldown
	bandwidthTopupTRX, minEnergy, minBandwidth := s.resources.BandwidthTopupTRX, s.resources.MinEnergy, s.resources.MinBandwidth
	s.mu.RUnlock()
	sort.Slice(assets, func(i, j int) bool { return assets[i]["symbol"].(string) < assets[j]["symbol"].(string) })
	sort.Strings(consumers)
	// API-043/CFG-011: this explicit projection has no field capable of holding a credential.
	writeJSON(w, http.StatusOK, map[string]any{
		"assets":     assets,
		"withdrawal": map[string]any{"require_totp": withdrawalRequireTOTP, "daily_limit_usd": dailyLimit},
		"tron":       map[string]any{"confirmations_required": confirmations, "reorg_depth": reorgDepth},
		"orders":     map[string]any{"default_ttl_seconds": int64(defaultTTL.Seconds())},
		"energy": map[string]any{"enabled": energyEnabled, "max_burn_trx": maxBurnTRX,
			"balance_warn_trx": balanceWarnTRX},
		"price": map[string]any{"stale_after_seconds": int64(priceStaleAfter.Seconds())},
		"wallet": map[string]any{"pool_min_free": poolMinFree, "pool_max_size": poolMaxSize,
			"cooldown_seconds": int64(cooldown.Seconds())},
		// WRES-023: the reserve the resource wallet is expected to cover, plus the
		// minimums a withdrawal is checked against. Without them a dashboard can show
		// what a wallet holds but not whether that is enough, which is the only
		// question being asked of this card.
		"resources": map[string]any{"bandwidth_topup_trx": bandwidthTopupTRX,
			"min_energy": minEnergy, "min_bandwidth": minBandwidth},
		"consumers": consumers,
	})
}
