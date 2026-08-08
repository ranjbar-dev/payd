package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/awnumar/memguard"
	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/api"
	"payd/internal/chain"
	"payd/internal/config"
	"payd/internal/confirm"
	"payd/internal/decode"
	"payd/internal/energy"
	"payd/internal/follower"
	"payd/internal/ipn"
	"payd/internal/lifecycle"
	"payd/internal/matcher"
	"payd/internal/ops"
	"payd/internal/price"
	"payd/internal/seed"
	"payd/internal/store"
	walletpool "payd/internal/wallet"
	"payd/internal/withdraw"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("payd stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("payd", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "path to YAML config")
	if err := flags.Parse(args); err != nil || *configPath == "" || flags.NArg() != 0 {
		return errors.New("usage: payd --config /path/to/payd.yaml")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)
	logAssets(logger, cfg.Assets)
	energyProvider, err := energy.New(cfg.Energy)
	if err != nil {
		return err
	}
	if !energyProviderReachable(logger, cfg.Energy) {
		cfg.Energy.Enabled = false
		energyProvider = nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	db, err := store.Open(ctx, cfg.Database.Path)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("close store", "error", err)
		}
	}()

	// KEY-005/006/007: decrypt to locked memory, transfer ownership, and purge at exit.
	defer memguard.Purge()
	mnemonic, err := seed.DecryptFile(cfg.Wallet.SeedFile)
	if err != nil {
		return err
	}
	wallet, err := hdwallet.FromMnemonicBuffer(mnemonic)
	if err != nil {
		return errors.New("load wallet: encrypted seed does not contain a valid BIP-39 mnemonic")
	}
	defer wallet.Destroy()
	if err := db.InitializeWallet(ctx, wallet, cfg.Wallet.Account, cfg.Wallet.PoolInitialSize, cfg.Resources.ResourceWalletIndex); err != nil {
		return err
	}
	depositPool, err := walletpool.NewPool(db, wallet, cfg)
	if err != nil {
		return err
	}
	apiServer, err := api.New(db, depositPool, cfg, logger)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr: cfg.Server.Listen, Handler: apiServer.Handler(),
		ReadTimeout: cfg.Server.ReadTimeout, WriteTimeout: cfg.Server.WriteTimeout,
	}
	chainClient, err := chain.New(cfg.Tron, logger)
	if err != nil {
		return err
	}
	priceWorker, err := price.New(cfg.Price, db, logger)
	if err != nil {
		return err
	}
	parameterWorker, err := chain.NewParameterWorker(chainClient.Read, db, logger, cfg.Energy.MaxBurnTRX)
	if err != nil {
		return err
	}
	apiServer.SetBurnCeilingHealthy(parameterWorker.BurnCeilingHealthy)
	events := store.NewEventConfig(cfg.IPN, cfg.Assets)
	orderMatcher := matcher.New(events)
	paymentDecoder, err := decode.New(chainClient.Read, db, cfg.Assets, func(write *store.BlockWrite, payment store.PaymentRecord) error {
		return orderMatcher.Match(write, payment)
	})
	if err != nil {
		return err
	}
	followerWorker, err := follower.New(chainClient.Read, db, paymentDecoder.Prepare, cfg.Tron.PollInterval, cfg.Tron.ReorgDepth, logger, events)
	if err != nil {
		return err
	}
	lifecycleWorker, err := lifecycle.New(db, depositPool, cfg.Wallet.Cooldown, logger, events)
	if err != nil {
		return err
	}
	confirmationWorker, err := confirm.New(chainClient.Solidity, db, cfg.Tron.ConfirmationsRequired, cfg.Wallet.Cooldown, logger, events)
	if err != nil {
		return err
	}
	resourceMonitor, err := walletpool.NewMonitor(chainClient.Read, db, cfg, logger)
	if err != nil {
		return err
	}
	balanceReconciler, err := walletpool.NewReconciler(chainClient.Read, db, cfg, logger)
	if err != nil {
		return err
	}
	if err := balanceReconciler.EnableSafetyNet(chainClient.Read, paymentDecoder,
		func(write *store.BlockWrite, payment store.PaymentRecord) error {
			return orderMatcher.Match(write, payment)
		}); err != nil {
		return err
	}
	withdrawalWorker, err := withdraw.New(chainClient.Read, chainClient.Solidity, chainClient.Broadcast, db, wallet, energyProvider, cfg, logger)
	if err != nil {
		return err
	}
	ipnWorker, err := ipn.New(db, cfg.IPN, logger)
	if err != nil {
		return err
	}
	operations := ops.New(db, chainClient, followerWorker, ipnWorker, cfg)
	apiServer.SetOperations(operations.Ready, operations)
	var workers sync.WaitGroup
	workers.Add(9)
	go func() {
		defer workers.Done()
		priceWorker.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		parameterWorker.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		followerWorker.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		lifecycleWorker.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		confirmationWorker.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		resourceMonitor.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		balanceReconciler.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		withdrawalWorker.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		ipnWorker.Run(ctx)
	}()
	serverErrors := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()
	defer func() {
		stop()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			logger.Error("shut down API", "error", err)
		}
		workers.Wait()
	}()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	logger.Info("payd started", "pool_addresses", cfg.Wallet.PoolInitialSize, "resource_wallet_index", cfg.Resources.ResourceWalletIndex)
	for {
		select {
		case <-ctx.Done():
			logger.Info("payd stopped")
			return nil
		case err := <-serverErrors:
			return fmt.Errorf("serve API: %w", err)
		case <-hup:
			next, err := reload(ctx, logger, db, wallet, *configPath, cfg)
			if err != nil {
				logger.Error("config reload rejected", "error", err)
				continue
			}
			if err := paymentDecoder.UpdateAssets(next.Assets); err != nil {
				return fmt.Errorf("reload decoder assets: %w", err)
			}
			nextEnergyProvider, err := energy.New(next.Energy)
			if err != nil {
				logger.Error("config reload rejected", "error", err)
				continue
			}
			if err := parameterWorker.UpdateMaxBurnTRX(ctx, next.Energy.MaxBurnTRX); err != nil {
				logger.Error("config reload rejected", "error", err)
				continue
			}
			events = store.NewEventConfig(next.IPN, next.Assets)
			orderMatcher.UpdateEvents(events)
			followerWorker.UpdateEvents(events)
			lifecycleWorker.UpdateEvents(events)
			confirmationWorker.UpdateEvents(events)
			resourceMonitor.UpdateConfig(next)
			balanceReconciler.UpdateConfig(next)
			withdrawalWorker.UpdateProvider(nextEnergyProvider, next)
			ipnWorker.UpdateConfig(next.IPN)
			depositPool.UpdateConfig(next)
			apiServer.UpdateConfig(next)
			operations.UpdateConfig(next)
			cfg = next
		}
	}
}

