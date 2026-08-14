import 'server-only';

import { z } from 'zod';

const openObject = <T extends z.ZodRawShape>(shape: T) => z.object(shape).catchall(z.unknown());
const unknownRecordSchema = z.record(z.string(), z.unknown());

// OpenAPI component: Amount.
export const amountSchema = z.string().regex(/^(0|[1-9][0-9]*)(\.[0-9]+)?$/);

// OpenAPI component: Error. `details` is explicitly free-form.
export const apiErrorSchema = z.strictObject({
  error: z.strictObject({
    code: z.string(),
    message: z.string(),
    details: unknownRecordSchema,
  }),
});

// OpenAPI components: Order, Payment, Withdrawal, and related response schemas.
export const orderSchema = openObject({
  id: z.string(),
  address: z.string(),
  asset: z.string(),
  amount: z.string(),
  received: z.string(),
  overpaid: z.string(),
  status: z.string(),
  consumer: z.string(),
  metadata: z.unknown(),
  expires_at: z.number().int(),
  created_at: z.number().int(),
  updated_at: z.number().int(),
  address_released_at: z.number().int().nullable(),
  external_ref: z.string().optional(),
  price_usd: z.string().optional(),
  amount_usd: z.string().optional(),
  resolution: z.string().optional(),
  resolution_note: z.string().optional(),
  resolved_at: z.number().int().optional(),
});

export const paymentSchema = z.strictObject({
  id: z.number().int(),
  txid: z.string(),
  log_index: z.number().int(),
  direction: z.string(),
  block_height: z.number().int(),
  block_id: z.string(),
  block_timestamp: z.number().int(),
  from_address: z.string(),
  to_address: z.string(),
  order_id: z.string().nullable(),
  asset: z.string(),
  amount: z.string(),
  // WPAY-021: integer base units, as the chain recorded them. `amount` is this
  // divided by the asset's configured decimals.
  amount_raw: z.string(),
  status: z.string(),
  detected_at: z.number().int(),
  confirmed_at: z.number().int().nullable(),
  is_dust: z.boolean(),
  // Which ORD-020 attribution condition failed, judged at match time. Null for
  // attributed payments and for rows detected before the field existed.
  unattributed_reason: z.enum(["no_active_order", "asset_mismatch", "outside_window"]).nullable(),
  // WPAY-005: the withdrawal an outbound row settles. Null for inbound rows and
  // for an outbound transfer this service did not broadcast.
  withdrawal_id: z.string().nullable(),
});

export const withdrawalSchema = z.strictObject({
  id: z.string(),
  from_address: z.string(),
  to_address: z.string(),
  asset: z.string(),
  amount: z.string(),
  amount_raw: z.string(),
  amount_usd: z.string(),
  status: z.string(),
  txid: z.string(),
  failure_reason: z.string(),
  resolved_by: z.string(),
  broadcast_response: z.string(),
  fee_raw: z.string(),
  network_fee_trx: z.string(),
  energy_used: z.number().int(),
  energy_source: z.string(),
  energy_cost_trx: z.string(),
  bandwidth_source: z.string(),
  bandwidth_cost_trx: z.string(),
  resource_fee_trx: z.string(),
  total_cost_trx: z.string(),
  last_lookup_error: z.string(),
  created_at: z.number().int(),
  // WWD-012: when this withdrawal entered its current status. Time in state,
  // not time since request.
  status_updated_at: z.number().int(),
  broadcast_at: z.number().int().nullable(),
  confirmed_at: z.number().int().nullable(),
});

export const orderListSchema = z.strictObject({ orders: z.array(orderSchema), next_cursor: z.string() });
export const fundedOrderSchema = orderSchema.extend({ payers: z.array(z.string()) });
export const fundedOrderListSchema = z.strictObject({ orders: z.array(fundedOrderSchema), next_cursor: z.string() });
export const paymentListSchema = z.strictObject({ payments: z.array(paymentSchema), next_cursor: z.string() });
export const withdrawalListSchema = z.strictObject({ items: z.array(withdrawalSchema), next_cursor: z.string() });
export const withdrawalLimitsSchema = z.strictObject({
  daily_limit_usd: z.string(),
  used_usd: z.string(),
  remaining_usd: z.string(),
});

export const resourceStateSchema = z.strictObject({
  available: z.number().int(),
  limit: z.number().int(),
  required: z.number().int(),
  sufficient: z.boolean(),
});

