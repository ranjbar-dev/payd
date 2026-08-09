// Package withdraw implements the crash-safe W-008 withdrawal state machine.
package withdraw

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	tronpb "github.com/ranjbar-dev/hd-wallet/txproto/tron"
	"google.golang.org/protobuf/proto"

	"payd/internal/chain"
	"payd/internal/config"
	"payd/internal/energy"
	"payd/internal/store"
)

const (
	tickInterval = 2 * time.Second
	lookupEvery  = 15 * time.Second
)

type Reader interface {
	GetNowBlock(context.Context) (json.RawMessage, error)
	GetTransactionByID(context.Context, string) (json.RawMessage, error)
	GetAccount(context.Context, string) (json.RawMessage, error)
	GetAccountResource(context.Context, string) (json.RawMessage, error)
	DelegateResource(context.Context, string, string, string, int64) (json.RawMessage, error)
	GetCanDelegatedMaxSize(context.Context, string, string) (json.RawMessage, error)
}

type SolidityReader interface {
	GetTransactionInfoByID(context.Context, string) (json.RawMessage, error)
}

type Broadcaster interface {
	Send(context.Context, json.RawMessage) (chain.Response, error)
}

type Signer interface {
	SignTransaction(hdwallet.Chain, uint32, proto.Message) (proto.Message, error)
}

type Engine struct {
	reader      Reader
	solidity    SolidityReader
	broadcaster Broadcaster
	store       *store.Store
	signer      Signer
	provider    energy.Provider
	logger      *slog.Logger
	now         func() time.Time

	mu                 sync.RWMutex
	config             config.Config
	events             store.EventConfig
	afterAttempt       func()
	circuitUntil       time.Time
	nextBalanceCheck   time.Time
	nextEnergyPoll     map[string]time.Time
	providerBalanceTRX string
	providerBalanceLow bool
}

