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
	s.mu.RUnlock()
	sort.Slice(assets, func(i, j int) bool { return assets[i]["symbol"].(string) < assets[j]["symbol"].(string) })
	sort.Strings(consumers)
	// API-043/CFG-011: this explicit projection has no field capable of holding a credential.
	writeJSON(w, http.StatusOK, map[string]any{
		"assets":     assets,
		"withdrawal": map[string]any{"require_totp": withdrawalRequireTOTP, "daily_limit_usd": dailyLimit},
		"tron":       map[string]any{"confirmations_required": confirmations, "reorg_depth": reorgDepth},
		"orders":     map[string]any{"default_ttl_seconds": int64(defaultTTL.Seconds())},
		"energy":     map[string]any{"enabled": energyEnabled},
		"consumers":  consumers,
	})
}
