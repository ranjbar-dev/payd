package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/awnumar/memguard"
	hdwallet "github.com/ranjbar-dev/hd-wallet"

	"payd/internal/seed"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "seedtool:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	defer memguard.Purge()
	flags := flag.NewFlagSet("seedtool", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	out := flags.String("out", "seed.age", "encrypted seed output path")
	account := flags.Uint("account", 0, "BIP-44 account index")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *account >= 1<<31 {
		return errors.New("usage: seedtool [--out seed.age] [--account 0] < mnemonic")
	}

	// KEY-001/007: stdin goes directly into locked memory and is never converted to string.
	mnemonic, err := memguard.NewBufferFromEntireReader(io.LimitReader(stdin, 1025))
	if err != nil {
		return errors.New("could not read mnemonic from stdin")
	}
	if mnemonic.Size() == 0 || mnemonic.Size() > 1024 {
		mnemonic.Destroy()
		return errors.New("mnemonic input must be 1..1024 bytes")
	}
	blob, err := seed.Encrypt(mnemonic)
	if err != nil {
		mnemonic.Destroy()
		return fmt.Errorf("encrypt mnemonic: %w", err)
	}
	wallet, err := hdwallet.FromMnemonicBuffer(mnemonic)
	if err != nil {
		return errors.New("invalid BIP-39 mnemonic")
	}
	defer wallet.Destroy()
	xpub, err := wallet.AccountXPub(hdwallet.TRX, uint32(*account))
	if err != nil {
		return errors.New("could not derive TRX account xpub")
	}
	if err := seed.WriteExclusive(*out, blob); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, xpub) // KEY-008
	return err
}
