package api

import (
	"net/http"

	"payd/internal/store"
)

func (s *Server) volumeReport(w http.ResponseWriter, r *http.Request) {
	from, to, err := unixRange(r.URL.Query(), true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error(), nil)
		return
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "day"
	}
	if groupBy != "day" && groupBy != "asset" && groupBy != "consumer" {
		writeError(w, http.StatusBadRequest, "invalid_filter", "group_by must be day, asset, or consumer", nil)
		return
	}
	s.mu.RLock()
	decimals := make(map[string]int, len(s.assets))
	for symbol, asset := range s.assets {
		decimals[symbol] = asset.Decimals
	}
	s.mu.RUnlock()
	buckets, err := s.store.VolumeReport(r.Context(), *from, *to, groupBy, decimals)
	if err != nil {
		s.logger.Error("build volume report", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	items := make([]map[string]any, 0, len(buckets))
	for _, bucket := range buckets {
		volume := make(map[string]string, len(bucket.VolumeRaw))
		for asset, raw := range bucket.VolumeRaw {
			formatted, err := store.FormatUnits(raw, decimals[asset])
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
				return
			}
			volume[asset] = formatted
		}
		items = append(items, map[string]any{"key": bucket.Key, "order_count": bucket.OrderCount,
			"paid_count": bucket.PaidCount, "volume": volume, "usd_total": bucket.USDTotal,
			"unpriced_paid_count": bucket.UnpricedPaidCount})
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_by": groupBy, "from": *from, "to": *to, "buckets": items})
}

func (s *Server) feeReport(w http.ResponseWriter, r *http.Request) {
	from, to, err := unixRange(r.URL.Query(), true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error(), nil)
		return
	}
	report, err := s.store.FeeReport(r.Context(), *from, *to)
	if err != nil {
		s.logger.Error("build fee report", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": *from, "to": *to,
		"energy_by_source_trx": report.EnergyBySource, "bandwidth_by_source_trx": report.BandwidthBySource,
		"rental_spend_trx": report.RentalSpendTRX})
}
