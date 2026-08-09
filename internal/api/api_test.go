package api

import (
	"context"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	"golang.org/x/crypto/argon2"
	"gopkg.in/yaml.v3"

	"payd/internal/config"
	"payd/internal/energy"
	"payd/internal/store"
	walletpool "payd/internal/wallet"
)

const (
	testMnemonic      = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	testAPIKey        = "correct horse battery staple"
	testNoScopeAPIKey = "authenticated but unscoped"
	testTOTP          = "JBSWY3DPEHPK3PXP"
)

// API-020/API-025: the embedded contract must stay in lockstep with both authenticated and public routes.
func TestOpenAPIRoutesAndScopesMatchRouteTables(t *testing.T) {
	type operation struct {
		Scope       string `yaml:"x-required-scope"`
		Description string `yaml:"description"`
	}
	var document struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openAPIDocument, &document); err != nil {
		t.Fatalf("parse embedded OpenAPI document: %v", err)
	}

	want := make(map[string]string, len(apiRoutes)+len(publicRoutes))
	for _, registered := range append(append([]route(nil), apiRoutes...), publicRoutes...) {
		key := strings.ToLower(registered.method) + " " + registered.pattern
		if _, duplicate := want[key]; duplicate {
			t.Fatalf("duplicate route table entry %s", key)
		}
		want[key] = registered.scope
	}

	got := make(map[string]string, len(want))
	for path, pathItem := range document.Paths {
		for method, node := range pathItem {
			switch strings.ToLower(method) {
			case "get", "post", "put", "patch", "delete", "head", "options", "trace":
			default:
				continue
			}
			var documented operation
			if err := node.Decode(&documented); err != nil {
				t.Errorf("decode OpenAPI operation %s %s: %v", method, path, err)
				continue
			}
			key := strings.ToLower(method) + " " + path
			got[key] = documented.Scope
			wantScope, exists := want[key]
			if !exists {
				t.Errorf("OpenAPI operation %s has no route table entry", key)
				continue
			}
			if documented.Scope != wantScope {
				t.Errorf("OpenAPI operation %s scope = %q, route table = %q", key, documented.Scope, wantScope)
			}
			scopeText := "Requires no scope."
			if wantScope != "" {
				scopeText = "Requires scope `" + wantScope + "`."
			}
			if !strings.Contains(documented.Description, scopeText) {
				t.Errorf("OpenAPI operation %s description does not declare %q", key, scopeText)
			}
		}
	}
	for key := range want {
		if _, exists := got[key]; !exists {
			t.Errorf("route table entry %s is absent from OpenAPI paths", key)
		}
	}
}

func TestDocumentationRoutesNeedNoAuthentication(t *testing.T) {
	server, _, cleanup := testServer(t, 1)
	defer cleanup()

	for target, want := range map[string][2]string{
		"/openapi.yaml": {"application/yaml", "openapi: 3.1.0"},
		"/docs":         {"text/html; charset=utf-8", "SwaggerUIBundle"},
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != want[0] || !strings.Contains(response.Body.String(), want[1]) {
			t.Fatalf("%s = %d %q %q", target, response.Code, response.Header().Get("Content-Type"), response.Body.String())
		}
	}
}

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

func TestReadyDegradesForUnsafeBurnCeilingWithoutAuthentication(t *testing.T) {
	server, _, cleanup := testServer(t, 2)
	defer cleanup()
	server.SetBurnCeilingHealthy(func() bool { return false })
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "energy_burn_ceiling") {
		t.Fatalf("degraded readyz = %d %s", response.Code, response.Body.String())
	}
	server.SetBurnCeilingHealthy(func() bool { return true })
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("healthy readyz = %d %s", response.Code, response.Body.String())
	}
}

func TestHealthMetricsAndOperationalReadinessNeedNoAuthentication(t *testing.T) {
	server, _, cleanup := testServer(t, 2)
	defer cleanup()
	server.SetOperations(func(context.Context) []string { return []string{"clock_skew"} },
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("payd_clock_skew_seconds 40\n")) }))

	for target, want := range map[string]struct {
		code int
		body string
	}{
		"/healthz": {http.StatusOK, `"status":"ok"`},
		"/readyz":  {http.StatusServiceUnavailable, "clock_skew"},
		"/metrics": {http.StatusOK, "payd_clock_skew_seconds 40"},
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != want.code || !strings.Contains(response.Body.String(), want.body) {
			t.Fatalf("%s = %d %s", target, response.Code, response.Body.String())
		}
	}
}

func TestWithdrawalCostsIncludeRentalAndResourceFees(t *testing.T) {
	network, resource, bandwidth, total := withdrawalCosts(store.Withdrawal{
		FeeRaw: "100000", EnergySource: "rented", EnergyCostTRX: "3",
		EnergyGrantFeeRaw: "10000", BandwidthGrantFeeRaw: "20000",
	})
	if network != "0.1" || resource != "0.03" || bandwidth != "0.02" || total != "3.13" {
		t.Fatalf("WDR-025 costs = network=%s resource=%s bandwidth=%s total=%s", network, resource, bandwidth, total)
	}
}

// TST-015 / WDR-001a: idempotent replay is resolved before the spent TOTP is validated.
func TestWithdrawalIdempotentReplayReturnsOKBeforeTOTP(t *testing.T) {
	server, database, cleanup := testServer(t, 3)
	defer cleanup()
	ctx := context.Background()
	addresses, err := database.WalletAddresses(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	source, destination := addresses[0], addresses[1]
	addressID := source.ID
	if err := database.CommitBlock(ctx, store.BlockRecord{Height: 1, ID: "b1", ParentID: "b0", Timestamp: 1, ProcessedAt: 1}, 10,
		func(write *store.BlockWrite) error {
			_, err := write.UpsertPayment(store.PaymentRecord{TxID: "fund-withdrawal", LogIndex: 0, Direction: "in", BlockHeight: 1,
				BlockID: "b1", BlockTimestamp: 1, FromAddress: destination.Address, ToAddress: source.Address,
				AddressID: &addressID, Asset: "USDT", AmountRaw: "2000000", Status: "confirmed", DetectedAt: 1})
			return err
		}); err != nil {
		t.Fatal(err)
	}
	code := totpCode(server.totpSecret, time.Now().UTC().Unix()/30)
	body := fmt.Sprintf(`{"from_address":%q,"to_address":%q,"asset":"USDT","amount":"1"}`, source.Address, destination.Address)
	call := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/withdrawals", strings.NewReader(body))
		req.Header.Set("X-API-Key", testAPIKey)
		req.Header.Set("Idempotency-Key", "retry-one")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-TOTP", code)
		server.Handler().ServeHTTP(recorder, req)
		return recorder
	}
	// The replay reuses a code the first call already consumed: WDR-001a requires the
	// idempotent branch to answer before TOTP is inspected, so this must not 401.
	first, replay := call(), call()
	if first.Code != http.StatusCreated || replay.Code != http.StatusOK {
		t.Fatalf("first=%d %s replay=%d %s", first.Code, first.Body.String(), replay.Code, replay.Body.String())
	}
}

