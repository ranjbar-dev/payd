package ipn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"payd/internal/config"
)

// SendTest sends an unmistakable test event directly; no outbox state is created (API-035).
func SendTest(ctx context.Context, consumer config.Consumer, timeout time.Duration, now time.Time) (int, time.Duration, error) {
	body, err := json.Marshal(map[string]any{
		"event_id":   "test-" + strconv.FormatInt(now.UTC().UnixNano(), 10),
		"event_type": "test.ping",
		"consumer":   consumer.Name,
		"created_at": now.UTC().Unix(),
	})
	if err != nil {
		return 0, 0, err
	}
	timestamp := strconv.FormatInt(now.UTC().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, consumer.URL, bytes.NewReader(body))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-Id", "test-"+strconv.FormatInt(now.UTC().UnixNano(), 10))
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Consumer", consumer.Name)
	req.Header.Set("X-Signature", signature(consumer.Secret, timestamp, body))
	started := time.Now()
	response, err := (&http.Client{Timeout: timeout}).Do(req)
	latency := time.Since(started)
	if err != nil {
		return 0, latency, err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, latency, fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	return response.StatusCode, latency, nil
}
