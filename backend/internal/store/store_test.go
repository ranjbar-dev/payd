package store

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/awnumar/memguard"
	hdwallet "github.com/ranjbar-dev/hd-wallet"
)

const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

// OPS-010: sqlite3 can take a consistent online backup while payd holds an
// active WAL write transaction.
func TestSQLiteCLIBackupWhileStoreIsOpen(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI is not installed")
	}
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "live.db")
	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	write, err := database.normal.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Rollback() }()
	if _, err := write.ExecContext(ctx, "UPDATE crawler_state SET updated_at=updated_at+1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if output, err := exec.CommandContext(ctx, "sqlite3", databasePath, ".backup "+filepath.ToSlash(backupPath)).CombinedOutput(); err != nil {
		t.Fatalf("online .backup: %v: %s", err, output)
	}
	output, err := exec.CommandContext(ctx, "sqlite3", backupPath, "PRAGMA integrity_check; SELECT COUNT(*) FROM schema_migrations;").CombinedOutput()
	if err != nil || strings.Join(strings.Fields(string(output)), " ") != "ok 9" {
		t.Fatalf("verify backup: %v: %q", err, output)
	}
}

func TestOpenMigrateAndInitializeWallet(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "payd.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})

	assertPragma(t, s.normal, "journal_mode", "wal")
	assertPragma(t, s.normal, "busy_timeout", "5000")
	assertPragma(t, s.normal, "foreign_keys", "1")
	assertPragma(t, s.normal, "synchronous", "1")
	assertPragma(t, s.full, "journal_mode", "wal")
	assertPragma(t, s.full, "busy_timeout", "5000")
	assertPragma(t, s.full, "foreign_keys", "1")
	assertPragma(t, s.full, "synchronous", "2")
	var migrations, tables int
	if err := s.normal.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil || migrations != 9 {
		t.Fatalf("migrations count = %d, err = %v", migrations, err)
	}
	if err := s.normal.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil || tables != 18 {
		t.Fatalf("table count = %d, err = %v", tables, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("database mode = %v, err = %v", info.Mode().Perm(), err)
		}
	}

	defer memguard.Purge()
	wallet, err := hdwallet.FromMnemonicBuffer(memguard.NewBufferFromBytes([]byte(testMnemonic)))
	if err != nil {
		t.Fatal(err)
	}
	defer wallet.Destroy()
	if err := s.InitializeWallet(ctx, wallet, 0, 20, 1000); err != nil {
		t.Fatal(err)
	}
	if err := s.InitializeWallet(ctx, wallet, 0, 20, 1000); err != nil {
		t.Fatalf("idempotent initialization: %v", err)
	}
	var count int
	if err := s.normal.QueryRow("SELECT COUNT(*) FROM addresses").Scan(&count); err != nil || count != 21 {
		t.Fatalf("address count = %d, err = %v", count, err)
	}
	var state string
	if err := s.normal.QueryRow("SELECT state FROM addresses WHERE hd_index = 1000").Scan(&state); err != nil || state != "disabled" {
		t.Fatalf("resource wallet state = %q, err = %v", state, err)
	}
	if _, err := s.normal.Exec("UPDATE addresses SET state = 'free' WHERE hd_index = 1000"); err != nil {
		t.Fatal(err)
	}
	if err := s.InitializeWallet(ctx, wallet, 0, 20, 1000); err == nil {
		t.Fatal("non-disabled resource wallet accepted")
	}
}

func assertPragma(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, name, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow("PRAGMA " + name).Scan(&got); err != nil || got != want {
		t.Fatalf("PRAGMA %s = %q, want %q, err = %v", name, got, want, err)
	}
}
