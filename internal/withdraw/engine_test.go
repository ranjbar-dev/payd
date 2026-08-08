package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	"google.golang.org/protobuf/proto"

	"payd/internal/chain"
	"payd/internal/config"
	"payd/internal/energy"
	"payd/internal/store"
)

const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

type fakeReader struct {
	transaction, resources, delegation, delegatable json.RawMessage
	delegationCalls, resourceCalls                  int
	delegatedResource                               string
	delegatedBalance                                int64
}

func (f *fakeReader) GetNowBlock(context.Context) (json.RawMessage, error) {
	return referenceBlock(), nil
}
func (f *fakeReader) GetTransactionByID(context.Context, string) (json.RawMessage, error) {
	return f.transaction, nil
}
func (f *fakeReader) GetAccountResource(context.Context, string) (json.RawMessage, error) {
	f.resourceCalls++
	if f.resources != nil {
		return f.resources, nil
	}
	return json.RawMessage(`{"EnergyLimit":100000,"EnergyUsed":0,"NetLimit":1000,"NetUsed":0}`), nil
}

// TST-012: overpriced rent falls through failed self-delegation and a live burn cap.
func TestEnergyFallbackChainFailsCleanly(t *testing.T) {
	ctx := context.Background()
	engine, database, _, reader, _, cleanup := withdrawalFixture(t)
	defer cleanup()
	provider := &fakeEnergyProvider{quote: energy.Quote{PriceTRX: "7"}, balance: "100"}
	cfg := engine.config
	cfg.Energy = config.Energy{Enabled: true, Provider: "tronzap", RentAmount: 131000, RentDuration: time.Hour,
		MaxPriceTRX: "6", BalanceWarnTRX: "50", PollInterval: time.Second, PollTimeout: time.Minute,
		FallbackToBurn: true, MaxBurnTRX: "20"}
	cfg.Resources.MinEnergy = 131000
	engine.UpdateProvider(provider, cfg)
	reader.resources = json.RawMessage(`{"EnergyLimit":0,"TotalEnergyLimit":1000000,"TotalEnergyWeight":1000000,"NetLimit":1000}`)
	reader.delegatable = json.RawMessage(`{"max_size":0}`)
	if _, err := database.UpsertChainParameters(ctx, 420, 100, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	withdrawal := onlyWithdrawal(t, database)
	if withdrawal.Status != "failed" || withdrawal.FailureReason != "energy_burn_limit" || withdrawal.TxID != "" {
		t.Fatalf("fallback withdrawal = %#v", withdrawal)
	}
	purchase, found, err := database.EnergyPurchaseForWithdrawal(ctx, withdrawal.ID)
	if err != nil || !found || purchase.Status != "failed" || purchase.QuotedTRX != "7" {
		t.Fatalf("purchase=%#v found=%v err=%v", purchase, found, err)
	}
	grant, found, err := database.ResourceGrantForWithdrawal(ctx, withdrawal.ID, "ENERGY")
	if err != nil || !found || grant.Status != "failed" {
		t.Fatalf("grant=%#v found=%v err=%v", grant, found, err)
	}
	if provider.quoteCalls != 1 || provider.purchaseCalls != 0 || reader.delegationCalls != 0 {
		t.Fatalf("quote=%d purchase=%d delegation=%d", provider.quoteCalls, provider.purchaseCalls, reader.delegationCalls)
	}
}

func TestRentedEnergyPollsAtConfiguredIntervalAndAuditsArrival(t *testing.T) {
	ctx := context.Background()
	engine, database, _, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Second)
	engine.now = func() time.Time { return now }
	provider := &fakeEnergyProvider{
		quote: energy.Quote{PriceTRX: "3.8"}, order: energy.Order{ID: "provider-order", State: "pending", ActualTRX: "3.7"},
		status: energy.Status{State: "success", ActualTRX: "3.7", DelegationTxID: "delegation-txid"}, balance: "100",
	}
	cfg := engine.config
	cfg.Energy = config.Energy{Enabled: true, Provider: "tronzap", RentAmount: 100, RentDuration: time.Hour,
		MaxPriceTRX: "6", BalanceWarnTRX: "50", PollInterval: 2 * time.Second, PollTimeout: time.Minute}
	cfg.Resources.MinEnergy = 100
	engine.UpdateProvider(provider, cfg)
	reader.resources = json.RawMessage(`{"EnergyLimit":0,"NetLimit":1000}`)
	broadcast.response = chain.Response{StatusCode: http.StatusOK, Body: json.RawMessage(`{"result":true}`)}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.purchaseCalls != 1 || reader.resourceCalls != 1 {
		t.Fatalf("purchase=%d resource polls=%d", provider.purchaseCalls, reader.resourceCalls)
	}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if reader.resourceCalls != 1 {
		t.Fatalf("poll_interval ignored: %d resource calls", reader.resourceCalls)
	}
	now = now.Add(2 * time.Second)
	reader.resources = json.RawMessage(`{"EnergyLimit":100,"NetLimit":1000}`)
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	withdrawal := onlyWithdrawal(t, database)
	if withdrawal.Status != "broadcast" || withdrawal.EnergySource != "rented" {
		t.Fatalf("rented withdrawal = %#v", withdrawal)
	}
	purchase, found, err := database.EnergyPurchaseForWithdrawal(ctx, withdrawal.ID)
	if err != nil || !found || purchase.Status != "delegated" || purchase.ActualTRX != "3.7" || purchase.DelegationTxID != "delegation-txid" {
		t.Fatalf("purchase=%#v found=%v err=%v", purchase, found, err)
	}
	if provider.statusCalls != 1 || provider.purchaseCalls != 1 || reader.resourceCalls != 2 {
		t.Fatalf("status=%d purchase=%d polls=%d", provider.statusCalls, provider.purchaseCalls, reader.resourceCalls)
	}
}

