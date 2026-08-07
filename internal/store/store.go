// Package store is the only package that opens SQLite handles (ARC-005).
package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	sqlite "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() {
	err := sqlite.RegisterDeterministicScalarFunction("payd_decimal_sum_within", 3,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			var values []string
			encoded, ok := args[0].(string)
			if !ok {
				return nil, errors.New("daily withdrawal values are not JSON text")
			}
			if err := json.Unmarshal([]byte(encoded), &values); err != nil {
				return nil, err
			}
			total := new(big.Rat)
			for _, text := range append(values, fmt.Sprint(args[1])) {
				value, ok := new(big.Rat).SetString(text)
				if !ok {
					return nil, fmt.Errorf("invalid withdrawal USD value %q", text)
				}
				total.Add(total, value)
			}
			limit, ok := new(big.Rat).SetString(fmt.Sprint(args[2]))
			if !ok {
				return nil, fmt.Errorf("invalid withdrawal daily limit %q", args[2])
			}
			if total.Cmp(limit) <= 0 {
				return int64(1), nil
			}
			return int64(0), nil
		})
	if err != nil {
		panic(fmt.Sprintf("register exact withdrawal limit function: %v", err))
	}
}

type Store struct {
	normal      *sql.DB
	full        *sql.DB // ARC-006a: reserved for irreversible-side-effect writes only.
	outboxReady chan struct{}
	referenceMu sync.RWMutex
	reference   ReferenceBlock
}

type ChainParameters struct {
	EnergyFee      int64
	TransactionFee int64
	FetchedAt      int64
}

type Balance struct {
	ConfirmedRaw string
	PendingRaw   string
	ChainRaw     *string
	Drift        bool
}

type Price struct {
	Symbol    string
	PriceUSD  string
	Source    string
	FetchedAt int64
}

