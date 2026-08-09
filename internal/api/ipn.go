package api

import (
	"net/http"
	"sort"
	"time"

	"payd/internal/config"
)

func (s *Server) listDeadIPN(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	events, err := s.store.ListDeadIPN(r.Context(), r.URL.Query().Get("consumer"), cursor, limit+1)
	if err != nil {
		s.logger.Error("list dead IPN", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	next := ""
	if len(events) > limit {
		next = encodeCursor(events[limit-1].ID)
	}
	items := make([]map[string]any, 0, min(len(events), limit))
	for _, event := range events[:min(len(events), limit)] {
		items = append(items, map[string]any{
			"id": event.ID, "order_id": event.OrderID, "consumer": event.Consumer,
			"event_type": event.EventType, "attempts": event.Attempts, "last_error": event.LastError,
			"last_status_code": event.LastStatusCode, "created_at": event.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": items, "next_cursor": next})
}

func (s *Server) retryIPN(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	retried, err := s.store.RetryIPN(r.Context(), id, time.Now())
	if err != nil {
		s.logger.Error("retry IPN", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	if !retried {
		status, found, err := s.store.IPNStatus(r.Context(), id)
		if err != nil {
			s.logger.Error("read IPN status", "id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "not_found", "IPN event was not found", nil)
			return
		}
		writeError(w, http.StatusConflict, "invalid_state", "only dead IPN events can be retried", map[string]any{"status": status})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "pending"})
}

func (s *Server) listIPNConsumers(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	metrics, err := s.store.OperationalMetrics(r.Context())
	if err != nil {
		s.logger.Error("load IPN consumer metrics", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	s.mu.RLock()
	consumers := append([]config.Consumer(nil), s.ipn.Consumers...)
	s.mu.RUnlock()
	sort.Slice(consumers, func(i, j int) bool { return consumers[i].Name < consumers[j].Name })
	start := 0
	for start < len(consumers) && consumers[start].Name <= cursor {
		start++
	}
	end := min(start+limit, len(consumers))
	items := make([]map[string]any, 0, end-start)
	for _, consumer := range consumers[start:end] {
		items = append(items, map[string]any{
			"name": consumer.Name, "enabled": consumer.Enabled, "receives_global": consumer.ReceivesGlobal,
			"pending": metrics.IPNQueue[consumer.Name], "dead": metrics.IPNDead[consumer.Name],
		})
	}
	next := ""
	if end < len(consumers) && len(items) > 0 {
		next = encodeCursor(consumers[end-1].Name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"consumers": items, "next_cursor": next})
}