func TestEnergyPurchaseTimeoutEmitsFailureAndFallsThrough(t *testing.T) {
	ctx := context.Background()
	engine, database, _, reader, _, cleanup := withdrawalFixture(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Second)
	engine.now = func() time.Time { return now }
	provider := &fakeEnergyProvider{quote: energy.Quote{PriceTRX: "3"}, order: energy.Order{ID: "pending", State: "pending"},
		status: energy.Status{State: "pending"}, balance: "100"}
	cfg := engine.config
	cfg.IPN.Consumers = []config.Consumer{{Name: "ops", URL: "https://example.test/ipn", Enabled: true, ReceivesGlobal: true}}
	cfg.Energy = config.Energy{Enabled: true, Provider: "tronzap", RentAmount: 100, RentDuration: time.Hour,
		MaxPriceTRX: "6", BalanceWarnTRX: "50", PollInterval: time.Second, PollTimeout: 2 * time.Second}
	cfg.Resources.MinEnergy = 100
	engine.UpdateProvider(provider, cfg)
	reader.resources = json.RawMessage(`{"EnergyLimit":0,"NetLimit":1000,"TotalEnergyLimit":1000000,"TotalEnergyWeight":1000000}`)
	reader.delegatable = json.RawMessage(`{"max_size":0}`)
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	withdrawal := onlyWithdrawal(t, database)
	purchase, found, err := database.EnergyPurchaseForWithdrawal(ctx, withdrawal.ID)
	if err != nil || !found || purchase.Status != "failed" || withdrawal.Status != "failed" {
		t.Fatalf("withdrawal=%#v purchase=%#v found=%v err=%v", withdrawal, purchase, found, err)
	}
	if count, err := database.OutboxCount(ctx, "energy.purchase_failed"); err != nil || count != 1 {
		t.Fatalf("purchase_failed events=%d err=%v", count, err)
	}
}

func TestEnergyProviderCircuitOpensAfterFiveFailures(t *testing.T) {
	engine, _, _, _, _, cleanup := withdrawalFixture(t)
	defer cleanup()
	now := time.Now()
	engine.now = func() time.Time { return now }
	provider := &fakeEnergyProvider{balance: "100"}
	cfg := engine.config
	cfg.Energy.Enabled, cfg.Energy.Provider = true, "tronzap"
	engine.UpdateProvider(provider, cfg)
	for i := 0; i < 5; i++ {
		if err := engine.recordProviderFailure(context.Background(), "tronzap", errors.New("down")); err != nil {
			t.Fatal(err)
		}
	}
	if engine.providerEnabled() {
		t.Fatal("provider circuit remained closed after five failures")
	}
	now = now.Add(10 * time.Minute)
	if !engine.providerEnabled() {
		t.Fatal("provider circuit did not reopen after ten minutes")
	}
}

