// Package httpx holds HTTP plumbing: JSON helpers, errors and middleware
// (request id, logging, recover, CORS, security headers, rate limiting, metrics).
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"pagdandi/api/internal/cache"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyUser
)

// RequestID returns the request's id.
func RequestID(ctx context.Context) string {
	s, _ := ctx.Value(keyRequestID).(string)
	return s
}

// WithUser stores an authenticated principal.
func WithUser(ctx context.Context, u any) context.Context { return context.WithValue(ctx, keyUser, u) }

// User returns the authenticated principal (or nil).
func User(ctx context.Context) any { return ctx.Value(keyUser) }

// APIError is the JSON error envelope {"error":{"code","message"}}.
type APIError struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Err builds an APIError.
func Err(status int, code, msg string) *APIError {
	return &APIError{Status: status, Code: code, Message: msg}
}

// WriteJSON writes v with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// WriteError writes an error envelope; non-API errors become 500s.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *APIError
	if !errors.As(err, &ae) {
		slog.ErrorContext(r.Context(), "internal error", "err", err, "request_id", RequestID(r.Context()))
		ae = Err(http.StatusInternalServerError, "INTERNAL", "Something went wrong on our side. Please try again.")
	}
	if ae.Status == http.StatusTooManyRequests {
		if ra, ok := ae.Details["retry_after_s"].(int); ok {
			w.Header().Set("Retry-After", strconv.Itoa(ra))
		}
	}
	WriteJSON(w, ae.Status, map[string]any{"error": ae})
}

// DecodeJSON reads a size-limited JSON body into v, rejecting unknown fields.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return Err(http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Request body is too large.")
		}
		return Err(http.StatusBadRequest, "BAD_JSON", "Invalid JSON: "+err.Error())
	}
	if _, err := dec.Token(); err != io.EOF {
		return Err(http.StatusBadRequest, "BAD_JSON", "Unexpected data after JSON body.")
	}
	return nil
}

// Handler is an http handler that returns an error.
type Handler func(w http.ResponseWriter, r *http.Request) error

// H adapts a Handler.
func H(h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			WriteError(w, r, err)
		}
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Observer receives per-request metrics.
type Observer func(route string, status int, d time.Duration)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware outermost-first.
func Chain(h http.Handler, ms ...Middleware) http.Handler {
	for i := len(ms) - 1; i >= 0; i-- {
		h = ms[i](h)
	}
	return h
}

// RequestIDs assigns a ULID to every request (honouring a sane inbound X-Request-ID).
func RequestIDs() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if len(id) == 0 || len(id) > 64 || strings.ContainsAny(id, " \t\r\n") {
				id = ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
			}
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyRequestID, id)))
		})
	}
}

// Logging logs one structured line per request and reports metrics.
func Logging(log *slog.Logger, obs Observer) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			d := time.Since(start)
			if sw.status == 0 {
				sw.status = http.StatusOK
			}
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			if obs != nil {
				obs(route, sw.status, d)
			}
			if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
				return
			}
			log.LogAttrs(r.Context(), slog.LevelInfo, "http",
				slog.String("request_id", RequestID(r.Context())),
				slog.String("method", r.Method), slog.String("route", route),
				slog.Int("status", sw.status), slog.Int("bytes", sw.bytes),
				slog.Float64("ms", float64(d.Microseconds())/1000))
		})
	}
}

// Recover turns panics into 500s.
func Recover(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					log.Error("panic", "value", fmt.Sprint(v), "stack", string(debug.Stack()), "request_id", RequestID(r.Context()))
					WriteError(w, r, Err(http.StatusInternalServerError, "INTERNAL", "Something went wrong on our side. Please try again."))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS allows credentialed requests from an allow-list of origins.
func CORS(origins []string) Middleware {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Add("Vary", "Origin")
				if r.Method == http.MethodOptions {
					h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
					h.Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
					h.Set("Access-Control-Max-Age", "600")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders sets CSP and friends on API responses.
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("X-Frame-Options", "DENY")
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the caller's IP, trusting X-Forwarded-For only from loopback/private proxies.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if cand := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(cand) != nil {
				return cand
			}
		}
	}
	return host
}

// RateLimit applies a token bucket keyed by the function's result. A nil key skips limiting.
func RateLimit(c cache.Cache, rate int, per time.Duration, key func(r *http.Request) (string, int)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c == nil {
				next.ServeHTTP(w, r)
				return
			}
			k, lim := key(r)
			if k == "" {
				next.ServeHTTP(w, r)
				return
			}
			if lim == 0 {
				lim = rate
			}
			ok, wait, err := c.Allow(r.Context(), k, lim, per)
			if err != nil {
				slog.Warn("rate limiter unavailable", "err", err)
			}
			if !ok {
				e := Err(http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests. Please wait a moment.")
				e.Details = map[string]any{"retry_after_s": int(wait.Seconds()) + 1}
				WriteError(w, r, e)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