func New(reader Reader, solidity SolidityReader, broadcaster Broadcaster, database *store.Store, signer Signer, provider energy.Provider, cfg config.Config, logger *slog.Logger) (*Engine, error) {
	if reader == nil || solidity == nil || broadcaster == nil || database == nil || signer == nil {
		return nil, errors.New("withdrawal engine requires chain clients, store, and signer")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{reader: reader, solidity: solidity, broadcaster: broadcaster, store: database, signer: signer,
		provider: provider, logger: logger, now: time.Now, config: cfg, events: store.NewEventConfig(cfg.IPN, cfg.Assets),
		nextEnergyPoll: make(map[string]time.Time)}, nil
}

func (e *Engine) UpdateConfig(cfg config.Config) {
	e.mu.Lock()
	e.config, e.events = cfg, store.NewEventConfig(cfg.IPN, cfg.Assets)
	e.mu.Unlock()
}

func (e *Engine) UpdateProvider(provider energy.Provider, cfg config.Config) {
	e.mu.Lock()
	e.provider, e.config, e.events = provider, cfg, store.NewEventConfig(cfg.IPN, cfg.Assets)
	e.circuitUntil, e.nextBalanceCheck = time.Time{}, time.Time{}
	clear(e.nextEnergyPoll)
	e.mu.Unlock()
}

// ProviderBalanceMetric exposes ENR-011 state for the P14 metrics endpoint.
func (e *Engine) ProviderBalanceMetric() (trx string, low bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.providerBalanceTRX, e.providerBalanceLow
}

// EstimateResources performs the resource tier selection without persisting, signing, or broadcasting (API-032).
func (e *Engine) EstimateResources(ctx context.Context, address, assetKind string) (energy.ResourceEstimate, error) {
	if assetKind == "native" {
		return energy.ResourceEstimate{EnergySource: "existing", TRXCost: "0"}, nil
	}
	e.mu.RLock()
	cfg, provider := e.config, e.provider
	e.mu.RUnlock()
	body, err := e.reader.GetAccountResource(ctx, address)
	if err != nil {
		return energy.ResourceEstimate{}, err
	}
	resources, err := parseResources(body)
	if err != nil {
		return energy.ResourceEstimate{}, err
	}
	missing := cfg.Resources.MinEnergy - resources.energy
	if missing <= 0 {
		return energy.ResourceEstimate{EnergySource: "existing", TRXCost: "0"}, nil
	}
	if cfg.Energy.Enabled && provider != nil && e.providerEnabled() {
		quote, quoteErr := provider.Quote(address, "ENERGY", cfg.Energy.RentAmount, cfg.Energy.RentDuration)
		if quoteErr == nil {
			price, priceOK := new(big.Rat).SetString(quote.PriceTRX)
			maximum, maximumOK := new(big.Rat).SetString(cfg.Energy.MaxPriceTRX)
			if priceOK && maximumOK && price.Sign() >= 0 && price.Cmp(maximum) <= 0 {
				return energy.ResourceEstimate{EnergySource: "rented", TRXCost: quote.PriceTRX}, nil
			}
		}
	}
	amount, ratioErr := delegationAmount("ENERGY", missing, resources)
	if ratioErr == nil {
		_, owner, walletErr := e.store.ResourceWallet(ctx, cfg.Resources.ResourceWalletIndex)
		if walletErr == nil {
			maximumRaw, maximumErr := e.reader.GetCanDelegatedMaxSize(ctx, owner, "ENERGY")
			var maximum struct {
				Size int64 `json:"max_size"`
			}
			if maximumErr == nil && json.Unmarshal(maximumRaw, &maximum) == nil && maximum.Size >= amount {
				return energy.ResourceEstimate{EnergySource: "self_delegated", TRXCost: "0"}, nil
			}
		}
	}
	if !cfg.Energy.FallbackToBurn {
		return energy.ResourceEstimate{BlockedBy: "energy_unavailable", TRXCost: "0"}, nil
	}
	params, err := e.store.LoadChainParameters(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return energy.ResourceEstimate{BlockedBy: "chain_parameters_unavailable", TRXCost: "0"}, nil
	}
	if err != nil {
		return energy.ResourceEstimate{}, err
	}
	cost := energy.BurnCostSun(missing, params.EnergyFee)
	ceiling, ceilingErr := store.ParseUnits(cfg.Energy.MaxBurnTRX, 6)
	if ceilingErr != nil || cost.Cmp(ceiling) > 0 {
		return energy.ResourceEstimate{BlockedBy: "energy_burn_limit", TRXCost: "0"}, nil
	}
	formatted, err := store.FormatUnits(cost.String(), 6)
	if err != nil {
		return energy.ResourceEstimate{}, err
	}
	return energy.ResourceEstimate{EnergySource: "burned", TRXCost: formatted}, nil
}

// DelegateResources converts requested ENERGY/BANDWIDTH units using the live network ratio,
// then enters the same durable single-broadcast path used by withdrawal resource sourcing (RES-010..013).
func (e *Engine) DelegateResources(ctx context.Context, address, resourceType string, requestedUnits int64, actor, ip string) (store.ResourceGrant, error) {
	if resourceType != "ENERGY" && resourceType != "BANDWIDTH" {
		return store.ResourceGrant{}, errors.New("resource_type must be ENERGY or BANDWIDTH")
	}
	if requestedUnits <= 0 {
		return store.ResourceGrant{}, errors.New("amount must be positive")
	}
	if _, err := e.store.WalletAddress(ctx, address); err != nil {
		return store.ResourceGrant{}, err
	}
	body, err := e.reader.GetAccountResource(ctx, address)
	if err != nil {
		return store.ResourceGrant{}, err
	}
	resources, err := parseResources(body)
	if err != nil {
		return store.ResourceGrant{}, err
	}
	amountSun, err := delegationAmount(resourceType, requestedUnits, resources)
	if err != nil {
		return store.ResourceGrant{}, err
	}
	grant, err := e.store.CreateManualResourceGrant(ctx, address, resourceType, fmt.Sprint(amountSun), requestedUnits, actor, ip, e.now())
	if err != nil {
		return store.ResourceGrant{}, err
	}
	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()
	if _, err := e.startDelegation(ctx, grant, cfg); err != nil {
		return store.ResourceGrant{}, err
	}
	return e.store.ResourceGrant(ctx, grant.ID)
}

func (e *Engine) Run(ctx context.Context) {
	// WDR-019a: forced startup reconciliation must succeed before the first claim.
	for ctx.Err() == nil {
		if err := e.recover(ctx, true); err == nil {
			break
		} else {
			e.logger.Error("withdrawal startup recovery", "error", err)
		}
		timer := time.NewTimer(tickInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	if ctx.Err() != nil {
		return
	}
	for ctx.Err() == nil {
		if err := e.Tick(ctx); err != nil && ctx.Err() == nil {
			e.logger.Error("withdrawal tick", "error", err)
		}
		timer := time.NewTimer(tickInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (e *Engine) Tick(ctx context.Context) error {
	if err := e.recover(ctx, false); err != nil {
		return err
	}
	if err := e.checkProviderBalance(ctx); err != nil {
		return err
	}
	e.mu.RLock()
	enabled := e.config.Withdrawal.Enabled
	e.mu.RUnlock()
	if !enabled {
		return nil
	}
	awaiting, err := e.store.AwaitingResourceWithdrawals(ctx)
	if err != nil {
		return err
	}
	for _, withdrawal := range awaiting {
		if err := e.acquireAndSend(ctx, withdrawal); err != nil {
			return err
		}
	}
	w, found, err := e.store.NextRequestedWithdrawal(ctx)
	if err != nil || !found {
		return err
	}
	w, claimed, err := e.store.ClaimWithdrawal(ctx, w.ID)
	if err != nil || !claimed {
		return err
	}
	return e.acquireAndSend(ctx, w)
}

func (e *Engine) recover(ctx context.Context, force bool) error {
	now := e.now()
	e.mu.RLock()
	energyTimeout := e.config.Energy.PollTimeout
	e.mu.RUnlock()
	manualGrants, err := e.store.ManualResourceGrantCandidates(ctx)
	if err != nil {
		return err
	}
	for _, grant := range manualGrants {
		e.mu.RLock()
		cfg := e.config
		e.mu.RUnlock()
		var err error
		if grant.Status == "requested" {
			_, err = e.startDelegation(ctx, grant, cfg)
		} else {
			_, err = e.reconcileResourceGrant(ctx, grant, cfg)
		}
		if err != nil {
			return err
		}
	}
	candidates, err := e.store.WithdrawalRecoveryCandidates(ctx, now, energyTimeout, force)
	if err != nil {
		return err
	}
	for _, w := range candidates {
		if w.TxID == "" {
			if w.Status == "signing" { // WDR-018b: never infer that pre-txid signing was harmless.
				if err := e.store.WithdrawalNeedsOperator(ctx, w.ID, "signing state has no persisted txid", now, e.eventConfig()); err != nil {
					return err
				}
				continue
			}
			if w.Status == "awaiting_energy" {
				if !force {
					continue
				}
				body, err := e.reader.GetAccountResource(ctx, w.FromAddress)
				if err != nil {
					return err
				}
				resources, err := parseResources(body)
				if err != nil {
					return err
				}
				e.mu.RLock()
				minimum := e.config.Resources.MinEnergy
				e.mu.RUnlock()
				if err := e.store.ReconcileWithdrawalEnergy(ctx, w.ID, resources.energy >= minimum, now); err != nil { // WDR-019
					return err
				}
			}
			if w.Status == "awaiting_resources" || w.Status == "awaiting_energy" { // WDR-019
				if err := e.acquireAndSend(ctx, w); err != nil {
					return err
				}
			}
			continue
		}
		if !force {
			if w.Status == "signing" && w.BroadcastAttemptedAt != nil && now.Unix()-*w.BroadcastAttemptedAt < 60 {
				continue
			}
			if w.LastLookupAt != nil && now.Unix()-*w.LastLookupAt < int64(lookupEvery/time.Second) {
				continue
			}
		}
		if err := e.resolve(ctx, w, ""); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) acquireAndSend(ctx context.Context, w store.Withdrawal) error {
	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()
	if w.Status == "awaiting_energy" {
		purchase, found, err := e.store.EnergyPurchaseForWithdrawal(ctx, w.ID)
		if err != nil {
			return err
		}
		if found && purchase.Status == "purchased" && !e.energyPollDue(purchase, cfg.Energy.PollInterval) {
			return nil
		}
	}
	body, err := e.reader.GetAccountResource(ctx, w.FromAddress)
	if err != nil {
		return err
	}
	resources, err := parseResources(body)
	if err != nil {
		return err
	}
	asset, found := configuredAsset(cfg.Assets, w.Asset)
	if !found {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "withdrawal asset is no longer configured", e.now(), e.eventConfig())
	}
	energySource := w.EnergySource
	if asset.Kind == "trc20" {
		purchase, purchaseFound, err := e.store.EnergyPurchaseForWithdrawal(ctx, w.ID)
		if err != nil {
			return err
		}
		if resources.energy >= cfg.Resources.MinEnergy && purchaseFound && purchase.Status == "purchased" {
			ready, err := e.finishRental(ctx, purchase, cfg)
			if err != nil || !ready {
				return err
			}
			energySource = "rented"
		} else if purchaseFound && purchase.Status == "delegated" {
			energySource = "rented"
		}
		if resources.energy < cfg.Resources.MinEnergy && energySource != "burned" {
			if w.Status != "awaiting_energy" {
				changed, err := e.store.SetWithdrawalResourceState(ctx, w.ID, w.Status, "awaiting_energy", "", "")
				if err != nil || !changed {
					return err
				}
				w.Status = "awaiting_energy"
			}
			ready, source, err := e.sourceEnergy(ctx, w, resources, cfg)
			if err != nil || !ready {
				return err
			}
			energySource = source
		} else if energySource == "" {
			energySource = "existing"
			if grant, found, err := e.store.ResourceGrantForWithdrawal(ctx, w.ID, "ENERGY"); err != nil {
				return err
			} else if found && grant.Status != "failed" {
				if grant.TxID != "" && grant.FeeRaw == "" {
					pending, err := e.reconcileResourceGrant(ctx, grant, cfg)
					if err != nil || pending {
						return err
					}
				}
				energySource = "self_delegated"
				if err := e.store.ConfirmResourceGrant(ctx, grant.ID, e.now()); err != nil {
					return err
				}
			}
		}
	}
	w.EnergySource = energySource

	// WDR-009h / RES-006: this is deliberately the last gate before signing for both asset kinds.
	bandwidthGrant, bandwidthGrantFound, err := e.store.ResourceGrantForWithdrawal(ctx, w.ID, "BANDWIDTH")
	if err != nil {
		return err
	}
	bandwidthSource := "free"
	if resources.bandwidth < cfg.Resources.MinBandwidth {
		if e.canBurnBandwidth(ctx, w, asset, cfg, bandwidthGrant, bandwidthGrantFound) {
			bandwidthSource = "burned"
			if bandwidthGrantFound && bandwidthGrant.Status != "failed" && bandwidthGrant.Source == "topup" {
				bandwidthSource = "topup"
				if err := e.store.ConfirmResourceGrant(ctx, bandwidthGrant.ID, e.now()); err != nil {
					return err
				}
			}
		} else {
			if w.Status != "awaiting_resources" {
				changed, err := e.store.SetWithdrawalResourceState(ctx, w.ID, w.Status, "awaiting_resources", energySource, "")
				if err != nil || !changed {
					return err
				}
				w.Status = "awaiting_resources"
			}
			pending, source, err := e.sourceBandwidth(ctx, w, resources, cfg)
			if err != nil {
				return err
			}
			if _, err = e.store.SetWithdrawalResourceState(ctx, w.ID, w.Status, w.Status, energySource, source); err != nil {
				return err
			}
			if pending {
				return nil
			}
			return e.store.FailWithdrawalResources(ctx, w.ID, "bandwidth_unavailable", e.now(), e.eventConfig()) // RES-009
		}
	} else if bandwidthGrantFound && bandwidthGrant.Status != "failed" {
		if bandwidthGrant.TxID != "" && bandwidthGrant.FeeRaw == "" {
			pending, err := e.reconcileResourceGrant(ctx, bandwidthGrant, cfg)
			if err != nil || pending {
				return err
			}
		}
		if err := e.store.ConfirmResourceGrant(ctx, bandwidthGrant.ID, e.now()); err != nil {
			return err
		}
		if bandwidthGrant.Source == "topup" {
			bandwidthSource = "topup"
		} else {
			bandwidthSource = "delegated"
		}
	}
	changed, err := e.store.SetWithdrawalResourceState(ctx, w.ID, w.Status, "signing", energySource, bandwidthSource)
	if err != nil || !changed {
		return err
	}
	w.Status, w.EnergySource, w.BandwidthSource = "signing", energySource, bandwidthSource
	return e.signAndBroadcast(ctx, w, asset, cfg)
}

func (e *Engine) signAndBroadcast(ctx context.Context, w store.Withdrawal, asset config.Asset, cfg config.Config) error {
	reference, err := e.referenceBlock(ctx)
	if err != nil {
		return err
	}
	input, err := signingInput(w, asset, reference, cfg.Withdrawal)
	if err != nil {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, err.Error(), e.now(), e.eventConfig())
	}
	out, err := e.signer.SignTransaction(hdwallet.TRX, w.HDIndex, input)
	if err != nil {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "signing failed: "+err.Error(), e.now(), e.eventConfig())
	}
	txid, err := hdwallet.TransactionID(out)
	if err != nil {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "txid extraction failed: "+err.Error(), e.now(), e.eventConfig())
	}
	payload, err := hdwallet.BroadcastPayload(hdwallet.TRX, out)
	if err != nil {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "broadcast payload failed: "+err.Error(), e.now(), e.eventConfig())
	}
	now := e.now()
	expires := reference.TimestampMS/1000 + int64(cfg.Withdrawal.Expiration/time.Second)
	if err := e.store.PersistWithdrawalAttempt(ctx, w.ID, txid, now, expires); err != nil { // WDR-015 happens before Send.
		return err
	}
	if e.afterAttempt != nil {
		e.afterAttempt()
	}
	response, sendErr := e.broadcaster.Send(ctx, json.RawMessage(payload)) // WDR-014a: this call appears once in the state machine.
	rawResponse := string(response.Body)
	if rawResponse == "" && sendErr != nil {
		rawResponse = sendErr.Error()
	}
	w.TxID, w.ExpirationAt = txid, &expires
	classification, code := classifyBroadcast(response, sendErr)
	if classification != "deterministic" {
		return e.store.RecordWithdrawalBroadcast(ctx, w.ID, rawResponse, now) // WDR-017(a,c): ambiguity means broadcast.
	}
	if err := e.store.RecordWithdrawalBroadcast(ctx, w.ID, rawResponse, now); err != nil {
		return err
	}
	return e.resolve(ctx, w, code)
}

func (e *Engine) referenceBlock(ctx context.Context) (store.ReferenceBlock, error) {
	reference, found := e.store.LatestReferenceBlock()
	if found {
		if cursor, ok, err := e.store.Cursor(ctx); err != nil {
			return store.ReferenceBlock{}, err
		} else if ok && cursor.LastHeight-reference.Height > 10 {
			found = false
		}
	}
	if found {
		return reference, nil
	}
	raw, err := e.reader.GetNowBlock(ctx)
	if err != nil {
		return store.ReferenceBlock{}, err
	}
	return parseReferenceBlock(raw)
}

func (e *Engine) canBurnBandwidth(ctx context.Context, w store.Withdrawal, asset config.Asset, cfg config.Config,
	grant store.ResourceGrant, grantFound bool) bool {
	params, err := e.store.LoadChainParameters(ctx)
	if err != nil {
		return false
	}
	balance, err := e.store.BalanceForWithdrawal(ctx, w.FromAddress, "TRX")
	if err != nil {
		return false
	}
	available, ok := new(big.Int).SetString(balance.ConfirmedRaw, 10)
	if !ok {
		return false
	}
	// A solid top-up is owned-to-owned, so use the confirmed chain balance for
	// RES-006 instead of waiting for the general payment ledger to mirror it.
	if grantFound && grant.Source == "topup" && grant.FeeRaw != "" {
		body, readErr := e.reader.GetAccount(ctx, w.FromAddress)
		if readErr != nil {
			return false
		}
		var account struct {
			Balance json.Number `json:"balance"`
		}
		if json.Unmarshal(body, &account) != nil {
			return false
		}
		chainBalance, valid := new(big.Int).SetString(string(account.Balance), 10)
		if !valid || chainBalance.Sign() < 0 {
			return false
		}
		available = chainBalance
	}
	if asset.Kind == "native" {
		amount, valid := new(big.Int).SetString(w.AmountRaw, 10)
		if !valid {
			return false
		}
		available.Sub(available, amount)
	}
	if w.EnergySource == "burned" {
		available.Sub(available, energy.BurnCostSun(cfg.Resources.MinEnergy, params.EnergyFee))
	}
	cost := new(big.Int).Mul(big.NewInt(cfg.Resources.MinBandwidth), big.NewInt(params.TransactionFee))
	return available.Cmp(cost) >= 0
}

func (e *Engine) sourceEnergy(ctx context.Context, w store.Withdrawal, resources resourceState, cfg config.Config) (bool, string, error) {
	ready, pending, err := e.tryRental(ctx, w, cfg)
	if err != nil || pending {
		return false, "", err
	}
	if ready {
		return true, "rented", nil
	}
	pending, err = e.ensureDelegation(ctx, w, "ENERGY", cfg.Resources.MinEnergy-resources.energy, resources, cfg)
	if err != nil || pending {
		return false, "", err
	}
	if !cfg.Energy.FallbackToBurn {
		return false, "", e.store.FailWithdrawalResources(ctx, w.ID, "self_delegation_unavailable", e.now(), e.eventConfig()) // WDR-009e
	}
	params, err := e.store.LoadChainParameters(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = e.store.SetWithdrawalResourceState(ctx, w.ID, w.Status, "awaiting_resources", "", "") // RES-022
		return false, "", err
	}
	if err != nil {
		return false, "", err
	}
	cost := energy.BurnCostSun(cfg.Resources.MinEnergy-resources.energy, params.EnergyFee)
	ceiling, err := store.ParseUnits(cfg.Energy.MaxBurnTRX, 6)
	if err != nil || cost.Cmp(ceiling) > 0 {
		return false, "", e.store.FailWithdrawalResources(ctx, w.ID, "energy_burn_limit", e.now(), e.eventConfig())
	}
	balance, err := e.store.BalanceForWithdrawal(ctx, w.FromAddress, "TRX")
	available, ok := new(big.Int).SetString(balance.ConfirmedRaw, 10)
	if err != nil || !ok || available.Cmp(cost) < 0 {
		return false, "", e.store.FailWithdrawalResources(ctx, w.ID, "energy_burn_unavailable", e.now(), e.eventConfig()) // WDR-009e
	}
	burn, _ := store.FormatUnits(cost.String(), 6)
	e.logger.Warn("self-delegation unavailable; using TRX burn", "withdrawal_id", w.ID, "estimated_burn_trx", burn) // RES-015
	return true, "burned", nil
}

func (e *Engine) tryRental(ctx context.Context, w store.Withdrawal, cfg config.Config) (ready, pending bool, err error) {
	purchase, found, err := e.store.EnergyPurchaseForWithdrawal(ctx, w.ID)
	if err != nil {
		return false, false, err
	}
	provider := e.currentProvider()
	if found {
		switch purchase.Status {
		case "delegated":
			return true, false, nil
		case "failed", "expired":
			return false, false, nil
		case "purchased":
			if e.purchaseTimedOut(purchase, cfg.Energy.PollTimeout) {
				if err := e.failRental(ctx, purchase, "delegated energy did not arrive before poll timeout"); err != nil {
					return false, false, err
				}
				return false, false, nil
			}
			if provider == nil {
				e.delayEnergyPoll(purchase.ID, cfg.Energy.PollInterval)
				return false, true, nil
			}
			status, statusErr := provider.Status(purchase.ProviderOrderID)
			if statusErr != nil {
				e.logger.Warn("energy provider status failed; waiting for on-chain arrival", "purchase_id", purchase.ID, "error", statusErr)
				e.delayEnergyPoll(purchase.ID, cfg.Energy.PollInterval)
				return false, true, nil
			}
			if err := e.store.RecordEnergyPurchaseOrder(ctx, purchase.ID, "", status.ActualTRX, status.DelegationTxID); err != nil {
				return false, false, err
			}
			if status.State == "failed" {
				if err := e.failRental(ctx, purchase, "provider reported failed"); err != nil {
					return false, false, err
				}
				return false, false, nil
			}
			e.delayEnergyPoll(purchase.ID, cfg.Energy.PollInterval)
			return false, true, nil
		}
	}
	if !cfg.Energy.Enabled || provider == nil || !e.providerEnabled() {
		return false, false, nil
	}
	quote, quoteErr := provider.Quote(w.FromAddress, "ENERGY", cfg.Energy.RentAmount, cfg.Energy.RentDuration) // ENR-001/003/015
	if quoteErr != nil {
		return false, false, e.recordProviderFailure(ctx, cfg.Energy.Provider, quoteErr)
	}
	quote.Receiver, quote.ResourceType, quote.Amount, quote.Duration = w.FromAddress, "ENERGY", cfg.Energy.RentAmount, cfg.Energy.RentDuration
	price, priceOK := new(big.Rat).SetString(quote.PriceTRX)
	maximum, maximumOK := new(big.Rat).SetString(cfg.Energy.MaxPriceTRX)
	if !priceOK || !maximumOK || price.Sign() < 0 {
		return false, false, errors.New("provider returned an invalid quote")
	}
	purchase, err = e.store.CreateEnergyPurchase(ctx, cfg.Energy.Provider, w.ID, w.AddressID, w.FromAddress,
		"ENERGY", cfg.Energy.RentAmount, cfg.Energy.RentDuration, quote.PriceTRX, e.now())
	if err != nil {
		return false, false, err
	}
	if price.Cmp(maximum) > 0 {
		if err := e.store.RejectEnergyQuote(ctx, purchase.ID, "quote exceeds energy.max_price_trx"); err != nil {
			return false, false, err
		}
		return false, false, nil // ENR-004: no Purchase call; tier 2 follows immediately.
	}
	externalID := "external:" + purchase.ID
	if err := e.store.MarkEnergyPurchaseAttempt(ctx, purchase.ID, externalID); err != nil {
		return false, false, err
	}
	quote.Reference = purchase.ID
	order, purchaseErr := provider.Purchase(quote) // ENR-013: Quote contains no wallet key or signing authority.
	if purchaseErr != nil {
		// The outcome is ambiguous. The durable external ID is reconciled; Purchase is never repeated.
		e.logger.Warn("energy purchase outcome ambiguous; polling by external id", "purchase_id", purchase.ID, "error", purchaseErr)
		e.delayEnergyPoll(purchase.ID, cfg.Energy.PollInterval)
		return false, true, nil
	}
	if err := e.store.RecordEnergyPurchaseOrder(ctx, purchase.ID, order.ID, order.ActualTRX, order.DelegationTxID); err != nil {
		return false, false, err
	}
	if order.State == "failed" {
		if err := e.failRental(ctx, purchase, "provider reported failed"); err != nil {
			return false, false, err
		}
		return false, false, nil
	}
	e.delayEnergyPoll(purchase.ID, cfg.Energy.PollInterval)
	return false, true, nil
}

func (e *Engine) finishRental(ctx context.Context, purchase store.EnergyPurchase, cfg config.Config) (bool, error) {
	provider := e.currentProvider()
	actual, txid := purchase.ActualTRX, purchase.DelegationTxID
	if provider != nil {
		status, err := provider.Status(purchase.ProviderOrderID)
		if err == nil {
			if status.ActualTRX != "" {
				actual = status.ActualTRX
			}
			if status.DelegationTxID != "" {
				txid = status.DelegationTxID
			}
			if status.State != "success" && !e.purchaseTimedOut(purchase, cfg.Energy.PollTimeout) {
				e.delayEnergyPoll(purchase.ID, cfg.Energy.PollInterval)
				return false, nil
			}
		} else if !e.purchaseTimedOut(purchase, cfg.Energy.PollTimeout) {
			e.delayEnergyPoll(purchase.ID, cfg.Energy.PollInterval)
			return false, nil
		}
	}
	if actual == "" {
		actual = purchase.QuotedTRX
	}
	if err := e.store.CompleteEnergyPurchase(ctx, purchase.ID, actual, txid, e.now()); err != nil {
		return false, err
	}
	if err := e.store.ResetEnergyProviderFailures(ctx, purchase.Provider); err != nil {
		return false, err
	}
	e.mu.Lock()
	delete(e.nextEnergyPoll, purchase.ID)
	e.circuitUntil = time.Time{}
	e.mu.Unlock()
	return true, nil
}

func (e *Engine) failRental(ctx context.Context, purchase store.EnergyPurchase, reason string) error {
	if err := e.store.FailEnergyPurchase(ctx, purchase.ID, reason, e.now(), e.eventConfig()); err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.nextEnergyPoll, purchase.ID)
	e.mu.Unlock()
	return e.recordProviderFailure(ctx, purchase.Provider, errors.New(reason))
}

func (e *Engine) recordProviderFailure(ctx context.Context, provider string, providerErr error) error {
	failures, err := e.store.RecordEnergyProviderFailure(ctx, provider, providerErr.Error())
	if err != nil {
		return err
	}
	if failures >= 5 {
		e.mu.Lock()
		e.circuitUntil = e.now().Add(10 * time.Minute)
		e.mu.Unlock()
		e.logger.Warn("energy provider circuit opened", "provider", provider, "failures", failures, "duration", 10*time.Minute) // ENR-012
	}
	return nil
}

func (e *Engine) providerEnabled() bool {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.provider == nil {
		return false
	}
	if !e.circuitUntil.IsZero() && now.Before(e.circuitUntil) {
		return false
	}
	if !e.circuitUntil.IsZero() {
		e.circuitUntil = time.Time{}
	}
	return true
}

func (e *Engine) currentProvider() energy.Provider {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.provider
}

func (e *Engine) energyPollDue(purchase store.EnergyPurchase, interval time.Duration) bool {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if due := e.nextEnergyPoll[purchase.ID]; !due.IsZero() && now.Before(due) {
		return false
	}
	e.nextEnergyPoll[purchase.ID] = now.Add(interval)
	return true
}

func (e *Engine) delayEnergyPoll(purchaseID string, interval time.Duration) {
	e.mu.Lock()
	e.nextEnergyPoll[purchaseID] = e.now().Add(interval)
	e.mu.Unlock()
}

func (e *Engine) purchaseTimedOut(purchase store.EnergyPurchase, timeout time.Duration) bool {
	return timeout > 0 && !e.now().Before(time.Unix(purchase.CreatedAt, 0).Add(timeout))
}

func (e *Engine) checkProviderBalance(ctx context.Context) error {
	now := e.now()
	e.mu.Lock()
	provider, cfg := e.provider, e.config
	if provider == nil || !cfg.Energy.Enabled || (!e.nextBalanceCheck.IsZero() && now.Before(e.nextBalanceCheck)) {
		e.mu.Unlock()
		return nil
	}
	e.nextBalanceCheck = now.Add(15 * time.Minute)
	e.mu.Unlock()
	balance, err := provider.Balance()
	if err != nil {
		if storeErr := e.store.RecordEnergyProviderBalanceError(ctx, cfg.Energy.Provider, now, err); storeErr != nil {
			return storeErr
		}
		e.logger.Warn("energy provider balance check failed", "provider", cfg.Energy.Provider, "error", err)
		return nil
	}
	value, valueOK := new(big.Rat).SetString(balance)
	warn, warnOK := new(big.Rat).SetString(cfg.Energy.BalanceWarnTRX)
	if !valueOK || !warnOK {
		return errors.New("energy provider returned an invalid balance")
	}
	low := value.Cmp(warn) < 0
	if err := e.store.RecordEnergyProviderBalance(ctx, cfg.Energy.Provider, balance, cfg.Energy.BalanceWarnTRX, now, e.eventConfig()); err != nil {
		return err
	}
	e.mu.Lock()
	e.providerBalanceTRX, e.providerBalanceLow = balance, low
	e.mu.Unlock()
	return nil
}

func (e *Engine) sourceBandwidth(ctx context.Context, w store.Withdrawal, resources resourceState, cfg config.Config) (bool, string, error) {
	if cfg.Resources.BandwidthStrategy == "delegate" {
		pending, err := e.ensureDelegation(ctx, w, "BANDWIDTH", cfg.Resources.MinBandwidth-resources.bandwidth, resources, cfg)
		return pending, "delegated", err
	}
	pending, err := e.ensureTopup(ctx, w, cfg)
	return pending, "topup", err
}

func (e *Engine) ensureDelegation(ctx context.Context, w store.Withdrawal, resourceType string, missing int64, resources resourceState, cfg config.Config) (bool, error) {
	grant, found, err := e.store.ResourceGrantForWithdrawal(ctx, w.ID, resourceType)
	if err != nil {
		return false, err
	}
	if !found {
		amount, err := delegationAmount(resourceType, missing, resources)
		if err != nil {
			return false, nil
		}
		grant, err = e.store.CreateResourceGrant(ctx, w.ID, w.AddressID, w.FromAddress, resourceType, "self_delegated", fmt.Sprint(amount), e.now())
		if err != nil {
			return false, err
		}
	}
	if grant.Status == "requested" {
		return e.startDelegation(ctx, grant, cfg)
	}
	return e.reconcileResourceGrant(ctx, grant, cfg)
}

func (e *Engine) startDelegation(ctx context.Context, grant store.ResourceGrant, cfg config.Config) (bool, error) {
	_, owner, err := e.store.ResourceWallet(ctx, cfg.Resources.ResourceWalletIndex)
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, "", err.Error())
		return false, nil
	}
	amount, ok := new(big.Int).SetString(grant.AmountSun, 10)
	if !ok || !amount.IsInt64() {
		_ = e.store.FailResourceGrant(ctx, grant.ID, "", "invalid delegation amount")
		return false, nil
	}
	maximumRaw, err := e.reader.GetCanDelegatedMaxSize(ctx, owner, grant.ResourceType)
	var maximum struct {
		Size int64 `json:"max_size"`
	}
	if err != nil || json.Unmarshal(maximumRaw, &maximum) != nil || maximum.Size < amount.Int64() {
		_ = e.store.FailResourceGrant(ctx, grant.ID, string(maximumRaw), "insufficient delegatable resource")
		return false, nil
	}
	unsigned, err := e.reader.DelegateResource(ctx, owner, grant.ReceiverAddress, grant.ResourceType, amount.Int64()) // RES-010
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, "", err.Error())
		return false, nil
	}
	txid, _, expiration, err := unsignedTransaction(unsigned)
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, string(unsigned), err.Error())
		return false, nil
	}
	out, err := e.signer.SignTransaction(hdwallet.TRX, cfg.Resources.ResourceWalletIndex,
		&tronpb.SigningInput{RawJson: string(unsigned)}) // RES-011/012: preserving txID invokes hd-wallet's mismatch guard.
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, string(unsigned), err.Error())
		return false, nil
	}
	signedID, err := hdwallet.TransactionID(out)
	if err != nil || !strings.EqualFold(signedID, txid) {
		_ = e.store.FailResourceGrant(ctx, grant.ID, string(unsigned), "signed delegation txid mismatch")
		return false, nil
	}
	if expiration == 0 {
		expiration = e.now().Add(cfg.Withdrawal.Expiration).Unix()
	}
	return e.broadcastResourceGrant(ctx, grant, out, expiration)
}

