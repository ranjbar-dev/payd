package wallet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/config"
	"payd/internal/follower"
	"payd/internal/store"
)

type safetyCall struct {
	trc20       bool
	inbound     bool
	fingerprint string
}

type fakeSafetyReader struct {
	target string
	calls  []safetyCall
}

func (f *fakeSafetyReader) GetAccountHistory(_ context.Context, _ string, trc20 bool, query url.Values) (json.RawMessage, error) {
	inbound := query.Get("only_to") == "true"
	fingerprint := query.Get("fingerprint")
	f.calls = append(f.calls, safetyCall{trc20, inbound, fingerprint})
	if query.Get("limit") != "200" || query.Get("min_timestamp") != "0" {
		return nil, fmt.Errorf("wrong pagination query: %s", query.Encode())
	}
	if fingerprint != "" {
		return json.RawMessage(`{"data":[],"meta":{}}`), nil
	}
	id := map[[2]bool]string{{true, true}: "trc20-in", {true, false}: "trc20-out", {false, true}: "trx-in", {false, false}: "trx-out"}[[2]bool{trc20, inbound}]
	if trc20 {
		from := "payer"
		if !inbound {
			from = f.target
		}
		return json.RawMessage(fmt.Sprintf(`{"data":[{"transaction_id":%q,"from":%q}],"meta":{"fingerprint":"next"}}`, id, from)), nil
	}
	owner, to := "payer", f.target
	if !inbound {
		owner, to = f.target, "recipient"
	}
	return json.RawMessage(fmt.Sprintf(`{"data":[{"txID":%q,"ret":[{"contractRet":"SUCCESS"}],"raw_data":{"contract":[{"type":"TransferContract","parameter":{"value":{"owner_address":%q,"to_address":%q,"amount":1}}}]}}],"meta":{"fingerprint":"next"}}`, id, owner, to)), nil
}

func (*fakeSafetyReader) GetTransactionInfoByID(_ context.Context, txid string) (json.RawMessage, error) {
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"blockNumber":10,"blockTimeStamp":10000,"receipt":{"result":"SUCCESS"}}`, txid)), nil
}

func (*fakeSafetyReader) GetBlockByNum(context.Context, int64) (json.RawMessage, error) {
	return json.RawMessage(`{"blockID":"block-10","block_header":{"raw_data":{"timestamp":10000}}}`), nil
}

type fakeSafetyDecoder struct{ addressID int64 }

func (f fakeSafetyDecoder) DecodeReconciled(_ context.Context, txid string, transaction, _ json.RawMessage, block follower.Block) ([]store.PaymentRecord, error) {
	trc20 := len(txid) >= 5 && txid[:5] == "trc20"
	if trc20 == (len(transaction) != 0) {
		return nil, fmt.Errorf("transaction raw presence for %s is wrong", txid)
	}
	direction := "in"
	if len(txid) >= 3 && txid[len(txid)-3:] == "out" {
		direction = "out"
	}
	return []store.PaymentRecord{{TxID: txid, LogIndex: 0, Direction: direction, BlockHeight: block.Height,
		BlockID: block.ID, BlockTimestamp: block.Timestamp, FromAddress: "from", ToAddress: "to",
		AddressID: &f.addressID, Asset: "TRX", AmountRaw: "1", DetectedAt: 11}}, nil
}

func TestSafetyNetCoversBothAssetsDirectionsAndAllFingerprintPages(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	hd, err := hdwallet.FromMnemonic(monitorTestMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hd.Destroy)
	if err := database.InitializeWallet(ctx, hd, 0, 1, 1000); err != nil {
		t.Fatal(err)
	}
	order, _, err := database.CreateOrder(ctx, nil, 1, store.CreateOrderParams{
		Asset: "TRX", ExpectedRaw: "10", ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	reader := &fakeSafetyReader{target: order.Address}
	reconciler, err := NewReconciler(&fakeWalletReader{}, database, testSafetyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	decoder := fakeSafetyDecoder{addressID: order.AddressID}
	if err := reconciler.EnableSafetyNet(reader, decoder, func(write *store.BlockWrite, payment store.PaymentRecord) error {
		_, err := write.UpsertPayment(payment)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.SafetyNet(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(reader.calls) != 8 {
		t.Fatalf("history calls = %v", reader.calls)
	}
	seen := make(map[safetyCall]bool)
	for _, call := range reader.calls {
		seen[call] = true
	}
	for _, trc20 := range []bool{true, false} {
		for _, inbound := range []bool{true, false} {
			for _, fingerprint := range []string{"", "next"} {
				if !seen[safetyCall{trc20, inbound, fingerprint}] {
					t.Fatalf("missing asset/direction/page call: trc20=%v inbound=%v fingerprint=%q", trc20, inbound, fingerprint)
				}
			}
		}
	}
	metrics, err := database.OperationalMetrics(ctx)
	if err != nil || metrics.Payments["seen"] != 4 {
		t.Fatalf("reconciled payments = %#v, %v", metrics.Payments, err)
	}
}

func testSafetyConfig() config.Config {
	return config.Config{Assets: []config.Asset{{Symbol: "TRX", Kind: "native", Decimals: 6}},
		Resources: config.Resources{SlowCheckInterval: 6 * time.Hour}, IPN: config.IPN{}}
}
