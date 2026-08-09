package api

import (
	"net/http"
	"time"

	"payd/internal/price"
)

func (s *Server) listPrices(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	prices, err := s.store.Prices(r.Context())
	if err != nil {
		s.logger.Error("list prices", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	s.mu.RLock()
	staleAfter := s.price.StaleAfter
	s.mu.RUnlock()
	start := 0
	for start < len(prices) && prices[start].Symbol <= cursor {
		start++
	}
	end := min(start+limit, len(prices))
	items := make([]map[string]any, 0, end-start)
	for _, item := range prices[start:end] {
		items = append(items, map[string]any{
			"symbol": item.Symbol, "price_usd": item.PriceUSD, "source": item.Source,
			"fetched_at": item.FetchedAt, "stale": price.IsStale(item.FetchedAt, time.Now(), staleAfter),
		})
	}
	next := ""
	if end < len(prices) && len(items) > 0 {
		next = encodeCursor(prices[end-1].Symbol)
	}
	writeJSON(w, http.StatusOK, map[string]any{"prices": items, "next_cursor": next})
}
