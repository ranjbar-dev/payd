package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"payd/internal/config"
	ipndelivery "payd/internal/ipn"
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
			// WIPN-031: the body as composed when the event was queued, so an operator
			// can see exactly what a redelivery would send. It is a snapshot and may
			// contradict the order's current state (WIPN-033).
			"payload": json.RawMessage(event.Payload),
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

func (s *Server) testIPN(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Consumer string `json:"consumer"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil || request.Consumer == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "consumer is required", nil)
		return
	}
	s.mu.RLock()
	timeout := s.ipn.Timeout
	consumers := append([]config.Consumer(nil), s.ipn.Consumers...)
	s.mu.RUnlock()
	for _, consumer := range consumers {
		if consumer.Name != request.Consumer {
			continue
		}
		status, latency, err := ipndelivery.SendTest(r.Context(), consumer, timeout, time.Now())
		if err != nil {
			writeError(w, http.StatusBadGateway, "delivery_failed", "test webhook delivery failed", map[string]any{
				"status_code": status, "latency_ms": latency.Milliseconds(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status_code": status, "latency_ms": latency.Milliseconds()})
		return
	}
	writeError(w, http.StatusBadRequest, "unknown_consumer", "consumer is not configured", nil)
}

func (s *Server) replayIPN(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Consumer string `json:"consumer"`
		From     *int64 `json:"from"`
		To       *int64 `json:"to"`
		DryRun   *bool  `json:"dry_run"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil ||
		(request.From != nil && request.To != nil && *request.From > *request.To) {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body or time range is invalid", nil)
		return
	}
	dryRun := true
	if request.DryRun != nil {
		dryRun = *request.DryRun
	}
	const batchLimit = 200
	count, err := s.store.ReplayDeadIPN(r.Context(), request.Consumer, request.From, request.To, batchLimit, dryRun, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}