func TestEnergyProviderBalanceCheckAlertsAndSetsMetric(t *testing.T) {
	ctx := context.Background()
	engine, database, _, _, _, cleanup := withdrawalFixture(t)
	defer cleanup()
	now := time.Now()
	engine.now = func() time.Time { return now }
	provider := &fakeEnergyProvider{balance: "12.5"}
	cfg := engine.config
	cfg.IPN.Consumers = []config.Consumer{{Name: "ops", URL: "https://example.test/ipn", Enabled: true, ReceivesGlobal: true}}
	cfg.Energy = config.Energy{Enabled: true, Provider: "tronzap", BalanceWarnTRX: "50"}
	engine.UpdateProvider(provider, cfg)
	if err := engine.checkProviderBalance(ctx); err != nil {
		t.Fatal(err)
	}
	if balance, low := engine.ProviderBalanceMetric(); balance != "12.5" || !low {
		t.Fatalf("provider metric = %q low=%v", balance, low)
	}
	if count, err := database.OutboxCount(ctx, "energy.balance_low"); err != nil || count != 1 {
		t.Fatalf("balance_low events=%d err=%v", count, err)
	}
	if err := engine.checkProviderBalance(ctx); err != nil || provider.balanceCalls != 1 {
		t.Fatalf("15-minute cadence calls=%d err=%v", provider.balanceCalls, err)
	}
}
func (f *fakeReader) DelegateResource(_ context.Context, _, _ string, resource string, balance int64) (json.RawMessage, error) {
	f.delegationCalls++
	f.delegatedResource, f.delegatedBalance = resource, balance
	return f.delegation, nil
}
func (f *fakeReader) GetCanDelegatedMaxSize(context.Context, string, string) (json.RawMessage, error) {
	return f.delegatable, nil
}

type fakeSolidity struct{ body json.RawMessage }

func (f fakeSolidity) GetTransactionInfoByID(context.Context, string) (json.RawMessage, error) {
	return f.body, nil
}

// fakeEnergyProvider is TST-013's no-prepaid-balance implementation.
type fakeEnergyProvider struct {
	quote                                                energy.Quote
	order                                                energy.Order
	status                                               energy.Status
	balance                                              string
	quoteErr, purchaseErr, statusErr, balanceErr         error
	quoteCalls, purchaseCalls, statusCalls, balanceCalls int
}

func (f *fakeEnergyProvider) Quote(receiver, resourceType string, amount int64, duration time.Duration) (energy.Quote, error) {
	f.quoteCalls++
	quote := f.quote
	quote.Receiver, quote.ResourceType, quote.Amount, quote.Duration = receiver, resourceType, amount, duration
	return quote, f.quoteErr
}

func (f *fakeEnergyProvider) Purchase(quote energy.Quote) (energy.Order, error) {
	f.purchaseCalls++
	return f.order, f.purchaseErr
}

func (f *fakeEnergyProvider) Status(string) (energy.Status, error) {
	f.statusCalls++
	return f.status, f.statusErr
}

func (f *fakeEnergyProvider) Balance() (string, error) {
	f.balanceCalls++
	return f.balance, f.balanceErr
}

type fakeBroadcast struct {
	response       chain.Response
	err            error
	calls          int
	panicAfterSend bool
	payloads       []json.RawMessage
}

func (f *fakeBroadcast) Send(_ context.Context, payload json.RawMessage) (chain.Response, error) {
	f.calls++
	f.payloads = append(f.payloads, append(json.RawMessage(nil), payload...))
	if f.panicAfterSend {
		panic("injected process death after broadcast request")
	}
	return f.response, f.err
}

