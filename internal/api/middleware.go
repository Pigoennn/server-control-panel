package api

import (
	"log/slog"
	"net/http"

	"golang.org/x/time/rate"
)

// A middleware is a function that takes a handler and returns a new
// handler that does something before (or after) calling it. Chaining them
// is how Go web code adds auth, logging and so on without a framework.
//
// These three are pass-throughs for now; Task 6 fills them in.

// requireToken rejects requests without "Authorization: Bearer <token>".
func requireToken(token string, next http.Handler) http.Handler {
	return next
}

// rateLimit rejects requests when the limiter is out of tokens.
func rateLimit(l *rate.Limiter, next http.Handler) http.Handler {
	return next
}

// logRequests logs one line per request.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return next
}
