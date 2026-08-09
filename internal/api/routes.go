package api

import "net/http"

type route struct {
	method  string
	pattern string
	scope   string
	handler func(*Server) http.HandlerFunc
}

var apiRoutes = []route{
	{http.MethodPost, "/api/v1/orders", "orders:write", func(s *Server) http.HandlerFunc { return s.createOrder }},
	{http.MethodGet, "/api/v1/orders", "orders:read", func(s *Server) http.HandlerFunc { return s.listOrders }},
	{http.MethodGet, "/api/v1/orders/funded-terminal", "orders:read", func(s *Server) http.HandlerFunc { return s.listFundedTerminal }},
	{http.MethodGet, "/api/v1/orders/{id}", "orders:read", func(s *Server) http.HandlerFunc { return s.getOrder }},
	{http.MethodPost, "/api/v1/orders/{id}/cancel", "orders:write", func(s *Server) http.HandlerFunc { return s.cancelOrder }},
	{http.MethodPost, "/api/v1/orders/{id}/resolve", "orders:write", func(s *Server) http.HandlerFunc { return s.resolveOrder }},
	{http.MethodGet, "/api/v1/payments/unattributed", "orders:read", func(s *Server) http.HandlerFunc { return s.listUnattributed }},
	{http.MethodGet, "/api/v1/payments/orphaned", "orders:read", func(s *Server) http.HandlerFunc { return s.listOrphaned }},
	{http.MethodPost, "/api/v1/payments/{id}/attribute", "orders:write", func(s *Server) http.HandlerFunc { return s.attributePayment }},
	{http.MethodGet, "/api/v1/wallets", "wallets:read", func(s *Server) http.HandlerFunc { return s.listWallets }},
	{http.MethodGet, "/api/v1/wallets/with-balance", "wallets:read", func(s *Server) http.HandlerFunc { return s.walletsWithBalance }},
	{http.MethodGet, "/api/v1/wallets/needs-resources", "wallets:read", func(s *Server) http.HandlerFunc { return s.walletsNeedingResources }},
	{http.MethodGet, "/api/v1/wallets/{address}", "wallets:read", func(s *Server) http.HandlerFunc { return s.getWallet }},
	{http.MethodPost, "/api/v1/wallets/{address}/disable", "wallets:write", func(s *Server) http.HandlerFunc { return s.disableWallet }},
	{http.MethodPost, "/api/v1/wallets/{address}/delegate", "resources:write", func(s *Server) http.HandlerFunc { return s.delegateWallet }},
	{http.MethodPost, "/api/v1/wallets/{address}/clear-drift", "wallets:write", func(s *Server) http.HandlerFunc { return s.clearBalanceDrift }},
	{http.MethodGet, "/api/v1/ipn/dead", "orders:read", func(s *Server) http.HandlerFunc { return s.listDeadIPN }},
	{http.MethodPost, "/api/v1/ipn/{id}/retry", "orders:write", func(s *Server) http.HandlerFunc { return s.retryIPN }},
	{http.MethodGet, "/api/v1/ipn/consumers", "orders:read", func(s *Server) http.HandlerFunc { return s.listIPNConsumers }},
	{http.MethodGet, "/api/v1/chain/params", "wallets:read", func(s *Server) http.HandlerFunc { return s.chainParameters }},
	{http.MethodGet, "/api/v1/prices", "", func(s *Server) http.HandlerFunc { return s.listPrices }},
	{http.MethodGet, "/api/v1/stats", "", func(s *Server) http.HandlerFunc { return s.stats }},
	{http.MethodGet, "/api/v1/energy/status", "wallets:read", func(s *Server) http.HandlerFunc { return s.energyStatus }},
	{http.MethodGet, "/api/v1/energy/purchases", "wallets:read", func(s *Server) http.HandlerFunc { return s.listEnergyPurchases }},
	{http.MethodPost, "/api/v1/withdrawals", "withdrawals:write", func(s *Server) http.HandlerFunc { return s.createWithdrawal }},
	{http.MethodGet, "/api/v1/withdrawals", "withdrawals:read", func(s *Server) http.HandlerFunc { return s.listWithdrawals }},
	{http.MethodGet, "/api/v1/withdrawals/limits", "withdrawals:read", func(s *Server) http.HandlerFunc { return s.withdrawalLimits }},
	{http.MethodGet, "/api/v1/withdrawals/{id}", "withdrawals:read", func(s *Server) http.HandlerFunc { return s.getWithdrawal }},
}

var publicRoutes = []route{
	{http.MethodGet, "/healthz", "", func(s *Server) http.HandlerFunc { return s.health }},
	{http.MethodGet, "/readyz", "", func(s *Server) http.HandlerFunc { return s.ready }},
	{http.MethodGet, "/metrics", "", func(s *Server) http.HandlerFunc { return s.serveMetrics }},
	{http.MethodGet, "/openapi.yaml", "", func(s *Server) http.HandlerFunc { return s.openAPI }},
	{http.MethodGet, "/docs", "", func(s *Server) http.HandlerFunc { return s.swaggerUI }},
}
