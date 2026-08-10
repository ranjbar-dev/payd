package energy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"payd/internal/config"
)

func TestTronZapProviderImplementsResourceTypeAndSignedCalls(t *testing.T) {
	const token, secret = "token", "secret"
	paths := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		sum := sha256.Sum256(append(append([]byte(nil), body...), secret...))
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-Signature") != hex.EncodeToString(sum[:]) {
			t.Errorf("invalid provider authentication headers")
		}
		if strings.Contains(string(body), "mnemonic") || strings.Contains(string(body), "private") {
			t.Errorf("signing authority leaked to provider: %s", body)
		}
		paths[r.URL.Path]++
		switch r.URL.Path {
		case "/v1/calculate":
			var request struct {
				Type     string `json:"type"`
				Amount   int64  `json:"amount"`
				Duration int64  `json:"duration"`
			}
			_ = json.Unmarshal(body, &request)
			if request.Type != "energy" || request.Amount != 131000 || request.Duration != 1 {
				t.Errorf("quote request = %#v", request)
			}
			_, _ = io.WriteString(w, `{"code":0,"result":{"total":3.8}}`)
		case "/v1/transaction/new":
			if !strings.Contains(string(body), `"external_id":"purchase-1"`) || !strings.Contains(string(body), `"service":"energy"`) {
				t.Errorf("purchase request = %s", body)
			}
			_, _ = io.WriteString(w, `{"code":0,"result":{"id":"order-1","status":"pending","amount":3.7}}`)
		case "/v1/transaction/check":
			_, _ = io.WriteString(w, `{"code":0,"result":{"id":"order-1","status":"success","amount":"3.7","hash":"txid"}}`)
		case "/v1/balance":
			_, _ = io.WriteString(w, `{"code":0,"result":{"balance":42.5}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := New(config.Energy{Enabled: true, Provider: "tronzap", APIURL: server.URL,
		APIKey: token, APISecret: secret, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	quote, err := provider.Quote(ctx, "receiver", "ENERGY", 131000, time.Hour)
	if err != nil || quote.PriceTRX != "3.8" {
		t.Fatalf("quote=%#v err=%v", quote, err)
	}
	quote.Reference = "purchase-1"
	order, err := provider.Purchase(ctx, quote)
	if err != nil || order.ID != "order-1" || order.ActualTRX != "3.7" {
		t.Fatalf("order=%#v err=%v", order, err)
	}
	status, err := provider.Status(ctx, order.ID)
	if err != nil || status.State != "success" || status.DelegationTxID != "txid" {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	balance, err := provider.Balance(ctx)
	if err != nil || balance != "42.5" {
		t.Fatalf("balance=%q err=%v", balance, err)
	}
	for _, path := range []string{"/v1/calculate", "/v1/transaction/new", "/v1/transaction/check", "/v1/balance"} {
		if paths[path] != 1 {
			t.Fatalf("%s calls = %d", path, paths[path])
		}
	}
}

func TestTronZapProviderRequestHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)

	provider, err := New(config.Energy{Enabled: true, Provider: "tronzap", APIURL: server.URL, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := provider.Quote(ctx, "receiver", "ENERGY", 131000, time.Hour)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("quote error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("provider request did not stop after context cancellation")
	}
}

func TestBurnCostSunUsesLiveFee(t *testing.T) {
	if got := BurnCostSun(131000, 210).String(); got != "27510000" {
		t.Fatalf("210 sun fee burn = %s", got)
	}
	if got := BurnCostSun(131000, 420).String(); got != "55020000" {
		t.Fatalf("420 sun fee burn = %s", got)
	}
}