func reload(ctx context.Context, logger *slog.Logger, db *store.Store, wallet *hdwallet.HDWallet, path string, current config.Config) (config.Config, error) {
	next, err := config.Load(path)
	if err != nil {
		return current, err
	}
	if err := config.CheckReload(current, next); err != nil {
		return current, err
	}
	disabled := config.DisabledConsumers(current, next)
	blocking, err := db.BlockingConsumerOrders(ctx, disabled)
	if err != nil {
		return current, err
	}
	if len(blocking) > 0 {
		return current, fmt.Errorf("consumers are named by non-terminal orders %s (CFG-006)", strings.Join(blocking, ","))
	}
	if err := db.ApplyConfigReload(ctx, wallet, next.Wallet.Account, next.Resources.ResourceWalletIndex, config.RemovedConsumers(current, next)); err != nil {
		return current, err
	}
	logAssets(logger, next.Assets)
	if !energyProviderReachable(logger, next.Energy) {
		next.Energy.Enabled = false
	}
	logger.Info("config reloaded")
	return next, nil
}

func newLogger(cfg config.Log) *slog.Logger {
	level := new(slog.LevelVar)
	_ = level.UnmarshalText([]byte(cfg.Level))
	options := &slog.HandlerOptions{Level: level}
	if cfg.Format == "console" {
		return slog.New(slog.NewTextHandler(os.Stderr, options))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, options))
}

func logAssets(logger *slog.Logger, assets []config.Asset) {
	for _, asset := range assets {
		logger.Warn("verified asset configured", "symbol", asset.Symbol, "contract", asset.Contract, "decimals", asset.Decimals) // CFG-014
	}
}

func energyProviderReachable(logger *slog.Logger, cfg config.Energy) bool {
	if !cfg.Enabled {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, cfg.APIURL, nil)
	if err == nil {
		resp, requestErr := http.DefaultClient.Do(req)
		if requestErr == nil {
			_ = resp.Body.Close()
			return true
		}
		err = requestErr
	}
	logger.Warn("energy provider unreachable; continuing with burn-only sourcing", "provider", cfg.Provider, "error", err) // CFG-012
	return false
}
