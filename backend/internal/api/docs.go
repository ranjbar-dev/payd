package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openAPIDocument []byte

func (s *Server) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(openAPIDocument)
}