func (e *Engine) ensureTopup(ctx context.Context, w store.Withdrawal, cfg config.Config) (bool, error) {
	grant, found, err := e.store.ResourceGrantForWithdrawal(ctx, w.ID, "BANDWIDTH")
	if err != nil {
		return false, err
	}
	if !found {
		amount, err := store.ParseUnits(cfg.Resources.BandwidthTopupTRX, 6)
		if err != nil || !amount.IsInt64() || amount.Sign() <= 0 {
			return false, nil
		}
		grant, err = e.store.CreateResourceGrant(ctx, w.ID, w.AddressID, w.FromAddress, "BANDWIDTH", "topup", amount.String(), e.now())
		if err != nil {
			return false, err
		}
	}
	if grant.Status != "requested" {
		return e.reconcileResourceGrant(ctx, grant, cfg)
	}
	_, owner, err := e.store.ResourceWallet(ctx, cfg.Resources.ResourceWalletIndex)
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, "", err.Error())
		return false, nil
	}
	reference, err := e.referenceBlock(ctx)
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, "", err.Error())
		return false, nil
	}
	input, err := signingInput(store.Withdrawal{FromAddress: owner, ToAddress: grant.ReceiverAddress, AmountRaw: grant.AmountSun},
		config.Asset{Kind: "native"}, reference, cfg.Withdrawal)
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, "", err.Error())
		return false, nil
	}
	out, err := e.signer.SignTransaction(hdwallet.TRX, cfg.Resources.ResourceWalletIndex, input)
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, "", err.Error())
		return false, nil
	}
	expiration := reference.TimestampMS/1000 + int64(cfg.Withdrawal.Expiration/time.Second)
	return e.broadcastResourceGrant(ctx, grant, out, expiration)
}

