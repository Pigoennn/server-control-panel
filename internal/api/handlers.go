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
	// StoppingAt is when an admin's countdown will stop the server. The
	// zero time.Time (no countdown) is left out, thanks to omitzero:
	// time.Time has an IsZero method, and omitzero uses it.
	StoppingAt time.Time `json:"stopping_at,omitzero"`
}

// playersOnlineResponse is the 409 body when a non-admin tries to stop a
// server with players on it. The bot uses Players to say who.
//
//	Players: nil      -> no "players" key: RCON didn't answer, so the API
//	                     doesn't know who's on and assumes someone is
//	Players: [...]    -> who's online
type playersOnlineResponse struct {
	Error   string   `json:"error"` // always "players are online"
	Players []string `json:"players,omitzero"`
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
			Name:       game.Name(),
			State:      status.State,
			Players:    nil,
			StoppingAt: s.stoppingAt(game.Name()),
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
		names = playerNames(players)
	}

	response := gameResponse{
		Name:       game.Name(),
		State:      status.State,
		Players:    names,
		StoppingAt: s.stoppingAt(game.Name()),
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

	s.startMu.Lock()
	defer s.startMu.Unlock()

	// only one game can be on at a time
	for _, sgame := range s.games {
		if game.Name() == sgame.Name() {
			continue
		}

		status, err := sgame.Status(r.Context())
		if err != nil {
			s.writeError(w, r, err)
			return
		}

		if status.State != games.Stopped {
			msg := fmt.Sprintf("%s is already running; stop it first", sgame.Name())
			writeJSON(w, http.StatusConflict, errorResponse{Error: msg})
			return
		}
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

	if count := s.stoppingAt(game.Name()); !count.IsZero() {
		// reply 202
		status, err := game.Status(r.Context())
		if err != nil {
			s.writeError(w, r, err)
			return
		}

		writeJSON(w, http.StatusAccepted, gameResponse{
			Name:       game.Name(),
			State:      status.State,
			StoppingAt: count,
		})
		return
	}

	status, err := game.Status(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if status.State != games.Stopped && s.blockedByPlayers(w, r, game) {
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
	status, err = game.Status(ctx)
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

// blockedByPlayers checks whether anyone may be on game before a stop. It
// returns true if it has already written the response (409 for a friend;
// 202 for an admin, having started the countdown), so the caller must return.
// It returns false, having written nothing, when nobody is online and the
// stop can go ahead.
//
// "May be online" includes a failed Players call: if RCON doesn't answer,
// nobody knows who's on, so the stop is refused rather than risk someone's
// unsaved progress.
func (s *server) blockedByPlayers(w http.ResponseWriter, r *http.Request, game games.Game) bool {
	players, err := game.Players(r.Context())
	if err != nil {
		s.logger.Warn("list players", "game", game.Name(), "err", err)
	}

	// no need to do anything, cool to go through
	if err == nil && len(players) == 0 {
		return false
	}

	// if Admin, then we'll allow the stop after a bit
	if actorFrom(r.Context()).Admin {
		stoppingAt := s.startCountdown(game, actorFrom(r.Context()))

		writeJSON(w, http.StatusAccepted, gameResponse{
			Name:       game.Name(),
			State:      games.Running,
			StoppingAt: stoppingAt,
		})
		return true
	}

	var names []string
	if err == nil {
		names = playerNames(players)
	}
	response := playersOnlineResponse{Error: "players are online", Players: names}
	writeJSON(w, http.StatusConflict, response)
	return true
}

// playerNames turns players into just their names, for JSON responses.
func playerNames(players []games.Player) []string {
	names := make([]string, 0, len(players))
	for _, player := range players {
		names = append(names, player.Name)
	}

	return names
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
