package api

import "net/http"

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	check := s.burnCeilingHealthy
	readyChecks := s.readyChecks
	s.mu.RUnlock()
	var reasons []string
	if check != nil && !check() {
		reasons = append(reasons, "energy_burn_ceiling") // ENR-017
	}
	if readyChecks != nil {
		reasons = append(reasons, readyChecks(r.Context())...)
	}
	if len(reasons) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "degraded", "reasons": reasons}) // OPS-001
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"}) // OPS-002
}

func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	handler := s.metrics
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, r)
}
