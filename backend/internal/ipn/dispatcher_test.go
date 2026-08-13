package ipn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"payd/internal/config"
	"payd/internal/store"
)

type receivedRequest struct {
	header http.Header
	body   []byte
}

// TST-011: a blocked pair cannot occupy another pair's worker, and signatures use recipient secrets.
func TestSlowConsumerDoesNotBlockSignedDeliveryToAnother(t *testing.T) {
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	slowRequest := make(chan receivedRequest, 1)
	fastRequest := make(chan receivedRequest, 1)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slowRequest <- captureRequest(t, r)
		close(slowStarted)
		<-releaseSlow
		w.WriteHeader(http.StatusNoContent)
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fastRequest <- captureRequest(t, r)
		w.WriteHeader(http.StatusOK)
	}))
	defer fast.Close()

	cfg := testConfig(slow.URL, fast.URL, 2)
	database := testStore(t)
	events := store.NewEventConfig(cfg, nil)
	if err := database.EnqueueGlobalEvent(context.Background(), events, "global", "balance.drift_detected", map[string]any{"status": "open"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	dispatcher, err := New(database, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { dispatcher.Run(ctx); close(done) }()

	wait(t, slowStarted, "slow consumer did not start")
	fastCall := wait(t, fastRequest, "fast consumer was delayed by slow consumer")
	slowCall := wait(t, slowRequest, "slow request was not captured")
	assertSignature(t, fastCall, "fast-secret", "fast")
	assertSignature(t, slowCall, "slow-secret", "slow")
	close(releaseSlow)
	cancel()
	wait(t, done, "dispatcher did not stop")
}

// TST-023: even concurrent workers cannot claim a later row while the pair's head is unresolved.
func TestWithdrawalEventsStayOrderedUnderConcurrentDispatch(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	received := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			EventType string `json:"event_type"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Error(err)
		}
		received <- payload.EventType
		if payload.EventType == "withdrawal.broadcast" {
			close(firstStarted)
			<-releaseFirst
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testConfig(server.URL, "", 4)
	cfg.Consumers = cfg.Consumers[:1]
	database := testStore(t)
	events := store.NewEventConfig(cfg, nil)
	now := time.Now()
	for _, eventType := range []string{"withdrawal.broadcast", "withdrawal.confirmed"} {
		if err := database.EnqueueGlobalEvent(context.Background(), events, "withdrawal:w-1", eventType, map[string]any{"status": eventType}, now); err != nil {
			t.Fatal(err)
		}
	}
	dispatcher, err := New(database, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { dispatcher.Run(ctx); close(done) }()

	wait(t, firstStarted, "broadcast did not start")
	if first := wait(t, received, "broadcast was not received"); first != "withdrawal.broadcast" {
		t.Fatalf("first event = %q", first)
	}
	select {
	case eventType := <-received:
		t.Fatalf("later event %q bypassed unresolved broadcast", eventType)
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseFirst)
	if second := wait(t, received, "confirmed did not follow broadcast"); second != "withdrawal.confirmed" {
		t.Fatalf("second event = %q", second)
	}
	cancel()
	wait(t, done, "dispatcher did not stop")
}

func TestBackoffDeadLetterAndManualRetry(t *testing.T) {
	var status atomic.Int64
	status.Store(http.StatusInternalServerError)
	ids := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids <- r.Header.Get("X-Event-Id")
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	cfg := testConfig(server.URL, "", 1)
	cfg.Consumers = cfg.Consumers[:1]
	database := testStore(t)
	now := time.Unix(100, 0)
	if err := database.EnqueueGlobalEvent(context.Background(), store.NewEventConfig(cfg, nil), "global", "energy.balance_low", map[string]any{"status": "low"}, now); err != nil {
		t.Fatal(err)
	}
	dispatcher, err := New(database, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.now = func() time.Time { return now }
	if sent, err := dispatcher.DispatchOne(context.Background()); err != nil || !sent {
		t.Fatalf("first attempt sent=%v err=%v", sent, err)
	}
	id := wait(t, ids, "first attempt missing")
	if sent, err := dispatcher.DispatchOne(context.Background()); err != nil || sent {
		t.Fatalf("backoff claim sent=%v err=%v", sent, err)
	}
	now = now.Add(11 * time.Second)
	if sent, err := dispatcher.DispatchOne(context.Background()); err != nil || !sent {
		t.Fatalf("second attempt sent=%v err=%v", sent, err)
	}
	if secondID := wait(t, ids, "second attempt missing"); secondID != id {
		t.Fatal("retry changed the idempotency key")
	}
	now = now.Add(time.Hour)
	if sent, err := dispatcher.DispatchOne(context.Background()); err != nil || sent {
		t.Fatalf("dead event sent=%v err=%v", sent, err)
	}
	if retried, err := database.RetryIPN(context.Background(), id, now); err != nil || !retried {
		t.Fatalf("manual retry reset=%v err=%v", retried, err)
	}
	status.Store(http.StatusNoContent)
	if sent, err := dispatcher.DispatchOne(context.Background()); err != nil || !sent {
		t.Fatalf("manual delivery sent=%v err=%v", sent, err)
	}
}

func testConfig(slowURL, fastURL string, workers int) config.IPN {
	return config.IPN{
		Consumers: []config.Consumer{
			{Name: "slow", URL: slowURL, Secret: "slow-secret", ReceivesGlobal: true, Enabled: true},
			{Name: "fast", URL: fastURL, Secret: "fast-secret", ReceivesGlobal: true, Enabled: true},
		},
		DefaultConsumer: "slow", Timeout: time.Second, MaxAttempts: 2, Workers: workers,
		Backoff: []time.Duration{10 * time.Second, 20 * time.Second},
	}
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "payd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func captureRequest(t *testing.T, request *http.Request) receivedRequest {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Error(err)
	}
	return receivedRequest{header: request.Header.Clone(), body: body}
}

func assertSignature(t *testing.T, request receivedRequest, secret, consumer string) {
	t.Helper()
	if request.header.Get("X-Consumer") != consumer || request.header.Get("X-Event-Id") == "" {
		t.Fatalf("incorrect delivery headers: %v", request.header)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(request.header.Get("X-Timestamp") + "."))
	_, _ = mac.Write(request.body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(request.header.Get("X-Signature")), []byte(want)) {
		t.Fatal("signature does not match consumer secret")
	}
}

func wait[T any](t *testing.T, channel <-chan T, message string) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal(message)
		var zero T
		return zero
	}
}
