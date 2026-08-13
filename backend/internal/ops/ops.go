// Package ops exposes process readiness and Prometheus metrics (OPS-001..007).
package ops

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"payd/internal/chain"
	"payd/internal/config"
	"payd/internal/follower"
	"payd/internal/ipn"
	"payd/internal/store"
)

type Observer struct {
	store    *store.Store
	chain    *chain.Client
	follower *follower.Worker
	ipn      *ipn.Dispatcher
	now      func() time.Time

	mu         sync.RWMutex
	staleAfter time.Duration
}

func New(database *store.Store, chainClient *chain.Client, followerWorker *follower.Worker,
	dispatcher *ipn.Dispatcher, cfg config.Config) *Observer {
	return &Observer{store: database, chain: chainClient, follower: followerWorker, ipn: dispatcher,
		now: time.Now, staleAfter: cfg.Price.StaleAfter}
}

func (o *Observer) UpdateConfig(cfg config.Config) {
	o.mu.Lock()
	o.staleAfter = cfg.Price.StaleAfter
	o.mu.Unlock()
}

func (o *Observer) Ready(ctx context.Context) []string {
	var reasons []string
	if err := o.store.Writable(ctx); err != nil {
		return []string{"database_unwritable"}
	}
	status, err := o.store.OperationalStatus(ctx)
	if err != nil {
		return []string{"database_unavailable"}
	}
	now := o.now().UTC().Unix()
	if lag := o.follower.LatestHeight() - status.LastHeight; lag > 20 {
		reasons = append(reasons, "chain_lag")
	}
	if status.SolidifiedUpdatedAt == 0 || now-status.SolidifiedUpdatedAt > 300 {
		reasons = append(reasons, "solidified_stale")
	}
	o.mu.RLock()
	staleAfter := o.staleAfter
	o.mu.RUnlock()
	if !status.HasPrice || now-status.OldestPriceFetchedAt > int64(staleAfter/time.Second) {
		reasons = append(reasons, "price_stale")
	}
	if o.follower.Halted() {
		reasons = append(reasons, "reorg_depth_exceeded")
	}
	if skew, checked := o.follower.ClockSkew(); !checked {
		reasons = append(reasons, "clock_unavailable")
	} else if skew > 30 {
		reasons = append(reasons, "clock_skew")
	}
	if o.chain.QuotaProjectionRatio() >= .90 {
		reasons = append(reasons, "trongrid_quota_projection")
	}
	return reasons
}

func (o *Observer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status, err := o.store.OperationalStatus(r.Context())
	if err != nil {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	database, err := o.store.OperationalMetrics(r.Context())
	if err != nil {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	now := o.now().UTC().Unix()
	lag := max(o.follower.LatestHeight()-status.LastHeight, 0)
	metric(w, "payd_chain_lag_blocks", "gauge", nil, strconv.FormatInt(lag, 10))
	metric(w, "payd_trongrid_requests_total", "gauge", nil, strconv.FormatInt(o.chain.RequestsToday(), 10))
	groupedMetric(w, "payd_trongrid_errors_total", "counter", "code", o.chain.ErrorCounts())
	metric(w, "payd_trongrid_stale_reads_total", "counter", nil, fmt.Sprint(o.follower.StaleReads()))
	metric(w, "payd_trongrid_quota_projection_ratio", "gauge", nil, formatFloat(o.chain.QuotaProjectionRatio()))
	metric(w, "payd_reorg_suspected_total", "counter", nil, fmt.Sprint(o.follower.ReorgSuspicions()))
	metric(w, "payd_reorg_confirmed_total", "counter", nil, fmt.Sprint(o.follower.ReorgsConfirmed()))
	groupedMetric(w, "payd_orders_total", "gauge", "status", database.Orders)
	groupedMetric(w, "payd_payments_total", "gauge", "status", database.Payments)
	metric(w, "payd_payments_orphaned_unresolved", "gauge", nil, fmt.Sprint(database.OrphanedUnresolved))
	attempts := o.ipn.AttemptMetrics()
	_, _ = fmt.Fprintln(w, "# TYPE payd_ipn_attempts_total counter")
	for _, attempt := range attempts {
		sample(w, "payd_ipn_attempts_total", map[string]string{"consumer": attempt.Consumer, "outcome": attempt.Outcome}, fmt.Sprint(attempt.Count))
	}
	groupedMetric(w, "payd_ipn_dead_total", "gauge", "consumer", database.IPNDead)
	groupedMetric(w, "payd_ipn_queue_depth", "gauge", "consumer", database.IPNQueue)
	priceAge := "+Inf"
	if status.HasPrice {
		priceAge = strconv.FormatInt(max(now-status.OldestPriceFetchedAt, 0), 10)
	}
	metric(w, "payd_price_age_seconds", "gauge", nil, priceAge)
	skew, _ := o.follower.ClockSkew()
	metric(w, "payd_clock_skew_seconds", "gauge", nil, strconv.FormatInt(skew, 10))
	groupedMetric(w, "payd_withdrawals_total", "gauge", "status", database.Withdrawals)
	metric(w, "payd_withdrawals_needs_operator", "gauge", nil, fmt.Sprint(database.NeedsOperator))
	metric(w, "payd_addresses_needing_resources", "gauge", nil, fmt.Sprint(database.AddressesNeeding))
	metric(w, "payd_addresses_with_balance", "gauge", nil, fmt.Sprint(database.AddressesFunded))
	metric(w, "payd_balance_drift_addresses", "gauge", nil, fmt.Sprint(database.BalanceDrift))
	groupedMetric(w, "payd_energy_purchases_total", "gauge", "outcome", database.EnergyPurchases)
	if _, ok := database.EnergyCosts["burned"]; !ok {
		database.EnergyCosts["burned"] = "0"
	}
	if _, ok := database.EnergyCosts["rented"]; !ok {
		database.EnergyCosts["rented"] = "0"
	}
	groupedMetric(w, "payd_energy_cost_trx_total", "counter", "source", database.EnergyCosts)
	metric(w, "payd_energy_fee_sun", "gauge", nil, fmt.Sprint(database.EnergyFeeSun))
	providerBalance := database.ProviderBalanceTRX
	if providerBalance == "" {
		providerBalance = "0"
	}
	metric(w, "payd_energy_provider_balance_trx", "gauge", nil, providerBalance)
}

func metric(w http.ResponseWriter, name, kind string, labels map[string]string, value string) {
	_, _ = fmt.Fprintf(w, "# TYPE %s %s\n", name, kind)
	sample(w, name, labels, value)
}

func groupedMetric[T ~uint64 | ~string](w http.ResponseWriter, name, kind, label string, values map[string]T) {
	_, _ = fmt.Fprintf(w, "# TYPE %s %s\n", name, kind)
	for _, key := range sortedKeys(values) {
		sample(w, name, map[string]string{label: key}, fmt.Sprint(values[key]))
	}
}

func sample(w http.ResponseWriter, name string, labels map[string]string, value string) {
	_, _ = fmt.Fprintf(w, "%s%s %s\n", name, labelText(labels), value)
}

func labelText(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := sortedKeys(labels)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(labels[key])
		parts = append(parts, fmt.Sprintf(`%s="%s"`, key, value))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatFloat(value float64) string { return strconv.FormatFloat(value, 'f', 6, 64) }
