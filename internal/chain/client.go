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
	"payd/internal/store"
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
	errors    *errorCounter
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
	errors  *errorCounter
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
func New(cfg config.Tron, logger *slog.Logger, database *store.Store) (*Client, error) {
	if cfg.RequestTimeout != readTimeout {
		return nil, fmt.Errorf("TronGrid read timeout must be 10s (CHN-024)")
	}
	if cfg.DailyRequestQuota <= 0 {
		return nil, errors.New("TronGrid daily request quota must be positive")
	}
	if database == nil {
		return nil, errors.New("TronGrid client requires a store")
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
	counter, err := newDailyCounter(cfg.DailyRequestQuota, logger, database)
	if err != nil {
		return nil, fmt.Errorf("load TronGrid request history: %w", err)
	}
	errorCounts := newErrorCounter()
	httpClient := &http.Client{}
	core := &clientCore{
		http:    httpClient,
		pool:    &endpointPool{endpoints: states, backoff: time.Second, now: time.Now},
		counter: counter,
		errors:  errorCounts,
		sleep:   sleepContext,
	}
	solidityCore := &clientCore{
		http: httpClient,
		pool: &endpointPool{endpoints: []endpointState{{
			base: strings.TrimRight(cfg.SolidityURL, "/"), apiKey: solidityKey,
		}}, backoff: time.Second, now: time.Now},
		counter: counter,
		errors:  errorCounts,
		sleep:   sleepContext,
	}
	return &Client{
		Read:      &ReadClient{core: core},
		Solidity:  &SolidityClient{ReadClient: &ReadClient{core: solidityCore}},
		Broadcast: &BroadcastClient{core: core},
		counter:   counter,
		errors:    errorCounts,
	}, nil
}

// RequestsToday is the UTC-day request count used by the later metrics endpoint (CHN-023, DB-002a, RL-004).
func (c *Client) RequestsToday() int64 { return c.counter.requestsToday() }

// QuotaProjectionRatio is tomorrow's least-squares projection from the last
// seven complete UTC days, bounded below by today's actual usage (RL-006).
func (c *Client) QuotaProjectionRatio() float64 { return c.counter.projectionRatio() }

func (c *Client) ErrorCounts() map[string]uint64 { return c.errors.snapshot() }

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

func (c *ReadClient) GetTransactionInfoByID(ctx context.Context, txid string) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/gettransactioninfobyid", map[string]string{"value": txid})
}

func (c *SolidityClient) GetTransactionInfoByID(ctx context.Context, txid string) (json.RawMessage, error) {
	return c.post(ctx, "/walletsolidity/gettransactioninfobyid", map[string]string{"value": txid})
}

func (c *ReadClient) GetAccountResource(ctx context.Context, address string) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/getaccountresource", map[string]any{"address": address, "visible": true})
}

// DelegateResource builds an unsigned Stake 2.0 transaction; only the separately signed
// broadcast moves resources (RES-010).
func (c *ReadClient) DelegateResource(ctx context.Context, owner, receiver, resource string, balance int64) (json.RawMessage, error) {
	return c.post(ctx, "/wallet/delegateresource", map[string]any{
		"owner_address": owner, "receiver_address": receiver, "balance": balance,
		"resource": resource, "lock": false, "visible": true,
	})
}

