// Package api exposes the authenticated HTTP boundary for payd (W-009).
package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1" // RFC 6238 uses HMAC-SHA-1 by default.
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	"golang.org/x/crypto/argon2"

	"payd/internal/config"
	"payd/internal/price"
	"payd/internal/store"
	walletpool "payd/internal/wallet"
)

var (
	ErrInvalidTOTP = errors.New("invalid TOTP")
	ErrTOTPReplay  = errors.New("TOTP was already used")
)

type Server struct {
	store      *store.Store
	pool       *walletpool.Pool
	logger     *slog.Logger
	handler    http.Handler
	keys       []apiKey
	totpSecret []byte

	mu         sync.RWMutex
	assets     map[string]config.Asset
	price      config.Price
	resources  config.Resources
	withdrawal config.Withdrawal
	cooldown   time.Duration

	rateMu sync.Mutex
	rates  map[string]rateWindow
}

type apiKey struct {
	name    string
	scopes  map[string]bool
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
	hash    []byte
}

type rateWindow struct {
	started time.Time
	count   int
}

type contextKey uint8

const stateContextKey contextKey = 1

type requestState struct {
	keyName string
	scopes  map[string]bool
}

func New(database *store.Store, pool *walletpool.Pool, cfg config.Config, logger *slog.Logger) (*Server, error) {
	if database == nil || pool == nil {
		return nil, errors.New("API requires store and wallet pool")
	}
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{store: database, pool: pool, logger: logger, rates: make(map[string]rateWindow)}
	server.UpdateConfig(cfg)
	if len(cfg.Auth.APIKeys) == 0 {
		return nil, errors.New("auth.api_keys must not be empty")
	}
	for _, configured := range cfg.Auth.APIKeys {
		key, err := parseAPIKey(configured)
		if err != nil {
			return nil, fmt.Errorf("auth key %q: %w", configured.Name, err)
		}
		server.keys = append(server.keys, key)
	}
	if cfg.Auth.TOTPSecret != "" {
		secret := strings.ToUpper(strings.ReplaceAll(cfg.Auth.TOTPSecret, " ", ""))
		decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(secret, "="))
		if err != nil || len(decoded) == 0 {
			return nil, errors.New("auth.totp_secret is not valid base32")
		}
		server.totpSecret = decoded
	}

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/orders", server.requireScope("orders:write", server.createOrder))
	mux.Handle("GET /api/v1/orders", server.requireScope("orders:read", server.listOrders))
	mux.Handle("GET /api/v1/orders/funded-terminal", server.requireScope("orders:read", server.listFundedTerminal))
	mux.Handle("GET /api/v1/orders/{id}", server.requireScope("orders:read", server.getOrder))
	mux.Handle("POST /api/v1/orders/{id}/cancel", server.requireScope("orders:write", server.cancelOrder))
	mux.Handle("POST /api/v1/orders/{id}/resolve", server.requireScope("orders:write", server.resolveOrder))
	mux.Handle("GET /api/v1/payments/unattributed", server.requireScope("orders:read", server.listUnattributed))
	mux.Handle("GET /api/v1/payments/orphaned", server.requireScope("orders:read", server.listOrphaned))
	mux.Handle("POST /api/v1/payments/{id}/attribute", server.requireScope("orders:write", server.attributePayment))
	mux.Handle("GET /api/v1/wallets/needs-resources", server.requireScope("wallets:read", server.walletsNeedingResources))
	mux.Handle("POST /api/v1/wallets/{address}/clear-drift", server.requireScope("wallets:write", server.clearBalanceDrift))
	mux.Handle("POST /api/v1/withdrawals", server.requireScope("withdrawals:write", server.createWithdrawal))
	mux.Handle("GET /api/v1/withdrawals", server.requireScope("withdrawals:read", server.listWithdrawals))
	mux.Handle("GET /api/v1/withdrawals/limits", server.requireScope("withdrawals:read", server.withdrawalLimits))
	mux.Handle("GET /api/v1/withdrawals/{id}", server.requireScope("withdrawals:read", server.getWithdrawal))
	server.handler = server.logRequests(server.normalizeErrors(server.authenticate(server.rateLimit(mux))))
	return server, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) UpdateConfig(cfg config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assets == nil {
		s.assets = make(map[string]config.Asset)
	}
	clear(s.assets)
	for _, asset := range cfg.Assets {
		s.assets[asset.Symbol] = asset
	}
	s.price, s.resources, s.withdrawal, s.cooldown = cfg.Price, cfg.Resources, cfg.Withdrawal, cfg.Wallet.Cooldown
}

