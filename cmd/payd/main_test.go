package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"payd/internal/config"
)

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
