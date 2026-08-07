// Package chain provides the side-effect-aware TronGrid clients.
package chain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcutil/base58"

	"payd/internal/config"
)

const (
	readTimeout       = 10 * time.Second
	circuitRetryAfter = 60 * time.Second
)

var errNoEndpoint = errors.New("all TronGrid endpoint circuits are open")

// Client separates repeatable reads from irreversible broadcasts in its public shape.
type Client struct {
	Read      *ReadClient
	Solidity  *SolidityClient
	Broadcast *BroadcastClient
	counter   *dailyCounter
}

type ReadClient struct{ core *clientCore }
type SolidityClient struct{ *ReadClient }
type BroadcastClient struct{ core *clientCore }

type Response struct {
	StatusCode int
	Body       []byte
}

type HTTPError struct {
	StatusCode int
	Body       []byte
}

func (e *HTTPError) Error() string { return fmt.Sprintf("TronGrid returned HTTP %d", e.StatusCode) }

type clientCore struct {
	http    *http.Client
	pool    *endpointPool
	counter *dailyCounter
	sleep   func(context.Context, time.Duration) error
}

type endpointRef struct {
	index  int
	base   string
	apiKey string
}

type endpointState struct {
	base             string
	apiKey           string
	failures         int
	unavailableUntil time.Time
}

type endpointPool struct {
	mu        sync.Mutex
	endpoints []endpointState
	next      int
	backoff   time.Duration
	now       func() time.Time
}

// New validates the endpoint boundary and builds separate read and broadcast clients (CHN-020..025).
func New(cfg config.Tron, logger *slog.Logger) (*Client, error) {
	if cfg.RequestTimeout != readTimeout {
		return nil, fmt.Errorf("TronGrid read timeout must be 10s (CHN-024)")
	}
	if cfg.DailyRequestQuota <= 0 {
		return nil, errors.New("TronGrid daily request quota must be positive")
	}
	if logger == nil {
		logger = slog.Default()
	}
	states := make([]endpointState, 0, len(cfg.Endpoints))
	hosts := make(map[string]struct{}, len(cfg.Endpoints))
	for _, endpoint := range cfg.Endpoints {
		u, err := url.Parse(endpoint.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return nil, fmt.Errorf("invalid TronGrid endpoint %q", endpoint.URL)
		}
		host := strings.ToLower(u.Hostname())
		if _, exists := hosts[host]; exists {
			return nil, fmt.Errorf("duplicate TronGrid endpoint hostname %q (CHN-025/CFG-015)", host)
		}
		hosts[host] = struct{}{}
		states = append(states, endpointState{base: strings.TrimRight(endpoint.URL, "/"), apiKey: endpoint.APIKey})
	}
	if len(states) == 0 {
		return nil, errors.New("at least one TronGrid endpoint is required")
	}
	solidityURL, err := url.Parse(cfg.SolidityURL)
	if err != nil || (solidityURL.Scheme != "http" && solidityURL.Scheme != "https") || solidityURL.Hostname() == "" {
		return nil, fmt.Errorf("invalid solidity endpoint %q", cfg.SolidityURL)
	}
	var solidityKey string
	if _, shared := hosts[strings.ToLower(solidityURL.Hostname())]; shared {
		for _, endpoint := range states {
			endpointURL, _ := url.Parse(endpoint.base)
			if strings.EqualFold(endpointURL.Hostname(), solidityURL.Hostname()) {
				solidityKey = endpoint.apiKey
				break
			}
		}
		logger.Warn("solidity endpoint shares a TronGrid host; independent host preferred", "host", solidityURL.Hostname()) // CHN-026
	}
	counter := newDailyCounter(cfg.DailyRequestQuota, logger)
	httpClient := &http.Client{}
	core := &clientCore{
		http:    httpClient,
		pool:    &endpointPool{endpoints: states, backoff: time.Second, now: time.Now},
		counter: counter,
		sleep:   sleepContext,
	}
	solidityCore := &clientCore{
		http: httpClient,
		pool: &endpointPool{endpoints: []endpointState{{
			base: strings.TrimRight(cfg.SolidityURL, "/"), apiKey: solidityKey,
		}}, backoff: time.Second, now: time.Now},
		counter: counter,
		sleep:   sleepContext,
	}
	return &Client{
		Read:      &ReadClient{core: core},
		Solidity:  &SolidityClient{ReadClient: &ReadClient{core: solidityCore}},
		Broadcast: &BroadcastClient{core: core},
		counter:   counter,
	}, nil
}