func (e *Engine) broadcastResourceGrant(ctx context.Context, grant store.ResourceGrant, out proto.Message, expiration int64) (bool, error) {
	txid, err := hdwallet.TransactionID(out)
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, "", err.Error())
		return false, nil
	}
	payload, err := hdwallet.BroadcastPayload(hdwallet.TRX, out)
	if err != nil {
		_ = e.store.FailResourceGrant(ctx, grant.ID, "", err.Error())
		return false, nil
	}
	now := e.now()
	if err := e.store.PersistResourceGrantAttempt(ctx, grant.ID, txid, now, expiration); err != nil {
		return false, err
	}
	if e.afterAttempt != nil {
		e.afterAttempt()
	}
	response, sendErr := e.broadcaster.Send(ctx, json.RawMessage(payload)) // RES-008/013: exactly one broadcast after durable attempt marker.
	rawResponse := string(response.Body)
	if rawResponse == "" && sendErr != nil {
		rawResponse = sendErr.Error()
	}
	if err := e.store.RecordResourceGrantBroadcast(ctx, grant.ID, rawResponse); err != nil {
		return false, err
	}
	classification, code := classifyBroadcast(response, sendErr)
	if classification != "deterministic" {
		return true, nil
	}
	body, lookupErr := e.reader.GetTransactionByID(ctx, txid)
	found, parseErr := transactionFound(body)
	if lookupErr != nil || parseErr != nil {
		return true, nil
	}
	if found {
		return true, nil
	}
	return false, e.store.FailResourceGrant(ctx, grant.ID, rawResponse, code)
}

