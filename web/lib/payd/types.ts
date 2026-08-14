import 'server-only';

import type { z } from 'zod';
import type {
  amountSchema,
  apiErrorSchema,
  assetsResponseSchema,
  auditResponseSchema,
  chainParametersSchema,
  chainQuotaResponseSchema,
  chainStatusResponseSchema,
  clearDriftResponseSchema,
  configResponseSchema,
  deadIpnPageSchema,
  energyPurchasePageSchema,
  energyStatusSchema,
  feesReportResponseSchema,
  fundedOrderListSchema,
  fundedOrderSchema,
  healthSchema,
  ipnConsumerPageSchema,
  ipnReplayResponseSchema,
  ipnRetryResponseSchema,
  ipnTestResponseSchema,
  operationalStatsSchema,
  orderDetailResponseSchema,
  orderEventsResponseSchema,
  orderListSchema,
  orderResolutionResponseSchema,
  orderSchema,
  paymentAttributeResponseSchema,
  paymentListSchema,
  paymentSchema,
  pricePageSchema,
  readinessSchema,
  resourceGrantSchema,
  resourceGrantsResponseSchema,
  resourceStateSchema,
  resourceWalletResponseSchema,
  volumeReportResponseSchema,
  walletDetailSchema,
  walletDisableResponseSchema,
  walletPageSchema,
  walletResourceListSchema,
  walletResourceSchema,
  whoamiResponseSchema,
  withdrawalEstimateResponseSchema,
  withdrawalLimitsSchema,
  withdrawalListSchema,
  withdrawalSchema,
  workersResponseSchema,
} from './schemas';

// Each alias is inferred from the OpenAPI-derived Zod schema in schemas.ts.
export type Amount = z.infer<typeof amountSchema>;
export type ApiError = z.infer<typeof apiErrorSchema>;
export type Order = z.infer<typeof orderSchema>;
export type Payment = z.infer<typeof paymentSchema>;
export type Withdrawal = z.infer<typeof withdrawalSchema>;
export type OrderList = z.infer<typeof orderListSchema>;
export type FundedOrder = z.infer<typeof fundedOrderSchema>;
export type FundedOrderList = z.infer<typeof fundedOrderListSchema>;
export type PaymentList = z.infer<typeof paymentListSchema>;
export type WithdrawalList = z.infer<typeof withdrawalListSchema>;
export type WithdrawalLimits = z.infer<typeof withdrawalLimitsSchema>;
export type ResourceState = z.infer<typeof resourceStateSchema>;
export type WalletResource = z.infer<typeof walletResourceSchema>;
export type WalletPage = z.infer<typeof walletPageSchema>;
export type WalletDetail = z.infer<typeof walletDetailSchema>;
export type WalletResourceList = z.infer<typeof walletResourceListSchema>;
export type ResourceGrant = z.infer<typeof resourceGrantSchema>;
export type DeadIpnPage = z.infer<typeof deadIpnPageSchema>;
export type IpnConsumerPage = z.infer<typeof ipnConsumerPageSchema>;
export type ChainParameters = z.infer<typeof chainParametersSchema>;
export type PricePage = z.infer<typeof pricePageSchema>;
export type OperationalStats = z.infer<typeof operationalStatsSchema>;
export type EnergyStatus = z.infer<typeof energyStatusSchema>;
export type EnergyPurchasePage = z.infer<typeof energyPurchasePageSchema>;
export type Health = z.infer<typeof healthSchema>;
export type Readiness = z.infer<typeof readinessSchema>;
export type ChainStatusResponse = z.infer<typeof chainStatusResponseSchema>;
export type ChainQuotaResponse = z.infer<typeof chainQuotaResponseSchema>;
export type WorkersResponse = z.infer<typeof workersResponseSchema>;
export type AuditResponse = z.infer<typeof auditResponseSchema>;
export type ResourceGrantsResponse = z.infer<typeof resourceGrantsResponseSchema>;
export type ResourceWalletResponse = z.infer<typeof resourceWalletResponseSchema>;
export type ConfigResponse = z.infer<typeof configResponseSchema>;
export type VolumeReportResponse = z.infer<typeof volumeReportResponseSchema>;
export type FeesReportResponse = z.infer<typeof feesReportResponseSchema>;
export type OrderEventsResponse = z.infer<typeof orderEventsResponseSchema>;
export type WithdrawalEstimateResponse = z.infer<typeof withdrawalEstimateResponseSchema>;
export type WhoamiResponse = z.infer<typeof whoamiResponseSchema>;
export type AssetsResponse = z.infer<typeof assetsResponseSchema>;
export type IpnTestResponse = z.infer<typeof ipnTestResponseSchema>;
export type IpnReplayResponse = z.infer<typeof ipnReplayResponseSchema>;
export type OrderDetailResponse = z.infer<typeof orderDetailResponseSchema>;
export type OrderResolutionResponse = z.infer<typeof orderResolutionResponseSchema>;
export type PaymentAttributeResponse = z.infer<typeof paymentAttributeResponseSchema>;
export type WalletDisableResponse = z.infer<typeof walletDisableResponseSchema>;
export type IpnRetryResponse = z.infer<typeof ipnRetryResponseSchema>;
export type ClearDriftResponse = z.infer<typeof clearDriftResponseSchema>;
