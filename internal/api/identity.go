package api

import (
	"net/http"
	"sort"
)

func (s *Server) whoami(w http.ResponseWriter, r *http.Request) {
	state := requestStateFrom(r.Context())
	scopes := make([]string, 0, len(state.scopes))
	for scope := range state.scopes {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	writeJSON(w, http.StatusOK, map[string]any{"key_name": state.keyName, "scopes": scopes})
}

func (s *Server) listAssets(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	items := make([]map[string]any, 0, len(s.assets))
	for _, asset := range s.assets {
		items = append(items, map[string]any{"symbol": asset.Symbol, "kind": asset.Kind, "contract": asset.Contract,
			"decimals": asset.Decimals, "min_deposit": asset.MinDeposit, "verified": asset.Verified})
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i]["symbol"].(string) < items[j]["symbol"].(string) })
	writeJSON(w, http.StatusOK, map[string]any{"assets": items})
}