func (e *Engine) reconcileResourceGrant(ctx context.Context, grant store.ResourceGrant, cfg config.Config) (bool, error) {
	if grant.Status == "failed" || grant.Status == "confirmed" {
		return false, nil
	}
	if grant.TxID == "" || grant.BroadcastAttemptedAt == nil {
		return false, nil
	}
	body, err := e.reader.GetTransactionByID(ctx, grant.TxID)
	if err != nil {
		return e.resourceGrantLookupFailure(ctx, grant, err)
	}
	found, err := transactionFound(body)
	if err != nil {
		return e.resourceGrantLookupFailure(ctx, grant, err)
	}
	if _, err := e.store.RecordResourceGrantLookup(ctx, grant.ID, nil); err != nil {
		return false, err
	}
	if found {
		receiptBody, receiptErr := e.solidity.GetTransactionInfoByID(ctx, grant.TxID)
		receipt, solid, parseErr := parseReceipt(receiptBody)
		if receiptErr != nil {
			return e.resourceGrantLookupFailure(ctx, grant, receiptErr)
		}
		if parseErr != nil {
			return e.resourceGrantLookupFailure(ctx, grant, parseErr)
		}
		if !solid {
			return true, nil
		}
		if err := e.store.RecordResourceGrantReceipt(ctx, grant.ID, receipt.FeeRaw, receipt.BlockHeight,
			receipt.BlockTimestamp, cfg.Resources.ResourceWalletIndex, e.now()); err != nil {
			return false, err
		}
		if _, err := e.store.RecordResourceGrantLookup(ctx, grant.ID, nil); err != nil {
			return false, err
		}
		if receipt.Result != "" && receipt.Result != "SUCCESS" {
			return false, e.store.FailResourceGrant(ctx, grant.ID, grant.BroadcastResponse, "solidified transaction result: "+receipt.Result)
		}
		if grant.WithdrawalID == "" {
			return false, e.store.ConfirmResourceGrant(ctx, grant.ID, e.now())
		}
		timeout := 5 * time.Minute
		if grant.ResourceType == "ENERGY" && cfg.Energy.PollTimeout > 0 {
			timeout = cfg.Energy.PollTimeout
		}
		if e.now().Unix()-grant.CreatedAt >= int64(timeout/time.Second) {
			return false, e.store.FailResourceGrant(ctx, grant.ID, grant.BroadcastResponse, "resource not visible after confirmed grant")
		}
		return true, nil
	}
	if grant.ExpirationAt == nil || e.now().Unix() < *grant.ExpirationAt {
		return true, nil
	}
	return false, e.store.FailResourceGrant(ctx, grant.ID, grant.BroadcastResponse, "transaction absent after expiration")
}

