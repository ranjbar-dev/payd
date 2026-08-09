// Package api exposes the authenticated HTTP boundary for payd (W-009).
package api

import (
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"payd/internal/config"
	"payd/internal/energy"
	"payd/internal/store"
	walletpool "payd/internal/wallet"
)

type Server struct {
	store      *store.Store
	pool       *walletpool.Pool
	logger     *slog.Logger
	handler    http.Handler
	keys       []apiKey
	totpSecret []byte

	mu                 sync.RWMutex
	assets             map[string]config.Asset
	price              config.Price
	ipn                config.IPN
	energy             config.Energy
	resources          config.Resources
	withdrawal         config.Withdrawal
	cooldown           time.Duration
	delegator          resourceDelegator
	burnCeilingHealthy func() bool
	readyChecks        func(context.Context) []string
	metrics            http.Handler

	rateMu sync.Mutex
	rates  map[string]rateWindow
}

type resourceDelegator interface {
	DelegateResources(context.Context, string, string, int64, string, string) (store.ResourceGrant, error)
	ProviderBalanceMetric() (string, bool)
	EstimateResources(context.Context, string, string) (energy.ResourceEstimate, error)
}

func New(database *store.Store, pool *walletpool.Pool, cfg config.Config, logger *slog.Logger) (*Server, error) {
	if database == nil || pool == nil {
		return nil, errors.New("API requires store and wallet pool")
	}
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{store: database, pool: pool, logger: logger, rates: make(map[string]rateWindow)}
	server.UpdateConfig(cfg)
	if len(cfg.Auth.APIKeys) == 0 {
		return nil, errors.New("auth.api_keys must not be empty")
	}
	for _, configured := range cfg.Auth.APIKeys {
		key, err := parseAPIKey(configured)
		if err != nil {
			return nil, fmt.Errorf("auth key %q: %w", configured.Name, err)
		}
		server.keys = append(server.keys, key)
	}
	if cfg.Auth.TOTPSecret != "" {
		secret := strings.ToUpper(strings.ReplaceAll(cfg.Auth.TOTPSecret, " ", ""))
		decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(secret, "="))
		if err != nil || len(decoded) == 0 {
			return nil, errors.New("auth.totp_secret is not valid base32")
		}
		server.totpSecret = decoded
	}

	mux := http.NewServeMux()
	for _, registered := range apiRoutes {
		mux.Handle(registered.method+" "+registered.pattern, server.requireScope(registered.scope, registered.handler(server)))
	}
	root := http.NewServeMux()
	// Operational and documentation routes intentionally bypass auth and rate limiting: they expose no secrets,
	// the API surface is public documentation, and server.listen defaults to 127.0.0.1.
	for _, registered := range publicRoutes {
		root.HandleFunc(registered.method+" "+registered.pattern, registered.handler(server))
	}
	root.Handle("/", server.authenticate(server.rateLimit(mux)))
	server.handler = server.logRequests(server.normalizeErrors(root))
	return server, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) SetBurnCeilingHealthy(check func() bool) {
	s.mu.Lock()
	s.burnCeilingHealthy = check
	s.mu.Unlock()
}

func (s *Server) SetOperations(ready func(context.Context) []string, metrics http.Handler) {
	s.mu.Lock()
	s.readyChecks, s.metrics = ready, metrics
	s.mu.Unlock()
}

func (s *Server) SetDelegator(delegator resourceDelegator) {
	s.mu.Lock()
	s.delegator = delegator
	s.mu.Unlock()
}

func (s *Server) UpdateConfig(cfg config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assets == nil {
		s.assets = make(map[string]config.Asset)
	}
	clear(s.assets)
	for _, asset := range cfg.Assets {
		s.assets[asset.Symbol] = asset
	}
	s.price, s.ipn, s.energy = cfg.Price, cfg.IPN, cfg.Energy
	s.resources, s.withdrawal, s.cooldown = cfg.Resources, cfg.Withdrawal, cfg.Wallet.Cooldown
}
