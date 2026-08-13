package store

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sort"
	"time"
)

type VolumeBucket struct {
	Key               string
	OrderCount        int64
	PaidCount         int64
	VolumeRaw         map[string]string
	USDTotal          string
	UnpricedPaidCount int64
}

type volumeAccumulator struct {
	orders, paid, unpriced int64
	volume                 map[string]*big.Int
	usd                    *big.Rat
}

// VolumeReport groups orders created in the inclusive UTC range (API-044, DB-002a).
// Volume is actual received value on paid/confirmed orders; USD totals use immutable order price snapshots.
func (s *Store) VolumeReport(ctx context.Context, from, to int64, groupBy string, decimals map[string]int) ([]VolumeBucket, error) {
	rows, err := s.normal.QueryContext(ctx, `SELECT asset,consumer,status,received_raw,price_usd,created_at
		FROM orders WHERE created_at>=? AND created_at<=? ORDER BY created_at,id`, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	buckets := make(map[string]*volumeAccumulator)
	for rows.Next() {
		var asset, consumer, status, received string
		var price sql.NullString
		var createdAt int64
		if err := rows.Scan(&asset, &consumer, &status, &received, &price, &createdAt); err != nil {
			return nil, err
		}
		key := consumer
		switch groupBy {
		case "day":
			key = time.Unix(createdAt, 0).UTC().Format("2006-01-02")
		case "asset":
			key = asset
		}
		bucket := buckets[key]
		if bucket == nil {
			bucket = &volumeAccumulator{volume: make(map[string]*big.Int), usd: new(big.Rat)}
			buckets[key] = bucket
		}
		bucket.orders++
		if status != "paid" && status != "confirmed" {
			continue
		}
		bucket.paid++
		amount, ok := new(big.Int).SetString(received, 10)
		if !ok || amount.Sign() < 0 {
			return nil, fmt.Errorf("invalid received amount %q", received)
		}
		if bucket.volume[asset] == nil {
			bucket.volume[asset] = new(big.Int)
		}
		bucket.volume[asset].Add(bucket.volume[asset], amount)
		if !price.Valid {
			bucket.unpriced++
			continue
		}
		quote, ok := new(big.Rat).SetString(price.String)
		decimalsValue, configured := decimals[asset]
		if !ok || !configured || decimalsValue < 0 {
			return nil, fmt.Errorf("invalid price snapshot or decimals for %s", asset)
		}
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimalsValue)), nil)
		usd := new(big.Rat).Mul(new(big.Rat).SetInt(amount), quote)
		usd.Quo(usd, new(big.Rat).SetInt(scale))
		bucket.usd.Add(bucket.usd, usd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	report := make([]VolumeBucket, 0, len(keys))
	for _, key := range keys {
		accumulator := buckets[key]
		volume := make(map[string]string, len(accumulator.volume))
		for asset, raw := range accumulator.volume {
			volume[asset] = raw.String()
		}
		report = append(report, VolumeBucket{Key: key, OrderCount: accumulator.orders, PaidCount: accumulator.paid,
			VolumeRaw: volume, USDTotal: rationalDecimal(accumulator.usd), UnpricedPaidCount: accumulator.unpriced})
	}
	return report, nil
}

type FeeReport struct {
	EnergyBySource    map[string]string
	BandwidthBySource map[string]string
	RentalSpendTRX    string
}

func (s *Store) FeeReport(ctx context.Context, from, to int64) (FeeReport, error) {
	report := FeeReport{EnergyBySource: map[string]string{"rented": "0", "burned": "0", "self_delegated": "0"},
		BandwidthBySource: map[string]string{"existing": "0", "delegated": "0", "topup": "0"}, RentalSpendTRX: "0"}
	energy, err := s.EnergyCostTotals(ctx, &from, &to)
	if err != nil {
		return report, err
	}
	for source, total := range energy {
		report.EnergyBySource[source] = total
	}
	rows, err := s.normal.QueryContext(ctx, `SELECT w.bandwidth_source,COALESCE(g.fee_raw,'0') FROM withdrawals w
		LEFT JOIN resource_grants g ON g.withdrawal_id=w.id AND g.resource_type='BANDWIDTH'
		WHERE w.created_at>=? AND w.created_at<=? AND w.bandwidth_source IS NOT NULL`, from, to)
	if err != nil {
		return report, err
	}
	bandwidth := make(map[string]*big.Rat)
	for rows.Next() {
		var source, raw string
		if err := rows.Scan(&source, &raw); err != nil {
			_ = rows.Close()
			return report, err
		}
		amount, ok := new(big.Int).SetString(raw, 10)
		if !ok || amount.Sign() < 0 {
			_ = rows.Close()
			return report, fmt.Errorf("invalid bandwidth fee %q", raw)
		}
		if bandwidth[source] == nil {
			bandwidth[source] = new(big.Rat)
		}
		bandwidth[source].Add(bandwidth[source], new(big.Rat).SetFrac(amount, big.NewInt(1_000_000)))
	}
	if err := rows.Close(); err != nil {
		return report, err
	}
	for source, total := range bandwidth {
		report.BandwidthBySource[source] = rationalDecimal(total)
	}
	var rentalRows *sql.Rows
	rentalRows, err = s.normal.QueryContext(ctx, `SELECT COALESCE(e.actual_trx,e.quoted_trx) FROM energy_purchases e
		JOIN withdrawals w ON w.id=e.withdrawal_id
		WHERE w.created_at>=? AND w.created_at<=? AND e.provider_order_id IS NOT NULL`, from, to)
	if err != nil {
		return report, err
	}
	rental := new(big.Rat)
	for rentalRows.Next() {
		var text string
		if err := rentalRows.Scan(&text); err != nil {
			_ = rentalRows.Close()
			return report, err
		}
		value, ok := new(big.Rat).SetString(text)
		if !ok || value.Sign() < 0 {
			_ = rentalRows.Close()
			return report, fmt.Errorf("invalid rental spend %q", text)
		}
		rental.Add(rental, value)
	}
	if err := rentalRows.Close(); err != nil {
		return report, err
	}
	report.RentalSpendTRX = rationalDecimal(rental)
	return report, nil
}
