// Package store is the only package that opens SQLite handles (ARC-005).
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	normal *sql.DB
	full   *sql.DB // ARC-006a: reserved for irreversible-side-effect writes only.
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
	s := &Store{normal: normal, full: full}
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
		reason := fmt.Sprintf("consumer %q removed or disabled by config reload", consumer)
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
