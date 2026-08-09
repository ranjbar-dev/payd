package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
)

func exactUSD(raw *big.Int, decimals int, quote string) (string, bool) {
	price, ok := new(big.Rat).SetString(quote)
	if !ok || price.Sign() <= 0 {
		return "", false
	}
	value := new(big.Rat).Mul(new(big.Rat).SetInt(raw), price)
	value.Quo(value, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
	return decimalRat(value), true
}

func decimalRat(value *big.Rat) string {
	for digits := 0; digits <= 64; digits++ {
		text := value.FloatString(digits)
		if parsed, ok := new(big.Rat).SetString(text); ok && parsed.Cmp(value) == 0 {
			if strings.Contains(text, ".") {
				text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
			}
			return text
		}
	}
	return value.FloatString(64)
}

func amountUSD(raw string, decimals int, price string) (string, bool) {
	amount, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return "", false
	}
	quote, ok := new(big.Rat).SetString(price)
	if !ok {
		return "", false
	}
	value := new(big.Rat).Mul(new(big.Rat).SetInt(amount), quote)
	value.Quo(value, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
	return value.FloatString(2), true
}

func pagination(r *http.Request, numeric bool) (int, string, error) {
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 200 {
			return 0, "", errors.New("limit must be between 1 and 200")
		}
		limit = parsed
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor == "" {
		return limit, "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(decoded) == 0 {
		return 0, "", errors.New("cursor is invalid")
	}
	if numeric {
		if _, err := strconv.ParseInt(string(decoded), 10, 64); err != nil {
			return 0, "", errors.New("cursor is invalid")
		}
	}
	return limit, string(decoded), nil
}

func encodeCursor(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, allowEmpty bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "details": details}}) // API-024
}
