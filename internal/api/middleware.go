package api

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// A middleware is a function that takes a handler and returns a new
// handler that does something before (or after) calling it. Chaining them
// is how Go web code adds auth, logging and so on without a framework.
//
// The shape is always the same:
//
//	func mw(next http.Handler) http.Handler {
//		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			// before: may answer the request itself and return early
//			next.ServeHTTP(w, r)
//			// after
//		})
//	}
//
// http.HandlerFunc is a type conversion, not a call: it turns a plain
// function with the right signature into something that satisfies the
// http.Handler interface (it has a ServeHTTP method that calls itself).
// The returned function is a closure: it keeps access to next (and to
// token or l below) after mw has returned.

// requireToken rejects requests without "Authorization: Bearer <token>".
func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		got := r.Header.Get("Authorization")

		if subtle.ConstantTimeCompare(want, []byte(got)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimit rejects requests when the limiter is out of tokens.
//
// rate.Limiter is a token bucket: it holds up to `burst` tokens and refills
// at `r` tokens a second. Allow() takes one token if there is one and
// returns true, or returns false straight away if the bucket is empty.
// It's safe to call from many goroutines at once (net/http runs each
// request in its own goroutine).
func rateLimit(l *rate.Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow() {
			writeJSON(w, http.StatusTooManyRequests, errorResponse{Error: "too many requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder wraps an http.ResponseWriter to remember the status code,
// because ResponseWriter has no way to read it back.
//
// Embedding (the bare http.ResponseWriter field with no name) gives
// statusRecorder every method of the wrapped writer for free, so it still
// satisfies the http.ResponseWriter interface. We then override just
// WriteHeader.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real writer underneath
// (for flushing or per-request deadlines), which embedding alone hides.
func (rec *statusRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}

// logRequests logs one line per request: method, path, status, duration and
// the client's address. It never logs headers, so the token stays out of
// the log.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// A handler that writes a body without calling WriteHeader gets an
		// implicit 200, so that's the starting value.
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)
		actor := actorFrom(r.Context())

		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
			"remote", r.RemoteAddr,
			// Who asked: the audit trail for starts and stops.
			"actor", actor.ID,
			"admin", actor.Admin,
		)
	})
}
