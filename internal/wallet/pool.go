// Package wallet owns deposit-address pool allocation and order creation.
package wallet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/config"
	"payd/internal/price"
	"payd/internal/store"
)

var (
	ErrInvalidOrder    = errors.New("invalid order")
	ErrUnknownConsumer = errors.New("unknown or disabled consumer")
	ErrStalePrice      = price.ErrUnavailable
)

type CreateOrderRequest struct {
	ExternalRef *string
	Asset       string
	ExpectedRaw string
	TTL         time.Duration
	Metadata    string
	Consumer    string
	PriceUSD    *string
	PriceAt     *int64
}

type Pool struct {
	store     *store.Store
	wallet    *hdwallet.HDWallet
	mu        sync.RWMutex
	config    config.Config
	consumers map[string]bool
}

func NewPool(database *store.Store, hd *hdwallet.HDWallet, cfg config.Config) (*Pool, error) {
	if database == nil || hd == nil {
		return nil, errors.New("wallet pool requires store and HD wallet")
	}
	consumers := make(map[string]bool, len(cfg.IPN.Consumers))
	for _, consumer := range cfg.IPN.Consumers {
		consumers[consumer.Name] = consumer.Enabled
	}
	return &Pool{store: database, wallet: hd, config: cfg, consumers: consumers}, nil
}

func (p *Pool) UpdateConfig(cfg config.Config) {
	consumers := make(map[string]bool, len(cfg.IPN.Consumers))
	for _, consumer := range cfg.IPN.Consumers {
		consumers[consumer.Name] = consumer.Enabled
	}
	p.mu.Lock()
	p.config.Assets, p.config.IPN = cfg.Assets, cfg.IPN
	p.consumers = consumers
	p.mu.Unlock()
}

func (p *Pool) CreateOrder(ctx context.Context, request CreateOrderRequest, now time.Time) (store.Order, bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	amount, ok := new(big.Int).SetString(request.ExpectedRaw, 10)
	if !ok || amount.Sign() <= 0 || !p.configuredAsset(request.Asset) {
		return store.Order{}, false, ErrInvalidOrder
	}
	metadata := request.Metadata
	if metadata == "" {
		metadata = "{}"
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(metadata), &object); err != nil || object == nil {
		return store.Order{}, false, fmt.Errorf("%w: metadata must be a JSON object", ErrInvalidOrder)
	}
	consumer := request.Consumer
	if consumer == "" {
		consumer = p.config.IPN.DefaultConsumer
	}
	if !p.consumers[consumer] {
		return store.Order{}, false, ErrUnknownConsumer
	}
	ttl := request.TTL
	if ttl == 0 {
		ttl = p.config.Orders.DefaultTTL
	}
	if ttl <= 0 {
		return store.Order{}, false, ErrInvalidOrder
	}
	priceUSD, priceAt := request.PriceUSD, request.PriceAt
	if priceUSD == nil || priceAt == nil {
		price, fetchedAt, err := p.price(ctx, request.Asset, now)
		if err != nil {
			return store.Order{}, false, err
		}
		priceUSD, priceAt = &price, &fetchedAt
	}
	params := store.CreateOrderParams{
		ExternalRef: request.ExternalRef, Asset: request.Asset, ExpectedRaw: amount.String(), Metadata: metadata,
		Consumer: consumer, ExpiresAt: now.Add(ttl).UTC().Unix(), PriceUSD: priceUSD, PriceAt: priceAt,
	}
	return p.store.CreateOrder(ctx, p.derive, p.config.Wallet.PoolMaxSize, params, now)
}

func (p *Pool) TopUp(ctx context.Context, now time.Time) (int, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.store.TopUpPool(ctx, p.derive, p.config.Wallet.PoolMinFree,
		p.config.Wallet.PoolInitialSize, p.config.Wallet.PoolMaxSize, now)
}

func (p *Pool) derive(index uint32) (string, error) {
	if index >= 1000 {
		return "", store.ErrPoolExhausted
	}
	return p.wallet.AddressAt(hdwallet.TRX, p.config.Wallet.Account, 0, index)
}

func (p *Pool) configuredAsset(symbol string) bool {
	for _, asset := range p.config.Assets {
		if asset.Symbol == symbol {
			return true
		}
	}
	return false
}

func (p *Pool) price(ctx context.Context, asset string, now time.Time) (string, int64, error) {
	quote, err := price.Current(ctx, p.store, p.config.Price, asset, now)
	if err != nil {
		return "", 0, err // ORD-009 / PRC-005
	}
	return quote.USD, quote.FetchedAt, nil
}
