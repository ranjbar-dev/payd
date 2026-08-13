package api

import (
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"payd/internal/store"
)

const (
	defaultExportRows = 10_000
	maximumExportRows = 100_000
)

func writeCSV(output *csv.Writer, record []string) error {
	for i, value := range record {
		if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
			record[i] = "'" + value
		}
	}
	return output.Write(record)
}

func exportLimit(r *http.Request) (int, error) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return defaultExportRows, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maximumExportRows {
		return 0, errors.New("limit must be between 1 and 100000")
	}
	return limit, nil
}

func (s *Server) exportOrdersCSV(w http.ResponseWriter, r *http.Request) {
	limit, err := exportLimit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error(), nil)
		return
	}
	filter, err := orderFilter(r, "", limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="orders.csv"`)
	output := csv.NewWriter(w)
	_ = writeCSV(output, []string{"id", "external_ref", "address", "asset", "expected_raw", "received_raw", "overpaid_raw",
		"status", "consumer", "expires_at", "created_at", "updated_at", "metadata"})
	err = s.store.StreamOrders(r.Context(), filter, limit, func(order store.Order) error {
		external := ""
		if order.ExternalRef != nil {
			external = *order.ExternalRef
		}
		return writeCSV(output, []string{order.ID, external, order.Address, order.Asset, order.ExpectedRaw, order.ReceivedRaw,
			order.OverpaidRaw, order.Status, order.Consumer, strconv.FormatInt(order.ExpiresAt, 10),
			strconv.FormatInt(order.CreatedAt, 10), strconv.FormatInt(order.UpdatedAt, 10), order.Metadata})
	})
	output.Flush()
	if err == nil {
		err = output.Error()
	}
	if err != nil {
		s.logger.Error("stream orders CSV", "error", err)
	}
}

func (s *Server) exportWithdrawalsCSV(w http.ResponseWriter, r *http.Request) {
	limit, err := exportLimit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="withdrawals.csv"`)
	output := csv.NewWriter(w)
	_ = writeCSV(output, []string{"id", "idempotency_key", "from_address", "to_address", "asset", "amount_raw", "amount_usd",
		"status", "txid", "fee_raw", "energy_source", "energy_cost_trx", "bandwidth_source", "failure_reason",
		"resolved_by", "requested_by", "requested_ip", "created_at"})
	err = s.store.StreamWithdrawals(r.Context(), r.URL.Query().Get("status"), limit, func(withdrawal store.Withdrawal) error {
		return writeCSV(output, []string{withdrawal.ID, withdrawal.IdempotencyKey, withdrawal.FromAddress, withdrawal.ToAddress,
			withdrawal.Asset, withdrawal.AmountRaw, withdrawal.AmountUSD, withdrawal.Status, withdrawal.TxID, withdrawal.FeeRaw,
			withdrawal.EnergySource, withdrawal.EnergyCostTRX, withdrawal.BandwidthSource, withdrawal.FailureReason,
			withdrawal.ResolvedBy, withdrawal.RequestedBy, withdrawal.RequestedIP, strconv.FormatInt(withdrawal.CreatedAt, 10)})
	})
	output.Flush()
	if err == nil {
		err = output.Error()
	}
	if err != nil {
		s.logger.Error("stream withdrawals CSV", "error", err)
	}
}
