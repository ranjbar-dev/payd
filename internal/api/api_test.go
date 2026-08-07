package api

import (
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	"golang.org/x/crypto/argon2"

	"payd/internal/config"
	"payd/internal/store"
	walletpool "payd/internal/wallet"
)

const (
	testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	testAPIKey   = "correct horse battery staple"
	testTOTP     = "JBSWY3DPEHPK3PXP"
)

// TST-022 / API-002: a reused external_ref is idempotent only for an exact request match.
func TestExternalRefMismatchReturnsConflict(t *testing.T) {
	server, _, cleanup := testServer(t, 3)
	defer cleanup()

	first := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount":"25.00","external_ref":"invoice-2291"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first order = %d %s", first.Code, first.Body.String())
	}
	var created map[string]any
	decodeResponse(t, first, &created)

	replay := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount":"25.00","external_ref":"invoice-2291"}`)
	if replay.Code != http.StatusOK {
		t.Fatalf("exact replay = %d %s", replay.Code, replay.Body.String())
	}
	var replayed map[string]any
	decodeResponse(t, replay, &replayed)
	if replayed["id"] != created["id"] {
		t.Fatalf("replay id = %v, want %v", replayed["id"], created["id"])
	}

	conflict := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount":"26.00","external_ref":"invoice-2291"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("mismatched replay = %d %s", conflict.Code, conflict.Body.String())
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Fields []string `json:"fields"`
			} `json:"details"`
		} `json:"error"`
	}
	decodeResponse(t, conflict, &envelope)
	if envelope.Error.Code != "external_ref_conflict" || len(envelope.Error.Details.Fields) != 1 || envelope.Error.Details.Fields[0] != "expected_raw" {
		t.Fatalf("conflict envelope = %#v", envelope)
	}
	usd := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount_usd":"3.50","external_ref":"invoice-usd"}`)
	if usd.Code != http.StatusCreated || !strings.Contains(usd.Body.String(), `"amount":"3.5"`) {
		t.Fatalf("USD order = %d %s", usd.Code, usd.Body.String())
	}
}

func TestTOTPReplayPersistsAndPreviousStepIsAccepted(t *testing.T) {
	server, database, cleanup := testServer(t, 2)
	defer cleanup()
	now := time.Unix(1_750_000_000, 0)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(testTOTP)
	if err != nil {
		t.Fatal(err)
	}
	code := totpCode(secret, now.Unix()/30-1)
	if err := server.ValidateTOTP(context.Background(), code, now); err != nil {
		t.Fatalf("first TOTP validation: %v", err)
	}
	if err := server.ValidateTOTP(context.Background(), code, now); err != ErrTOTPReplay {
		t.Fatalf("same-process replay = %v", err)
	}

	restarted, err := New(database, server.pool, testConfig(2), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.ValidateTOTP(context.Background(), code, now); err != ErrTOTPReplay {
		t.Fatalf("post-restart replay = %v", err)
	}
}

func TestAuthPaginationAndPaymentRoutes(t *testing.T) {
	server, database, cleanup := testServer(t, 3)
	defer cleanup()

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil))
	if unauthorized.Code != http.StatusUnauthorized || !strings.Contains(unauthorized.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("unauthorized response = %d %s", unauthorized.Code, unauthorized.Body.String())
	}

	created := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount":"1","external_ref":"page-1"}`)
	var order map[string]any
	decodeResponse(t, created, &order)
	second := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount":"2","external_ref":"page-2"}`)
	if created.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("create paged orders = %d/%d", created.Code, second.Code)
	}
	page := request(t, server.Handler(), http.MethodGet, "/api/v1/orders?limit=1", "")
	var listed struct {
		Orders     []map[string]any `json:"orders"`
		NextCursor string           `json:"next_cursor"`
	}
	decodeResponse(t, page, &listed)
	if len(listed.Orders) != 1 || listed.NextCursor == "" {
		t.Fatalf("first page = %#v", listed)
	}
	next := request(t, server.Handler(), http.MethodGet, "/api/v1/orders?limit=1&cursor="+listed.NextCursor, "")
	decodeResponse(t, next, &listed)
	if len(listed.Orders) != 1 {
		t.Fatalf("second page = %#v", listed)
	}

	addressID := int64(1)
	payment := store.PaymentRecord{TxID: "unattributed", Direction: "in", BlockHeight: 1, BlockID: "B1",
		BlockTimestamp: time.Now().Unix(), FromAddress: "TPayer", ToAddress: order["address"].(string),
		AddressID: &addressID, Asset: "USDT", AmountRaw: "1000000", Status: "unattributed", DetectedAt: time.Now().Unix()}
	if err := database.CommitBlock(context.Background(), store.BlockRecord{Height: 1, ID: "B1", ParentID: "B0", Timestamp: time.Now().Unix()}, 64,
		func(write *store.BlockWrite) error { _, err := write.UpsertPayment(payment); return err }); err != nil {
		t.Fatal(err)
	}
	payments := request(t, server.Handler(), http.MethodGet, "/api/v1/payments/unattributed?limit=1", "")
	if payments.Code != http.StatusOK || !strings.Contains(payments.Body.String(), `"txid":"unattributed"`) {
		t.Fatalf("unattributed payments = %d %s", payments.Code, payments.Body.String())
	}
	attributed := request(t, server.Handler(), http.MethodPost, "/api/v1/payments/1/attribute",
		fmt.Sprintf(`{"order_id":%q}`, order["id"]))
	if attributed.Code != http.StatusOK {
		t.Fatalf("attribute payment = %d %s", attributed.Code, attributed.Body.String())
	}
}

