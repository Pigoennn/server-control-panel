package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// stopTimeout bounds a stop request. The modded server can take 2 minutes
// to save, plus some slack.
const stopTimeout = 3 * time.Minute

// gameResponse is the JSON for one game.
//
// Struct tags (the `json:"..."` strings) set the JSON field names.
// `omitzero` (Go 1.24+) leaves a field out when it holds its zero value.
// For a slice the zero value is nil, so:
//
//	Players: nil         -> no "players" key (we couldn't ask the server)
//	Players: []string{}  -> "players": []    (nobody online)
//
// The older `omitempty` would drop both, losing that difference.
type gameResponse struct {
	Name    string      `json:"name"`
	State   games.State `json:"state"`
	Players []string    `json:"players,omitzero"`
}

// lookup finds a game by name.
func (s *server) lookup(name string) (games.Game, error) {

	for _, game := range s.games {
		if game.Name() == name {
			return game, nil
		}
	}

	return nil, games.ErrNotFound
}

// healthz answers GET /healthz with 200 {"status":"ok"}.
func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{"status": "ok"}

	writeJSON(w, http.StatusOK, response)
}

// listGames answers GET /games with every game's name and state:
// [{"name":"minecraft","state":"stopped"}, ...]
func (s *server) listGames(w http.ResponseWriter, r *http.Request) {

	// build one gameResponse per game in s.games
	responses := make([]gameResponse, 0, len(s.games)) // so zero games sends []
	for _, game := range s.games {
		status, err := game.Status(r.Context())
		if err != nil {
			s.writeError(w, r, err)
			return
		}

		responses = append(responses, gameResponse{
			Name:    game.Name(),
			State:   status.State,
			Players: nil,
		})
	}

	writeJSON(w, http.StatusOK, responses)
}

// getGame answers GET /games/{name} with the state and, if possible, who's
// online.
func (s *server) getGame(w http.ResponseWriter, r *http.Request) {

	// return if error
	game, err := s.lookup(r.PathValue("name"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	// return if error
	status, err := game.Status(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	// warn so it doesn't fail the request
	var names []string
	players, err := game.Players(r.Context())
	if err != nil {
		s.logger.Warn("list players", "game", game.Name(), "err", err)
	} else {
		names = make([]string, 0, len(players))
		for _, name := range players {
			names = append(names, name.Name)
		}
	}

	response := gameResponse{
		Name:    game.Name(),
		State:   status.State,
		Players: names,
	}

	writeJSON(w, http.StatusOK, response)
}

// startGame answers POST /games/{name}/start. It returns as soon as Docker
// has started the container; the state is then usually "starting".
func (s *server) startGame(w http.ResponseWriter, r *http.Request) {

	game, err := s.lookup(r.PathValue("name"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	err = game.Start(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	s.logger.Info("start", "game", game.Name())

	// return if error
	status, err := game.Status(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	response := gameResponse{
		Name:    game.Name(),
		State:   status.State,
		Players: nil,
	}

	writeJSON(w, http.StatusOK, response)
}

// stopGame answers POST /games/{name}/stop. It waits until the server has
// stopped, which can take a couple of minutes.
func (s *server) stopGame(w http.ResponseWriter, r *http.Request) {

	game, err := s.lookup(r.PathValue("name"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	// ignore caller's cancel but still has a TimeOut limit in case of HTTP client timing out
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), stopTimeout)
	defer cancel()

	err = game.Stop(ctx)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	s.logger.Info("stop", "game", game.Name())

	// return if error
	status, err := game.Status(ctx)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	response := gameResponse{
		Name:    game.Name(),
		State:   status.State,
		Players: nil,
	}

	writeJSON(w, http.StatusOK, response)
}

// writeError turns err into the right status code and JSON body.
//
//	errors.Is(err, games.ErrNotFound)   -> 404 "game not found"
//	errors.Is(err, games.ErrNotCreated) -> 409 "container not created: run
//	    docker compose -f deploy/<name>/compose.yaml up --no-start once"
//	    (<name> from r.PathValue("name"))
//	anything else                       -> log it with s.logger.Error, then
//	                                       500 "internal error"
//
// The 500 body is deliberately vague: internal details (proxy addresses,
// Docker messages) belong in the log, not in a response anyone could read.
func (s *server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	// TODO(jonathan): a `switch { case errors.Is(...): }` fits well here.
	// Respond with writeJSON(w, code, errorResponse{Error: msg}).

	switch {
	case errors.Is(err, games.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "game not found"})
	case errors.Is(err, games.ErrNotCreated):
		message := fmt.Sprintf("container not created: run docker compose -f deploy/%s/compose.yaml up --no-start once", r.PathValue("name"))
		writeJSON(w, http.StatusConflict, errorResponse{Error: message})
	default:
		s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal error"})
	}
}