// API-022: a code in the request body is refused outright, never quietly ignored — a caller
// that believes it sent a second factor must not be told the withdrawal simply succeeded.
func TestWithdrawalRejectsTOTPSuppliedInBody(t *testing.T) {
	server, database, cleanup := testServer(t, 3)
	defer cleanup()
	addresses, err := database.WalletAddresses(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	code := totpCode(server.totpSecret, time.Now().UTC().Unix()/30)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/withdrawals", strings.NewReader(fmt.Sprintf(
		`{"from_address":%q,"to_address":%q,"asset":"USDT","amount":"1","totp":%q}`,
		addresses[0].Address, addresses[1].Address, code)))
	req.Header.Set("X-API-Key", testAPIKey)
	req.Header.Set("Idempotency-Key", "totp-in-body")
	req.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "totp_in_body") {
		t.Fatalf("body TOTP = %d %s", recorder.Code, recorder.Body.String())
	}
	// The rejected request must not have consumed the code, or a correct retry would 401.
	if err := server.ValidateTOTP(context.Background(), code, time.Now()); err != nil {
		t.Fatalf("rejected request consumed the TOTP: %v", err)
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

func TestWalletCanWithdrawRequiresBandwidth(t *testing.T) {
	ctx := context.Background()
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	addresses, err := database.WalletAddresses(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAddressResources(ctx, store.ResourceReading{AddressID: addresses[0].ID,
		EnergyLimit: 100, BandwidthLimit: 255, NeedsResources: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(1)
	cfg.Resources.MinEnergy, cfg.Resources.MinBandwidth = 1, 345
	server.UpdateConfig(cfg)
	response := request(t, server.Handler(), http.MethodGet, "/api/v1/wallets/needs-resources", "")
	if response.Code != http.StatusOK {
		t.Fatalf("wallet resources = %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Addresses []struct {
			Bandwidth struct {
				Sufficient bool `json:"sufficient"`
			} `json:"bandwidth"`
			CanWithdraw map[string]bool `json:"can_withdraw"`
		} `json:"addresses"`
	}
	decodeResponse(t, response, &body)
	if len(body.Addresses) != 1 || body.Addresses[0].Bandwidth.Sufficient || body.Addresses[0].CanWithdraw["USDT"] {
		t.Fatalf("RES-016 response = %#v", body.Addresses)
	}
}

type fakeResourceDelegator struct {
	address, resourceType, actor, ip string
	amount                           int64
	err                              error
	providerBalance                  string
	estimate                         energy.ResourceEstimate
}

func (f *fakeResourceDelegator) DelegateResources(_ context.Context, address, resourceType string, amount int64, actor, ip string) (store.ResourceGrant, error) {
	f.address, f.resourceType, f.amount, f.actor, f.ip = address, resourceType, amount, actor, ip
	if f.err != nil {
		return store.ResourceGrant{}, f.err
	}
	return store.ResourceGrant{ID: "grant-1", ReceiverAddress: address, ResourceType: resourceType, AmountSun: "2000000", Status: "broadcast", TxID: "delegate-tx"}, nil
}

func (f *fakeResourceDelegator) ProviderBalanceMetric() (string, bool) {
	return f.providerBalance, false
}

func (f *fakeResourceDelegator) EstimateResources(context.Context, string, string) (energy.ResourceEstimate, error) {
	if f.err != nil {
		return energy.ResourceEstimate{}, f.err
	}
	if f.estimate.EnergySource == "" && f.estimate.BlockedBy == "" {
		return energy.ResourceEstimate{EnergySource: "existing", TRXCost: "0"}, nil
	}
	return f.estimate, nil
}

func TestTierANewRoutesRequireAuthenticationAndScopes(t *testing.T) {
	server, _, cleanup := testServer(t, 3)
	defer cleanup()
	server.SetDelegator(&fakeResourceDelegator{})
	routes := []struct {
		method, path string
		unscopedOK   bool
	}{
		{http.MethodGet, "/api/v1/wallets", false},
		{http.MethodGet, "/api/v1/wallets/with-balance", false},
		{http.MethodGet, "/api/v1/wallets/unknown", false},
		{http.MethodPost, "/api/v1/wallets/unknown/disable", false},
		{http.MethodPost, "/api/v1/wallets/unknown/delegate", false},
		{http.MethodGet, "/api/v1/ipn/dead", false},
		{http.MethodPost, "/api/v1/ipn/unknown/retry", false},
		{http.MethodGet, "/api/v1/ipn/consumers", false},
		{http.MethodGet, "/api/v1/chain/params", false},
		{http.MethodGet, "/api/v1/prices", true},
		{http.MethodGet, "/api/v1/stats", true},
		{http.MethodGet, "/api/v1/energy/status", false},
		{http.MethodGet, "/api/v1/energy/purchases", false},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			missing := httptest.NewRecorder()
			server.Handler().ServeHTTP(missing, httptest.NewRequest(route.method, route.path, nil))
			if missing.Code != http.StatusUnauthorized {
				t.Fatalf("missing key = %d %s", missing.Code, missing.Body.String())
			}
			unscoped := requestWithKey(t, server.Handler(), route.method, route.path, "", testNoScopeAPIKey)
			want := http.StatusUnauthorized
			if route.unscopedOK {
				want = http.StatusOK
			}
			if unscoped.Code != want {
				t.Fatalf("unscoped key = %d %s, want %d", unscoped.Code, unscoped.Body.String(), want)
			}
		})
	}
}

func TestA1ListWallets(t *testing.T) {
	server, _, cleanup := testServer(t, 2)
	defer cleanup()
	response := request(t, server.Handler(), http.MethodGet, "/api/v1/wallets", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"free"`) || !strings.Contains(response.Body.String(), `"next_cursor"`) {
		t.Fatalf("wallet list = %d %s", response.Code, response.Body.String())
	}
}

func TestA2WithBalanceLiteralRouteUsesConfirmedFunds(t *testing.T) {
	server, database, cleanup := testServer(t, 2)
	defer cleanup()
	addresses := fundTestWallets(t, database, 1)
	addressID := addresses[1].ID
	if err := database.CommitBlock(context.Background(), store.BlockRecord{Height: 2, ID: "pending-block", ParentID: "fund-block", Timestamp: 2, ProcessedAt: 2}, 64, func(write *store.BlockWrite) error {
		_, err := write.UpsertPayment(store.PaymentRecord{TxID: "pending-only", Direction: "in", BlockHeight: 2,
			BlockID: "pending-block", BlockTimestamp: 2, FromAddress: "payer", ToAddress: addresses[1].Address,
			AddressID: &addressID, Asset: "USDT", AmountRaw: "9000000", Status: "seen", DetectedAt: 2})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	response := request(t, server.Handler(), http.MethodGet, "/api/v1/wallets/with-balance", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), addresses[0].Address) || strings.Contains(response.Body.String(), addresses[1].Address) {
		t.Fatalf("with-balance route = %d %s", response.Code, response.Body.String())
	}
}

func TestA3WalletDetailAndUnknown(t *testing.T) {
	server, database, cleanup := testServer(t, 2)
	defer cleanup()
	addresses := fundTestWallets(t, database, 1)
	detail := request(t, server.Handler(), http.MethodGet, "/api/v1/wallets/"+addresses[0].Address, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"txid":"fund-0"`) {
		t.Fatalf("wallet detail = %d %s", detail.Code, detail.Body.String())
	}
	unknown := request(t, server.Handler(), http.MethodGet, "/api/v1/wallets/TUnknown", "")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown wallet = %d %s", unknown.Code, unknown.Body.String())
	}
}

func TestA4DisableWallet(t *testing.T) {
	server, database, cleanup := testServer(t, 2)
	defer cleanup()
	addresses, err := database.WalletAddresses(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server.Handler(), http.MethodPost, "/api/v1/wallets/"+addresses[0].Address+"/disable", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"disabled"`) {
		t.Fatalf("disable wallet = %d %s", response.Code, response.Body.String())
	}
	disabled, err := database.WalletAddress(context.Background(), addresses[0].Address)
	if err != nil || disabled.State != "disabled" {
		t.Fatalf("stored wallet = %+v, %v", disabled, err)
	}
	unknown := request(t, server.Handler(), http.MethodPost, "/api/v1/wallets/TUnknown/disable", "")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("disable unknown = %d %s", unknown.Code, unknown.Body.String())
	}
}

func TestA5DelegateWalletUsesEngine(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	addresses, _ := database.WalletAddresses(context.Background(), false)
	fake := &fakeResourceDelegator{}
	server.SetDelegator(fake)
	response := request(t, server.Handler(), http.MethodPost, "/api/v1/wallets/"+addresses[0].Address+"/delegate", `{"resource_type":"ENERGY","amount":131000}`)
	if response.Code != http.StatusAccepted || fake.address != addresses[0].Address || fake.resourceType != "ENERGY" || fake.amount != 131000 || fake.actor != "test" {
		t.Fatalf("delegate = %d %s fake=%+v", response.Code, response.Body.String(), fake)
	}
	server.SetDelegator(&fakeResourceDelegator{err: store.ErrAddressNotFound})
	unknown := request(t, server.Handler(), http.MethodPost, "/api/v1/wallets/TUnknown/delegate", `{"resource_type":"BANDWIDTH","amount":345}`)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("delegate unknown = %d %s", unknown.Code, unknown.Body.String())
	}
}

func TestA6DeadIPNList(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	id := createDeadIPN(t, database, testConfig(1), "dead-one")
	response := request(t, server.Handler(), http.MethodGet, "/api/v1/ipn/dead?consumer=shop", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), id) || !strings.Contains(response.Body.String(), `"last_error":"sink failed"`) {
		t.Fatalf("dead IPN = %d %s", response.Code, response.Body.String())
	}
}

func TestA7RetryDeadIPN(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	id := createDeadIPN(t, database, testConfig(1), "retry-one")
	response := request(t, server.Handler(), http.MethodPost, "/api/v1/ipn/"+id+"/retry", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"pending"`) {
		t.Fatalf("retry IPN = %d %s", response.Code, response.Body.String())
	}
	conflict := request(t, server.Handler(), http.MethodPost, "/api/v1/ipn/"+id+"/retry", "")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("retry non-dead = %d %s", conflict.Code, conflict.Body.String())
	}
	unknown := request(t, server.Handler(), http.MethodPost, "/api/v1/ipn/unknown/retry", "")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("retry unknown = %d %s", unknown.Code, unknown.Body.String())
	}
}

