// Package api is the HTTP control API: it turns requests like
// POST /games/minecraft/start into calls on a games.Game.
package api

import (
	"log/slog"
	"net/http"

	"golang.org/x/time/rate"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// Config is what New needs to build the API.
type Config struct {
	// Games are the servers the API controls, in the order GET /games lists them.
	Games []games.Game
	// Token is the shared secret every /games request must send as
	// "Authorization: Bearer <token>".
	Token string
	// Logger receives one line per request, plus errors.
	Logger *slog.Logger
	// Limiter caps how fast /games requests are accepted.
	Limiter *rate.Limiter
}

// server holds what the handlers share. Handlers are methods on it, so they
// reach the games and the logger through the receiver (s) instead of
// global variables.
type server struct {
	games  []games.Game
	logger *slog.Logger
}

// New returns the API as one http.Handler, ready for http.Server.
//
// Since Go 1.22 the standard ServeMux understands methods and wildcards in
// patterns: "POST /games/{name}/start" only matches POST, and the handler
// reads the name with r.PathValue("name"). A request with the wrong method
// gets 405 Method Not Allowed automatically.
//
// Middleware wrap handlers from the outside in. A request passes through
// logRequests, then rateLimit, then requireToken, and only then reaches a
// /games handler. /healthz is logged but needs no token and isn't limited,
// so monitoring can poll it freely.
func New(cfg Config) http.Handler {
	s := &server{games: cfg.Games, logger: cfg.Logger}

	gameRoutes := http.NewServeMux()
	gameRoutes.HandleFunc("GET /games", s.listGames)
	gameRoutes.HandleFunc("GET /games/{name}", s.getGame)
	gameRoutes.HandleFunc("POST /games/{name}/start", s.startGame)
	gameRoutes.HandleFunc("POST /games/{name}/stop", s.stopGame)
	protected := rateLimit(cfg.Limiter, requireToken(cfg.Token, gameRoutes))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.Handle("/games", protected)  // exactly /games
	mux.Handle("/games/", protected) // everything under /games/

	return logRequests(cfg.Logger, mux)
}
