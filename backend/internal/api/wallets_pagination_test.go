package api

import (
	"net/http"
	"reflect"
	"testing"

	"payd/internal/store"
)

// API-025: wallet limits apply to addresses, not joined balance rows, and cursors neither skip nor repeat wallets.
func TestWalletListsUseStoreKeysetPagination(t *testing.T) {
	server, database, cleanup := testServer(t, 4)
	defer cleanup()
	addresses := fundTestWallets(t, database, 3)

	for _, test := range []struct {
		path string
		want []store.WalletAddress
	}{
		{path: "/api/v1/wallets", want: addresses},
		{path: "/api/v1/wallets/with-balance", want: addresses[:3]},
	} {
		t.Run(test.path, func(t *testing.T) {
			var got []uint32
			cursor := ""
			for page := 0; ; page++ {
				target := test.path + "?limit=2"
				if cursor != "" {
					target += "&cursor=" + cursor
				}
				response := request(t, server.Handler(), http.MethodGet, target, "")
				var body struct {
					Wallets []struct {
						HDIndex uint32 `json:"hd_index"`
					} `json:"wallets"`
					NextCursor string `json:"next_cursor"`
				}
				decodeResponse(t, response, &body)
				if len(body.Wallets) > 2 {
					t.Fatalf("page %d returned %d wallets", page, len(body.Wallets))
				}
				for _, wallet := range body.Wallets {
					got = append(got, wallet.HDIndex)
				}
				if body.NextCursor == "" {
					break
				}
				if body.NextCursor == cursor || page > len(test.want) {
					t.Fatalf("cursor did not advance: %q", body.NextCursor)
				}
				cursor = body.NextCursor
			}

			want := make([]uint32, len(test.want))
			for i, address := range test.want {
				want[i] = address.HDIndex
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("wallet order = %v, want %v", got, want)
			}
		})
	}
}
