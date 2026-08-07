package chain

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"payd/internal/config"
)

func TestNileReadIntegration(t *testing.T) {
	if os.Getenv("PAYD_NILE_INTEGRATION") != "1" {
		t.Skip("set PAYD_NILE_INTEGRATION=1 to call Nile")
	}
	client, err := New(config.Tron{
		Endpoints:         []config.Endpoint{{URL: "https://nile.trongrid.io", Weight: 1}},
		SolidityURL:       "https://nile.trongrid.io",
		RequestTimeout:    10 * time.Second,
		DailyRequestQuota: 100_000,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	block, err := client.Read.GetNowBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		BlockID string `json:"blockID"`
	}
	if err := json.Unmarshal(block, &header); err != nil || header.BlockID == "" {
		t.Fatalf("invalid Nile block response: blockID=%q, err=%v", header.BlockID, err)
	}
	if _, err := client.Solidity.GetNowBlock(ctx); err != nil {
		t.Fatal(err)
	}
	parameters, err := client.Read.GetChainParameters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseChainParameters(parameters); err != nil {
		t.Fatal(err)
	}
}
