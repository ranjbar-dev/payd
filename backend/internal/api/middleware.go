package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"net"
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
		identity, limit, bucket := state.keyName, 100, "default"
		preAuth := identity == ""
		if preAuth {
			limit, bucket = 10, "preauth"
			identity, _, _ = net.SplitHostPort(r.RemoteAddr)
			if identity == "" {
				identity = r.RemoteAddr
			}
			if ip := net.ParseIP(identity); ip != nil {
				identity = ip.String()
			}
			if identity == "" {
				identity = "unknown"
			}
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/withdrawals") {
			limit, bucket = 10, "withdrawals"
		}
		now := time.Now()
		key := identity + "\x00" + bucket
		s.rateMu.Lock()
		if s.lastRateSweep.IsZero() || now.Sub(s.lastRateSweep) >= time.Minute {
			for candidate, entry := range s.rates {
				if now.Sub(entry.started) >= time.Minute {
					delete(s.rates, candidate)
				}
			}
			s.lastRateSweep = now
		}
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
		if preAuth {
			defer func() {
				if state.keyName == "" {
					return
				}
				s.rateMu.Lock()
				current := s.rates[key]
				if current.started.Equal(window.started) {
					current.count--
					if current.count == 0 {
						delete(s.rates, key)
					} else {
						s.rates[key] = current
					}
				}
				s.rateMu.Unlock()
			}()
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
		buffer := &bufferedResponse{real: w, header: make(http.Header), status: http.StatusOK}
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
		buffer.flush()
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
	real    http.ResponseWriter
	header  http.Header
	body    bytes.Buffer
	status  int
	wrote   bool
	flushed bool
}

func (w *bufferedResponse) Header() http.Header {
	if w.flushed {
		return w.real.Header()
	}
	return w.header
}

func (w *bufferedResponse) Write(body []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if w.flushed {
		return w.real.Write(body)
	}
	return w.body.Write(body)
}

func (w *bufferedResponse) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote, w.status = true, status
	if status < http.StatusBadRequest {
		w.flush() // API-046: successful exports must reach the client while rows are produced.
	}
}

func (w *bufferedResponse) flush() {
	if w.flushed {
		return
	}
	copyHeader(w.real.Header(), w.header)
	w.real.WriteHeader(w.status)
	w.flushed = true
	if w.body.Len() > 0 {
		_, _ = w.real.Write(w.body.Bytes())
	}
}

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
