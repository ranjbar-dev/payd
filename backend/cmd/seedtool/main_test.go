package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/awnumar/memguard"
	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/seed"
)

func TestSeedtoolRoundTrip(t *testing.T) {
	defer memguard.Purge()
	path := filepath.Join(t.TempDir(), "seed.age")
	var stdout bytes.Buffer
	input := strings.NewReader("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about\n")
	if err := run([]string{"--out", path, "--account", "0"}, input, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout.String(), "xpub") {
		t.Fatalf("xpub output = %q", stdout.String())
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("seed mode = %v, err = %v", info.Mode().Perm(), err)
		}
	}
	mnemonic, err := seed.DecryptFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wallet, err := hdwallet.FromMnemonicBuffer(mnemonic)
	if err != nil {
		t.Fatalf("decrypted mnemonic rejected: %v", err)
	}
	wallet.Destroy()
	if err := run([]string{"--out", path}, strings.NewReader("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about\n"), &stdout); err == nil {
		t.Fatal("existing seed was overwritten")
	}
}