func TestA8IPNConsumersNeverExposeSecretsOrURLs(t *testing.T) {
	server, _, cleanup := testServer(t, 1)
	defer cleanup()
	response := request(t, server.Handler(), http.MethodGet, "/api/v1/ipn/consumers", "")
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"name":"shop"`) || strings.Contains(body, "consumer-secret") || strings.Contains(body, "shop.invalid") {
		t.Fatalf("consumers = %d %s", response.Code, body)
	}
}

func TestA9ChainParameters(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	if _, err := database.UpsertChainParameters(context.Background(), 210, 1000, time.Now()); err != nil {
		t.Fatal(err)
	}
	response := request(t, server.Handler(), http.MethodGet, "/api/v1/chain/params", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"getEnergyFee":210`) {
		t.Fatalf("chain params = %d %s", response.Code, response.Body.String())
	}
}

func TestA10PricesForAnyAuthenticatedKey(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	if err := database.UpsertPrices(context.Background(), []store.Price{{Symbol: "TRX", PriceUSD: "0.123456", Source: "test", FetchedAt: time.Now().Unix()}}); err != nil {
		t.Fatal(err)
	}
	response := requestWithKey(t, server.Handler(), http.MethodGet, "/api/v1/prices", "", testNoScopeAPIKey)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"price_usd":"0.123456"`) {
		t.Fatalf("prices = %d %s", response.Code, response.Body.String())
	}
}

func TestA11StatsSharesOperationalMetrics(t *testing.T) {
	server, _, cleanup := testServer(t, 1)
	defer cleanup()
	response := requestWithKey(t, server.Handler(), http.MethodGet, "/api/v1/stats", "", testNoScopeAPIKey)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"orders"`) || !strings.Contains(response.Body.String(), `"energy_costs"`) {
		t.Fatalf("stats = %d %s", response.Code, response.Body.String())
	}
}

func TestA12EnergyStatusNeverExposesCredentials(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	cfg := testConfig(1)
	if err := database.RecordEnergyProviderBalance(context.Background(), cfg.Energy.Provider, "12.5", cfg.Energy.BalanceWarnTRX, time.Now(), store.NewEventConfig(cfg.IPN, cfg.Assets)); err != nil {
		t.Fatal(err)
	}
	server.SetDelegator(&fakeResourceDelegator{providerBalance: "13.5"})
	response := request(t, server.Handler(), http.MethodGet, "/api/v1/energy/status", "")
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"balance_trx":"13.5"`) || strings.Contains(body, "energy-key") || strings.Contains(body, "energy-secret") {
		t.Fatalf("energy status = %d %s", response.Code, body)
	}
}

func TestA13EnergyPurchases(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	addresses, _ := database.WalletAddresses(context.Background(), false)
	if _, err := database.CreateEnergyPurchase(context.Background(), "test-energy", "", addresses[0].ID, addresses[0].Address, "ENERGY", 131000, time.Hour, "3.25", time.Now()); err != nil {
		t.Fatal(err)
	}
	response := request(t, server.Handler(), http.MethodGet, "/api/v1/energy/purchases", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"quoted_trx":"3.25"`) || !strings.Contains(response.Body.String(), `"actual_trx":""`) {
		t.Fatalf("energy purchases = %d %s", response.Code, response.Body.String())
	}
}

func TestTierAListPaginationBoundsAndCursorRoundTrips(t *testing.T) {
	server, database, cleanup := testServer(t, 3)
	defer cleanup()
	fundTestWallets(t, database, 2)
	cfg := testConfig(3)
	createDeadIPN(t, database, cfg, "dead-a")
	createDeadIPN(t, database, cfg, "dead-b")
	if err := database.UpsertPrices(context.Background(), []store.Price{
		{Symbol: "AAA", PriceUSD: "1", Source: "test", FetchedAt: time.Now().Unix()},
		{Symbol: "BBB", PriceUSD: "2", Source: "test", FetchedAt: time.Now().Unix()},
	}); err != nil {
		t.Fatal(err)
	}
	addresses, _ := database.WalletAddresses(context.Background(), false)
	for i := range 2 {
		if _, err := database.CreateEnergyPurchase(context.Background(), "test-energy", "", addresses[0].ID, addresses[0].Address, "ENERGY", int64(100+i), time.Hour, "1", time.Now().Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	targets := []string{"/api/v1/wallets", "/api/v1/wallets/with-balance", "/api/v1/ipn/dead", "/api/v1/ipn/consumers", "/api/v1/prices", "/api/v1/energy/purchases"}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			for _, invalid := range []string{"0", "201"} {
				response := request(t, server.Handler(), http.MethodGet, target+"?limit="+invalid, "")
				if response.Code != http.StatusBadRequest {
					t.Fatalf("limit %s = %d %s", invalid, response.Code, response.Body.String())
				}
			}
			first := request(t, server.Handler(), http.MethodGet, target+"?limit=1", "")
			var page map[string]json.RawMessage
			decodeResponse(t, first, &page)
			var cursor string
			if err := json.Unmarshal(page["next_cursor"], &cursor); err != nil || cursor == "" {
				t.Fatalf("first page cursor = %q, %v; body=%s", cursor, err, first.Body.String())
			}
			second := request(t, server.Handler(), http.MethodGet, target+"?limit=1&cursor="+cursor, "")
			if second.Code != http.StatusOK || second.Body.String() == first.Body.String() {
				t.Fatalf("second page = %d %s; first=%s", second.Code, second.Body.String(), first.Body.String())
			}
		})
	}
}

