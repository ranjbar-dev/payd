package api

import (
	"context"
	"net/http"
	"testing"

	"payd/internal/store"
)

// API-025: embedded histories use the same bounded, cursor-based contract as payment lists.
func TestDetailPaymentHistoryIsBoundedAndContinues(t *testing.T) {
	server, database, cleanup := testServer(t, 1)
	defer cleanup()

	created := request(t, server.Handler(), http.MethodPost, "/api/v1/orders", `{"asset":"USDT","amount":"1000"}`)
	var createdOrder struct {
		ID      string `json:"id"`
		Address string `json:"address"`
	}
	decodeResponse(t, created, &createdOrder)
	order, err := database.Order(context.Background(), createdOrder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBlock(context.Background(), store.BlockRecord{
		Height: 1, ID: "dust-block", ParentID: "genesis", Timestamp: order.CreatedAt,
	}, 64, func(write *store.BlockWrite) error {
		for index := range 201 {
			if _, err := write.UpsertPayment(store.PaymentRecord{
				TxID: "dust-batch", LogIndex: index, Direction: "in", BlockHeight: 1,
				BlockID: "dust-block", BlockTimestamp: order.CreatedAt, FromAddress: "payer",
				ToAddress: order.Address, AddressID: &order.AddressID, OrderID: &order.ID,
				Asset: "USDT", AmountRaw: "1", IsDust: true, Status: "seen", DetectedAt: order.CreatedAt,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"order":  "/api/v1/orders/" + order.ID,
		"wallet": "/api/v1/wallets/" + order.Address,
	} {
		t.Run(name, func(t *testing.T) {
			seen := make(map[int64]struct{}, 201)
			cursor := ""
			for pageNumber, want := range []int{200, 1} {
				target := path + "?limit=200"
				if cursor != "" {
					target += "&cursor=" + cursor
				}
				response := request(t, server.Handler(), http.MethodGet, target, "")
				if response.Code != http.StatusOK {
					t.Fatalf("page %d = %d %s", pageNumber+1, response.Code, response.Body.String())
				}
				var page struct {
					Payments []struct {
						ID int64 `json:"id"`
					} `json:"payments"`
					NextCursor string `json:"next_cursor"`
				}
				decodeResponse(t, response, &page)
				if len(page.Payments) != want {
					t.Fatalf("page %d payments = %d, want %d", pageNumber+1, len(page.Payments), want)
				}
				for _, payment := range page.Payments {
					if _, duplicate := seen[payment.ID]; duplicate {
						t.Fatalf("payment %d repeated across pages", payment.ID)
					}
					seen[payment.ID] = struct{}{}
				}
				cursor = page.NextCursor
				if (pageNumber == 0) == (cursor == "") {
					t.Fatalf("page %d next_cursor = %q", pageNumber+1, cursor)
				}
			}
			if len(seen) != 201 {
				t.Fatalf("continued history contains %d payments, want 201", len(seen))
			}
		})
	}
}
