package api

import (
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
	"time"

	"payd/internal/store"
)

// API-046: CSV exports must not turn caller-controlled fields into spreadsheet formulas.
func TestWriteCSVEscapesSpreadsheetFormulaLeadIns(t *testing.T) {
	values := []string{"=formula", "+formula", "-formula", "@formula", "\tformula", "\rformula", "", "safe", " formula"}
	var encoded strings.Builder
	output := csv.NewWriter(&encoded)
	if err := writeCSV(output, append([]string(nil), values...)); err != nil {
		t.Fatal(err)
	}
	output.Flush()
	if err := output.Error(); err != nil {
		t.Fatal(err)
	}
	record, err := csv.NewReader(strings.NewReader(encoded.String())).Read()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"'=formula", "'+formula", "'-formula", "'@formula", "'\tformula", "'\rformula", "", "safe", " formula"}
	for i := range want {
		if record[i] != want[i] {
			t.Errorf("field %q = %q, want %q", values[i], record[i], want[i])
		}
	}
}

func TestCSVExportsEscapeCallerControlledFields(t *testing.T) {
	server, database, cleanup := testServer(t, 3)
	defer cleanup()

	created := request(t, server.Handler(), http.MethodPost, "/api/v1/orders",
		`{"asset":"USDT","amount":"1","external_ref":"=formula","metadata":{"note":"=safe inside JSON"}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create order = %d %s", created.Code, created.Body.String())
	}
	orders := request(t, server.Handler(), http.MethodGet, "/api/v1/export/orders.csv?external_ref=%3Dformula", "")
	rows, err := csv.NewReader(strings.NewReader(orders.Body.String())).ReadAll()
	if err != nil || orders.Code != http.StatusOK || len(rows) != 2 {
		t.Fatalf("orders export = %d rows=%v err=%v", orders.Code, rows, err)
	}
	if rows[1][1] != "'=formula" || rows[1][12] != `{"note":"=safe inside JSON"}` {
		t.Fatalf("orders row external_ref=%q metadata=%q", rows[1][1], rows[1][12])
	}

	addresses := fundTestWallets(t, database, 1)
	withdrawal, _, err := database.CreateWithdrawal(t.Context(), store.CreateWithdrawal{
		IdempotencyKey: "+formula", FromAddress: addresses[0].Address, ToAddress: addresses[1].Address,
		Asset: "USDT", AmountRaw: "1", AmountUSD: "1", DailyLimitUSD: "1000",
		RequestedBy: "test", IP: "@formula", Now: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	withdrawals := request(t, server.Handler(), http.MethodGet, "/api/v1/export/withdrawals.csv", "")
	rows, err = csv.NewReader(strings.NewReader(withdrawals.Body.String())).ReadAll()
	if err != nil || withdrawals.Code != http.StatusOK || len(rows) != 2 {
		t.Fatalf("withdrawals export = %d rows=%v err=%v", withdrawals.Code, rows, err)
	}
	if rows[1][0] != withdrawal.ID || rows[1][1] != "'+formula" || rows[1][16] != "'@formula" {
		t.Fatalf("withdrawal row id=%q key=%q ip=%q", rows[1][0], rows[1][1], rows[1][16])
	}
}