func TestLargeMoneyFormattingRoundTripsWithoutPrecisionLoss(t *testing.T) {
	raw := new(big.Int).Exp(big.NewInt(10), big.NewInt(40), nil)
	raw.Add(raw, big.NewInt(123456))
	formatted, err := store.FormatUnits(raw.String(), 6)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := store.ParseUnits(formatted, 6)
	if err != nil || parsed.Cmp(raw) != 0 {
		t.Fatalf("raw=%s formatted=%s parsed=%v err=%v", raw, formatted, parsed, err)
	}
}

func TestB1PaymentSearchAndB2OrderFilters(t *testing.T) {
	server, database, cleanup := testServer(t, 3)
	defer cleanup()
	firstResponse := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount":"1","external_ref":"invoice-search","consumer":"shop"}`)
	secondResponse := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount":"2","external_ref":"invoice-other","consumer":"analytics"}`)
	if firstResponse.Code != http.StatusCreated || secondResponse.Code != http.StatusCreated {
		t.Fatalf("create orders = %d/%d", firstResponse.Code, secondResponse.Code)
	}
	var first, second map[string]any
	decodeResponse(t, firstResponse, &first)
	decodeResponse(t, secondResponse, &second)
	firstOrder, err := database.Order(context.Background(), first["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	firstID, secondID := firstOrder.ID, second["id"].(string)
	addressID, orderID := firstOrder.AddressID, firstOrder.ID
	if err := database.CommitBlock(context.Background(), store.BlockRecord{Height: 1, ID: "search-block", ParentID: "genesis", Timestamp: 200}, 64,
		func(write *store.BlockWrite) error {
			for _, payment := range []store.PaymentRecord{
				{TxID: "customer-tx", LogIndex: 0, Direction: "in", BlockHeight: 1, BlockID: "search-block", BlockTimestamp: 150,
					FromAddress: "TPayerSearch", ToAddress: firstOrder.Address, AddressID: &addressID, OrderID: &orderID,
					Asset: "USDT", AmountRaw: "1000000", Status: "confirmed", DetectedAt: 151},
				{TxID: "other-tx", LogIndex: 0, Direction: "out", BlockHeight: 1, BlockID: "search-block", BlockTimestamp: 250,
					FromAddress: firstOrder.Address, ToAddress: "TOther", AddressID: &addressID,
					Asset: "USDT", AmountRaw: "2", Status: "seen", DetectedAt: 251},
			} {
				if _, err := write.UpsertPayment(payment); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		t.Fatal(err)
	}

	search := fmt.Sprintf("/api/v1/payments?txid=customer-tx&address=%s&order_id=%s&status=confirmed&direction=in&asset=USDT&from=100&to=200",
		firstOrder.Address, firstOrder.ID)
	matched := request(t, server.Handler(), http.MethodGet, search, "")
	if matched.Code != http.StatusOK || !strings.Contains(matched.Body.String(), `"txid":"customer-tx"`) || strings.Contains(matched.Body.String(), `"txid":"other-tx"`) {
		t.Fatalf("payment search = %d %s", matched.Code, matched.Body.String())
	}
	var page struct {
		Payments   []map[string]any `json:"payments"`
		NextCursor string           `json:"next_cursor"`
	}
	firstPage := request(t, server.Handler(), http.MethodGet, "/api/v1/payments?limit=1", "")
	decodeResponse(t, firstPage, &page)
	if len(page.Payments) != 1 || page.NextCursor == "" {
		t.Fatalf("payment first page = %#v", page)
	}
	secondPage := request(t, server.Handler(), http.MethodGet, "/api/v1/payments?limit=1&cursor="+page.NextCursor, "")
	decodeResponse(t, secondPage, &page)
	if len(page.Payments) != 1 {
		t.Fatalf("payment second page = %#v", page)
	}
	if invalid := request(t, server.Handler(), http.MethodGet, "/api/v1/payments?limit=0", ""); invalid.Code != http.StatusBadRequest {
		t.Fatalf("payment invalid limit = %d %s", invalid.Code, invalid.Body.String())
	}

	orders := request(t, server.Handler(), http.MethodGet,
		"/api/v1/orders?external_ref=invoice-search&consumer=shop&address="+firstOrder.Address, "")
	if orders.Code != http.StatusOK || !strings.Contains(orders.Body.String(), firstID) || strings.Contains(orders.Body.String(), secondID) {
		t.Fatalf("order filters = %d %s", orders.Code, orders.Body.String())
	}
}

func TestB3OrderExtensionCapAndB4EventPagination(t *testing.T) {
	server, database, cleanup := testServer(t, 2)
	defer cleanup()
	created := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount":"1","external_ref":"extend-events"}`)
	var body map[string]any
	decodeResponse(t, created, &body)
	id := body["id"].(string)
	extended := request(t, server.Handler(), http.MethodPost, "/api/v1/orders/"+id+"/extend", `{"ttl_seconds":3600}`)
	if extended.Code != http.StatusOK || !strings.Contains(extended.Body.String(), `"updated_at"`) {
		t.Fatalf("extend = %d %s", extended.Code, extended.Body.String())
	}
	overCap := request(t, server.Handler(), http.MethodPost, "/api/v1/orders/"+id+"/extend", `{"ttl_seconds":86400}`)
	if overCap.Code != http.StatusConflict || !strings.Contains(overCap.Body.String(), "order_lifetime_exceeded") {
		t.Fatalf("over-cap extend = %d %s", overCap.Code, overCap.Body.String())
	}
	events := store.NewEventConfig(testConfig(2).IPN, testConfig(2).Assets)
	if err := database.CommitBlock(context.Background(), store.BlockRecord{Height: 1, ID: "event-block", ParentID: "genesis", Timestamp: 1}, 64,
		func(write *store.BlockWrite) error {
			if err := write.EnqueueOrderEvent(events, id, "order.test-one", map[string]any{"order_id": id}, time.Unix(10, 0)); err != nil {
				return err
			}
			return write.EnqueueOrderEvent(events, id, "order.test-two", map[string]any{"order_id": id}, time.Unix(11, 0))
		}); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Events     []map[string]any `json:"events"`
		NextCursor string           `json:"next_cursor"`
	}
	firstPage := request(t, server.Handler(), http.MethodGet, "/api/v1/orders/"+id+"/events?limit=1", "")
	decodeResponse(t, firstPage, &page)
	if len(page.Events) != 1 || page.NextCursor == "" || page.Events[0]["consumer"] != "shop" {
		t.Fatalf("event first page = %#v", page)
	}
	secondPage := request(t, server.Handler(), http.MethodGet, "/api/v1/orders/"+id+"/events?limit=1&cursor="+page.NextCursor, "")
	decodeResponse(t, secondPage, &page)
	if len(page.Events) != 1 || page.Events[0]["event_type"] != "order.test-two" {
		t.Fatalf("event second page = %#v", page)
	}
	if invalid := request(t, server.Handler(), http.MethodGet, "/api/v1/orders/"+id+"/events?limit=201", ""); invalid.Code != http.StatusBadRequest {
		t.Fatalf("event invalid limit = %d %s", invalid.Code, invalid.Body.String())
	}
	if cancelled := request(t, server.Handler(), http.MethodPost, "/api/v1/orders/"+id+"/cancel", `{}`); cancelled.Code != http.StatusOK {
		t.Fatalf("cancel = %d %s", cancelled.Code, cancelled.Body.String())
	}
	terminal := request(t, server.Handler(), http.MethodPost, "/api/v1/orders/"+id+"/extend", `{"ttl_seconds":1}`)
	if terminal.Code != http.StatusConflict || !strings.Contains(terminal.Body.String(), "order_terminal") {
		t.Fatalf("terminal extend = %d %s", terminal.Code, terminal.Body.String())
	}
}

func TestB5OperatorResolutionPreservesTxIDAndTOTPReplay(t *testing.T) {
	server, database, path, cleanup := testServerWithPath(t, 3)
	defer cleanup()
	addresses := fundTestWallets(t, database, 2)
	ctx := context.Background()
	now := time.Now().UTC()
	resolvedCandidate, _, err := database.CreateWithdrawal(ctx, store.CreateWithdrawal{IdempotencyKey: "resolve-one",
		FromAddress: addresses[0].Address, ToAddress: addresses[2].Address, Asset: "USDT", AmountRaw: "1",
		AmountUSD: "1", DailyLimitUSD: "1000", RequestedBy: "test", IP: "127.0.0.1", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := database.ClaimWithdrawal(ctx, resolvedCandidate.ID); err != nil || !claimed {
		t.Fatalf("claim withdrawal: claimed=%v err=%v", claimed, err)
	}
	if changed, err := database.SetWithdrawalResourceState(ctx, resolvedCandidate.ID, "awaiting_resources", "signing", "existing", "existing"); err != nil || !changed {
		t.Fatalf("set signing: changed=%v err=%v", changed, err)
	}
	if err := database.PersistWithdrawalAttempt(ctx, resolvedCandidate.ID, "immutable-txid", now, now.Add(time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := database.WithdrawalNeedsOperator(ctx, resolvedCandidate.ID, "ambiguous", now,
		store.NewEventConfig(testConfig(3).IPN, testConfig(3).Assets)); err != nil {
		t.Fatal(err)
	}
	nonCandidate, _, err := database.CreateWithdrawal(ctx, store.CreateWithdrawal{IdempotencyKey: "resolve-two",
		FromAddress: addresses[1].Address, ToAddress: addresses[2].Address, Asset: "USDT", AmountRaw: "1",
		AmountUSD: "1", DailyLimitUSD: "1000", RequestedBy: "test", IP: "127.0.0.1", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	call := func(id, code, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/withdrawals/"+id+"/resolve", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", testAPIKey)
		req.Header.Set("X-TOTP", code)
		server.Handler().ServeHTTP(recorder, req)
		return recorder
	}
	step := time.Now().UTC().Unix() / 30
	code := totpCode(server.totpSecret, step)
	resolved := call(resolvedCandidate.ID, code, `{"outcome":"confirmed","failure_reason":"chain verified manually"}`)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve = %d %s", resolved.Code, resolved.Body.String())
	}
	stored, err := database.Withdrawal(ctx, resolvedCandidate.ID)
	if err != nil || stored.Status != "confirmed" || stored.TxID != "immutable-txid" || stored.ResolvedBy != "operator" {
		t.Fatalf("resolved withdrawal = %#v err=%v", stored, err)
	}
	replay := call(resolvedCandidate.ID, code, `{"outcome":"failed","failure_reason":"replay"}`)
	if replay.Code != http.StatusUnauthorized || !strings.Contains(replay.Body.String(), "invalid_totp") {
		t.Fatalf("TOTP replay = %d %s", replay.Code, replay.Body.String())
	}
	wrongState := call(nonCandidate.ID, totpCode(server.totpSecret, step-1), `{"outcome":"failed","failure_reason":"operator rejected"}`)
	if wrongState.Code != http.StatusConflict {
		t.Fatalf("non-needs_operator = %d %s", wrongState.Code, wrongState.Body.String())
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var audits int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='withdrawal.resolved' AND actor='test' AND ip<>''`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("resolution audits=%d err=%v", audits, err)
	}
	// Server has no signer or broadcaster dependency; retaining txid verifies the fund-moving path is unreachable here.
}

func TestB6EstimatePerformsZeroStateWrites(t *testing.T) {
	server, database, path, cleanup := testServerWithPath(t, 2)
	defer cleanup()
	addresses := fundTestWallets(t, database, 1)
	server.SetDelegator(&fakeResourceDelegator{estimate: energy.ResourceEstimate{EnergySource: "burned", TRXCost: "2.75"}})
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	before := snapshotRowCounts(t, raw)
	response := request(t, server.Handler(), http.MethodPost, "/api/v1/withdrawals/estimate", fmt.Sprintf(
		`{"from_address":%q,"to_address":%q,"asset":"USDT","amount":"0.5"}`, addresses[0].Address, addresses[1].Address))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"projected_energy_source":"burned"`) ||
		!strings.Contains(response.Body.String(), `"projected_trx_cost":"2.75"`) {
		t.Fatalf("estimate = %d %s", response.Code, response.Body.String())
	}
	after := snapshotRowCounts(t, raw)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("estimate changed row counts: before=%v after=%v", before, after)
	}
}

func TestB7WhoamiAndB8AssetsAllowAnyAuthenticatedKey(t *testing.T) {
	server, _, cleanup := testServer(t, 1)
	defer cleanup()
	for _, target := range []string{"/api/v1/auth/whoami", "/api/v1/assets"} {
		missing := httptest.NewRecorder()
		server.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodGet, target, nil))
		if missing.Code != http.StatusUnauthorized {
			t.Fatalf("%s missing auth = %d", target, missing.Code)
		}
		response := requestWithKey(t, server.Handler(), http.MethodGet, target, "", testNoScopeAPIKey)
		if response.Code != http.StatusOK {
			t.Fatalf("%s unscoped auth = %d %s", target, response.Code, response.Body.String())
		}
	}
	who := requestWithKey(t, server.Handler(), http.MethodGet, "/api/v1/auth/whoami", "", testNoScopeAPIKey)
	if !strings.Contains(who.Body.String(), `"key_name":"unscoped"`) || !strings.Contains(who.Body.String(), `"scopes":[]`) {
		t.Fatalf("whoami = %s", who.Body.String())
	}
	assets := request(t, server.Handler(), http.MethodGet, "/api/v1/assets", "")
	if !strings.Contains(assets.Body.String(), `"decimals":6`) || !strings.Contains(assets.Body.String(), `"verified":true`) {
		t.Fatalf("assets = %s", assets.Body.String())
	}
}

func TestB9SignedTestIPNBypassesOutbox(t *testing.T) {
	var received bool
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = r.Header.Get("X-Signature") != "" && r.Header.Get("X-Timestamp") != "" &&
			r.Header.Get("X-Consumer") == "shop" && strings.Contains(string(body), `"event_type":"test.ping"`)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	cfg := testConfig(1)
	cfg.IPN.Timeout = time.Second
	cfg.IPN.Consumers[0].URL = sink.URL
	server.UpdateConfig(cfg)
	before, err := database.OutboxCount(context.Background(), "test.ping")
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server.Handler(), http.MethodPost, "/api/v1/ipn/test", `{"consumer":"shop"}`)
	after, err := database.OutboxCount(context.Background(), "test.ping")
	if err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !received || before != after {
		t.Fatalf("test IPN = %d received=%v outbox=%d/%d body=%s", response.Code, received, before, after, response.Body.String())
	}
}

func TestB10ReplayDefaultsToDryRun(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	id := createDeadIPN(t, database, testConfig(1), "bulk-default")
	dry := request(t, server.Handler(), http.MethodPost, "/api/v1/ipn/replay", `{"consumer":"shop"}`)
	status, found, err := database.IPNStatus(context.Background(), id)
	if dry.Code != http.StatusOK || err != nil || !found || status != "dead" || !strings.Contains(dry.Body.String(), `"count":1`) {
		t.Fatalf("default dry replay = %d status=%q found=%v err=%v body=%s", dry.Code, status, found, err, dry.Body.String())
	}
	live := request(t, server.Handler(), http.MethodPost, "/api/v1/ipn/replay", `{"consumer":"shop","dry_run":false}`)
	status, found, err = database.IPNStatus(context.Background(), id)
	if live.Code != http.StatusOK || err != nil || !found || status != "pending" {
		t.Fatalf("live replay = %d status=%q found=%v err=%v body=%s", live.Code, status, found, err, live.Body.String())
	}
}

func TestTierBRouteAuthenticationAndScopes(t *testing.T) {
	server, _, cleanup := testServer(t, 1)
	defer cleanup()
	routes := []struct {
		method, path string
		unscopedOK   bool
	}{
		{http.MethodGet, "/api/v1/payments", false},
		{http.MethodGet, "/api/v1/orders?external_ref=x", false},
		{http.MethodPost, "/api/v1/orders/x/extend", false},
		{http.MethodGet, "/api/v1/orders/x/events", false},
		{http.MethodPost, "/api/v1/withdrawals/x/resolve", false},
		{http.MethodPost, "/api/v1/withdrawals/estimate", false},
		{http.MethodGet, "/api/v1/auth/whoami", true},
		{http.MethodGet, "/api/v1/assets", true},
		{http.MethodPost, "/api/v1/ipn/test", false},
		{http.MethodPost, "/api/v1/ipn/replay", false},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			missing := httptest.NewRecorder()
			server.Handler().ServeHTTP(missing, httptest.NewRequest(route.method, route.path, nil))
			if missing.Code != http.StatusUnauthorized {
				t.Fatalf("missing key = %d %s", missing.Code, missing.Body.String())
			}
			unscoped := requestWithKey(t, server.Handler(), route.method, route.path, "", testNoScopeAPIKey)
			if route.unscopedOK && unscoped.Code == http.StatusUnauthorized {
				t.Fatalf("unscoped authenticated key rejected: %s", unscoped.Body.String())
			}
			if !route.unscopedOK && unscoped.Code != http.StatusUnauthorized {
				t.Fatalf("missing scope = %d %s", unscoped.Code, unscoped.Body.String())
			}
		})
	}
}

func TestC1ChainStatusC2QuotaAndC3WorkerPagination(t *testing.T) {
	server, database, path, cleanup := testServerWithPath(t, 2)
	defer cleanup()
	now := time.Now().UTC()
	if err := database.CommitBlock(context.Background(), store.BlockRecord{Height: 1, ID: "ops-block", ParentID: "genesis",
		Timestamp: now.Add(-30 * time.Second).Unix(), ProcessedAt: now.Unix()}, 64, nil); err != nil {
		t.Fatal(err)
	}
	if err := database.SetReorgSuspicion(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	status := request(t, server.Handler(), http.MethodGet, "/api/v1/chain/status", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"last_height":1`) ||
		!strings.Contains(status.Body.String(), `"reorg_suspected":true`) || !strings.Contains(status.Body.String(), `"lag_seconds"`) {
		t.Fatalf("chain status = %d %s", status.Code, status.Body.String())
	}
	for range 2 {
		if _, err := database.RecordTronGridRequest(context.Background(), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.RecordTronGridRequest(context.Background(), now.Add(-6*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordTronGridRequest(context.Background(), now.Add(-7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	quota := request(t, server.Handler(), http.MethodGet, "/api/v1/chain/quota", "")
	if quota.Code != http.StatusOK || !strings.Contains(quota.Body.String(), `"requests_today":2`) ||
		strings.Contains(quota.Body.String(), strconv.FormatInt(now.Add(-7*24*time.Hour).Truncate(24*time.Hour).Unix(), 10)) {
		t.Fatalf("chain quota = %d %s", quota.Code, quota.Body.String())
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(`INSERT INTO worker_health(worker,last_tick_at,last_error,error_count,restarts) VALUES
		('confirm',?,'stalled',2,1),('follower',?,NULL,0,0)`, now.Add(-10*time.Second).Unix(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Workers    []map[string]any `json:"workers"`
		NextCursor string           `json:"next_cursor"`
	}
	first := request(t, server.Handler(), http.MethodGet, "/api/v1/workers?limit=1", "")
	decodeResponse(t, first, &page)
	if len(page.Workers) != 1 || page.NextCursor == "" || page.Workers[0]["worker"] != "confirm" {
		t.Fatalf("worker first page = %#v", page)
	}
	second := request(t, server.Handler(), http.MethodGet, "/api/v1/workers?limit=1&cursor="+page.NextCursor, "")
	decodeResponse(t, second, &page)
	if len(page.Workers) != 1 || page.Workers[0]["worker"] != "follower" {
		t.Fatalf("worker second page = %#v", page)
	}
	if invalid := request(t, server.Handler(), http.MethodGet, "/api/v1/workers?limit=0", ""); invalid.Code != http.StatusBadRequest {
		t.Fatalf("worker invalid limit = %d", invalid.Code)
	}
}

func TestC4AuditC5GrantPaginationAndC6ResourceWallet(t *testing.T) {
	server, database, path, cleanup := testServerWithPath(t, 2)
	defer cleanup()
	now := time.Now().UTC()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(`INSERT INTO audit_log(actor,action,subject,detail,ip,created_at) VALUES
		('operator','wallet.disable','first','{}','127.0.0.1',?),
		('operator','wallet.disable','second','{}','127.0.0.2',?)`, now.Add(-time.Second).Unix(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	var auditPage struct {
		Entries    []map[string]any `json:"entries"`
		NextCursor string           `json:"next_cursor"`
	}
	firstAudit := request(t, server.Handler(), http.MethodGet, "/api/v1/audit?actor=operator&action=wallet.disable&limit=1", "")
	decodeResponse(t, firstAudit, &auditPage)
	if len(auditPage.Entries) != 1 || auditPage.NextCursor == "" || auditPage.Entries[0]["subject"] != "second" {
		t.Fatalf("audit first page = %#v", auditPage)
	}
	secondAudit := request(t, server.Handler(), http.MethodGet, "/api/v1/audit?limit=1&cursor="+auditPage.NextCursor, "")
	decodeResponse(t, secondAudit, &auditPage)
	if len(auditPage.Entries) != 1 || auditPage.Entries[0]["subject"] != "first" {
		t.Fatalf("audit second page = %#v", auditPage)
	}
	if invalid := request(t, server.Handler(), http.MethodGet, "/api/v1/audit?limit=201", ""); invalid.Code != http.StatusBadRequest {
		t.Fatalf("audit invalid limit = %d", invalid.Code)
	}
	addresses, err := database.WalletAddresses(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	energyGrant, err := database.CreateManualResourceGrant(context.Background(), addresses[0].Address, "ENERGY", "1000000", 1,
		"operator", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	bandwidthGrant, err := database.CreateManualResourceGrant(context.Background(), addresses[1].Address, "BANDWIDTH", "2000000", 2,
		"operator", "127.0.0.1", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	filtered := request(t, server.Handler(), http.MethodGet, "/api/v1/resources/grants?resource_type=ENERGY", "")
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), energyGrant.ID) || strings.Contains(filtered.Body.String(), bandwidthGrant.ID) {
		t.Fatalf("grant filter = %d %s", filtered.Code, filtered.Body.String())
	}
	var grantPage struct {
		Grants     []map[string]any `json:"grants"`
		NextCursor string           `json:"next_cursor"`
	}
	firstGrant := request(t, server.Handler(), http.MethodGet, "/api/v1/resources/grants?limit=1", "")
	decodeResponse(t, firstGrant, &grantPage)
	if len(grantPage.Grants) != 1 || grantPage.NextCursor == "" {
		t.Fatalf("grant first page = %#v", grantPage)
	}
	secondGrant := request(t, server.Handler(), http.MethodGet, "/api/v1/resources/grants?limit=1&cursor="+grantPage.NextCursor, "")
	decodeResponse(t, secondGrant, &grantPage)
	if len(grantPage.Grants) != 1 {
		t.Fatalf("grant second page = %#v", grantPage)
	}
	if invalid := request(t, server.Handler(), http.MethodGet, "/api/v1/resources/grants?limit=0", ""); invalid.Code != http.StatusBadRequest {
		t.Fatalf("grant invalid limit = %d", invalid.Code)
	}
	resourceID, resourceAddress, err := database.ResourceWallet(context.Background(), testConfig(2).Resources.ResourceWalletIndex)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAddressResources(context.Background(), store.ResourceReading{AddressID: resourceID,
		EnergyLimit: 1000, EnergyUsed: 100, BandwidthLimit: 500, BandwidthUsed: 50}, now); err != nil {
		t.Fatal(err)
	}
	wallet := request(t, server.Handler(), http.MethodGet, "/api/v1/resources/wallet", "")
	if wallet.Code != http.StatusOK || !strings.Contains(wallet.Body.String(), resourceAddress) ||
		!strings.Contains(wallet.Body.String(), `"available":900`) || !strings.Contains(wallet.Body.String(), `"ENERGY"`) {
		t.Fatalf("resource wallet = %d %s", wallet.Code, wallet.Body.String())
	}
}

func TestC7EffectiveConfigIsStrictlyRedacted(t *testing.T) {
	server, _, cleanup := testServer(t, 1)
	defer cleanup()
	cfg := testConfig(1)
	cfg.Tron.Endpoints = []config.Endpoint{{URL: "https://secret-endpoint.invalid", APIKey: "tron-endpoint-secret", Weight: 1}}
	cfg.Tron.SolidityURL = "https://secret-solidity.invalid"
	cfg.Energy.Enabled = true
	server.UpdateConfig(cfg)
	response := request(t, server.Handler(), http.MethodGet, "/api/v1/config", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"confirmations_required":19`) ||
		!strings.Contains(response.Body.String(), `"default_ttl_seconds":1800`) || !strings.Contains(response.Body.String(), `"enabled":true`) {
		t.Fatalf("effective config = %d %s", response.Code, response.Body.String())
	}
	for _, secret := range []string{"consumer-secret", "analytics-secret", "energy-key", "energy-secret", testTOTP,
		"tron-endpoint-secret", "secret-endpoint.invalid", "secret-solidity.invalid", testKeyHash(testAPIKey)} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("effective config exposed %q: %s", secret, response.Body.String())
		}
	}
}

