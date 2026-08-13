package api

import "net/http"

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.store.OperationalMetrics(r.Context())
	if err != nil {
		s.logger.Error("load operational stats", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}