func (e *Engine) resourceGrantLookupFailure(ctx context.Context, grant store.ResourceGrant, lookupErr error) (bool, error) {
	failures, err := e.store.RecordResourceGrantLookup(ctx, grant.ID, lookupErr)
	if err != nil || failures < 10 {
		return true, err
	}
	if err := e.store.ResourceGrantNeedsOperator(ctx, grant.ID, lookupErr.Error()); err != nil {
		return false, err
	}
	if grant.WithdrawalID == "" {
		return false, nil
	}
	return false, e.store.WithdrawalNeedsOperator(ctx, grant.WithdrawalID, lookupErr.Error(), e.now(), e.eventConfig()) // WDR-000b
}

func (e *Engine) resolve(ctx context.Context, w store.Withdrawal, deterministicCode string) error {
	now := e.now()
	body, err := e.reader.GetTransactionByID(ctx, w.TxID)
	if err != nil {
		if deterministicCode != "" {
			return e.store.WithdrawalNeedsOperator(ctx, w.ID, err.Error(), now, e.eventConfig()) // WDR-022a
		}
		return e.store.RecordWithdrawalLookup(ctx, w.ID, false, err, now, e.eventConfig())
	}
	found, err := transactionFound(body)
	if err != nil {
		return e.store.RecordWithdrawalLookup(ctx, w.ID, false, err, now, e.eventConfig())
	}
	if !found {
		if deterministicCode != "" {
			return e.store.FailWithdrawalAbsent(ctx, w.ID, deterministicCode, now, e.eventConfig())
		}
		if w.ExpirationAt != nil && now.Unix() >= *w.ExpirationAt {
			return e.store.FailWithdrawalAbsent(ctx, w.ID, "transaction absent after expiration", now, e.eventConfig())
		}
		return e.store.RecordWithdrawalLookup(ctx, w.ID, false, nil, now, e.eventConfig())
	}
	if err := e.store.RecordWithdrawalLookup(ctx, w.ID, true, nil, now, e.eventConfig()); err != nil {
		return err
	}
	receiptBody, err := e.solidity.GetTransactionInfoByID(ctx, w.TxID)
	if err != nil {
		return nil
	}
	receipt, solid, err := parseReceipt(receiptBody)
	if err != nil || !solid {
		return err
	}
	if receipt.Result != "" && receipt.Result != "SUCCESS" {
		return e.store.WithdrawalNeedsOperator(ctx, w.ID, "solidified transaction result: "+receipt.Result, now, e.eventConfig())
	}
	return e.store.ConfirmWithdrawal(ctx, w.ID, receipt.FeeRaw, receipt.EnergyUsed, receipt.BlockHeight, receipt.BlockTimestamp, e.eventConfig(), now)
}

