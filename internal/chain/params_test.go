package chain

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"payd/internal/store"
)

func TestParameterRefreshKeepsAtomicLiveValues(t *testing.T) {
	ctx := context.Background()
	client := testClient(t)
	body := `{"chainParameter":[{"key":"getEnergyFee","value":100},{"key":"getTransactionFee","value":1000}]}`
	client.Read.core.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	worker, err := NewParameterWorker(client.Read, database, slog.New(slog.NewTextHandler(io.Discard, nil)), "20")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	params, err := database.LoadChainParameters(ctx)
	if err != nil || params.EnergyFee != 100 || params.TransactionFee != 1000 {
		t.Fatalf("stored parameters = %+v, err = %v", params, err)
	}
	if !worker.BurnCeilingHealthy() {
		t.Fatal("20 TRX ceiling should cover 13.1 TRX worst case")
	}

	body = `{"chainParameter":[{"key":"getEnergyFee","value":420}]}`
	if err := worker.Refresh(ctx); err == nil {
		t.Fatal("partial parameter response accepted")
	}
	params, err = database.LoadChainParameters(ctx)
	if err != nil || params.EnergyFee != 100 || params.TransactionFee != 1000 {
		t.Fatalf("failed fetch overwrote cached parameters: %+v, err = %v", params, err)
	}

	body = `{"chainParameter":[{"key":"getEnergyFee","value":420},{"key":"getTransactionFee","value":1000}]}`
	if err := worker.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if worker.BurnCeilingHealthy() {
		t.Fatal("20 TRX ceiling should not cover 55.02 TRX worst case")
	}
}
