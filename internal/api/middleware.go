package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

type rateWindow struct {
	started time.Time
	count   int
}

type contextKey uint8

const stateContextKey contextKey = 1

type requestState struct {
	keyName string
	scopes  map[string]bool
}

func (s *Server) requireScope(scope string, handler http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if scope != "" && !requestStateFrom(r.Context()).scopes[scope] {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication failed", nil) // API-021
			return
		}
		handler(w, r)
	})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		supplied := r.Header.Get("X-API-Key")
		state := requestStateFrom(r.Context())
		for _, key := range s.keys {
			candidate := argon2.IDKey([]byte(supplied), key.salt, key.time, key.memory, key.threads, uint32(len(key.hash)))
			if subtle.ConstantTimeCompare(candidate, key.hash) == 1 {
				state.keyName, state.scopes = key.name, key.scopes
				next.ServeHTTP(w, r)
				return
			}
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication failed", nil) // API-020/021
	})
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := requestStateFrom(r.Context())
		limit, bucket := 100, "default"
		if strings.HasPrefix(r.URL.Path, "/api/v1/withdrawals") {
			limit, bucket = 10, "withdrawals"
		}
		now := time.Now()
		key := state.keyName + "\x00" + bucket
		s.rateMu.Lock()
		window := s.rates[key]
		if window.started.IsZero() || now.Sub(window.started) >= time.Minute {
			window = rateWindow{started: now}
		}
		window.count++
		s.rates[key] = window
		s.rateMu.Unlock()
		if window.count > limit {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		state := &requestState{}
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r.WithContext(context.WithValue(r.Context(), stateContextKey, state)))
		s.logger.Info("API request", "method", r.Method, "path", r.URL.Path, "key", state.keyName,
			"status", wrapped.status, "duration", time.Since(started)) // API-026: bodies/TOTP are never logged.
	})
}

func (s *Server) normalizeErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := &bufferedResponse{header: make(http.Header), status: http.StatusOK}
		next.ServeHTTP(buffer, r)
		if buffer.status >= 400 && !strings.HasPrefix(buffer.header.Get("Content-Type"), "application/json") {
			code, message := "http_error", http.StatusText(buffer.status)
			switch buffer.status {
			case http.StatusNotFound:
				code, message = "not_found", "route not found"
			case http.StatusMethodNotAllowed:
				code, message = "method_not_allowed", "method not allowed"
			}
			writeError(w, buffer.status, code, message, nil)
			return
		}
		copyHeader(w.Header(), buffer.header)
		w.WriteHeader(buffer.status)
		_, _ = w.Write(buffer.body.Bytes())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *bufferedResponse) Header() http.Header            { return w.header }
func (w *bufferedResponse) Write(body []byte) (int, error) { return w.body.Write(body) }
func (w *bufferedResponse) WriteHeader(status int)         { w.status = status }

func requestStateFrom(ctx context.Context) *requestState {
	state, _ := ctx.Value(stateContextKey).(*requestState)
	if state == nil {
		return &requestState{}
	}
	return state
}

func copyHeader(destination, source http.Header) {
	for key, values := range source {
		destination[key] = append([]string(nil), values...)
	}
}
