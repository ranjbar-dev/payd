package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"payd/internal/store"
)

// API-001: a USD amount is converted at the current price and rounded up to the
// next whole base unit. Before the fix, a TRX order with amount_usd was rejected
// 400 invalid_amount for practically every value because the exact conversion
// almost never landed on a whole base unit.
func TestOrderAmountUSDRoundsUpToWholeBaseUnit(t *testing.T) {
	server, database, cleanup := testServer(t, 3)
	defer cleanup()

	if err := database.UpsertPrices(context.Background(), []store.Price{
		{Symbol: "TRX", PriceUSD: "0.283100", Source: "test", FetchedAt: time.Now().Unix()},
	}); err != nil {
		t.Fatal(err)
	}

	// 5.00 USD / 0.2831 = 17661603.67… base units -> ceil -> 17661604 -> "17.661604".
	response := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"TRX","amount_usd":"5.00","external_ref":"trx-usd"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("USD TRX order = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"amount":"17.661604"`) {
		t.Fatalf("rounded amount missing: %s", response.Body.String())
	}

	// A non-positive value still fails 400 invalid_amount.
	bad := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"TRX","amount_usd":"0","external_ref":"trx-usd-bad"}`)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "invalid_amount") {
		t.Fatalf("zero USD amount = %d %s", bad.Code, bad.Body.String())
	}
}
