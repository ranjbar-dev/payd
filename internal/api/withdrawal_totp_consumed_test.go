package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"payd/internal/store"
)

func TestCreateWithdrawalConflictReportsConsumedTOTP(t *testing.T) {
	server, database, cleanup := testServer(t, 2)
	defer cleanup()
	addresses := fundTestWallets(t, database, 1)
	cfg := testConfig(2)
	cfg.Withdrawal.DailyLimitUSD = "0.5"
	server.UpdateConfig(cfg)

	code := totpCode(server.totpSecret, time.Now().UTC().Unix()/30)
	response := withdrawalRequest(t, server, addresses[0].Address, addresses[1].Address, "1", "daily-limit", code)
	assertWithdrawalError(t, response, http.StatusConflict, "daily_limit_exceeded", true)
}

func TestCreateWithdrawalDoesNotClaimUnconsumedTOTP(t *testing.T) {
	t.Run("invalid TOTP", func(t *testing.T) {
		server, database, cleanup := testServer(t, 2)
		defer cleanup()
		addresses, err := database.WalletAddresses(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		response := withdrawalRequest(t, server, addresses[0].Address, addresses[1].Address, "1", "invalid-totp", "bad")
		assertWithdrawalError(t, response, http.StatusUnauthorized, "invalid_totp", false)
	})

	t.Run("TOTP disabled", func(t *testing.T) {
		server, database, cleanup := testServer(t, 2)
		defer cleanup()
		addresses := fundTestWallets(t, database, 1)
		cfg := testConfig(2)
		cfg.Withdrawal.RequireTOTP = false
		server.UpdateConfig(cfg)
		response := withdrawalRequest(t, server, addresses[0].Address, addresses[1].Address, "2", "no-totp", "")
		assertWithdrawalError(t, response, http.StatusConflict, "insufficient_confirmed_balance", false)
	})

	t.Run("pre-validation error", func(t *testing.T) {
		server, database, cleanup := testServer(t, 1)
		defer cleanup()
		addresses, err := database.WalletAddresses(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		code := totpCode(server.totpSecret, now.UTC().Unix()/30)
		response := withdrawalRequest(t, server, addresses[0].Address, "not-an-address", "1", "invalid-withdrawal", code)
		assertWithdrawalError(t, response, http.StatusBadRequest, "invalid_withdrawal", false)
		if err := server.ValidateTOTP(context.Background(), code, now); err != nil {
			t.Fatalf("pre-validation error consumed TOTP: %v", err)
		}
	})
}

func TestWithdrawalConflictDetailsCoverCreateMappings(t *testing.T) {
	server := &Server{}
	for err, code := range map[error]string{
		store.ErrIdempotencyReuse:  "idempotency_key_reuse",
		store.ErrBalanceDrift:      "balance_drift",
		store.ErrDailyLimit:        "daily_limit_exceeded",
		store.ErrInsufficientFunds: "insufficient_confirmed_balance",
	} {
		t.Run(code, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.writeWithdrawalErrorDetails(response, err, map[string]any{"totp_consumed": true})
			assertWithdrawalError(t, response, http.StatusConflict, code, true)
		})
	}
}

func withdrawalRequest(t *testing.T, server *Server, from, to, amount, key, code string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/withdrawals", strings.NewReader(fmt.Sprintf(
		`{"from_address":%q,"to_address":%q,"asset":"USDT","amount":%q}`, from, to, amount)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", testAPIKey)
	request.Header.Set("Idempotency-Key", key)
	request.Header.Set("X-TOTP", code)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func assertWithdrawalError(t *testing.T, response *httptest.ResponseRecorder, status int, code string, consumed bool) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
	got, present := envelope.Error.Details["totp_consumed"]
	if response.Code != status || envelope.Error.Code != code || present != consumed || (consumed && got != true) {
		t.Fatalf("response=%d code=%q details=%v", response.Code, envelope.Error.Code, envelope.Error.Details)
	}
}
