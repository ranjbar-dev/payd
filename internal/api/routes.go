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
	{http.MethodGet, "/api/v1/wallets/needs-resources", "wallets:read", func(s *Server) http.HandlerFunc { return s.walletsNeedingResources }},
	{http.MethodPost, "/api/v1/wallets/{address}/clear-drift", "wallets:write", func(s *Server) http.HandlerFunc { return s.clearBalanceDrift }},
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