// API-023 keeps ordinary and fund-moving route budgets separate per key.
func TestPerKeyRateLimits(t *testing.T) {
	server := &Server{rates: make(map[string]rateWindow)}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := server.rateLimit(next)
	call := func(path string) int {
		recorder := httptest.NewRecorder()
		state := &requestState{keyName: "test"}
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request = request.WithContext(context.WithValue(request.Context(), stateContextKey, state))
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}
	for range 100 {
		if status := call("/api/v1/orders"); status != http.StatusNoContent {
			t.Fatalf("default request rejected early with %d", status)
		}
	}
	if status := call("/api/v1/orders"); status != http.StatusTooManyRequests {
		t.Fatalf("101st default request = %d", status)
	}
	for range 10 {
		if status := call("/api/v1/withdrawals"); status != http.StatusNoContent {
			t.Fatalf("withdrawal request rejected early with %d", status)
		}
	}
	if status := call("/api/v1/withdrawals"); status != http.StatusTooManyRequests {
		t.Fatalf("11th withdrawal request = %d", status)
	}
}

func TestBalanceDriftMapsToConflictAndCanBeCleared(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	ctx := context.Background()
	addressID := int64(1)
	address := "TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH"
	if err := database.CommitBlock(ctx, store.BlockRecord{Height: 1, ID: "B1", ParentID: "B0", Timestamp: 1}, 64,
		func(write *store.BlockWrite) error {
			_, err := write.UpsertPayment(store.PaymentRecord{TxID: "drift", Direction: "in", BlockHeight: 1,
				BlockID: "B1", BlockTimestamp: 1, FromAddress: "payer", ToAddress: address,
				AddressID: &addressID, Asset: "USDT", AmountRaw: "1000000", Status: "confirmed", DetectedAt: 1})
			return err
		}); err != nil {
		t.Fatal(err)
	}
	if err := database.ReconcileChainBalances(ctx, []store.ChainBalance{{AddressID: addressID, Address: address, Asset: "USDT", Raw: "2"}},
		store.NewEventConfig(testConfig(1).IPN, testConfig(1).Assets), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, status, code, err := server.ValidateWithdrawalSource(ctx, address, "USDT"); !errors.Is(err, store.ErrBalanceDrift) || status != http.StatusConflict || code != "balance_drift" {
		t.Fatalf("drift validation = status %d code %q err %v", status, code, err)
	}
	cleared := request(t, server.Handler(), http.MethodPost, "/api/v1/wallets/"+address+"/clear-drift", "")
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear drift = %d %s", cleared.Code, cleared.Body.String())
	}
	if balance, _, _, err := server.ValidateWithdrawalSource(ctx, address, "USDT"); err != nil || balance.ConfirmedRaw != "1000000" {
		t.Fatalf("post-clear validation = %+v, %v", balance, err)
	}
}

func testServer(t *testing.T, poolSize int) (*Server, *store.Store, func()) {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	hd, err := hdwallet.FromMnemonic(testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeWallet(context.Background(), hd, 0, poolSize, 1000); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(poolSize)
	pool, err := walletpool.NewPool(database, hd, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(database, pool, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return server, database, func() { hd.Destroy(); _ = database.Close() }
}

func testConfig(poolSize int) config.Config {
	return config.Config{
		Wallet: config.Wallet{Account: 0, PoolInitialSize: poolSize, PoolMinFree: 1, PoolMaxSize: poolSize, Cooldown: time.Hour},
		Assets: []config.Asset{{Symbol: "USDT", Kind: "trc20", Decimals: 6, Verified: true}},
		Orders: config.Orders{DefaultTTL: 30 * time.Minute},
		IPN:    config.IPN{DefaultConsumer: "shop", Consumers: []config.Consumer{{Name: "shop", Enabled: true}}},
		Price:  config.Price{Pairs: []string{"TRXUSDT"}, StaleAfter: 5 * time.Minute},
		Auth: config.Auth{TOTPSecret: testTOTP, APIKeys: []config.APIKey{{
			Name: "test", KeyHash: testKeyHash(testAPIKey), Scopes: []string{"orders:read", "orders:write", "wallets:read", "wallets:write"},
		}}},
	}
}

func testKeyHash(key string) string {
	salt := []byte("0123456789abcdef")
	hash := argon2.IDKey([]byte(key), salt, 1, 64, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=64,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
}

func request(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-API-Key", testAPIKey)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
}
