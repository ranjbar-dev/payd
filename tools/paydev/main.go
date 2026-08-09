// Command paydev is a local development helper. It is NOT part of the payd
// daemon and never touches the database, the seed, or the chain.
//
// ponytail: one file, four subcommands. Split it only if it grows a fifth.
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // RFC 6238 uses HMAC-SHA-1, matching internal/api.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"golang.org/x/crypto/argon2"
)

const usage = `usage:
  paydev apikey                  generate an API key and its auth.api_keys[].key_hash
  paydev totp-secret             generate an auth.totp_secret (base32)
  paydev totp <base32-secret>    print the current 6-digit code for that secret
  paydev ipnsink <secret> [addr] run an IPN receiver that verifies X-Signature
                                 (addr defaults to 127.0.0.1:9090, path /ipn)`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "paydev:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
	case "apikey":
		key, hash, err := newAPIKey()
		if err != nil {
			return err
		}
		fmt.Printf("X-API-Key:  %s\nkey_hash:   %s\n", key, hash)
		return nil
	case "totp-secret":
		secret := make([]byte, 20)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
		fmt.Println(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret))
		return nil
	case "totp":
		if len(args) != 2 {
			return fmt.Errorf("%s", usage)
		}
		code, err := totp(args[1], time.Now())
		if err != nil {
			return err
		}
		fmt.Println(code)
		return nil
	case "ipnsink":
		if len(args) < 2 || len(args) > 3 {
			return fmt.Errorf("%s", usage)
		}
		addr := "127.0.0.1:9090"
		if len(args) == 3 {
			addr = args[2]
		}
		return ipnsink(args[1], addr)
	default:
		return fmt.Errorf("%s", usage)
	}
}

// newAPIKey returns a random key and the Argon2id PHC string internal/api.parseAPIKey expects.
func newAPIKey() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", "", err
	}
	key := base64.RawURLEncoding.EncodeToString(raw)
	const memory, iterations, threads = 65536, 3, 2
	sum := argon2.IDKey([]byte(key), salt, iterations, memory, threads, 32)
	hash := fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", memory, iterations, threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(sum))
	return key, hash, nil
}

func totp(secret string, now time.Time) (string, error) {
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(decoded) == 0 {
		return "", fmt.Errorf("secret is not valid base32")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.UTC().Unix()/30))
	mac := hmac.New(sha1.New, decoded)
	_, _ = mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000), nil
}

func ipnsink(secret, addr string) error {
	http.HandleFunc("/ipn", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		// Mirror internal/ipn.signature: hex(HMAC-SHA256(secret, timestamp + "." + body)).
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = io.WriteString(mac, r.Header.Get("X-Timestamp")+".")
		_, _ = mac.Write(body)
		want := hex.EncodeToString(mac.Sum(nil))
		valid := subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Signature")), []byte(want)) == 1
		var event struct {
			EventID       string `json:"event_id"`
			EventType     string `json:"event_type"`
			CurrentStatus string `json:"current_status"`
		}
		_ = json.Unmarshal(body, &event)
		fmt.Printf("signature=%v consumer=%q event=%s type=%s current_status=%s\n%s\n\n",
			valid, r.Header.Get("X-Consumer"), event.EventID, event.EventType, event.CurrentStatus, body)
		if !valid {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	fmt.Printf("ipnsink listening on http://%s/ipn\n", addr)
	server := &http.Server{Addr: addr, ReadHeaderTimeout: 5 * time.Second}
	return server.ListenAndServe()
}
