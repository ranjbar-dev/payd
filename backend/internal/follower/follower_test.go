package follower

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"payd/internal/store"
)

type fakeChain struct {
	tip      json.RawMessage
	byHeight map[int64]json.RawMessage
	fetched  []int64
}

func (f *fakeChain) GetNowBlock(context.Context) (json.RawMessage, error) {
	return f.tip, nil
}

func (f *fakeChain) GetBlockByNum(_ context.Context, height int64) (json.RawMessage, error) {
	f.fetched = append(f.fetched, height)
	block, ok := f.byHeight[height]
	if !ok {
		return nil, errors.New("missing fake block")
	}
	return block, nil
}

func rawBlock(t *testing.T, height int64, id, parent string, transactions int) json.RawMessage {
	t.Helper()
	txs := make([]map[string]any, transactions)
	for i := range txs {
		txs[i] = map[string]any{"txID": i}
	}
	body, err := json.Marshal(map[string]any{
		"blockID": id,
		"block_header": map[string]any{"raw_data": map[string]any{
			"number": height, "timestamp": 1_700_000_000_000 + height*3000, "parentHash": parent,
		}},
		"transactions": txs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func newTestWorker(t *testing.T, chain *fakeChain, depth int) (*Worker, *store.Store) {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	worker, err := New(chain, database, nil, 3*time.Second, depth, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return worker, database
}

func TestGapCatchUpSameHeightAndRegression(t *testing.T) {
	chain := &fakeChain{byHeight: make(map[int64]json.RawMessage)}
	chain.tip = rawBlock(t, 1, "A", "0", 1)
	worker, database := newTestWorker(t, chain, 64)
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for height := int64(2); height <= 104; height++ {
		chain.byHeight[height] = rawBlock(t, height, stringID(height), stringID(height-1), 0)
	}
	chain.byHeight[2] = rawBlock(t, 2, stringID(2), "A", 0)
	chain.tip = rawBlock(t, 102, stringID(102), stringID(101), 0)
	var catchUpSleeps int
	worker.sleep = func(context.Context, time.Duration) error { catchUpSleeps++; return nil }
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	cursor, _, err := database.Cursor(context.Background())
	if err != nil || cursor.LastHeight != 102 {
		t.Fatalf("cursor = %+v, err = %v", cursor, err)
	}
	if catchUpSleeps != 100 {
		t.Fatalf("catch-up throttles = %d, want 100", catchUpSleeps)
	}
	fetches := len(chain.fetched)
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(chain.fetched) != fetches {
		t.Fatal("same-height poll fetched a block")
	}
	chain.tip = rawBlock(t, 101, stringID(101), stringID(100), 0)
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if worker.StaleReads() != 1 {
		t.Fatalf("stale reads = %d, want 1", worker.StaleReads())
	}
}

func TestClockSkewUsesLatestBlockHeader(t *testing.T) {
	chain := &fakeChain{tip: rawBlock(t, 1, "A", "0", 0)}
	worker, _ := newTestWorker(t, chain, 64)
	block, err := parseBlock(chain.tip)
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return time.Unix(block.Timestamp+40, 0) }
	var logs bytes.Buffer
	worker.logger = slog.New(slog.NewTextHandler(&logs, nil))
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker.checkClock()
	if skew, checked := worker.ClockSkew(); !checked || skew != 40 {
		t.Fatalf("clock skew = %d checked=%v", skew, checked)
	}
	if !bytes.Contains(logs.Bytes(), []byte("level=ERROR")) {
		t.Fatalf("40-second skew did not log an error: %s", logs.String())
	}
}

func TestReplayThousandBlocks(t *testing.T) {
	chain := &fakeChain{byHeight: make(map[int64]json.RawMessage, 999)}
	chain.tip = rawBlock(t, 1, "block-1", "genesis", 0)
	worker, database := newTestWorker(t, chain, 64)
	worker.sleep = func(context.Context, time.Duration) error { return nil }
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for height := int64(2); height <= 1000; height++ {
		chain.byHeight[height] = rawBlock(t, height, fmtID(height), fmtID(height-1), 0)
	}
	chain.byHeight[2] = rawBlock(t, 2, fmtID(2), "block-1", 0)
	chain.tip = rawBlock(t, 1001, fmtID(1001), fmtID(1000), 0)
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCursor(t, database, 1001)
	if len(chain.fetched) != 999 {
		t.Fatalf("recorded replay fetched %d gap blocks, want 999", len(chain.fetched))
	}
}

func TestRecordedBlockFixturesCoverTST002(t *testing.T) {
	data, err := os.ReadFile("testdata/recorded_blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Tags         []string          `json:"tags"`
			OwnedAddress string            `json:"owned_address_hex"`
			Block        json.RawMessage   `json:"block"`
			Receipts     []json.RawMessage `json:"receipts"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{
		"trx_transfer": false, "trc20_transfer": false, "failed_contract_call": false,
		"reverted_transfer": false, "multiple_transfer_logs": false, "outbound_transfer": false, "empty_block": false,
	}
	transferTopic := []byte("ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	for _, fixture := range corpus.Cases {
		block, err := parseBlock(fixture.Block)
		if err != nil {
			t.Fatal(err)
		}
		receipts := joinRaw(fixture.Receipts)
		for _, tag := range fixture.Tags {
			required[tag] = true
			switch tag {
			case "empty_block":
				if len(block.Transactions) != 0 {
					t.Fatal("empty block fixture contains transactions")
				}
			case "trx_transfer":
				if !bytes.Contains(fixture.Block, []byte("TransferContract")) {
					t.Fatal("TRX fixture lacks TransferContract")
				}
			case "trc20_transfer":
				if !bytes.Contains(fixture.Block, []byte("a9059cbb")) || !bytes.Contains(receipts, transferTopic) {
					t.Fatal("TRC-20 fixture lacks transfer selector or log")
				}
			case "failed_contract_call":
				if !bytes.Contains(receipts, []byte("FAILED")) {
					t.Fatal("failed fixture lacks FAILED receipt")
				}
			case "reverted_transfer":
				if !bytes.Contains(fixture.Block, []byte("REVERT")) {
					t.Fatal("reverted fixture lacks REVERT result")
				}
			case "multiple_transfer_logs":
				if bytes.Count(receipts, transferTopic) < 2 {
					t.Fatal("multi-log fixture has fewer than two Transfer logs")
				}
			case "outbound_transfer":
				if fixture.OwnedAddress == "" || !bytes.Contains(fixture.Block, []byte(fixture.OwnedAddress)) {
					t.Fatal("outbound fixture lacks its owned sender")
				}
			}
		}
	}
	for name, covered := range required {
		if !covered {
			t.Errorf("TST-002 fixture %q is missing", name)
		}
	}
}

func TestReorgRequiresSameMismatchOnePollApart(t *testing.T) {
	chain := &fakeChain{byHeight: make(map[int64]json.RawMessage)}
	canonical := []json.RawMessage{
		rawBlock(t, 1, "A", "0", 0),
		rawBlock(t, 2, "B", "A", 0),
		rawBlock(t, 3, "C", "B", 0),
	}
	chain.tip = canonical[0]
	worker, database := newTestWorker(t, chain, 64)
	now := time.Unix(1_700_000_000, 0)
	worker.now = func() time.Time { return now }
	for _, block := range canonical {
		chain.tip = block
		if err := worker.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	chain.byHeight[1] = canonical[0]
	chain.byHeight[2] = rawBlock(t, 2, "B2", "A", 0)
	chain.byHeight[3] = rawBlock(t, 3, "C2", "B2", 0)
	chain.tip = rawBlock(t, 4, "D2", "C2", 0)

	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCursor(t, database, 3)
	now = now.Add(2 * time.Second)
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCursor(t, database, 3)
	now = now.Add(time.Second)
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCursor(t, database, 4)
	for height, wantID := range map[int64]string{1: "A", 2: "B2", 3: "C2", 4: "D2"} {
		block, found, err := database.Block(context.Background(), height)
		if err != nil || !found || block.ID != wantID {
			t.Fatalf("block %d = %+v, found=%v, err=%v", height, block, found, err)
		}
	}
	if worker.ReorgSuspicions() != 3 || worker.ReorgsConfirmed() != 1 {
		t.Fatalf("reorg counters suspected=%d confirmed=%d", worker.ReorgSuspicions(), worker.ReorgsConfirmed())
	}
}

func TestReorgDepthExceededHaltsIngest(t *testing.T) {
	chain := &fakeChain{byHeight: make(map[int64]json.RawMessage)}
	worker, _ := newTestWorker(t, chain, 2)
	now := time.Unix(1_700_000_000, 0)
	worker.now = func() time.Time { return now }
	for height, id := range []string{"A", "B", "C"} {
		parent := "0"
		if height > 0 {
			parent = []string{"A", "B", "C"}[height-1]
		}
		chain.tip = rawBlock(t, int64(height+1), id, parent, 0)
		if err := worker.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	chain.byHeight[3] = rawBlock(t, 3, "C2", "B2", 0)
	chain.byHeight[2] = rawBlock(t, 2, "B2", "A2", 0)
	chain.byHeight[1] = rawBlock(t, 1, "A2", "0", 0)
	chain.tip = rawBlock(t, 4, "D2", "C2", 0)
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	err := worker.Tick(context.Background())
	if !errors.Is(err, ErrReorgDepthExceeded) || !worker.Halted() {
		t.Fatalf("deep reorg err=%v halted=%v", err, worker.Halted())
	}
	if err := worker.Tick(context.Background()); !errors.Is(err, ErrHalted) {
		t.Fatalf("halted tick error = %v", err)
	}
}

func assertCursor(t *testing.T, database *store.Store, want int64) {
	t.Helper()
	cursor, exists, err := database.Cursor(context.Background())
	if err != nil || !exists || cursor.LastHeight != want {
		t.Fatalf("cursor=%+v exists=%v err=%v, want %d", cursor, exists, err, want)
	}
}

func stringID(height int64) string {
	return "block-" + time.Unix(height, 0).UTC().Format("150405")
}

func fmtID(height int64) string { return "recorded-" + stringID(height) }

func joinRaw(values []json.RawMessage) []byte {
	var joined []byte
	for _, value := range values {
		joined = append(joined, value...)
	}
	return joined
}