func (s *Server) createWithdrawal(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FromAddress string `json:"from_address"`
		ToAddress   string `json:"to_address"`
		Asset       string `json:"asset"`
		Amount      string `json:"amount"`
		TOTP        string `json:"totp"`
	}
	if err := decodeJSON(w, r, &request, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is invalid", nil)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key is required", nil)
		return
	}
	existing, exists, err := s.store.WithdrawalByIdempotency(r.Context(), key)
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	decimals, configured := s.assetDecimals(request.Asset)
	raw, parseErr := store.ParseUnits(request.Amount, decimals)
	if exists { // WDR-001a: this branch completes before TOTP is inspected or consumed.
		if !configured || parseErr != nil || existing.FromAddress != request.FromAddress || existing.ToAddress != request.ToAddress ||
			existing.Asset != request.Asset || existing.AmountRaw != raw.String() {
			writeError(w, http.StatusConflict, "idempotency_key_reuse", "Idempotency-Key belongs to a different withdrawal request", nil)
			return
		}
		writeJSON(w, http.StatusOK, s.withdrawalJSON(existing))
		return
	}
	s.mu.RLock()
	cfg, priceCfg := s.withdrawal, s.price
	s.mu.RUnlock()
	if !cfg.Enabled {
		writeError(w, http.StatusServiceUnavailable, "withdrawals_disabled", "withdrawals are disabled", nil)
		return
	}
	if !configured {
		writeError(w, http.StatusBadRequest, "invalid_asset", "asset is not configured", nil)
		return
	}
	if !hdwallet.IsValidAddress(hdwallet.TRX, request.ToAddress) || raw == nil || parseErr != nil || raw.Sign() <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_withdrawal", "destination and positive amount are required", nil)
		return
	}
	if cfg.RequireTOTP {
		if err := s.ValidateTOTP(r.Context(), request.TOTP, time.Now()); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_totp", "TOTP is invalid or already used", nil)
			return
		}
	}
	quote, err := price.Current(r.Context(), s.store, priceCfg, request.Asset, time.Now())
	if err != nil {
		if errors.Is(err, price.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "price_unavailable", "asset price is unavailable or stale", nil)
			return
		}
		s.writeWithdrawalError(w, err)
		return
	}
	usd, ok := exactUSD(raw, decimals, quote.USD)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "stored price is invalid", nil)
		return
	}
	state := requestStateFrom(r.Context())
	created, fresh, err := s.store.CreateWithdrawal(r.Context(), store.CreateWithdrawal{IdempotencyKey: key,
		FromAddress: request.FromAddress, ToAddress: request.ToAddress, Asset: request.Asset, AmountRaw: raw.String(),
		AmountUSD: usd, DailyLimitUSD: cfg.DailyLimitUSD, RequestedBy: state.keyName, IP: r.RemoteAddr, Now: time.Now()})
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	status := http.StatusOK
	if fresh {
		status = http.StatusCreated
	}
	writeJSON(w, status, s.withdrawalJSON(created))
}

func (s *Server) getWithdrawal(w http.ResponseWriter, r *http.Request) {
	withdrawal, err := s.store.Withdrawal(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.withdrawalJSON(withdrawal))
}