func (e *Engine) eventConfig() store.EventConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.events
}

func configuredAsset(assets []config.Asset, symbol string) (config.Asset, bool) {
	for _, asset := range assets {
		if asset.Symbol == symbol {
			return asset, true
		}
	}
	return config.Asset{}, false
}

type resourceState struct {
	energy, bandwidth                         int64
	totalEnergyLimit, totalEnergyWeight       int64
	totalBandwidthLimit, totalBandwidthWeight int64
}

func parseResources(body []byte) (resourceState, error) {
	var response struct {
		EnergyLimit, EnergyUsed, NetLimit, NetUsed, FreeNetLimit, FreeNetUsed int64
		TotalEnergyLimit, TotalEnergyCurrentLimit, TotalEnergyWeight          int64
		TotalNetLimit, TotalNetWeight                                         int64
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return resourceState{}, err
	}
	energyLimit := response.TotalEnergyLimit
	if energyLimit == 0 {
		energyLimit = response.TotalEnergyCurrentLimit
	}
	return resourceState{
		energy:           response.EnergyLimit - response.EnergyUsed,
		bandwidth:        response.NetLimit - response.NetUsed + response.FreeNetLimit - response.FreeNetUsed,
		totalEnergyLimit: energyLimit, totalEnergyWeight: response.TotalEnergyWeight,
		totalBandwidthLimit: response.TotalNetLimit, totalBandwidthWeight: response.TotalNetWeight,
	}, nil
}