// RequestsToday is the UTC-day request count used by the later metrics endpoint (CHN-023, DB-002a, RL-004).
func (c *Client) RequestsToday() int64 { return c.counter.requestsToday() }

// SoftCap is 70% of the configured quota, derived from RL-001 (CHN-023).
func (c *Client) SoftCap() int64 { return c.counter.softCap }

func (c *ReadClient) GetNowBlock(ctx context.Context) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/getnowblock", struct{}{})
}

func (c *SolidityClient) GetNowBlock(ctx context.Context) (json.RawMessage, error) {
	return c.post(ctx, "/walletsolidity/getnowblock", struct{}{})
}

func (c *ReadClient) GetBlockByNum(ctx context.Context, number int64) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/getblockbynum", map[string]int64{"num": number})
}

func (c *ReadClient) GetTransactionInfoByBlockNum(ctx context.Context, number int64) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/gettransactioninfobyblocknum", map[string]int64{"num": number})
}

func (c *ReadClient) GetTransactionByID(ctx context.Context, txid string) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/gettransactionbyid", map[string]string{"value": txid})
}

func (c *SolidityClient) GetTransactionInfoByID(ctx context.Context, txid string) (json.RawMessage, error) {
	return c.post(ctx, "/walletsolidity/gettransactioninfobyid", map[string]string{"value": txid})
}

func (c *ReadClient) GetAccountResource(ctx context.Context, address string) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/getaccountresource", map[string]any{"address": address, "visible": true})
}

func (c *ReadClient) GetAccount(ctx context.Context, address string) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/getaccount", map[string]any{"address": address, "visible": true})
}

// GetTRC20Balance performs the read-only balanceOf call used by RL-003.
func (c *ReadClient) GetTRC20Balance(ctx context.Context, address, contract string) (json.RawMessage, error) {
	payload, version, err := base58.CheckDecode(address)
	if err != nil || version != 0x41 || len(payload) != 20 {
		return nil, fmt.Errorf("invalid TRON address %q", address)
	}
	parameter := make([]byte, 32)
	copy(parameter[12:], payload)
	return c.post(ctx, "/wallet/triggerconstantcontract", map[string]any{
		"owner_address": address, "contract_address": contract, "visible": true,
		"function_selector": "balanceOf(address)", "parameter": hex.EncodeToString(parameter),
	})
}

func (c *ReadClient) GetChainParameters(ctx context.Context) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/getchainparameters", struct{}{})
}

// GetAccountHistory reads either /transactions or /transactions/trc20 and follows caller-supplied cursor parameters.
func (c *ReadClient) GetAccountHistory(ctx context.Context, address string, trc20 bool, query url.Values) (json.RawMessage, error) {
	path := "/v1/accounts/" + url.PathEscape(address) + "/transactions"
	if trc20 {
		path += "/trc20"
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	response, err := c.do(ctx, http.MethodGet, path, nil)
	return response.Body, err
}

func (c *ReadClient) post(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode TronGrid request: %w", err)
	}
	response, err := c.do(ctx, http.MethodPost, path, body)
	return response.Body, err
}

// do is the only retry loop. BroadcastClient cannot call it because it is a method on ReadClient (CHN-024/024a).
func (c *ReadClient) do(ctx context.Context, method, path string, body []byte) (Response, error) {
	networkRetries, throttledEndpoints := 0, 0
	for {
		endpoint, err := c.core.pool.choose()
		if err != nil {
			return Response{}, err
		}
		response, err := c.core.sendOnce(ctx, endpoint, method, path, body)
		if err != nil {
			c.core.pool.failed(endpoint.index, false)
			if ctx.Err() != nil {
				return Response{}, ctx.Err()
			}
			if networkRetries == 2 {
				return Response{}, fmt.Errorf("TronGrid network error after three attempts: %w", err)
			}
			networkRetries++
			continue
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			c.core.pool.succeeded(endpoint.index)
			return response, nil
		}
		throttled := response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden
		backoff := c.core.pool.failed(endpoint.index, throttled)
		httpErr := &HTTPError{StatusCode: response.StatusCode, Body: response.Body}
		if !throttled {
			return response, httpErr
		}
		throttledEndpoints++
		if throttledEndpoints >= len(c.core.pool.endpoints) {
			return response, httpErr
		}
		if err := c.core.sleep(ctx, backoff); err != nil {
			return response, err
		}
	}
}

