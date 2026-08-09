// Package ipn delivers signed transactional-outbox notifications (W-004).
package ipn

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"payd/internal/config"
	"payd/internal/store"
)

type runtimeConfig struct {
	consumers   map[string]string
	enabled     []string
	maxAttempts int
	backoff     []time.Duration
	client      *http.Client
	workers     int
}

type Dispatcher struct {
	store    *store.Store
	logger   *slog.Logger
	now      func() time.Time
	mu       sync.RWMutex
	config   runtimeConfig
	attempts map[attemptKey]uint64
}

type attemptKey struct{ consumer, outcome string }

type AttemptMetric struct {
	Consumer string
	Outcome  string
	Count    uint64
}

func New(database *store.Store, cfg config.IPN, logger *slog.Logger) (*Dispatcher, error) {
	if database == nil || cfg.Workers <= 0 || cfg.MaxAttempts <= 0 || len(cfg.Backoff) != cfg.MaxAttempts || cfg.Timeout <= 0 {
		return nil, errors.New("IPN dispatcher requires store, positive timeout/workers/max_attempts, and one backoff per attempt")
	}
	if logger == nil {
		logger = slog.Default()
	}
	d := &Dispatcher{store: database, logger: logger, now: time.Now, attempts: make(map[attemptKey]uint64)}
	d.UpdateConfig(cfg)
	return d, nil
}

// UpdateConfig changes delivery credentials and policy without mutating queued target URLs (IPN-004).
func (d *Dispatcher) UpdateConfig(cfg config.IPN) {
	consumers := make(map[string]string, len(cfg.Consumers))
	var enabled []string
	for _, consumer := range cfg.Consumers {
		consumers[consumer.Name] = consumer.Secret
		if consumer.Enabled {
			enabled = append(enabled, consumer.Name)
		}
	}
	slices.Sort(enabled)
	d.mu.Lock()
	d.config = runtimeConfig{
		consumers: consumers, enabled: enabled, maxAttempts: cfg.MaxAttempts,
		backoff: slices.Clone(cfg.Backoff), workers: cfg.Workers,
		client: &http.Client{
			Timeout:       cfg.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	d.mu.Unlock()
}

func (d *Dispatcher) Run(ctx context.Context) {
	if err := d.store.RecoverIPNClaims(ctx); err != nil {
		d.logger.Error("recover IPN claims", "error", err)
	}
	d.mu.RLock()
	workers := d.config.workers
	d.mu.RUnlock()
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			d.runWorker(ctx)
		}()
	}
	group.Wait()
}

func (d *Dispatcher) runWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second) // W-004
	defer ticker.Stop()
	for {
		delivered, err := d.DispatchOne(ctx)
		// ponytail: every dispatch goroutine reports into the one "ipn" row — the pool is
		// interchangeable, so liveness is a property of the pool, not of any single worker.
		// Give them per-goroutine row names only if you ever need to spot one wedged worker.
		_ = d.store.RecordWorkerTick(ctx, "ipn", err, time.Now()) // OPS-008
		if err != nil && !errors.Is(err, context.Canceled) {
			d.logger.Error("dispatch IPN", "error", err)
		}
		if delivered {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.store.OutboxReady():
		}
	}
}

// DispatchOne claims before network I/O and commits the outcome afterward (ARC-007).
func (d *Dispatcher) DispatchOne(ctx context.Context) (bool, error) {
	cfg := d.snapshotConfig()
	now := d.now().UTC()
	event, found, err := d.store.ClaimIPN(ctx, now, cfg.enabled)
	if err != nil || !found {
		return false, err
	}
	secret, ok := cfg.consumers[event.Consumer]
	if !ok {
		d.recordAttempt(event.Consumer, "failure")
		return true, d.store.FailIPN(ctx, event.ID, "consumer removed", nil, true, now)
	}
	currentStatus, err := d.store.IPNCurrentStatus(ctx, event)
	if err != nil {
		d.recordAttempt(event.Consumer, "failure")
		return true, d.fail(ctx, cfg, event, now, fmt.Errorf("load current status: %w", err), nil)
	}
	body, err := deliveryBody(event, currentStatus, now)
	if err != nil {
		d.recordAttempt(event.Consumer, "failure")
		return true, d.fail(ctx, cfg, event, now, err, nil)
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, event.TargetURL, bytes.NewReader(body))
	if err != nil {
		d.recordAttempt(event.Consumer, "failure")
		return true, d.fail(ctx, cfg, event, now, err, nil)
	}
	req.Header.Set("Content-Type", "application/json") // IPN-005
	req.Header.Set("X-Event-Id", event.ID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Consumer", event.Consumer)
	req.Header.Set("X-Signature", signature(secret, timestamp, body)) // IPN-006/007
	response, err := cfg.client.Do(req)
	if err != nil {
		d.recordAttempt(event.Consumer, "failure")
		return true, d.fail(ctx, cfg, event, now, err, nil)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 { // IPN-008
		d.recordAttempt(event.Consumer, "failure")
		return true, d.fail(ctx, cfg, event, now, fmt.Errorf("HTTP status %d", response.StatusCode), &response.StatusCode)
	}
	d.recordAttempt(event.Consumer, "success")
	return true, d.store.DeliverIPN(ctx, event.ID, response.StatusCode, d.now().UTC())
}

func (d *Dispatcher) recordAttempt(consumer, outcome string) {
	d.mu.Lock()
	d.attempts[attemptKey{consumer, outcome}]++
	d.mu.Unlock()
}

func (d *Dispatcher) AttemptMetrics() []AttemptMetric {
	d.mu.RLock()
	defer d.mu.RUnlock()
	metrics := make([]AttemptMetric, 0, len(d.attempts))
	for key, count := range d.attempts {
		metrics = append(metrics, AttemptMetric{Consumer: key.consumer, Outcome: key.outcome, Count: count})
	}
	return metrics
}

func (d *Dispatcher) snapshotConfig() runtimeConfig {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return runtimeConfig{
		consumers: d.config.consumers, enabled: d.config.enabled, maxAttempts: d.config.maxAttempts,
		backoff: d.config.backoff, client: d.config.client, workers: d.config.workers,
	}
}

func (d *Dispatcher) fail(ctx context.Context, cfg runtimeConfig, event store.IPNEvent, now time.Time, cause error, statusCode *int) error {
	dead := event.Attempts >= cfg.maxAttempts // IPN-009
	next := now
	if !dead {
		next = now.Add(cfg.backoff[event.Attempts-1])
	}
	if err := d.store.FailIPN(ctx, event.ID, cause.Error(), statusCode, dead, next); err != nil {
		return errors.Join(cause, err)
	}
	return nil
}

func deliveryBody(event store.IPNEvent, currentStatus string, now time.Time) ([]byte, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &body); err != nil {
		return nil, fmt.Errorf("decode immutable payload: %w", err)
	}
	status, _ := json.Marshal(currentStatus)
	age, _ := json.Marshal(max(now.Unix()-event.CreatedAt, 0))
	body["current_status"] = status
	body["snapshot_age_seconds"] = age
	return json.Marshal(body) // IPN-021a: only these two fields are added to the stored snapshot.
}

func signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, timestamp+".")
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
