package price

import (
	"errors"
	"testing"
	"time"

	"payd/internal/config"
	"payd/internal/store"
)

func TestCurrentFromPricesAppliesStablecoinAndStalenessRules(t *testing.T) {
	now := time.Unix(1_750_000_000, 0)
	cfg := config.Price{Pairs: []string{"TRXUSDT"}, StaleAfter: time.Minute}
	prices := []store.Price{{Symbol: "TRX", PriceUSD: "0.25", FetchedAt: now.Unix()}}

	quote, err := CurrentFromPrices(prices, cfg, "TRX", now)
	if err != nil || quote.USD != "0.25" {
		t.Fatalf("snapshot quote = %+v, %v", quote, err)
	}
	stable, err := CurrentFromPrices(prices, cfg, "USDT", now)
	if err != nil || stable.USD != "1.00" {
		t.Fatalf("implicit stablecoin quote = %+v, %v", stable, err)
	}
	if _, err := CurrentFromPrices(prices, cfg, "TRX", now.Add(time.Minute+time.Second)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale snapshot error = %v", err)
	}
}