// Open creates the database privately and opens the NORMAL and FULL connections (ARC-006/006a, DB-006).
func Open(ctx context.Context, path string) (*Store, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	f, err := os.OpenFile(absPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("set database mode: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close database bootstrap file: %w", err)
	}

	normal, err := openConnection(ctx, absPath, "NORMAL")
	if err != nil {
		return nil, err
	}
	full, err := openConnection(ctx, absPath, "FULL")
	if err != nil {
		_ = normal.Close()
		return nil, err
	}
	s := &Store{normal: normal, full: full, outboxReady: make(chan struct{}, 1)}
	if err := s.migrate(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func openConnection(ctx context.Context, path, synchronous string) (*sql.DB, error) {
	q := make(url.Values)
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "synchronous("+synchronous+")")
	dsn := "file:" + filepath.ToSlash(path) + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite %s connection: %w", synchronous, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping SQLite %s connection: %w", synchronous, err)
	}
	return db, nil
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.normal.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        applied_at INTEGER NOT NULL
    )`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := migrationVersion(entry.Name())
		if err != nil {
			return err
		}
		var applied int
		err = s.normal.QueryRowContext(ctx, "SELECT 1 FROM schema_migrations WHERE version = ?", version).Scan(&applied)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		script, err := fs.ReadFile(migrations, "migrations/"+entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		tx, err := s.normal.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.ExecContext(ctx, string(script)); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)", version, entry.Name(), time.Now().UTC().Unix())
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("migration %q has no numeric prefix", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("migration %q has an invalid version", name)
	}
	return version, nil
}

func (s *Store) Close() error {
	return errors.Join(s.full.Close(), s.normal.Close())
}

// UpsertChainParameters replaces both live fee parameters atomically (RES-020/021).
func (s *Store) UpsertChainParameters(ctx context.Context, energyFee, transactionFee int64, fetchedAt time.Time) (*int64, error) {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin chain parameter update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var old int64
	var previous *int64
	err = tx.QueryRowContext(ctx, "SELECT value FROM chain_params WHERE name = 'getEnergyFee'").Scan(&old)
	if err == nil {
		previous = &old
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("read previous energy fee: %w", err)
	}
	const upsert = `INSERT INTO chain_params(name, value, fetched_at) VALUES (?, ?, ?)
        ON CONFLICT(name) DO UPDATE SET value = excluded.value, fetched_at = excluded.fetched_at`
	stamp := fetchedAt.UTC().Unix()
	if _, err := tx.ExecContext(ctx, upsert, "getEnergyFee", energyFee, stamp); err != nil {
		return nil, fmt.Errorf("upsert energy fee: %w", err)
	}
	if _, err := tx.ExecContext(ctx, upsert, "getTransactionFee", transactionFee, stamp); err != nil {
		return nil, fmt.Errorf("upsert transaction fee: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit chain parameter update: %w", err)
	}
	return previous, nil
}

// LoadChainParameters returns no row until both required live values exist (RES-022).
func (s *Store) LoadChainParameters(ctx context.Context) (ChainParameters, error) {
	var energy, transaction, fetched sql.NullInt64
	err := s.normal.QueryRowContext(ctx, `SELECT
        MAX(CASE WHEN name = 'getEnergyFee' THEN value END),
        MAX(CASE WHEN name = 'getTransactionFee' THEN value END),
        MIN(CASE WHEN name IN ('getEnergyFee','getTransactionFee') THEN fetched_at END)
        FROM chain_params`).Scan(&energy, &transaction, &fetched)
	if err != nil {
		return ChainParameters{}, fmt.Errorf("load chain parameters: %w", err)
	}
	if !energy.Valid || !transaction.Valid || !fetched.Valid {
		return ChainParameters{}, sql.ErrNoRows
	}
	return ChainParameters{EnergyFee: energy.Int64, TransactionFee: transaction.Int64, FetchedAt: fetched.Int64}, nil
}

func (s *Store) AssetPrice(ctx context.Context, symbol string) (string, int64, error) {
	var price string
	var fetchedAt int64
	if err := s.normal.QueryRowContext(ctx, "SELECT price_usd, fetched_at FROM prices WHERE symbol = ?", symbol).Scan(&price, &fetchedAt); err != nil {
		return "", 0, fmt.Errorf("load %s price: %w", symbol, err)
	}
	return price, fetchedAt, nil
}

// UpsertPrices atomically records one successful provider response (PRC-001/004).
func (s *Store) UpsertPrices(ctx context.Context, prices []Price) error {
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin price update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	const query = `INSERT INTO prices(symbol, price_usd, source, fetched_at) VALUES (?, ?, ?, ?)
        ON CONFLICT(symbol) DO UPDATE SET price_usd = excluded.price_usd,
        source = excluded.source, fetched_at = excluded.fetched_at`
	for _, price := range prices {
		if _, err := tx.ExecContext(ctx, query, price.Symbol, price.PriceUSD, price.Source, price.FetchedAt); err != nil {
			return fmt.Errorf("upsert %s price: %w", price.Symbol, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit price update: %w", err)
	}
	return nil
}

func (s *Store) Balance(ctx context.Context, addressID int64, asset string) (Balance, error) {
	var balance Balance
	var chain sql.NullString
	if err := s.normal.QueryRowContext(ctx, `SELECT confirmed_raw, pending_raw, chain_raw, drift_detected
        FROM balances WHERE address_id = ? AND asset = ?`, addressID, asset).
		Scan(&balance.ConfirmedRaw, &balance.PendingRaw, &chain, &balance.Drift); err != nil {
		return Balance{}, fmt.Errorf("load balance: %w", err)
	}
	if chain.Valid {
		balance.ChainRaw = &chain.String
	}
	return balance, nil
}

// InitializeWallet derives the configured account's initial pool and disabled resource wallet (CFG-013).
func (s *Store) InitializeWallet(ctx context.Context, wallet *hdwallet.HDWallet, account uint32, poolSize int, resourceIndex uint32) error {
	type derived struct {
		index   uint32
		address string
		state   string
	}
	addresses := make([]derived, 0, poolSize+1)
	for i := range poolSize {
		address, err := wallet.AddressAt(hdwallet.TRX, account, 0, uint32(i))
		if err != nil {
			return fmt.Errorf("derive pool address %d: %w", i, err)
		}
		addresses = append(addresses, derived{index: uint32(i), address: address, state: "free"})
	}
	resourceAddress, err := wallet.AddressAt(hdwallet.TRX, account, 0, resourceIndex)
	if err != nil {
		return fmt.Errorf("derive resource wallet: %w", err)
	}
	addresses = append(addresses, derived{index: resourceIndex, address: resourceAddress, state: "disabled"})

	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin wallet initialization: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Unix()
	for _, item := range addresses {
		if _, err := tx.ExecContext(ctx, `INSERT INTO addresses(hd_index, address, state, created_at)
            VALUES (?, ?, ?, ?) ON CONFLICT(hd_index) DO NOTHING`, item.index, item.address, item.state, now); err != nil {
			return fmt.Errorf("insert wallet address %d: %w", item.index, err)
		}
		var storedAddress, storedState string
		if err := tx.QueryRowContext(ctx, "SELECT address, state FROM addresses WHERE hd_index = ?", item.index).Scan(&storedAddress, &storedState); err != nil {
			return fmt.Errorf("verify wallet address %d: %w", item.index, err)
		}
		if storedAddress != item.address {
			return fmt.Errorf("hd index %d belongs to a different wallet address", item.index)
		}
		if item.index == resourceIndex && storedState != "disabled" {
			return fmt.Errorf("resource wallet index %d has state %q, want disabled (CFG-013)", resourceIndex, storedState)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit wallet initialization: %w", err)
	}
	return nil
}

// BlockingConsumerOrders returns non-terminal orders that name a consumer (CFG-006).
func (s *Store) BlockingConsumerOrders(ctx context.Context, consumers []string) ([]string, error) {
	var ids []string
	for _, consumer := range consumers {
		rows, err := s.normal.QueryContext(ctx, `SELECT id FROM orders
            WHERE consumer = ? AND status NOT IN ('confirmed','expired','expired_funded','cancelled','cancelled_funded')
            ORDER BY id`, consumer)
		if err != nil {
			return nil, fmt.Errorf("query orders for consumer %q: %w", consumer, err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan blocking order: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// ApplyConfigReload atomically initializes a changed resource wallet and dead-letters removed consumers (CFG-006/010/013).
func (s *Store) ApplyConfigReload(ctx context.Context, wallet *hdwallet.HDWallet, account, resourceIndex uint32, consumers []string) error {
	resourceAddress, err := wallet.AddressAt(hdwallet.TRX, account, 0, resourceIndex)
	if err != nil {
		return fmt.Errorf("derive resource wallet: %w", err)
	}
	tx, err := s.normal.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin config reload: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO addresses(hd_index, address, state, created_at)
        VALUES (?, ?, 'disabled', ?) ON CONFLICT(hd_index) DO NOTHING`, resourceIndex, resourceAddress, time.Now().UTC().Unix()); err != nil {
		return fmt.Errorf("insert resource wallet: %w", err)
	}
	var storedAddress, state string
	if err := tx.QueryRowContext(ctx, "SELECT address, state FROM addresses WHERE hd_index = ?", resourceIndex).Scan(&storedAddress, &state); err != nil {
		return fmt.Errorf("verify resource wallet: %w", err)
	}
	if storedAddress != resourceAddress || state != "disabled" {
		return fmt.Errorf("resource wallet index %d is not the configured disabled address (CFG-013)", resourceIndex)
	}
	for _, consumer := range consumers {
		reason := fmt.Sprintf("consumer %q removed by config reload", consumer)
		if _, err := tx.ExecContext(ctx, `UPDATE ipn_outbox SET status = 'dead', last_error = ?
			WHERE consumer = ? AND status = 'pending'`, reason, consumer); err != nil {
			return fmt.Errorf("dead-letter consumer %q: %w", consumer, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit config reload: %w", err)
	}
	return nil
}
