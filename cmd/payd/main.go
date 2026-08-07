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

	"github.com/awnumar/memguard"
	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/chain"
	"payd/internal/config"
	"payd/internal/decode"
	"payd/internal/follower"
	"payd/internal/seed"
	"payd/internal/store"
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
	if !energyProviderReachable(logger, cfg.Energy) {
		cfg.Energy.Enabled = false
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
	chainClient, err := chain.New(cfg.Tron, logger)
	if err != nil {
		return err
	}
	parameterWorker, err := chain.NewParameterWorker(chainClient.Read, db, logger, cfg.Energy.MaxBurnTRX)
	if err != nil {
		return err
	}
	paymentDecoder, err := decode.New(chainClient.Read, db, cfg.Assets)
	if err != nil {
		return err
	}
	followerWorker, err := follower.New(chainClient.Read, db, paymentDecoder.Prepare, cfg.Tron.PollInterval, cfg.Tron.ReorgDepth, logger)
	if err != nil {
		return err
	}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		parameterWorker.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		followerWorker.Run(ctx)
	}()
	defer workers.Wait()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	logger.Info("payd started", "pool_addresses", cfg.Wallet.PoolInitialSize, "resource_wallet_index", cfg.Resources.ResourceWalletIndex)
	for {
		select {
		case <-ctx.Done():
			logger.Info("payd stopped")
			return nil
		case <-hup:
			next, err := reload(ctx, logger, db, wallet, *configPath, cfg)
			if err != nil {
				logger.Error("config reload rejected", "error", err)
				continue
			}
			if err := paymentDecoder.UpdateAssets(next.Assets); err != nil {
				return fmt.Errorf("reload decoder assets: %w", err)
			}
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
	if err := db.ApplyConfigReload(ctx, wallet, next.Wallet.Account, next.Resources.ResourceWalletIndex, disabled); err != nil {
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