export const walletResourceSchema = z.strictObject({
  address: z.string(),
  hd_index: z.number().int(),
  state: z.enum(['free', 'assigned', 'cooling', 'disabled']),
  needs_resources: z.boolean(),
  balances: z.array(openObject({
    asset: z.string(),
    confirmed: z.string(),
    // WADR-021: same base-unit scale as chain_raw, so drift reads as a difference.
    confirmed_raw: z.string(),
    pending: z.string(),
    chain_raw: z.string().optional(),
    usd: z.string().optional(),
    drift_detected: z.boolean(),
  })),
  energy: resourceStateSchema,
  bandwidth: resourceStateSchema,
  trx_for_bandwidth_burn: z.string(),
  can_withdraw: z.record(z.string(), z.boolean()),
  blocked_by: z.array(z.string()),
  drift_detected: z.boolean(),
  checked_at: z.number().int().optional(),
  estimated_burn_trx: z.string().optional(),
  energy_fee_sun: z.number().int().optional(),
  // POOL-004/POOL-005: which order still holds this address and until when it
  // stays out of rotation. Both nullable — a free address holds neither. Without
  // them a cooling address is indistinguishable from a free one that has not been
  // reassigned yet (WADR-003, WADR-004).
  cooling_until: z.number().int().nullable(),
  assigned_order_id: z.string().nullable(),
});

export const walletPageSchema = z.strictObject({ wallets: z.array(walletResourceSchema), next_cursor: z.string() });
export const walletDetailSchema = walletResourceSchema.extend({ payments: z.array(paymentSchema), next_cursor: z.string() });
export const walletResourceListSchema = z.strictObject({ addresses: z.array(walletResourceSchema), total: z.number().int().nonnegative() });
export const resourceGrantSchema = z.strictObject({
  id: z.string(),
  address: z.string(),
  resource_type: z.enum(['ENERGY', 'BANDWIDTH']),
  amount: z.number().int(),
  stake_trx: z.string(),
  status: z.string(),
  txid: z.string(),
});

export const deadIpnPageSchema = z.strictObject({
  events: z.array(openObject({
    id: z.string(),
    order_id: z.string(),
    consumer: z.string(),
    event_type: z.string(),
    attempts: z.number().int(),
    last_error: z.string(),
    last_status_code: z.number().int(),
    created_at: z.number().int(),
    // WIPN-031: the body as composed when the event was queued. A snapshot, never
    // rewritten, so it can contradict the order's current status (WIPN-033).
    payload: z.record(z.string(), z.unknown()),
  })),
  next_cursor: z.string(),
});

export const ipnConsumerPageSchema = z.strictObject({
  consumers: z.array(z.strictObject({
    name: z.string(),
    enabled: z.boolean(),
    receives_global: z.boolean(),
    pending: z.number().int(),
    dead: z.number().int(),
  })),
  next_cursor: z.string(),
});

export const chainParametersSchema = z.strictObject({
  getEnergyFee: z.number().int(),
  getTransactionFee: z.number().int(),
  fetched_at: z.number().int(),
  stale: z.boolean(),
});

export const pricePageSchema = z.strictObject({
  prices: z.array(z.strictObject({
    symbol: z.string(),
    price_usd: z.string(),
    source: z.string(),
    fetched_at: z.number().int(),
    stale: z.boolean(),
  })),
  next_cursor: z.string(),
});

export const operationalStatsSchema = unknownRecordSchema;
export const energyStatusSchema = z.strictObject({
  provider: z.string(),
  balance_trx: z.string(),
  last_checked_at: z.number().int().nullable().optional(),
  last_error: z.string().optional(),
  consecutive_failures: z.number().int(),
  purchases: z.record(z.string(), z.number().int()),
});
export const energyPurchasePageSchema = z.strictObject({
  purchases: z.array(z.strictObject({
    id: z.string(),
    provider: z.string(),
    provider_order_id: z.string(),
    withdrawal_id: z.string(),
    receiver_address: z.string(),
    resource_type: z.enum(['ENERGY', 'BANDWIDTH']),
    amount: z.number().int(),
    duration_seconds: z.number().int(),
    quoted_trx: z.string(),
    actual_trx: z.string(),
    status: z.string(),
    failure_reason: z.string(),
    delegation_txid: z.string(),
    created_at: z.number().int(),
    delegated_at: z.number().int().nullable(),
  })),
  next_cursor: z.string(),
});
export const healthSchema = z.strictObject({ status: z.literal('ok') });
export const readinessSchema = z.strictObject({ status: z.enum(['ready', 'degraded']), reasons: z.array(z.string()).optional() });

