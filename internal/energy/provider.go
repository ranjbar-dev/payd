// Package energy defines third-party resource rental without wallet signing authority.
package energy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"payd/internal/config"
)

// Provider is the exact ENR-001 resource-market boundary. resourceType is required.
type Provider interface {
	Quote(context.Context, string, string, int64, time.Duration) (Quote, error)
	Purchase(context.Context, Quote) (Order, error)
	Status(context.Context, string) (Status, error)
	Balance(context.Context) (trx string, err error)
}

type Quote struct {
	Receiver, ResourceType, PriceTRX, Reference string
	Amount                                      int64
	Duration                                    time.Duration
}

type Order struct {
	ID, State, ActualTRX, DelegationTxID string
}

type Status struct {
	State, ActualTRX, DelegationTxID string
}

type ResourceEstimate struct {
	EnergySource string
	TRXCost      string
	BlockedBy    string
}

// BurnCostSun is the live tier-3 formula: required energy × getEnergyFee (ENR-016).
func BurnCostSun(energyRequired, energyFee int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(energyRequired), big.NewInt(energyFee))
}

// New selects the configured concrete provider (ENR-002). Disabled rental has no provider.
func New(cfg config.Energy) (Provider, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.Provider != "tronzap" {
		return nil, fmt.Errorf("unsupported energy.provider %q", cfg.Provider)
	}
	return &tronZap{
		baseURL: strings.TrimRight(cfg.APIURL, "/"),
		token:   string(cfg.APIKey),
		secret:  string(cfg.APISecret),
		client:  &http.Client{Timeout: cfg.Timeout},
	}, nil
}

const externalOrderPrefix = "external:"

type tronZap struct {
	baseURL, token, secret string
	client                 *http.Client
}

func (p *tronZap) Quote(ctx context.Context, receiver, resourceType string, amount int64, duration time.Duration) (Quote, error) {
	hours, err := rentalHours(resourceType, amount, duration)
	if err != nil {
		return Quote{}, err
	}
	request := map[string]any{"address": receiver, "amount": amount, "duration": hours, "type": strings.ToLower(resourceType)}
	var response struct {
		Code   int `json:"code"`
		Result struct {
			Total decimalString `json:"total"`
		} `json:"result"`
	}
	if err := p.post(ctx, "/v1/calculate", request, &response); err != nil {
		return Quote{}, err
	}
	if response.Code != 0 || response.Result.Total == "" {
		return Quote{}, fmt.Errorf("tronzap quote failed with code %d", response.Code)
	}
	return Quote{Receiver: receiver, ResourceType: resourceType, Amount: amount, Duration: duration, PriceTRX: string(response.Result.Total)}, nil
}

func (p *tronZap) Purchase(ctx context.Context, quote Quote) (Order, error) {
	hours, err := rentalHours(quote.ResourceType, quote.Amount, quote.Duration)
	if err != nil {
		return Order{}, err
	}
	request := map[string]any{
		"external_id": quote.Reference,
		"service":     strings.ToLower(quote.ResourceType),
		"params": map[string]any{
			"address": quote.Receiver, "amount": quote.Amount, "duration": hours,
		},
	}
	var response transactionResponse
	if err := p.post(ctx, "/v1/transaction/new", request, &response); err != nil {
		return Order{}, err
	}
	if response.Code != 0 || response.Result.ID == "" {
		return Order{}, fmt.Errorf("tronzap purchase failed with code %d", response.Code)
	}
	return Order{ID: response.Result.ID, State: response.Result.Status, ActualTRX: string(response.Result.Amount), DelegationTxID: response.Result.Hash}, nil
}

func (p *tronZap) Status(ctx context.Context, orderID string) (Status, error) {
	request := map[string]any{"id": orderID}
	if strings.HasPrefix(orderID, externalOrderPrefix) {
		request = map[string]any{"external_id": strings.TrimPrefix(orderID, externalOrderPrefix)}
	}
	var response transactionResponse
	if err := p.post(ctx, "/v1/transaction/check", request, &response); err != nil {
		return Status{}, err
	}
	if response.Code != 0 {
		return Status{}, fmt.Errorf("tronzap status failed with code %d", response.Code)
	}
	return Status{State: response.Result.Status, ActualTRX: string(response.Result.Amount), DelegationTxID: response.Result.Hash}, nil
}

func (p *tronZap) Balance(ctx context.Context) (string, error) {
	var response struct {
		Code   int `json:"code"`
		Result struct {
			Balance decimalString `json:"balance"`
		} `json:"result"`
	}
	if err := p.post(ctx, "/v1/balance", map[string]any{}, &response); err != nil {
		return "", err
	}
	if response.Code != 0 || response.Result.Balance == "" {
		return "", fmt.Errorf("tronzap balance failed with code %d", response.Code)
	}
	return string(response.Result.Balance), nil
}

type transactionResponse struct {
	Code   int `json:"code"`
	Result struct {
		ID     string        `json:"id"`
		Status string        `json:"status"`
		Hash   string        `json:"hash"`
		Amount decimalString `json:"amount"`
	} `json:"result"`
}

func (p *tronZap) post(ctx context.Context, path string, payload, destination any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, _ = hash.Write(body)
	_, _ = hash.Write([]byte(p.secret))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("X-Signature", hex.EncodeToString(hash.Sum(nil)))
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req) // ENR-014: this dedicated client is outside the TronGrid quota client.
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("tronzap HTTP status %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(destination); err != nil {
		return fmt.Errorf("decode tronzap response: %w", err)
	}
	return nil
}

func rentalHours(resourceType string, amount int64, duration time.Duration) (int64, error) {
	if resourceType != "ENERGY" && resourceType != "BANDWIDTH" {
		return 0, errors.New("resourceType must be ENERGY or BANDWIDTH")
	}
	if amount <= 0 || duration <= 0 || duration%time.Hour != 0 {
		return 0, errors.New("rental amount and whole-hour duration must be positive")
	}
	return int64(duration / time.Hour), nil
}

type decimalString string

func (d *decimalString) UnmarshalJSON(raw []byte) error {
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
	}
	parsed, ok := new(big.Rat).SetString(value)
	if !ok || parsed.Sign() < 0 {
		return fmt.Errorf("invalid decimal %q", value)
	}
	*d = decimalString(value)
	return nil
}