// Send performs exactly one HTTP request. It has no access to ReadClient.do's retry loop (CHN-024a, WDR-000/014a).
func (c *BroadcastClient) Send(ctx context.Context, payload json.RawMessage) (Response, error) {
	endpoint, err := c.core.pool.choose()
	if err != nil {
		return Response{}, err
	}
	response, err := c.core.sendOnce(ctx, endpoint, http.MethodPost, "/wallet/broadcasttransaction", payload)
	if err != nil {
		c.core.pool.failed(endpoint.index, false)
		return Response{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		throttled := response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden
		c.core.pool.failed(endpoint.index, throttled)
		return response, &HTTPError{StatusCode: response.StatusCode, Body: response.Body}
	}
	c.core.pool.succeeded(endpoint.index)
	return response, nil
}

func (c *clientCore) sendOnce(ctx context.Context, endpoint endpointRef, method, path string, body []byte) (Response, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(attemptCtx, method, endpoint.base+path, reader)
	if err != nil {
		return Response{}, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if endpoint.apiKey != "" {
		request.Header.Set("TRON-PRO-API-KEY", endpoint.apiKey) // CHN-020
	}
	c.counter.increment()
	response, err := c.http.Do(request)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return Response{}, err
	}
	return Response{StatusCode: response.StatusCode, Body: responseBody}, nil
}

func (p *endpointPool) choose() (endpointRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for offset := range len(p.endpoints) {
		index := (p.next + offset) % len(p.endpoints)
		endpoint := &p.endpoints[index]
		if !now.Before(endpoint.unavailableUntil) {
			return endpointRef{index: index, base: endpoint.base, apiKey: endpoint.apiKey}, nil
		}
	}
	return endpointRef{}, errNoEndpoint
}

func (p *endpointPool) failed(index int, throttled bool) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	endpoint := &p.endpoints[index]
	endpoint.failures++
	now := p.now()
	var delay time.Duration
	if throttled {
		delay = p.backoff
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		endpoint.unavailableUntil = now.Add(delay)
		p.backoff = min(delay*2, 30*time.Second)
	}
	if endpoint.failures >= 5 {
		endpoint.unavailableUntil = now.Add(circuitRetryAfter) // CHN-022
	}
	p.next = (index + 1) % len(p.endpoints)
	return delay
}

func (p *endpointPool) succeeded(index int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.endpoints[index].failures = 0
	p.next = index
	p.backoff = time.Second
}

type dailyCounter struct {
	mu      sync.Mutex
	day     string
	count   int64
	softCap int64
	warned  bool
	now     func() time.Time
	logger  *slog.Logger
}

func newDailyCounter(quota int64, logger *slog.Logger) *dailyCounter {
	return &dailyCounter{softCap: quota * 70 / 100, now: time.Now, logger: logger}
}

func (c *dailyCounter) increment() {
	c.mu.Lock()
	c.rollUTC()
	c.count++
	warnAt := (c.softCap*80 + 99) / 100
	shouldWarn := !c.warned && c.count >= warnAt
	if shouldWarn {
		c.warned = true
	}
	count, day := c.count, c.day
	c.mu.Unlock()
	if shouldWarn {
		c.logger.Warn("TronGrid daily request usage reached 80% of soft cap", "utc_day", day, "requests", count, "soft_cap", c.softCap) // CHN-023
	}
}

func (c *dailyCounter) requestsToday() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollUTC()
	return c.count
}

func (c *dailyCounter) rollUTC() {
	day := c.now().UTC().Format(time.DateOnly)
	if day != c.day {
		c.day, c.count, c.warned = day, 0, false // DB-002a
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