func delegationAmount(resourceType string, missing int64, resources resourceState) (int64, error) {
	limit, weight := resources.totalBandwidthLimit, resources.totalBandwidthWeight
	if resourceType == "ENERGY" {
		limit, weight = resources.totalEnergyLimit, resources.totalEnergyWeight
	}
	if missing <= 0 || limit <= 0 || weight <= 0 {
		return 0, errors.New("resource delegation ratio is unavailable")
	}
	// Network weights are TRX while DelegateResource.balance is sun. Round up and obey the protocol's 1 TRX minimum.
	numerator := new(big.Int).Mul(big.NewInt(missing), big.NewInt(weight))
	numerator.Mul(numerator, big.NewInt(1_000_000))
	denominator := big.NewInt(limit)
	numerator.Add(numerator, new(big.Int).Sub(denominator, big.NewInt(1)))
	amount := numerator.Div(numerator, denominator)
	if amount.Cmp(big.NewInt(1_000_000)) < 0 {
		amount.SetInt64(1_000_000)
	}
	if !amount.IsInt64() {
		return 0, errors.New("resource delegation amount exceeds int64")
	}
	return amount.Int64(), nil
}

func unsignedTransaction(body []byte) (txid, rawData string, expiration int64, err error) {
	var transaction struct {
		TxID       string `json:"txID"`
		RawDataHex string `json:"raw_data_hex"`
		RawData    struct {
			Expiration int64 `json:"expiration"`
		} `json:"raw_data"`
	}
	if err = json.Unmarshal(body, &transaction); err != nil {
		return "", "", 0, err
	}
	if transaction.TxID == "" || transaction.RawDataHex == "" {
		return "", "", 0, errors.New("unsigned delegation is missing txID or raw_data_hex (RES-012)")
	}
	return transaction.TxID, transaction.RawDataHex, transaction.RawData.Expiration / 1000, nil
}

func parseReferenceBlock(raw []byte) (store.ReferenceBlock, error) {
	var response struct {
		ID     string `json:"blockID"`
		Header struct {
			Raw struct {
				Number         int64  `json:"number"`
				Timestamp      int64  `json:"timestamp"`
				TxTrieRoot     string `json:"txTrieRoot"`
				ParentHash     string `json:"parentHash"`
				WitnessAddress string `json:"witness_address"`
				Version        int32  `json:"version"`
			} `json:"raw_data"`
		} `json:"block_header"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return store.ReferenceBlock{}, err
	}
	r := response.Header.Raw
	if response.ID == "" || r.Timestamp <= 0 {
		return store.ReferenceBlock{}, errors.New("reference block is incomplete")
	}
	return store.ReferenceBlock{Height: r.Number, TimestampMS: r.Timestamp, ID: response.ID, TxTrieRoot: r.TxTrieRoot,
		ParentHash: r.ParentHash, WitnessAddress: r.WitnessAddress, Version: r.Version}, nil
}

func signingInput(w store.Withdrawal, asset config.Asset, reference store.ReferenceBlock, cfg config.Withdrawal) (*tronpb.SigningInput, error) {
	amount, ok := new(big.Int).SetString(w.AmountRaw, 10)
	if !ok || amount.Sign() <= 0 {
		return nil, errors.New("invalid withdrawal amount")
	}
	decode := func(name, value string) ([]byte, error) {
		b, err := hex.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("invalid reference block %s: %w", name, err)
		}
		return b, nil
	}
	txRoot, err := decode("tx trie root", reference.TxTrieRoot)
	if err != nil {
		return nil, err
	}
	parent, err := decode("parent hash", reference.ParentHash)
	if err != nil {
		return nil, err
	}
	witness, err := decode("witness address", reference.WitnessAddress)
	if err != nil {
		return nil, err
	}
	// WDR-010/010a: TAPOS, timestamp, and expiration share one authoritative reference block; the host clock is not transaction input.
	tx := &tronpb.Transaction{Timestamp: reference.TimestampMS, Expiration: reference.TimestampMS + cfg.Expiration.Milliseconds(),
		FeeLimit: cfg.FeeLimitTRX * 1_000_000, BlockHeader: &tronpb.BlockHeader{Timestamp: reference.TimestampMS,
			TxTrieRoot: txRoot, ParentHash: parent, Number: reference.Height, WitnessAddress: witness, Version: reference.Version}}
	if asset.Kind == "native" { // WDR-013: native and TRC-20 transfers use their distinct signer contracts.
		if !amount.IsInt64() {
			return nil, errors.New("TRX withdrawal exceeds signer integer range")
		}
		tx.ContractOneof = &tronpb.Transaction_Transfer{Transfer: &tronpb.TransferContract{OwnerAddress: w.FromAddress, ToAddress: w.ToAddress, Amount: amount.Int64()}}
	} else {
		tx.ContractOneof = &tronpb.Transaction_TransferTrc20{TransferTrc20: &tronpb.TransferTRC20Contract{
			OwnerAddress: w.FromAddress, ContractAddress: asset.Contract, ToAddress: w.ToAddress, Amount: amount.Bytes()}}
	}
	return &tronpb.SigningInput{Transaction: tx}, nil
}

func classifyBroadcast(response chain.Response, sendErr error) (string, string) {
	var body struct {
		Result bool   `json:"result"`
		Code   string `json:"code"`
	}
	if json.Unmarshal(response.Body, &body) == nil && body.Result && sendErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		return "accepted", ""
	}
	code := strings.ToUpper(body.Code)
	switch code {
	case "SIGERROR", "TAPOS_ERROR", "TRANSACTION_EXPIRATION_ERROR", "CONTRACT_VALIDATE_ERROR", "BANDWITH_ERROR":
		return "deterministic", code
	default:
		if sendErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
			return "ambiguous", code
		}
		return "ambiguous", code
	}
}

func transactionFound(body []byte) (bool, error) {
	var result map[string]json.RawMessage
	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}
	_, txid := result["txID"]
	_, id := result["id"]
	return txid || id, nil
}

type receipt struct {
	FeeRaw                                  string
	EnergyUsed, BlockHeight, BlockTimestamp int64
	Result                                  string
}

func parseReceipt(body []byte) (receipt, bool, error) {
	var response struct {
		ID             string      `json:"id"`
		Fee            json.Number `json:"fee"`
		BlockNumber    int64       `json:"blockNumber"`
		BlockTimeStamp int64       `json:"blockTimeStamp"`
		Result         string      `json:"result"`
		Receipt        struct {
			EnergyUsageTotal int64 `json:"energy_usage_total"`
		} `json:"receipt"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		return receipt{}, false, err
	}
	if response.ID == "" {
		return receipt{}, false, nil
	}
	fee := response.Fee.String()
	if fee == "" {
		fee = "0"
	}
	return receipt{FeeRaw: fee, EnergyUsed: response.Receipt.EnergyUsageTotal, BlockHeight: response.BlockNumber,
		BlockTimestamp: response.BlockTimeStamp / 1000, Result: response.Result}, true, nil
}