func (c *ReadClient) GetCanDelegatedMaxSize(ctx context.Context, owner, resource string) (json.RawMessage, error) {
	typeCode := 0
	if resource == "ENERGY" {
		typeCode = 1
	}
	return c.post(ctx, "/wallet/getcandelegatedmaxsize", map[string]any{"owner_address": owner, "type": typeCode, "visible": true})
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
	if err := c.counter.increment(ctx); err != nil {
		return Response{}, fmt.Errorf("record TronGrid request: %w", err)
	}
	response, err := c.http.Do(request)
	if err != nil {
		c.errors.add("network")
		return Response{}, err
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		c.errors.add("read")
		return Response{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		c.errors.add(fmt.Sprint(response.StatusCode))
	}
	return Response{StatusCode: response.StatusCode, Body: responseBody}, nil
}

type errorCounter struct {
	mu     sync.Mutex
	counts map[string]uint64
}

func newErrorCounter() *errorCounter { return &errorCounter{counts: make(map[string]uint64)} }

func (c *errorCounter) add(code string) {
	c.mu.Lock()
	c.counts[code]++
	c.mu.Unlock()
}

func (c *errorCounter) snapshot() map[string]uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]uint64, len(c.counts))
	for code, count := range c.counts {
		result[code] = count
	}
	return result
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
	mu               sync.Mutex
	day              int64
	count            int64
	quota            int64
	softCap          int64
	projectionWarned bool
	softCapWarned    bool
	now              func() time.Time
	logger           *slog.Logger
	store            *store.Store
	counts           map[int64]int64
}

func newDailyCounter(quota int64, logger *slog.Logger, database *store.Store) (*dailyCounter, error) {
	c := &dailyCounter{quota: quota, softCap: quota * 70 / 100, now: time.Now, logger: logger,
		store: database, counts: make(map[int64]int64)}
	history, err := database.TronGridRequestHistory(context.Background(), c.now())
	if err != nil {
		return nil, err
	}
	for _, day := range history {
		c.counts[day.DayStart] = day.Requests
	}
	c.rollUTC()
	return c, nil
}

func (c *dailyCounter) increment(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollUTC()
	count, err := c.store.RecordTronGridRequest(ctx, c.now())
	if err != nil {
		return err
	}
	c.count, c.counts[c.day] = count, count
	warnAt := (c.softCap*80 + 99) / 100
	projection := c.projectionRatioLocked()
	shouldWarn := !c.projectionWarned && projection >= .60
	shouldSoftCapWarn := !c.softCapWarned && c.count >= warnAt
	if shouldWarn {
		c.projectionWarned = true
	}
	if shouldSoftCapWarn {
		c.softCapWarned = true
	}
	day := time.Unix(c.day, 0).UTC().Format(time.DateOnly)
	if shouldWarn {
		c.logger.Warn("TronGrid seven-day quota projection crossed 60%", "utc_day", day, "requests", count,
			"projection_ratio", projection, "daily_quota", c.quota) // RL-006
	}
	if shouldSoftCapWarn {
		c.logger.Warn("TronGrid daily request usage reached 80% of soft cap", "utc_day", day, "requests", count, "soft_cap", c.softCap) // CHN-023
	}
	return nil
}

func (c *dailyCounter) requestsToday() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollUTC()
	return c.count
}

func (c *dailyCounter) projectionRatio() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollUTC()
	return c.projectionRatioLocked()
}

func (c *dailyCounter) projectionRatioLocked() float64 {
	projected := float64(c.count)
	values := make([]float64, 7)
	complete := true
	for i := range 7 {
		day := c.day - int64(7-i)*int64(24*time.Hour/time.Second)
		count, ok := c.counts[day]
		if !ok {
			complete = false
			break
		}
		values[i] = float64(count)
	}
	if complete {
		var mean, numerator float64
		for _, value := range values {
			mean += value
		}
		mean /= float64(len(values))
		for i, value := range values {
			numerator += (float64(i) - 3) * (value - mean)
		}
		projected = max(projected, mean+4*numerator/28)
	}
	return max(projected, 0) / float64(c.quota)
}

func (c *dailyCounter) rollUTC() {
	day := c.now().UTC().Truncate(24 * time.Hour).Unix()
	if day != c.day {
		c.day, c.count, c.projectionWarned, c.softCapWarned = day, c.counts[day], false, false // DB-002a
		for recorded := range c.counts {
			if recorded < day-int64(7*24*time.Hour/time.Second) {
				delete(c.counts, recorded)
			}
		}
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