func (s *Server) listWithdrawals(w http.ResponseWriter, r *http.Request) {
	limit, _, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	withdrawals, err := s.store.ListWithdrawals(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(withdrawals))
	for _, withdrawal := range withdrawals {
		items = append(items, s.withdrawalJSON(withdrawal))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) withdrawalLimits(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	limit := s.withdrawal.DailyLimitUSD
	s.mu.RUnlock()
	used, err := s.store.WithdrawalUSDUsed(r.Context(), time.Now())
	if err != nil {
		s.writeWithdrawalError(w, err)
		return
	}
	remaining, _ := new(big.Rat).SetString(limit)
	remaining.Sub(remaining, used)
	if remaining.Sign() < 0 {
		remaining.SetInt64(0)
	}
	writeJSON(w, http.StatusOK, map[string]any{"daily_limit_usd": limit, "used_usd": decimalRat(used), "remaining_usd": decimalRat(remaining)})
}

func (s *Server) withdrawalJSON(withdrawal store.Withdrawal) map[string]any {
	decimals, _ := s.assetDecimals(withdrawal.Asset)
	amount, _ := store.FormatUnits(withdrawal.AmountRaw, decimals)
	return map[string]any{"id": withdrawal.ID, "from_address": withdrawal.FromAddress, "to_address": withdrawal.ToAddress,
		"asset": withdrawal.Asset, "amount": amount, "amount_raw": withdrawal.AmountRaw, "amount_usd": withdrawal.AmountUSD,
		"status": withdrawal.Status, "txid": withdrawal.TxID, "failure_reason": withdrawal.FailureReason,
		"resolved_by": withdrawal.ResolvedBy, "broadcast_response": withdrawal.BroadcastResponse,
		"fee_raw": withdrawal.FeeRaw, "energy_used": withdrawal.EnergyUsed, "last_lookup_error": withdrawal.LastLookupError,
		"created_at": withdrawal.CreatedAt, "broadcast_at": withdrawal.BroadcastAt, "confirmed_at": withdrawal.ConfirmedAt}
}

func (s *Server) writeWithdrawalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrWithdrawalNotFound):
		writeError(w, http.StatusNotFound, "not_found", "withdrawal was not found", nil)
	case errors.Is(err, store.ErrIdempotencyReuse):
		writeError(w, http.StatusConflict, "idempotency_key_reuse", err.Error(), nil)
	case errors.Is(err, store.ErrBalanceDrift):
		writeError(w, http.StatusConflict, "balance_drift", err.Error(), nil)
	case errors.Is(err, store.ErrDailyLimit):
		writeError(w, http.StatusConflict, "daily_limit_exceeded", err.Error(), nil)
	case errors.Is(err, store.ErrInsufficientFunds):
		writeError(w, http.StatusConflict, "insufficient_confirmed_balance", err.Error(), nil)
	case errors.Is(err, store.ErrSourceUnavailable), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusBadRequest, "invalid_source", "source address is unavailable", nil)
	default:
		s.logger.Error("withdrawal API failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}

func exactUSD(raw *big.Int, decimals int, quote string) (string, bool) {
	price, ok := new(big.Rat).SetString(quote)
	if !ok || price.Sign() <= 0 {
		return "", false
	}
	value := new(big.Rat).Mul(new(big.Rat).SetInt(raw), price)
	value.Quo(value, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
	return decimalRat(value), true
}

func decimalRat(value *big.Rat) string {
	for digits := 0; digits <= 64; digits++ {
		text := value.FloatString(digits)
		if parsed, ok := new(big.Rat).SetString(text); ok && parsed.Cmp(value) == 0 {
			if strings.Contains(text, ".") {
				text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
			}
			return text
		}
	}
	return value.FloatString(64)
}

// ValidateTOTP verifies the RFC 6238 +/-1-step window then atomically consumes the matching code (API-022).
func (s *Server) ValidateTOTP(ctx context.Context, code string, now time.Time) error {
	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) >= 0 || len(s.totpSecret) == 0 {
		return ErrInvalidTOTP
	}
	current := now.UTC().Unix() / 30
	for step := current - 1; step <= current+1; step++ {
		if subtle.ConstantTimeCompare([]byte(code), []byte(totpCode(s.totpSecret, step))) != 1 {
			continue
		}
		used, err := s.store.UseTOTP(ctx, code, step, now)
		if err != nil {
			return err
		}
		if !used {
			return ErrTOTPReplay
		}
		return nil
	}
	return ErrInvalidTOTP
}

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
	order, err := s.store.Order(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeOrderError(w, err)
		return
	}
	paymentRows, err := s.store.OrderPayments(r.Context(), order.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	response, err := s.orderJSON(order)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	response["payments"] = s.paymentJSON(paymentRows)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error(), nil)
		return
	}
	filter := store.OrderFilter{Status: r.URL.Query().Get("status"), Asset: r.URL.Query().Get("asset"), After: cursor, Limit: limit + 1}
	if value := r.URL.Query().Get("created_from"); value != "" {
		stamp, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid_filter", "created_from must be a Unix timestamp", nil)
			return
		}
		filter.CreatedFrom = &stamp
	}
	if value := r.URL.Query().Get("created_to"); value != "" {
		stamp, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid_filter", "created_to must be a Unix timestamp", nil)
			return
		}
		filter.CreatedTo = &stamp
	}
	if filter.CreatedFrom != nil && filter.CreatedTo != nil && *filter.CreatedFrom > *filter.CreatedTo {
		writeError(w, http.StatusBadRequest, "invalid_filter", "created_from must not exceed created_to", nil)
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

func (s *Server) listUnattributed(w http.ResponseWriter, r *http.Request) {
	s.listPayments(w, r, "unattributed")
}

func (s *Server) listOrphaned(w http.ResponseWriter, r *http.Request) {
	s.listPayments(w, r, "orphaned")
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

func (s *Server) walletsNeedingResources(w http.ResponseWriter, r *http.Request) {
	addresses, err := s.store.WalletAddresses(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	items := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		item, err := s.walletJSON(r.Context(), address)
		if err != nil {
			s.logger.Error("format wallet resources", "address", address.Address, "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": items, "total": len(items)})
}

func (s *Server) clearBalanceDrift(w http.ResponseWriter, r *http.Request) {
	state := requestStateFrom(r.Context())
	if err := s.store.ClearBalanceDrift(r.Context(), r.PathValue("address"), state.keyName, r.RemoteAddr, time.Now()); err != nil {
		if errors.Is(err, store.ErrAddressNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "wallet address was not found", nil)
		} else {
			s.logger.Error("clear balance drift", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drift_detected": false})
}

func (s *Server) walletJSON(ctx context.Context, address store.WalletAddress) (map[string]any, error) {
	s.mu.RLock()
	assets := make(map[string]config.Asset, len(s.assets))
	for symbol, asset := range s.assets {
		assets[symbol] = asset
	}
	priceConfig, resources := s.price, s.resources
	s.mu.RUnlock()
	energyAvailable := max(address.EnergyLimit-address.EnergyUsed, 0)
	bandwidthAvailable := max(address.BandwidthLimit-address.BandwidthUsed, 0)
	energySufficient := energyAvailable >= resources.MinEnergy
	bandwidthSufficient := bandwidthAvailable >= resources.MinBandwidth
	balances := make([]map[string]any, 0, len(address.Balances))
	trxRaw := new(big.Int)
	drift := false
	for _, balance := range address.Balances {
		asset, ok := assets[balance.Asset]
		if !ok {
			return nil, fmt.Errorf("unknown wallet asset %s", balance.Asset)
		}
		confirmed, err := store.FormatUnits(balance.ConfirmedRaw, asset.Decimals)
		if err != nil {
			return nil, err
		}
		pending, err := store.FormatUnits(balance.PendingRaw, asset.Decimals)
		if err != nil {
			return nil, err
		}
		item := map[string]any{"asset": balance.Asset, "confirmed": confirmed, "pending": pending, "drift_detected": balance.Drift}
		if quote, err := price.Current(ctx, s.store, priceConfig, balance.Asset, time.Now()); err == nil {
			if usd, ok := amountUSD(balance.ConfirmedRaw, asset.Decimals, quote.USD); ok {
				item["usd"] = usd
			}
		}
		balances = append(balances, item)
		if balance.Asset == "TRX" {
			trxRaw.SetString(balance.ConfirmedRaw, 10)
		}
		drift = drift || balance.Drift
	}
	params, paramsErr := s.store.LoadChainParameters(ctx)
	if paramsErr != nil && !errors.Is(paramsErr, sql.ErrNoRows) {
		return nil, paramsErr
	}
	bandwidthBurnSufficient := false
	if paramsErr == nil {
		cost := new(big.Int).Mul(big.NewInt(resources.MinBandwidth), big.NewInt(params.TransactionFee))
		bandwidthBurnSufficient = trxRaw.Cmp(cost) >= 0
	}
	trxForBandwidth, err := store.FormatUnits(trxRaw.String(), 6)
	if err != nil {
		return nil, err
	}
	canWithdraw := make(map[string]bool, len(assets))
	for symbol, asset := range assets {
		canWithdraw[symbol] = !drift && (bandwidthSufficient || bandwidthBurnSufficient) && (asset.Kind == "native" || energySufficient)
	}
	blocked := make([]string, 0, 2)
	if !bandwidthSufficient && !bandwidthBurnSufficient {
		blocked = append(blocked, "bandwidth")
	}
	if !energySufficient {
		blocked = append(blocked, "energy")
	}
	item := map[string]any{
		"address": address.Address, "hd_index": address.HDIndex, "balances": balances,
		"energy":                 map[string]any{"available": energyAvailable, "limit": address.EnergyLimit, "required": resources.MinEnergy, "sufficient": energySufficient},
		"bandwidth":              map[string]any{"available": bandwidthAvailable, "limit": address.BandwidthLimit, "required": resources.MinBandwidth, "sufficient": bandwidthSufficient},
		"trx_for_bandwidth_burn": trxForBandwidth, "can_withdraw": canWithdraw,
		"blocked_by": blocked, "drift_detected": drift,
	}
	if address.ResourcesCheckedAt != nil {
		item["checked_at"] = *address.ResourcesCheckedAt
	}
	if paramsErr == nil {
		burnSun := new(big.Int).Mul(big.NewInt(resources.MinEnergy), big.NewInt(params.EnergyFee))
		burn, err := store.FormatUnits(burnSun.String(), 6)
		if err != nil {
			return nil, err
		}
		item["estimated_burn_trx"], item["energy_fee_sun"] = burn, params.EnergyFee
	}
	return item, nil
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

func (s *Server) assetDecimals(symbol string) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	asset, ok := s.assets[symbol]
	return asset.Decimals, ok
}

// ValidateWithdrawalSource maps BAL-002 into the HTTP conflict used by P11's handler.
func (s *Server) ValidateWithdrawalSource(ctx context.Context, address, asset string) (store.Balance, int, string, error) {
	balance, err := s.store.BalanceForWithdrawal(ctx, address, asset)
	if errors.Is(err, store.ErrBalanceDrift) {
		return store.Balance{}, http.StatusConflict, "balance_drift", err
	}
	return balance, 0, "", err
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
	if !raw.IsInt() {
		return nil, price.Quote{}, errors.New("USD amount does not resolve to a whole base unit")
	}
	return new(big.Int).Set(raw.Num()), quote, nil
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
	case errors.Is(err, walletpool.ErrInvalidOrder), errors.Is(err, walletpool.ErrUnknownConsumer), errors.Is(err, store.ErrInvalidResolution):
		writeError(w, http.StatusBadRequest, "invalid_order", err.Error(), nil)
	case errors.Is(err, store.ErrOrderRequiresForce):
		writeError(w, http.StatusConflict, "order_funded", "funded order cancellation requires force", nil)
	case errors.Is(err, store.ErrOrderTerminal):
		writeError(w, http.StatusConflict, "order_terminal", "order is already terminal", nil)
	case errors.Is(err, store.ErrOrderNotFound), errors.Is(err, store.ErrPaymentNotFound):
		writeError(w, http.StatusNotFound, "not_found", "order or payment was not found", nil)
	default:
		s.logger.Error("API request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}

func (s *Server) requireScope(scope string, handler http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requestStateFrom(r.Context()).scopes[scope] {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication failed", nil) // API-021
			return
		}
		handler(w, r)
	})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		supplied := r.Header.Get("X-API-Key")
		state := requestStateFrom(r.Context())
		for _, key := range s.keys {
			candidate := argon2.IDKey([]byte(supplied), key.salt, key.time, key.memory, key.threads, uint32(len(key.hash)))
			if subtle.ConstantTimeCompare(candidate, key.hash) == 1 {
				state.keyName, state.scopes = key.name, key.scopes
				next.ServeHTTP(w, r)
				return
			}
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication failed", nil) // API-020/021
	})
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := requestStateFrom(r.Context())
		limit, bucket := 100, "default"
		if strings.HasPrefix(r.URL.Path, "/api/v1/withdrawals") {
			limit, bucket = 10, "withdrawals"
		}
		now := time.Now()
		key := state.keyName + "\x00" + bucket
		s.rateMu.Lock()
		window := s.rates[key]
		if window.started.IsZero() || now.Sub(window.started) >= time.Minute {
			window = rateWindow{started: now}
		}
		window.count++
		s.rates[key] = window
		s.rateMu.Unlock()
		if window.count > limit {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		state := &requestState{}
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r.WithContext(context.WithValue(r.Context(), stateContextKey, state)))
		s.logger.Info("API request", "method", r.Method, "path", r.URL.Path, "key", state.keyName,
			"status", wrapped.status, "duration", time.Since(started)) // API-026: bodies/TOTP are never logged.
	})
}

func (s *Server) normalizeErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := &bufferedResponse{header: make(http.Header), status: http.StatusOK}
		next.ServeHTTP(buffer, r)
		if buffer.status >= 400 && !strings.HasPrefix(buffer.header.Get("Content-Type"), "application/json") {
			code, message := "http_error", http.StatusText(buffer.status)
			switch buffer.status {
			case http.StatusNotFound:
				code, message = "not_found", "route not found"
			case http.StatusMethodNotAllowed:
				code, message = "method_not_allowed", "method not allowed"
			}
			writeError(w, buffer.status, code, message, nil)
			return
		}
		copyHeader(w.Header(), buffer.header)
		w.WriteHeader(buffer.status)
		_, _ = w.Write(buffer.body.Bytes())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *bufferedResponse) Header() http.Header            { return w.header }
func (w *bufferedResponse) Write(body []byte) (int, error) { return w.body.Write(body) }
func (w *bufferedResponse) WriteHeader(status int)         { w.status = status }

func requestStateFrom(ctx context.Context) *requestState {
	state, _ := ctx.Value(stateContextKey).(*requestState)
	if state == nil {
		return &requestState{}
	}
	return state
}

func parseAPIKey(configured config.APIKey) (apiKey, error) {
	parts := strings.Split(strings.TrimPrefix(configured.KeyHash, "$"), "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return apiKey{}, errors.New("invalid Argon2id PHC string")
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil ||
		memory == 0 || iterations == 0 || threads == 0 || parts[2] != fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, threads) {
		return apiKey{}, errors.New("invalid Argon2id parameters")
	}
	salt, err := decodeBase64(parts[3])
	if err != nil || len(salt) == 0 {
		return apiKey{}, errors.New("invalid Argon2id salt")
	}
	hash, err := decodeBase64(parts[4])
	if err != nil || len(hash) == 0 {
		return apiKey{}, errors.New("invalid Argon2id hash")
	}
	scopes := make(map[string]bool, len(configured.Scopes))
	for _, scope := range configured.Scopes {
		scopes[scope] = true
	}
	return apiKey{name: configured.Name, scopes: scopes, memory: memory, time: iterations, threads: threads, salt: salt, hash: hash}, nil
}

func decodeBase64(value string) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(value)
	}
	return decoded, err
}

func totpCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

func amountUSD(raw string, decimals int, price string) (string, bool) {
	amount, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return "", false
	}
	quote, ok := new(big.Rat).SetString(price)
	if !ok {
		return "", false
	}
	value := new(big.Rat).Mul(new(big.Rat).SetInt(amount), quote)
	value.Quo(value, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
	return value.FloatString(2), true
}

func pagination(r *http.Request, numeric bool) (int, string, error) {
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 200 {
			return 0, "", errors.New("limit must be between 1 and 200")
		}
		limit = parsed
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor == "" {
		return limit, "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(decoded) == 0 {
		return 0, "", errors.New("cursor is invalid")
	}
	if numeric {
		if _, err := strconv.ParseInt(string(decoded), 10, 64); err != nil {
			return 0, "", errors.New("cursor is invalid")
		}
	}
	return limit, string(decoded), nil
}

func encodeCursor(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, allowEmpty bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "details": details}}) // API-024
}

func copyHeader(destination, source http.Header) {
	for key, values := range source {
		destination[key] = append([]string(nil), values...)
	}
}
