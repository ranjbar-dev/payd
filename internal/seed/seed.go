// Package seed owns the deployment key and encrypted mnemonic file format.
package seed

import (
	"crypto/rand"
	_ "embed"
	"errors"
	"fmt"
	"os"

	"github.com/awnumar/memguard"
	"golang.org/x/crypto/nacl/secretbox"
)

const (
	header    = "PAYDSEED\x01"
	nonceSize = 24
)

// KEY-002/003: this ignored file must contain exactly 32 random bytes generated per deployment.
//
//go:embed seed.key
var embeddedKey []byte

func key() ([32]byte, error) {
	var key [32]byte
	if len(embeddedKey) != len(key) {
		return key, fmt.Errorf("embedded deployment key must be exactly %d bytes; got %d", len(key), len(embeddedKey))
	}
	copy(key[:], embeddedKey)
	return key, nil
}

// Encrypt seals a locked mnemonic buffer with NaCl secretbox (KEY-002).
func Encrypt(mnemonic *memguard.LockedBuffer) ([]byte, error) {
	if mnemonic == nil || !mnemonic.IsAlive() || mnemonic.Size() == 0 {
		return nil, errors.New("mnemonic buffer is empty or destroyed")
	}
	k, err := key()
	if err != nil {
		return nil, err
	}
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("generate encryption nonce: %w", err)
	}
	out := make([]byte, len(header)+nonceSize, len(header)+nonceSize+mnemonic.Size()+secretbox.Overhead)
	copy(out, header)
	copy(out[len(header):], nonce[:])
	return secretbox.Seal(out, mnemonic.Bytes(), &nonce, &k), nil
}

// DecryptFile opens an encrypted seed directly into locked memory (KEY-005, KEY-007).
func DecryptFile(path string) (*memguard.LockedBuffer, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read encrypted seed: %w", err)
	}
	minimum := len(header) + nonceSize + secretbox.Overhead
	if len(blob) < minimum || string(blob[:len(header)]) != header {
		return nil, errors.New("encrypted seed has an invalid format")
	}
	k, err := key()
	if err != nil {
		return nil, err
	}
	var nonce [nonceSize]byte
	copy(nonce[:], blob[len(header):len(header)+nonceSize])
	ciphertext := blob[len(header)+nonceSize:]
	plaintext := memguard.NewBuffer(len(ciphertext) - secretbox.Overhead)
	if _, ok := secretbox.Open(plaintext.Bytes()[:0], ciphertext, &nonce, &k); !ok {
		plaintext.Destroy()
		return nil, errors.New("encrypted seed authentication failed")
	}
	plaintext.Freeze()
	return plaintext, nil
}

// WriteExclusive creates a new encrypted seed without overwriting an existing one (KEY-004).
func WriteExclusive(path string, blob []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create encrypted seed: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = f.Close()
			_ = os.Remove(path)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("set encrypted seed mode: %w", err)
	}
	if _, err := f.Write(blob); err != nil {
		return fmt.Errorf("write encrypted seed: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync encrypted seed: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close encrypted seed: %w", err)
	}
	complete = true
	return nil
}