// TST-018: after the first transfer consumes free bandwidth, the second is held and sourced.
func TestSecondTRC20WithdrawalSourcesBandwidth(t *testing.T) {
	ctx := context.Background()
	engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	broadcast.response = chain.Response{StatusCode: 200, Body: json.RawMessage(`{"result":true}`)}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	reader.transaction = json.RawMessage(`{"txID":"present"}`)
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	first := onlyWithdrawal(t, database)
	if first.Status != "confirmed" {
		t.Fatalf("first withdrawal status = %s", first.Status)
	}
	engine.mu.Lock()
	engine.config.Resources.MinBandwidth = 345
	engine.config.Resources.BandwidthStrategy = "delegate"
	engine.config.Resources.ResourceWalletIndex = 1000
	engine.mu.Unlock()
	reader.resources = json.RawMessage(`{"EnergyLimit":100000,"NetLimit":1000,"NetUsed":745,"TotalNetLimit":43200000000,"TotalNetWeight":30000000000}`)
	reader.delegatable = json.RawMessage(`{"max_size":1000000000}`)
	reader.delegation = json.RawMessage(`{"txID":"6e340b9cffb37a989ca544e6bb780a2c78901d3fb33738768511a30617afa01d","raw_data_hex":"00","raw_data":{"expiration":2000000000000}}`)
	if _, _, err := database.CreateWithdrawal(ctx, store.CreateWithdrawal{IdempotencyKey: "two", FromAddress: first.FromAddress,
		ToAddress: first.ToAddress, Asset: "USDT", AmountRaw: "1000000", AmountUSD: "1", DailyLimitUSD: "100",
		RequestedBy: "test", IP: "127.0.0.1", Now: time.Now().Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := database.ListWithdrawals(ctx, "", 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("withdrawals=%d err=%v", len(items), err)
	}
	second := items[0]
	if second.IdempotencyKey != "two" {
		second = items[1]
	}
	if second.Status != "awaiting_resources" || second.TxID != "" || second.BandwidthSource != "delegated" {
		t.Fatalf("second withdrawal = %#v", second)
	}
	grant, found, err := database.ResourceGrantForWithdrawal(ctx, second.ID, "BANDWIDTH")
	if err != nil || !found || grant.Status != "broadcast" || grant.Source != "self_delegated" || grant.BroadcastAttemptedAt == nil {
		t.Fatalf("bandwidth grant = %#v, found=%v err=%v", grant, found, err)
	}
	if broadcast.calls != 2 || signer.calls != 2 || reader.delegationCalls != 1 || reader.delegatedResource != "BANDWIDTH" || reader.delegatedBalance <= 0 {
		t.Fatalf("calls: broadcasts=%d signs=%d delegations=%d resource=%s balance=%d", broadcast.calls, signer.calls,
			reader.delegationCalls, reader.delegatedResource, reader.delegatedBalance)
	}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if broadcast.calls != 2 || signer.calls != 2 || reader.delegationCalls != 1 {
		t.Fatalf("resource retry: broadcasts=%d signs=%d delegations=%d", broadcast.calls, signer.calls, reader.delegationCalls)
	}
}

func TestBandwidthTopupBroadcastsOnce(t *testing.T) {
	engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	engine.mu.Lock()
	engine.config.Resources.MinBandwidth = 345
	engine.config.Resources.BandwidthStrategy = "topup"
	engine.mu.Unlock()
	reader.resources = json.RawMessage(`{"EnergyLimit":100000,"NetLimit":255}`)
	broadcast.response = chain.Response{StatusCode: 500, Body: json.RawMessage(`{"error":"busy"}`)}
	broadcast.err = errors.New("500")
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	withdrawal := onlyWithdrawal(t, database)
	grant, found, err := database.ResourceGrantForWithdrawal(context.Background(), withdrawal.ID, "BANDWIDTH")
	if err != nil || !found || grant.Source != "topup" || grant.Status != "broadcast" {
		t.Fatalf("topup grant=%#v found=%v err=%v", grant, found, err)
	}
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if broadcast.calls != 1 || signer.calls != 1 {
		t.Fatalf("RES-008 retry: broadcasts=%d signs=%d", broadcast.calls, signer.calls)
	}
}

func TestTRXWithdrawalAlsoSourcesBandwidth(t *testing.T) {
	ctx := context.Background()
	engine, database, _, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	broadcast.response = chain.Response{StatusCode: 200, Body: json.RawMessage(`{"result":true}`)}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	reader.transaction = json.RawMessage(`{"txID":"present"}`)
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	first := onlyWithdrawal(t, database)
	addressID := first.AddressID
	if err := database.CommitBlock(ctx, store.BlockRecord{Height: 2, ID: "block-2", ParentID: "block-1", Timestamp: 2,
		ProcessedAt: 2, Reference: mustReference(t)}, 10, func(write *store.BlockWrite) error {
		_, err := write.UpsertPayment(store.PaymentRecord{TxID: "trx-only-funding", Direction: "in", BlockHeight: 2,
			BlockID: "block-2", BlockTimestamp: 2, FromAddress: first.ToAddress, ToAddress: first.FromAddress,
			AddressID: &addressID, Asset: "TRX", AmountRaw: "1000010", Status: "confirmed", DetectedAt: 2})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpsertChainParameters(ctx, 100, 100, time.Now()); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	engine.config.Assets = append(engine.config.Assets, config.Asset{Symbol: "TRX", Kind: "native", Decimals: 6})
	engine.config.Resources.MinBandwidth = 345
	engine.config.Resources.BandwidthStrategy = "delegate"
	engine.mu.Unlock()
	reader.resources = json.RawMessage(`{"NetLimit":255,"TotalNetLimit":43200000000,"TotalNetWeight":30000000000}`)
	reader.delegatable = json.RawMessage(`{"max_size":1000000000}`)
	reader.delegation = json.RawMessage(`{"txID":"6e340b9cffb37a989ca544e6bb780a2c78901d3fb33738768511a30617afa01d","raw_data_hex":"00","raw_data":{"expiration":2000000000000}}`)
	if _, _, err := database.CreateWithdrawal(ctx, store.CreateWithdrawal{IdempotencyKey: "trx-two", FromAddress: first.FromAddress,
		ToAddress: first.ToAddress, Asset: "TRX", AmountRaw: "1000000", AmountUSD: "1", DailyLimitUSD: "100",
		RequestedBy: "test", IP: "127.0.0.1", Now: time.Now().Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := database.ListWithdrawals(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	trx := items[0]
	if trx.IdempotencyKey != "trx-two" {
		trx = items[1]
	}
	if trx.Status != "awaiting_resources" || trx.TxID != "" || trx.BandwidthSource != "delegated" || reader.delegatedResource != "BANDWIDTH" {
		t.Fatalf("TRX bandwidth gate = %#v resource=%s", trx, reader.delegatedResource)
	}
}

func TestDelegationRawJSONRejectsMismatchedTxID(t *testing.T) {
	engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	engine.mu.Lock()
	engine.config.Resources.MinBandwidth = 345
	engine.config.Resources.BandwidthStrategy = "delegate"
	engine.config.Resources.ResourceWalletIndex = 1000
	engine.mu.Unlock()
	reader.resources = json.RawMessage(`{"EnergyLimit":100000,"NetLimit":255,"TotalNetLimit":43200000000,"TotalNetWeight":30000000000}`)
	reader.delegatable = json.RawMessage(`{"max_size":1000000000}`)
	reader.delegation = json.RawMessage(`{"txID":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","raw_data_hex":"00"}`)
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if withdrawal := onlyWithdrawal(t, database); withdrawal.Status != "failed" || withdrawal.FailureReason != "bandwidth_unavailable" {
		t.Fatalf("withdrawal = %#v", withdrawal)
	}
	if broadcast.calls != 0 || signer.calls != 1 {
		t.Fatalf("guard calls: broadcasts=%d signs=%d", broadcast.calls, signer.calls)
	}
}

type countingSigner struct {
	wallet *hdwallet.HDWallet
	calls  int
}

func (s *countingSigner) SignTransaction(chain hdwallet.Chain, index uint32, input proto.Message) (proto.Message, error) {
	s.calls++
	return s.wallet.SignTransaction(chain, index, input)
}

func TestEnergyDelegationFailureFallsBackToBurn(t *testing.T) {
	ctx := context.Background()
	engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	withdrawal := onlyWithdrawal(t, database)
	addressID := withdrawal.AddressID
	if err := database.CommitBlock(ctx, store.BlockRecord{Height: 2, ID: "block-2", ParentID: "block-1", Timestamp: 2,
		ProcessedAt: 2, Reference: mustReference(t)}, 10, func(write *store.BlockWrite) error {
		_, err := write.UpsertPayment(store.PaymentRecord{TxID: "trx-funding", Direction: "in", BlockHeight: 2,
			BlockID: "block-2", BlockTimestamp: 2, FromAddress: withdrawal.ToAddress, ToAddress: withdrawal.FromAddress,
			AddressID: &addressID, Asset: "TRX", AmountRaw: "100000000", Status: "confirmed", DetectedAt: 2})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpsertChainParameters(ctx, 100, 100, time.Now()); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	engine.config.Resources.MinEnergy = 1000
	engine.config.Energy.FallbackToBurn = true
	engine.config.Energy.MaxBurnTRX = "20"
	engine.mu.Unlock()
	reader.resources = json.RawMessage(`{"NetLimit":1000,"TotalEnergyLimit":180000000000,"TotalEnergyWeight":30000000000}`)
	reader.delegatable = json.RawMessage(`{"max_size":1000000000}`)
	reader.delegation = json.RawMessage(`{"txID":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","raw_data_hex":"00"}`)
	broadcast.response = chain.Response{StatusCode: 200, Body: json.RawMessage(`{"result":true}`)}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	withdrawal = onlyWithdrawal(t, database)
	if withdrawal.Status != "broadcast" || withdrawal.EnergySource != "burned" {
		t.Fatalf("burn fallback withdrawal = %#v", withdrawal)
	}
	if broadcast.calls != 1 || signer.calls != 2 || reader.delegatedResource != "ENERGY" {
		t.Fatalf("fallback calls: broadcasts=%d signs=%d resource=%s", broadcast.calls, signer.calls, reader.delegatedResource)
	}
}

// TST-014: DUP_TRANSACTION_ERROR is evidence of an attempted transaction, never a terminal rejection.
func TestDuplicateBroadcastResolvesConfirmed(t *testing.T) {
	engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	broadcast.response = chain.Response{StatusCode: 200, Body: json.RawMessage(`{"result":false,"code":"DUP_TRANSACTION_ERROR"}`)}
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w := onlyWithdrawal(t, database)
	if w.Status != "broadcast" {
		t.Fatalf("status after duplicate = %s", w.Status)
	}
	reader.transaction = json.RawMessage(`{"txID":"present"}`)
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w = onlyWithdrawal(t, database)
	if w.Status != "confirmed" || broadcast.calls != 1 || signer.calls != 1 {
		t.Fatalf("resolved = status %s, broadcasts %d, signs %d", w.Status, broadcast.calls, signer.calls)
	}
}

// TST-016: recovery keys off the FULL-committed txid and never signs or broadcasts again.
func TestCrashAfterBroadcastRequestResolvesWithoutResigning(t *testing.T) {
	engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
	defer cleanup()
	broadcast.panicAfterSend = true
	func() {
		defer func() { _ = recover() }()
		_ = engine.Tick(context.Background())
	}()
	w := onlyWithdrawal(t, database)
	if w.TxID == "" || w.BroadcastAttemptedAt == nil || broadcast.calls != 1 {
		t.Fatalf("durability marker = %#v, calls=%d", w, broadcast.calls)
	}
	reader.transaction = json.RawMessage(`{"txID":"present"}`)
	broadcast.panicAfterSend = false
	if err := engine.recover(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	w = onlyWithdrawal(t, database)
	if w.Status != "confirmed" || broadcast.calls != 1 || signer.calls != 1 {
		t.Fatalf("restart = status %s, broadcasts %d, signs %d", w.Status, broadcast.calls, signer.calls)
	}
}

// TST-021: every broadcast-layer outcome consumes the one lifetime attempt.
func TestExactlyOneBroadcastAcrossOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		response chain.Response
		err      error
	}{
		{"accepted", chain.Response{StatusCode: 200, Body: json.RawMessage(`{"result":true}`)}, nil},
		{"duplicate", chain.Response{StatusCode: 200, Body: json.RawMessage(`{"result":false,"code":"DUP_TRANSACTION_ERROR"}`)}, nil},
		{"server-500", chain.Response{StatusCode: 500, Body: json.RawMessage(`{"error":"busy"}`)}, errors.New("500")},
		{"timeout", chain.Response{}, context.DeadlineExceeded},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			engine, database, signer, reader, broadcast, cleanup := withdrawalFixture(t)
			defer cleanup()
			broadcast.response, broadcast.err = test.response, test.err
			if err := engine.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			reader.transaction = json.RawMessage(`{"txID":"present"}`)
			if err := engine.recover(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if err := engine.recover(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if w := onlyWithdrawal(t, database); w.Status != "confirmed" || broadcast.calls != 1 || signer.calls != 1 {
				t.Fatalf("status=%s broadcasts=%d signs=%d", w.Status, broadcast.calls, signer.calls)
			}
		})
	}
}

func withdrawalFixture(t *testing.T) (*Engine, *store.Store, *countingSigner, *fakeReader, *fakeBroadcast, func()) {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	wallet, err := hdwallet.FromMnemonic(testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeWallet(ctx, wallet, 0, 3, 1000); err != nil {
		t.Fatal(err)
	}
	addresses, err := database.WalletAddresses(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	source, destination, contract := addresses[0], addresses[1], addresses[2]
	addressID := source.ID
	if err := database.CommitBlock(ctx, store.BlockRecord{Height: 1, ID: "block-1", ParentID: "block-0", Timestamp: 1,
		ProcessedAt: 1, Reference: mustReference(t)}, 10, func(write *store.BlockWrite) error {
		_, err := write.UpsertPayment(store.PaymentRecord{TxID: "funding", LogIndex: 0, Direction: "in", BlockHeight: 1,
			BlockID: "block-1", BlockTimestamp: 1, FromAddress: destination.Address, ToAddress: source.Address,
			AddressID: &addressID, Asset: "USDT", AmountRaw: "5000000", Status: "confirmed", DetectedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Assets: []config.Asset{{Symbol: "USDT", Kind: "trc20", Contract: contract.Address, Decimals: 6}},
		Resources: config.Resources{MinEnergy: 1, MinBandwidth: 1, ResourceWalletIndex: 1000, BandwidthStrategy: "topup", BandwidthTopupTRX: "2"},
		Energy:    config.Energy{PollTimeout: time.Minute}, Withdrawal: config.Withdrawal{Enabled: true, DailyLimitUSD: "100", FeeLimitTRX: 100, Expiration: time.Minute}}
	if _, _, err := database.CreateWithdrawal(ctx, store.CreateWithdrawal{IdempotencyKey: "one", FromAddress: source.Address,
		ToAddress: destination.Address, Asset: "USDT", AmountRaw: "1000000", AmountUSD: "1", DailyLimitUSD: "100",
		RequestedBy: "test", IP: "127.0.0.1", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	reader := &fakeReader{transaction: json.RawMessage(`{}`)}
	broadcast := &fakeBroadcast{}
	signer := &countingSigner{wallet: wallet}
	engine, err := New(reader, fakeSolidity{body: json.RawMessage(`{"id":"solid","fee":10,"blockNumber":2,"blockTimeStamp":2000,"receipt":{"energy_usage_total":3},"result":"SUCCESS"}`)},
		broadcast, database, signer, nil, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return engine, database, signer, reader, broadcast, func() { wallet.Destroy(); _ = database.Close() }
}

func onlyWithdrawal(t *testing.T, database *store.Store) store.Withdrawal {
	t.Helper()
	items, err := database.ListWithdrawals(context.Background(), "", 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("withdrawals=%d err=%v", len(items), err)
	}
	return items[0]
}

func referenceBlock() json.RawMessage {
	return json.RawMessage(`{"blockID":"0000000000000001d698d4192c56cb6be724a558448e2684802de4d6cd8690dc","block_header":{"raw_data":{"timestamp":1700000000000,"txTrieRoot":"0000000000000000000000000000000000000000000000000000000000000000","parentHash":"0000000000000000d698d4192c56cb6be724a558448e2684802de4d6cd8690dc","number":1,"witness_address":"41f24cbabb5cf3e639e1869a5cc6be49614f8e6a14","version":9}}}`)
}

func mustReference(t *testing.T) store.ReferenceBlock {
	t.Helper()
	reference, err := parseReferenceBlock(referenceBlock())
	if err != nil {
		t.Fatal(err)
	}
	return reference
}
