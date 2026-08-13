package api

import (
	"net/http"
	"strconv"
	"time"

	"payd/internal/store"
)

func (s *Server) listUnattributed(w http.ResponseWriter, r *http.Request) {
	s.listPayments(w, r, "unattributed")
}

func (s *Server) listOrphaned(w http.ResponseWriter, r *http.Request) {
	s.listPayments(w, r, "orphaned")
}

func (s *Server) listAllPayments(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	after := int64(0)
	if cursor != "" {
		after, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil || after < 0 {
			writeError(w, http.StatusBadRequest, "invalid_pagination", "cursor is invalid", nil)
			return
		}
	}
	query := r.URL.Query()
	filter := store.PaymentFilter{TxID: query.Get("txid"), Address: query.Get("address"), OrderID: query.Get("order_id"),
		Status: query.Get("status"), Direction: query.Get("direction"), Asset: query.Get("asset"), After: after, Limit: limit + 1}
	for name, target := range map[string]**int64{"from": &filter.From, "to": &filter.To} {
		if value := query.Get(name); value != "" {
			stamp, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil {
				writeError(w, http.StatusBadRequest, "invalid_filter", name+" must be a Unix timestamp", nil)
				return
			}
			*target = &stamp
		}
	}
	if filter.From != nil && filter.To != nil && *filter.From > *filter.To {
		writeError(w, http.StatusBadRequest, "invalid_filter", "from must not exceed to", nil)
		return
	}
	payments, err := s.store.ListPaymentsFiltered(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	next := ""
	if len(payments) > limit {
		next = encodeCursor(strconv.FormatInt(payments[limit-1].ID, 10))
	}
	writeJSON(w, http.StatusOK, map[string]any{"payments": s.paymentJSON(payments[:min(len(payments), limit)]), "next_cursor": next})
}

func (s *Server) listPayments(w http.ResponseWriter, r *http.Request, status string) {
	limit, cursor, err := pagination(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	after := int64(0)
	if cursor != "" {
		after, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil || after < 0 {
			writeError(w, http.StatusBadRequest, "invalid_pagination", "cursor is invalid", nil)
			return
		}
	}
	payments, err := s.store.ListPayments(r.Context(), status, after, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	next := ""
	if len(payments) > limit {
		next = encodeCursor(strconv.FormatInt(payments[limit-1].ID, 10))
	}
	writeJSON(w, http.StatusOK, map[string]any{"payments": s.paymentJSON(payments[:min(len(payments), limit)]), "next_cursor": next})
}

func (s *Server) attributePayment(w http.ResponseWriter, r *http.Request) {
	paymentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || paymentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_payment", "payment id is invalid", nil)
		return
	}
	var request struct {
		OrderID string `json:"order_id"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil || request.OrderID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "order_id is required", nil)
		return
	}
	if err := s.store.AttributePayment(r.Context(), paymentID, request.OrderID, time.Now()); err != nil {
		s.writeOrderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attributed": true})
}

func (s *Server) paymentJSON(payments []store.Payment) []map[string]any {
	response := make([]map[string]any, 0, len(payments))
	for _, payment := range payments {
		decimals, ok := s.assetDecimals(payment.Asset)
		amount := payment.AmountRaw
		if ok {
			if formatted, err := store.FormatUnits(payment.AmountRaw, decimals); err == nil {
				amount = formatted
			}
		}
		response = append(response, map[string]any{
			"id": payment.ID, "txid": payment.TxID, "log_index": payment.LogIndex,
			"direction": payment.Direction, "block_height": payment.BlockHeight,
			"block_timestamp": payment.BlockTimestamp, "from_address": payment.FromAddress,
			"to_address": payment.ToAddress, "order_id": payment.OrderID, "asset": payment.Asset,
			"amount": amount, "status": payment.Status, "detected_at": payment.DetectedAt,
			"confirmed_at": payment.ConfirmedAt,
		})
	}
	return response
}