func TestC8VolumeAndC9FeeReportsUseExactStoredValues(t *testing.T) {
	server, database, path, cleanup := testServerWithPath(t, 3)
	defer cleanup()
	firstResponse := request(t, server.Handler(), http.MethodPost, "/api/v1/orders", `{"asset":"USDT","amount":"1","external_ref":"report-one"}`)
	secondResponse := request(t, server.Handler(), http.MethodPost, "/api/v1/orders", `{"asset":"USDT","amount":"2","external_ref":"report-two"}`)
	var first, second map[string]any
	decodeResponse(t, firstResponse, &first)
	decodeResponse(t, secondResponse, &second)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(`UPDATE orders SET status='confirmed',received_raw='1000000',price_usd='1',created_at=? WHERE id=?`, day.Add(time.Hour).Unix(), first["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE orders SET status='paid',received_raw='2000000',price_usd=NULL,created_at=? WHERE id=?`, day.Add(2*time.Hour).Unix(), second["id"]); err != nil {
		t.Fatal(err)
	}
	volume := request(t, server.Handler(), http.MethodGet, fmt.Sprintf("/api/v1/reports/volume?from=%d&to=%d&group_by=day", day.Unix(), day.Add(24*time.Hour-time.Second).Unix()), "")
	if volume.Code != http.StatusOK || !strings.Contains(volume.Body.String(), `"order_count":2`) ||
		!strings.Contains(volume.Body.String(), `"paid_count":2`) || !strings.Contains(volume.Body.String(), `"USDT":"3"`) ||
		!strings.Contains(volume.Body.String(), `"usd_total":"1"`) || !strings.Contains(volume.Body.String(), `"unpriced_paid_count":1`) {
		t.Fatalf("volume report = %d %s", volume.Code, volume.Body.String())
	}
	addresses := fundTestWallets(t, database, 1)
	now := time.Now().UTC()
	withdrawal, _, err := database.CreateWithdrawal(context.Background(), store.CreateWithdrawal{IdempotencyKey: "fee-report",
		FromAddress: addresses[0].Address, ToAddress: addresses[1].Address, Asset: "USDT", AmountRaw: "1", AmountUSD: "1",
		DailyLimitUSD: "1000", RequestedBy: "test", IP: "127.0.0.1", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE withdrawals SET energy_source='burned',energy_cost_trx='0.5',bandwidth_source='delegated' WHERE id=?`, withdrawal.ID); err != nil {
		t.Fatal(err)
	}
	grant, err := database.CreateResourceGrant(context.Background(), withdrawal.ID, addresses[0].ID, addresses[0].Address,
		"BANDWIDTH", "self_delegated", "1000000", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE resource_grants SET fee_raw='100000',status='confirmed' WHERE id=?`, grant.ID); err != nil {
		t.Fatal(err)
	}
	purchase, err := database.CreateEnergyPurchase(context.Background(), "tronzap", withdrawal.ID, addresses[0].ID,
		addresses[0].Address, "ENERGY", 100, time.Hour, "3.5", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MarkEnergyPurchaseAttempt(context.Background(), purchase.ID, "provider-order"); err != nil {
		t.Fatal(err)
	}
	fees := request(t, server.Handler(), http.MethodGet, fmt.Sprintf("/api/v1/reports/fees?from=%d&to=%d", now.Add(-time.Minute).Unix(), now.Add(time.Minute).Unix()), "")
	if fees.Code != http.StatusOK || !strings.Contains(fees.Body.String(), `"burned":"0.5"`) ||
		!strings.Contains(fees.Body.String(), `"delegated":"0.1"`) || !strings.Contains(fees.Body.String(), `"rental_spend_trx":"3.5"`) {
		t.Fatalf("fee report = %d %s", fees.Code, fees.Body.String())
	}
}

func TestC10CSVExportsAreAttachmentsWithFiltersAndCaps(t *testing.T) {
	server, database, cleanup := testServer(t, 3)
	defer cleanup()
	created := request(t, server.Handler(), http.MethodPost, "/api/v1/orders", `{"asset":"USDT","amount":"1","external_ref":"csv-one"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create CSV order = %d %s", created.Code, created.Body.String())
	}
	addresses := fundTestWallets(t, database, 1)
	withdrawal, _, err := database.CreateWithdrawal(context.Background(), store.CreateWithdrawal{IdempotencyKey: "csv-withdrawal",
		FromAddress: addresses[0].Address, ToAddress: addresses[1].Address, Asset: "USDT", AmountRaw: "1", AmountUSD: "1",
		DailyLimitUSD: "1000", RequestedBy: "test", IP: "127.0.0.1", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	orders := request(t, server.Handler(), http.MethodGet, "/api/v1/export/orders.csv?external_ref=csv-one&limit=1", "")
	if orders.Code != http.StatusOK || orders.Header().Get("Content-Type") != "text/csv" ||
		!strings.Contains(orders.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(orders.Body.String(), "csv-one") {
		t.Fatalf("orders CSV = %d headers=%v body=%s", orders.Code, orders.Header(), orders.Body.String())
	}
	withdrawals := request(t, server.Handler(), http.MethodGet, "/api/v1/export/withdrawals.csv?status=requested", "")
	if withdrawals.Code != http.StatusOK || withdrawals.Header().Get("Content-Type") != "text/csv" ||
		!strings.Contains(withdrawals.Body.String(), withdrawal.ID) {
		t.Fatalf("withdrawals CSV = %d headers=%v body=%s", withdrawals.Code, withdrawals.Header(), withdrawals.Body.String())
	}
	for _, target := range []string{"/api/v1/export/orders.csv?limit=0", "/api/v1/export/withdrawals.csv?limit=100001"} {
		if invalid := request(t, server.Handler(), http.MethodGet, target, ""); invalid.Code != http.StatusBadRequest {
			t.Fatalf("invalid export cap %s = %d %s", target, invalid.Code, invalid.Body.String())
		}
	}
}

func TestTierCRouteAuthenticationAndScopes(t *testing.T) {
	server, _, cleanup := testServer(t, 1)
	defer cleanup()
	routes := []string{"/api/v1/chain/status", "/api/v1/chain/quota", "/api/v1/workers", "/api/v1/audit",
		"/api/v1/resources/grants", "/api/v1/resources/wallet", "/api/v1/config", "/api/v1/reports/volume",
		"/api/v1/reports/fees", "/api/v1/export/orders.csv", "/api/v1/export/withdrawals.csv"}
	for _, target := range routes {
		t.Run(target, func(t *testing.T) {
			missing := httptest.NewRecorder()
			server.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodGet, target, nil))
			if missing.Code != http.StatusUnauthorized {
				t.Fatalf("missing key = %d %s", missing.Code, missing.Body.String())
			}
			unscoped := requestWithKey(t, server.Handler(), http.MethodGet, target, "", testNoScopeAPIKey)
			if unscoped.Code != http.StatusUnauthorized {
				t.Fatalf("missing scope = %d %s", unscoped.Code, unscoped.Body.String())
			}
		})
	}
}

func snapshotRowCounts(t *testing.T, database *sql.DB) map[string]int64 {
	t.Helper()
	rows, err := database.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		var count int64
		if err := database.QueryRow(`SELECT COUNT(*) FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

func fundTestWallets(t *testing.T, database *store.Store, count int) []store.WalletAddress {
	t.Helper()
	ctx := context.Background()
	addresses, err := database.WalletAddresses(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBlock(ctx, store.BlockRecord{Height: 1, ID: "fund-block", ParentID: "genesis", Timestamp: 1, ProcessedAt: 1}, 64, func(write *store.BlockWrite) error {
		for i := 0; i < count; i++ {
			addressID := addresses[i].ID
			if _, err := write.UpsertPayment(store.PaymentRecord{TxID: fmt.Sprintf("fund-%d", i), LogIndex: 0, Direction: "in", BlockHeight: 1,
				BlockID: "fund-block", BlockTimestamp: 1, FromAddress: "payer", ToAddress: addresses[i].Address,
				AddressID: &addressID, Asset: "USDT", AmountRaw: "1000000", Status: "confirmed", DetectedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return addresses
}

func createDeadIPN(t *testing.T, database *store.Store, cfg config.Config, marker string) string {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	events := store.NewEventConfig(cfg.IPN, cfg.Assets)
	if err := database.EnqueueGlobalEvent(ctx, events, "test:"+marker, "payment.unattributed", map[string]any{"marker": marker}, now); err != nil {
		t.Fatal(err)
	}
	event, found, err := database.ClaimIPN(ctx, now.Add(time.Second), []string{"shop"})
	if err != nil || !found {
		t.Fatalf("claim IPN: found=%v err=%v", found, err)
	}
	status := 500
	if err := database.FailIPN(ctx, event.ID, "sink failed", &status, true, now); err != nil {
		t.Fatal(err)
	}
	return event.ID
}

func testServer(t *testing.T, poolSize int) (*Server, *store.Store, func()) {
	server, database, _, cleanup := testServerWithPath(t, poolSize)
	return server, database, cleanup
}

func testServerWithPath(t *testing.T, poolSize int) (*Server, *store.Store, string, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payd.db")
	database, err := store.Open(context.Background(), path)
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
	return server, database, path, func() { hd.Destroy(); _ = database.Close() }
}

func testConfig(poolSize int) config.Config {
	return config.Config{
		Wallet: config.Wallet{Account: 0, PoolInitialSize: poolSize, PoolMinFree: 1, PoolMaxSize: poolSize, Cooldown: time.Hour},
		Tron:   config.Tron{ConfirmationsRequired: 19, ReorgDepth: 64, DailyRequestQuota: 100000},
		Assets: []config.Asset{{Symbol: "USDT", Kind: "trc20", Decimals: 6, Verified: true}},
		Orders: config.Orders{DefaultTTL: 30 * time.Minute},
		IPN: config.IPN{DefaultConsumer: "shop", Timeout: time.Second, Consumers: []config.Consumer{
			{Name: "shop", URL: "https://shop.invalid/ipn", Secret: "consumer-secret", ReceivesGlobal: true, Enabled: true},
			{Name: "analytics", URL: "https://analytics.invalid/ipn", Secret: "analytics-secret", Enabled: true},
		}},
		Energy:     config.Energy{Provider: "test-energy", APIKey: config.Secret("energy-key"), APISecret: config.Secret("energy-secret"), BalanceWarnTRX: "1"},
		Price:      config.Price{Pairs: []string{"TRXUSDT"}, StaleAfter: 5 * time.Minute},
		Resources:  config.Resources{MinEnergy: 1, MinBandwidth: 1, ResourceWalletIndex: 1000, BandwidthStrategy: "delegate"},
		Withdrawal: config.Withdrawal{Enabled: true, DailyLimitUSD: "1000", FeeLimitTRX: 100, Expiration: time.Minute, RequireTOTP: true},
		Auth: config.Auth{TOTPSecret: testTOTP, APIKeys: []config.APIKey{
			{Name: "test", KeyHash: testKeyHash(testAPIKey), Scopes: []string{"orders:read", "orders:write", "wallets:read", "wallets:write", "withdrawals:read", "withdrawals:write", "resources:write", "admin:read"}},
			{Name: "unscoped", KeyHash: testKeyHash(testNoScopeAPIKey)},
		}},
	}
}

func testKeyHash(key string) string {
	salt := []byte("0123456789abcdef")
	hash := argon2.IDKey([]byte(key), salt, 1, 64, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=64,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
}

func request(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	return requestWithKey(t, handler, method, target, body, testAPIKey)
}

func requestWithKey(t *testing.T, handler http.Handler, method, target, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-API-Key", key)
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
