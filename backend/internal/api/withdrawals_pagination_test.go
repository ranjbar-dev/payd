package api

import (
	"context"
	"net/http"
	"sort"
	"testing"
	"time"

	"payd/internal/store"
)

func TestWithdrawalPaginationUsesCompoundCursor(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()
	address := fundTestWallets(t, database, 1)[0]

	created := make([]store.Withdrawal, 0, 4)
	for i, now := range []time.Time{
		time.Unix(100, 0),
		time.Unix(98, 0),
		time.Unix(100, 500_000_000),
		time.Unix(99, 0),
	} {
		withdrawal, _, err := database.CreateWithdrawal(context.Background(), store.CreateWithdrawal{
			IdempotencyKey: "page-" + string(rune('a'+i)), FromAddress: address.Address,
			ToAddress: "destination", Asset: "USDT", AmountRaw: "1", AmountUSD: "1",
			DailyLimitUSD: "100", RequestedBy: "test", IP: "127.0.0.1", Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, withdrawal)
	}
	sort.Slice(created, func(i, j int) bool {
		if created[i].CreatedAt == created[j].CreatedAt {
			return created[i].ID > created[j].ID
		}
		return created[i].CreatedAt > created[j].CreatedAt
	})

	var got []string
	cursor := ""
	for {
		target := "/api/v1/withdrawals?limit=1"
		if cursor != "" {
			target += "&cursor=" + cursor
		}
		response := request(t, server.Handler(), http.MethodGet, target, "")
		if response.Code != http.StatusOK {
			t.Fatalf("page = %d %s", response.Code, response.Body.String())
		}
		var page struct {
			Items      []map[string]any `json:"items"`
			NextCursor string           `json:"next_cursor"`
		}
		decodeResponse(t, response, &page)
		if len(page.Items) != 1 {
			t.Fatalf("page items = %#v", page.Items)
		}
		got = append(got, page.Items[0]["id"].(string))
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(got) != len(created) {
		t.Fatalf("got %d withdrawals, want %d: %v", len(got), len(created), got)
	}
	for i := range created {
		if got[i] != created[i].ID {
			t.Fatalf("withdrawal %d = %s, want %s; all=%v", i, got[i], created[i].ID, got)
		}
	}

	malformed := request(t, server.Handler(), http.MethodGet,
		"/api/v1/withdrawals?cursor="+encodeCursor("missing-compound-key"), "")
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed cursor = %d %s", malformed.Code, malformed.Body.String())
	}
}
