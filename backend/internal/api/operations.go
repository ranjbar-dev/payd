package api

import (
	"net/http"
	"strconv"
	"time"

	"payd/internal/chain"
	"payd/internal/confirm"
	"payd/internal/ipn"
	"payd/internal/lifecycle"
	"payd/internal/store"
	walletpool "payd/internal/wallet"
	"payd/internal/withdraw"
)

// workerIntervals reports each worker's configured tick cadence, so a client can
// tell a stalled loop from an idle one without hardcoding a second copy of these
// values (OPS-008, API-039). Every entry is the constant the worker itself ticks
// on, or the live config value where the cadence is configurable — never a
// literal restated here.
func (s *Server) workerIntervals() map[string]time.Duration {
	s.mu.RLock()
	pollInterval, priceInterval := s.tron.PollInterval, s.price.Interval
	s.mu.RUnlock()
	return map[string]time.Duration{
		"follower":              pollInterval,
		"price":                 priceInterval,
		"confirm":               confirm.PollInterval,
		"chain_params":          chain.ParameterInterval,
		"reconciler_balances":   walletpool.BalanceReconcileInterval,
		"reconciler_safety_net": walletpool.SafetyNetInterval,
		"lifecycle_10s":         lifecycle.ShortInterval,
		"lifecycle_60s":         lifecycle.LongInterval,
		"ipn":                   ipn.TickInterval,
		"withdraw":              withdraw.TickInterval,
	}
}

func (s *Server) workers(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	workers, err := s.store.WorkerHealth(r.Context(), cursor, limit+1)
	if err != nil {
		s.logger.Error("load worker health", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	now := time.Now().UTC().Unix()
	intervals := s.workerIntervals()
	items := make([]map[string]any, 0, min(len(workers), limit))
	for _, worker := range workers[:min(len(workers), limit)] {
		var seconds any
		if worker.LastTickAt != nil {
			seconds = max(now-*worker.LastTickAt, 0)
		}
		item := map[string]any{"worker": worker.Worker, "last_tick_at": worker.LastTickAt,
			"seconds_since_tick": seconds, "last_error": worker.LastError, "error_count": worker.ErrorCount,
			"restarts": worker.Restarts, "expected_interval_seconds": nil}
		if expected, known := intervals[worker.Worker]; known {
			item["expected_interval_seconds"] = int64(expected / time.Second)
		}
		items = append(items, item)
	}
	next := ""
	if len(workers) > limit {
		next = encodeCursor(workers[limit-1].Worker)
	}
	writeJSON(w, http.StatusOK, map[string]any{"workers": items, "next_cursor": next})
}

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	filter := store.AuditFilter{Actor: r.URL.Query().Get("actor"), Action: r.URL.Query().Get("action"),
		Subject: r.URL.Query().Get("subject"), Limit: limit + 1}
	if cursor != "" {
		filter.Before, _ = strconv.ParseInt(cursor, 10, 64)
	}
	filter.From, filter.To, err = unixRange(r.URL.Query(), false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error(), nil)
		return
	}
	entries, err := s.store.ListAudit(r.Context(), filter)
	if err != nil {
		s.logger.Error("list audit log", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	items := make([]map[string]any, 0, min(len(entries), limit))
	for _, entry := range entries[:min(len(entries), limit)] {
		items = append(items, map[string]any{"id": entry.ID, "actor": entry.Actor, "action": entry.Action,
			"subject": entry.Subject, "detail": entry.Detail, "ip": entry.IP, "created_at": entry.CreatedAt})
	}
	next := ""
	if len(entries) > limit {
		next = encodeCursor(strconv.FormatInt(entries[limit-1].ID, 10))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": items, "next_cursor": next})
}
