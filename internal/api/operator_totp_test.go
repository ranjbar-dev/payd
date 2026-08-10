package api

import (
	"context"
	"encoding/base32"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"payd/internal/energy"
	"payd/internal/store"
)

type countingDelegator struct{ calls int }

func (f *countingDelegator) DelegateResources(_ context.Context, address, resourceType string, amount int64, _, _ string) (store.ResourceGrant, error) {
	f.calls++
	return store.ResourceGrant{ID: "grant", ReceiverAddress: address, ResourceType: resourceType, AmountSun: "1", Status: "broadcast"}, nil
}

func (*countingDelegator) ProviderBalanceMetric() (string, bool) { return "", false }
func (*countingDelegator) EstimateResources(context.Context, string, string) (energy.ResourceEstimate, error) {
	return energy.ResourceEstimate{}, nil
}

func TestOperatorDelegationRequiresSingleUseTOTPBeforeBroadcast(t *testing.T) {
	server, _, cleanup := testServer(t, 1)
	defer cleanup()
	delegator := &countingDelegator{}
	server.SetDelegator(delegator)
	target := "/api/v1/wallets/TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH/delegate"
	body := `{"resource_type":"ENERGY","amount":1}`

	for name, code := range map[string]string{"missing": "", "invalid": "bad"} {
		t.Run(name, func(t *testing.T) {
			response := operatorRequest(server.Handler(), target, body, code)
			if response.Code != http.StatusUnauthorized || delegator.calls != 0 {
				t.Fatalf("response = %d %s, delegation calls = %d", response.Code, response.Body.String(), delegator.calls)
			}
		})
	}
	if response := operatorRequest(server.Handler(), target, `{"resource_type":"ENERGY","amount":1,"totp":"123456"}`, ""); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"totp_in_body"`) || delegator.calls != 0 {
		t.Fatalf("body TOTP = %d %s, delegation calls = %d", response.Code, response.Body.String(), delegator.calls)
	}

	code := operatorTOTP(t, 0)
	if response := operatorRequest(server.Handler(), target, body, code); response.Code != http.StatusAccepted || delegator.calls != 1 {
		t.Fatalf("valid TOTP = %d %s, delegation calls = %d", response.Code, response.Body.String(), delegator.calls)
	}
	if response := operatorRequest(server.Handler(), target, body, code); response.Code != http.StatusUnauthorized || delegator.calls != 1 {
		t.Fatalf("reused TOTP = %d %s, delegation calls = %d", response.Code, response.Body.String(), delegator.calls)
	}
}

func TestClearDriftAcknowledgesOneCurrentAssetBalance(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	ctx := context.Background()
	addressID := int64(1)
	address := "TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH"
	if err := database.CommitBlock(ctx, store.BlockRecord{Height: 1, ID: "B1", ParentID: "B0", Timestamp: 1}, 64, func(write *store.BlockWrite) error {
		for index, payment := range []struct{ asset, amount string }{{"USDT", "10"}, {"TRX", "20"}} {
			_, err := write.UpsertPayment(store.PaymentRecord{TxID: "drift-" + payment.asset, LogIndex: index, Direction: "in",
				BlockHeight: 1, BlockID: "B1", BlockTimestamp: 1, FromAddress: "payer", ToAddress: address,
				AddressID: &addressID, Asset: payment.asset, AmountRaw: payment.amount, Status: "confirmed", DetectedAt: 1})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.ReconcileChainBalances(ctx, []store.ChainBalance{
		{AddressID: addressID, Address: address, Asset: "USDT", Raw: "2"},
		{AddressID: addressID, Address: address, Asset: "TRX", Raw: "3"},
	}, store.NewEventConfig(testConfig(1).IPN, testConfig(1).Assets), time.Now()); err != nil {
		t.Fatal(err)
	}
	target := "/api/v1/wallets/" + address + "/clear-drift"
	mismatchCode := operatorTOTP(t, -1)
	mismatch := operatorRequest(server.Handler(), target, `{"asset":"USDT","chain_raw":"3"}`, mismatchCode)
	if mismatch.Code != http.StatusConflict || !strings.Contains(mismatch.Body.String(), `"code":"drift_ack_mismatch"`) {
		t.Fatalf("mismatch = %d %s", mismatch.Code, mismatch.Body.String())
	}
	if _, err := database.BalanceForWithdrawal(ctx, address, "USDT"); !errors.Is(err, store.ErrBalanceDrift) {
		t.Fatalf("mismatch cleared USDT drift: %v", err)
	}
	if replay := operatorRequest(server.Handler(), target, `{"asset":"USDT","chain_raw":"2"}`, mismatchCode); replay.Code != http.StatusUnauthorized {
		t.Fatalf("reused mismatch TOTP = %d %s", replay.Code, replay.Body.String())
	}

	match := operatorRequest(server.Handler(), target, `{"asset":"USDT","chain_raw":"2"}`, operatorTOTP(t, 0))
	if match.Code != http.StatusOK {
		t.Fatalf("matching acknowledgement = %d %s", match.Code, match.Body.String())
	}
	if _, err := database.BalanceForWithdrawal(ctx, address, "USDT"); err != nil {
		t.Fatalf("USDT drift remains: %v", err)
	}
	if _, err := database.BalanceForWithdrawal(ctx, address, "TRX"); !errors.Is(err, store.ErrBalanceDrift) {
		t.Fatalf("USDT acknowledgement cleared TRX drift: %v", err)
	}
}

func operatorRequest(handler http.Handler, target, body, code string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	request.Header.Set("X-API-Key", testAPIKey)
	request.Header.Set("Content-Type", "application/json")
	if code != "" {
		request.Header.Set("X-TOTP", code)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func operatorTOTP(t *testing.T, offset int64) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(testTOTP)
	if err != nil {
		t.Fatal(err)
	}
	return totpCode(secret, time.Now().UTC().Unix()/30+offset)
}
