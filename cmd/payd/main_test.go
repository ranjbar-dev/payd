package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"payd/internal/config"
)

func TestLogAssetsWritesOneDeterministicRecord(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logAssets(logger, []config.Asset{
		{Symbol: "USDT", Contract: "usdt-contract", Decimals: 6},
		{Symbol: "TRX", Decimals: 6},
	})

	decoder := json.NewDecoder(&output)
	var record map[string]json.RawMessage
	if err := decoder.Decode(&record); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("second log record: %v", err)
	}
	var level string
	if err := json.Unmarshal(record["level"], &level); err != nil || level != "INFO" {
		t.Fatalf("level = %q, err = %v", level, err)
	}
	var assets []map[string]any
	if err := json.Unmarshal(record["assets"], &assets); err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 || assets[0]["symbol"] != "TRX" || assets[1]["symbol"] != "USDT" {
		t.Fatalf("assets = %#v", assets)
	}
	for _, asset := range assets {
		if len(asset) != 3 {
			t.Fatalf("asset fields = %#v", asset)
		}
	}
}

func TestEnergyProviderReachabilityFallsBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Energy{Enabled: true, Provider: "test", APIURL: server.URL, Timeout: time.Second}
	if !energyProviderReachable(logger, cfg) {
		t.Fatal("reachable provider reported unavailable")
	}
	server.Close()
	if energyProviderReachable(logger, cfg) {
		t.Fatal("unreachable provider did not select burn-only fallback")
	}
}