// OpenAPI operation responses.
export const chainStatusResponseSchema = openObject({
  last_height: z.number().int(),
  solidified_height: z.number().int(),
  lag_blocks: z.number().int(),
  lag_seconds: z.number().int(),
  reorg_suspected: z.boolean(),
  last_block_timestamp: z.number().int(),
});
export const chainQuotaResponseSchema = openObject({
  requests_today: z.number().int(),
  daily_request_quota: z.number().int(),
  percent_used: z.number(),
  history: z.array(openObject({ day_start: z.number().int(), requests: z.number().int() })),
});
export const workersResponseSchema = openObject({
  workers: z.array(openObject({
    worker: z.string(),
    last_tick_at: z.number().int().nullable(),
    seconds_since_tick: z.number().int().nullable(),
    last_error: z.string(),
    error_count: z.number().int(),
    restarts: z.number().int(),
    expected_interval_seconds: z.number().int().nullable(),
  })),
  next_cursor: z.string(),
});
export const auditResponseSchema = openObject({
  entries: z.array(openObject({
    id: z.number().int(), actor: z.string(), action: z.string(), subject: z.string(), detail: z.string(), ip: z.string(), created_at: z.number().int(),
  })),
  next_cursor: z.string(),
});
export const resourceGrantsResponseSchema = openObject({ grants: z.array(unknownRecordSchema), next_cursor: z.string() });
export const resourceWalletResponseSchema = openObject({
  address: z.string(),
  trx_balance: z.string(),
  energy: unknownRecordSchema,
  bandwidth: unknownRecordSchema,
  outstanding_delegations: unknownRecordSchema,
});
export const configResponseSchema = openObject({
  assets: z.array(unknownRecordSchema),
  withdrawal: unknownRecordSchema,
  tron: unknownRecordSchema,
  orders: unknownRecordSchema,
  energy: z.strictObject({ enabled: z.boolean(), max_burn_trx: z.string(), balance_warn_trx: z.string() }),
  price: z.strictObject({ stale_after_seconds: z.number().int() }),
  wallet: z.strictObject({ pool_min_free: z.number().int(), pool_max_size: z.number().int(), cooldown_seconds: z.number().int() }),
  consumers: z.array(z.string()),
});
export const volumeReportResponseSchema = openObject({
  group_by: z.string(), from: z.number().int(), to: z.number().int(), buckets: z.array(unknownRecordSchema),
});
export const feesReportResponseSchema = openObject({
  from: z.number().int(),
  to: z.number().int(),
  energy_by_source_trx: z.record(z.string(), z.string()),
  bandwidth_by_source_trx: z.record(z.string(), z.string()),
  rental_spend_trx: z.string(),
});
export const orderEventsResponseSchema = openObject({
  events: z.array(openObject({
    id: z.string(), consumer: z.string(), event_type: z.string(), status: z.string(), attempts: z.number().int(), last_status_code: z.number().int(), last_error: z.string(), created_at: z.number().int(), delivered_at: z.number().int().nullable().optional(),
  })),
  next_cursor: z.string(),
});
export const withdrawalEstimateResponseSchema = openObject({
  // UI-060/WWD-070: what the service understood the request to be. The confirmation
  // screen restates the transfer from these, never from the form inputs. `amount` is
  // re-formatted from the parsed base units, so a normalised amount is visible before
  // the operator authorises it.
  from_address: z.string(),
  to_address: z.string(),
  asset: z.string(),
  amount: amountSchema,
  amount_raw: z.string(),
  amount_usd: z.string(),
  can_proceed: z.boolean(),
  confirmed_balance_sufficient: z.boolean(),
  trx_for_resources_sufficient: z.boolean(),
  projected_energy_source: z.enum(['existing', 'rented', 'self_delegated', 'burned', '']),
  projected_trx_cost: amountSchema,
  daily_cap_blocked: z.boolean(),
  blocked_by: z.array(z.enum(['withdrawals_disabled', 'confirmed_balance', 'trx_for_resources', 'daily_usd_cap', 'energy_unavailable', 'energy_burn_limit', 'chain_parameters_unavailable'])),
});
export const whoamiResponseSchema = openObject({ key_name: z.string(), scopes: z.array(z.string()) });
export const assetsResponseSchema = openObject({
  assets: z.array(openObject({ symbol: z.string(), kind: z.string(), contract: z.string(), decimals: z.number().int(), min_deposit: amountSchema, verified: z.boolean() })),
});
export const ipnTestResponseSchema = openObject({ status_code: z.number().int(), latency_ms: z.number().int() });
export const ipnReplayResponseSchema = openObject({ count: z.number().int().max(200) });
export const orderDetailResponseSchema = orderSchema.extend({ payments: z.array(paymentSchema), next_cursor: z.string() });
export const orderResolutionResponseSchema = openObject({ resolved: z.literal(true) });
export const paymentAttributeResponseSchema = openObject({ attributed: z.literal(true) });
export const walletDisableResponseSchema = openObject({ address: z.string(), state: z.literal('disabled') });
export const ipnRetryResponseSchema = openObject({ id: z.string(), status: z.literal('pending') });
export const clearDriftResponseSchema = openObject({ asset: z.string(), chain_raw: z.string(), drift_detected: z.literal(false) });
