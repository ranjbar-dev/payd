package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"payd/internal/price"
	"payd/internal/store"
	walletpool "payd/internal/wallet"
)

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Asset      string          `json:"asset"`
		Amount     string          `json:"amount"`
		AmountUSD  string          `json:"amount_usd"`
		External   *string         `json:"external_ref"`
		Consumer   string          `json:"consumer"`
		TTLSeconds int64           `json:"ttl_seconds"`
		Metadata   json.RawMessage `json:"metadata"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is invalid", nil)
		return
	}
	decimals, ok := s.assetDecimals(request.Asset)
	if !ok || (request.Amount == "") == (request.AmountUSD == "") || request.TTLSeconds < 0 ||
		request.TTLSeconds > int64((time.Duration(1<<63-1))/time.Second) {
		writeError(w, http.StatusBadRequest, "invalid_order", "asset and exactly one positive amount are required", nil)
		return
	}
	expectedRaw, err := store.ParseUnits(request.Amount, decimals)
	var priceUSD *string
	var priceAt *int64
	if request.AmountUSD != "" {
		var quote price.Quote
		expectedRaw, quote, err = s.rawFromUSD(r.Context(), request.Asset, request.AmountUSD, decimals, time.Now())
		priceUSD, priceAt = &quote.USD, &quote.FetchedAt
	}
	if errors.Is(err, price.ErrUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "price_unavailable", "asset price is unavailable or stale", nil)
		return
	}
	if err != nil || expectedRaw.Sign() <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_amount", "amount is not a positive asset value", nil)
		return
	}
	metadata := request.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}
	order, created, err := s.pool.CreateOrder(r.Context(), walletpool.CreateOrderRequest{
		ExternalRef: request.External, Asset: request.Asset, ExpectedRaw: expectedRaw.String(),
		TTL: time.Duration(request.TTLSeconds) * time.Second, Metadata: string(metadata), Consumer: request.Consumer,
		PriceUSD: priceUSD, PriceAt: priceAt,
	}, time.Now())
	if err != nil {
		s.writeOrderError(w, err)
		return
	}
	response, err := s.orderJSON(order)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, response)
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	after := int64(0)
	if cursor != "" {
		after, _ = strconv.ParseInt(cursor, 10, 64)
		if after < 0 {
			writeError(w, http.StatusBadRequest, "invalid_pagination", "cursor is invalid", nil)
			return
		}
	}
	order, err := s.store.Order(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeOrderError(w, err)
		return
	}
	paymentRows, err := s.store.OrderPayments(r.Context(), order.ID, after, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	response, err := s.orderJSON(order)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	next := ""
	if len(paymentRows) > limit {
		next = encodeCursor(strconv.FormatInt(paymentRows[limit-1].ID, 10))
	}
	response["payments"] = s.paymentJSON(paymentRows[:min(len(paymentRows), limit)])
	response["next_cursor"] = next
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	filter, err := orderFilter(r, cursor, limit+1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error(), nil)
		return
	}
	orders, err := s.store.ListOrders(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	response := make([]map[string]any, 0, min(len(orders), limit))
	for _, order := range orders[:min(len(orders), limit)] {
		item, err := s.orderJSON(order)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		response = append(response, item)
	}
	next := ""
	if len(orders) > limit {
		next = encodeCursor(orders[limit-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": response, "next_cursor": next})
}

func orderFilter(r *http.Request, after string, limit int) (store.OrderFilter, error) {
	filter := store.OrderFilter{Status: r.URL.Query().Get("status"), Asset: r.URL.Query().Get("asset"),
		ExternalRef: r.URL.Query().Get("external_ref"), Consumer: r.URL.Query().Get("consumer"),
		Address: r.URL.Query().Get("address"), After: after, Limit: limit}
	if value := r.URL.Query().Get("created_from"); value != "" {
		stamp, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil {
			return store.OrderFilter{}, errors.New("created_from must be a Unix timestamp")
		}
		filter.CreatedFrom = &stamp
	}
	if value := r.URL.Query().Get("created_to"); value != "" {
		stamp, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil {
			return store.OrderFilter{}, errors.New("created_to must be a Unix timestamp")
		}
		filter.CreatedTo = &stamp
	}
	if filter.CreatedFrom != nil && filter.CreatedTo != nil && *filter.CreatedFrom > *filter.CreatedTo {
		return store.OrderFilter{}, errors.New("created_from must not exceed created_to")
	}
	return filter, nil
}

func (s *Server) extendOrder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TTLSeconds int64 `json:"ttl_seconds"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil || request.TTLSeconds <= 0 ||
		request.TTLSeconds > int64((time.Duration(1<<63-1))/time.Second) {
		writeError(w, http.StatusBadRequest, "invalid_request", "ttl_seconds must be positive", nil)
		return
	}
	order, err := s.store.ExtendOrder(r.Context(), r.PathValue("id"), time.Duration(request.TTLSeconds)*time.Second, 24*time.Hour, time.Now())
	if err != nil {
		s.writeOrderError(w, err)
		return
	}
	response, err := s.orderJSON(order)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) orderEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Order(r.Context(), r.PathValue("id")); err != nil {
		s.writeOrderError(w, err)
		return
	}
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	events, err := s.store.ListOrderEvents(r.Context(), r.PathValue("id"), cursor, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	items := make([]map[string]any, 0, min(len(events), limit))
	for _, event := range events[:min(len(events), limit)] {
		items = append(items, map[string]any{"id": event.ID, "consumer": event.Consumer, "event_type": event.EventType,
			"status": event.Status, "attempts": event.Attempts, "last_status_code": event.LastStatusCode,
			"last_error": event.LastError, "created_at": event.CreatedAt, "delivered_at": event.DeliveredAt})
	}
	next := ""
	if len(events) > limit {
		next = encodeCursor(events[limit-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": items, "next_cursor": next})
}

func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Force bool `json:"force"`
	}
	if err := decodeJSON(w, r, &request, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is invalid", nil)
		return
	}
	s.mu.RLock()
	cooldown := s.cooldown
	s.mu.RUnlock()
	order, err := s.store.CancelOrder(r.Context(), r.PathValue("id"), request.Force, cooldown, time.Now())
	if err != nil {
		s.writeOrderError(w, err)
		return
	}
	response, err := s.orderJSON(order)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) listFundedTerminal(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	orders, err := s.store.ListFundedTerminalOrders(r.Context(), cursor, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	items := make([]map[string]any, 0, min(len(orders), limit))
	for _, funded := range orders[:min(len(orders), limit)] {
		item, err := s.orderJSON(funded.Order)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		item["payers"] = funded.Payers
		items = append(items, item)
	}
	next := ""
	if len(orders) > limit {
		next = encodeCursor(orders[limit-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": items, "next_cursor": next})
}

func (s *Server) resolveOrder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Resolution string `json:"resolution"`
		Note       string `json:"resolution_note"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is invalid", nil)
		return
	}
	state := requestStateFrom(r.Context())
	if err := s.store.ResolveFundedOrder(r.Context(), r.PathValue("id"), request.Resolution, request.Note, state.keyName, time.Now()); err != nil {
		s.writeOrderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": true})
}

func (s *Server) orderJSON(order store.Order) (map[string]any, error) {
	decimals, ok := s.assetDecimals(order.Asset)
	if !ok {
		return nil, fmt.Errorf("unknown asset %s", order.Asset)
	}
	expected, err := store.FormatUnits(order.ExpectedRaw, decimals)
	if err != nil {
		return nil, err
	}
	received, err := store.FormatUnits(order.ReceivedRaw, decimals)
	if err != nil {
		return nil, err
	}
	overpaid, err := store.FormatUnits(order.OverpaidRaw, decimals)
	if err != nil {
		return nil, err
	}
	response := map[string]any{
		"id": order.ID, "address": order.Address, "asset": order.Asset, "amount": expected,
		"received": received, "overpaid": overpaid, "status": order.Status, "consumer": order.Consumer,
		"metadata": json.RawMessage(order.Metadata), "expires_at": order.ExpiresAt,
		"created_at": order.CreatedAt, "updated_at": order.UpdatedAt,
		// Closes the ORD-002b assignment window, whose bounds are otherwise
		// invisible to a client trying to explain why a payment was attributed.
		"address_released_at": order.AddressReleasedAt,
	}
	if order.ExternalRef != nil {
		response["external_ref"] = *order.ExternalRef
	}
	if order.PriceUSD != nil {
		response["price_usd"] = *order.PriceUSD
		if usd, ok := amountUSD(order.ExpectedRaw, decimals, *order.PriceUSD); ok {
			response["amount_usd"] = usd
		}
	}
	if order.Resolution != nil {
		response["resolution"] = *order.Resolution
		response["resolution_note"] = order.ResolutionNote
		response["resolved_at"] = order.ResolvedAt
	}
	return response, nil
}

func (s *Server) assetDecimals(symbol string) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	asset, ok := s.assets[symbol]
	return asset.Decimals, ok
}

func (s *Server) rawFromUSD(ctx context.Context, asset, value string, decimals int, now time.Time) (*big.Int, price.Quote, error) {
	usd, ok := new(big.Rat).SetString(value)
	if !ok || usd.Sign() <= 0 {
		return nil, price.Quote{}, errors.New("invalid USD amount")
	}
	s.mu.RLock()
	priceConfig := s.price
	s.mu.RUnlock()
	quote, err := price.Current(ctx, s.store, priceConfig, asset, now)
	if err != nil {
		return nil, price.Quote{}, err
	}
	assetPrice, ok := new(big.Rat).SetString(quote.USD)
	if !ok || assetPrice.Sign() <= 0 {
		return nil, price.Quote{}, errors.New("invalid stored price")
	}
	raw := new(big.Rat).Mul(usd, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
	raw.Quo(raw, assetPrice)
	// API-001: a USD amount is converted at the current price. The result rarely
	// lands on a whole base unit for a non-stablecoin asset, so round up to the
	// next base unit — the payer is never asked for less than the USD figure,
	// and the overpay is at most one base unit (e.g. 0.000001 TRX). The price is
	// snapshotted (PRC-006) so the valuation stays reproducible.
	num, den := raw.Num(), raw.Denom()
	rawUnits := new(big.Int).Quo(num, den)
	if new(big.Int).Mul(rawUnits, den).Cmp(num) < 0 {
		rawUnits.Add(rawUnits, big.NewInt(1))
	}
	return rawUnits, quote, nil
}

func (s *Server) writeOrderError(w http.ResponseWriter, err error) {
	var conflict *store.ExternalRefConflictError
	switch {
	case errors.As(err, &conflict):
		writeError(w, http.StatusConflict, "external_ref_conflict", "external_ref belongs to a different order request", map[string]any{"fields": conflict.Fields})
	case errors.Is(err, store.ErrPoolExhausted):
		writeError(w, http.StatusServiceUnavailable, "address_pool_exhausted", "no deposit address is available", nil)
	case errors.Is(err, price.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "price_unavailable", "asset price is unavailable or stale", nil)
	// IPN-015: a consumer that is unknown or disabled has its own code. Sharing
	// `invalid_order` with amount and asset validation meant a client could only
	// guess which of the two it hit, and would name the wrong cause to the
	// operator (WORD-041). `unknown_consumer` is the same code /ipn/test already
	// uses for the same condition.
	case errors.Is(err, walletpool.ErrUnknownConsumer):
		writeError(w, http.StatusBadRequest, "unknown_consumer", err.Error(), nil)
	case errors.Is(err, walletpool.ErrInvalidOrder), errors.Is(err, store.ErrInvalidResolution):
		writeError(w, http.StatusBadRequest, "invalid_order", err.Error(), nil)
	case errors.Is(err, store.ErrOrderRequiresForce):
		writeError(w, http.StatusConflict, "order_funded", "funded order cancellation requires force", nil)
	case errors.Is(err, store.ErrOrderTerminal):
		writeError(w, http.StatusConflict, "order_terminal", "order is already terminal", nil)
	case errors.Is(err, store.ErrOrderLifetime):
		writeError(w, http.StatusConflict, "order_lifetime_exceeded", "order cannot exceed 24 hours from creation", nil)
	case errors.Is(err, store.ErrOrderNotFound), errors.Is(err, store.ErrPaymentNotFound):
		writeError(w, http.StatusNotFound, "not_found", "order or payment was not found", nil)
	default:
		s.logger.Error("API request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}
