package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" // RFC 6238 uses HMAC-SHA-1 by default.
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"payd/internal/config"
)

var (
	ErrInvalidTOTP = errors.New("invalid TOTP")
	ErrTOTPReplay  = errors.New("TOTP was already used")
)

type apiKey struct {
	name    string
	scopes  map[string]bool
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
	hash    []byte
}

// ValidateTOTP verifies the RFC 6238 +/-1-step window then atomically consumes the matching code (API-022).
func (s *Server) ValidateTOTP(ctx context.Context, code string, now time.Time) error {
	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) >= 0 || len(s.totpSecret) == 0 {
		return ErrInvalidTOTP
	}
	current := now.UTC().Unix() / 30
	for step := current - 1; step <= current+1; step++ {
		if subtle.ConstantTimeCompare([]byte(code), []byte(totpCode(s.totpSecret, step))) != 1 {
			continue
		}
		used, err := s.store.UseTOTP(ctx, code, step, now)
		if err != nil {
			return err
		}
		if !used {
			return ErrTOTPReplay
		}
		return nil
	}
	return ErrInvalidTOTP
}

func parseAPIKey(configured config.APIKey) (apiKey, error) {
	parts := strings.Split(strings.TrimPrefix(configured.KeyHash, "$"), "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return apiKey{}, errors.New("invalid Argon2id PHC string")
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil ||
		memory == 0 || iterations == 0 || threads == 0 || parts[2] != fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, threads) {
		return apiKey{}, errors.New("invalid Argon2id parameters")
	}
	salt, err := decodeBase64(parts[3])
	if err != nil || len(salt) == 0 {
		return apiKey{}, errors.New("invalid Argon2id salt")
	}
	hash, err := decodeBase64(parts[4])
	if err != nil || len(hash) == 0 {
		return apiKey{}, errors.New("invalid Argon2id hash")
	}
	scopes := make(map[string]bool, len(configured.Scopes))
	for _, scope := range configured.Scopes {
		scopes[scope] = true
	}
	return apiKey{name: configured.Name, scopes: scopes, memory: memory, time: iterations, threads: threads, salt: salt, hash: hash}, nil
}

func decodeBase64(value string) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(value)
	}
	return decoded, err
}

func totpCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}
