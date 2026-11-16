package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// When an admin stops a server that has players on it, the API doesn't
// stop it straight away. It warns the players in-game and stops it five
// minutes later, unless an admin cancels first.

// shutdownDelay is how long players get between an admin's stop and the
// server actually stopping.
const shutdownDelay = 5 * time.Minute

// warnAt lists when players are warned, as time left before the stop. The
// first is at the very start of the countdown.
var warnAt = []time.Duration{5 * time.Minute, time.Minute, 30 * time.Second, 10 * time.Second}

// cancelledMessage is announced in-game when an admin cancels.
const cancelledMessage = "Shutdown cancelled."

// warning is the in-game message with left to go, e.g.
// "Server stopping in 1 minute (requested by an admin)."
func warning(left time.Duration) string {
	var when string
	switch {
	case left >= time.Minute:
		when = plural(int(left/time.Minute), "minute")
	default:
		when = plural(int(left/time.Second), "second")
	}
	return fmt.Sprintf("Server stopping in %s (requested by an admin).", when)
}

// plural gives "1 minute" or "5 minutes".
func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// countdown is one game's pending stop.
type countdown struct {
	// stoppingAt is when the server will be stopped, in UTC.
	stoppingAt time.Time
	// cancel stops the countdown's goroutine early. Calling a
	// context.CancelFunc more than once is safe.
	cancel context.CancelFunc
}

// stoppingAt returns when the named game's countdown ends, or the zero
// time.Time if there is no countdown. listGames, getGame and stopGame use
// it; the zero value means "omit stopping_at" thanks to omitzero.
//
// Every read or write of s.countdowns happens with s.mu held: handlers run
// on many goroutines at once, plus the countdown goroutines themselves, and
// Go maps are not safe for concurrent use. (The race detector, -race,
// reports it if you forget.)
func (s *server) stoppingAt(name string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.countdowns[name]; ok {
		return c.stoppingAt
	}
	return time.Time{}
}

// startCountdown starts g's countdown and returns when it will stop the
// server. If g already has one, it returns that one's time and starts
// nothing: there is never more than one countdown per game.
func (s *server) startCountdown(g games.Game, a Actor) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	if count, ok := s.countdowns[g.Name()]; ok {
		return count.stoppingAt
	}

	stoppingAt := time.Now().UTC().Add(shutdownDelay)
	ctx, cancel := context.WithCancel(context.Background())

	s.countdowns[g.Name()] = &countdown{stoppingAt: stoppingAt, cancel: cancel}
	s.logger.Info("countdown started", "game", g.Name(), "actor", a.ID, "stopping_at", stoppingAt)

	go s.runCountdown(ctx, g, s.countdowns[g.Name()])

	return stoppingAt
}

// runCountdown is the countdown's goroutine: it warns the players at each
// warnAt point, then stops the server at stoppingAt. It ends early when
// ctx is cancelled (an admin cancelled), or when it finds the server
// already stopped (someone stopped it another way). However it ends, it
// removes g's entry from s.countdowns.
func (s *server) runCountdown(ctx context.Context, g games.Game, c *countdown) {
	// remove countdown when the function returns
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.countdowns[g.Name()] == c { // still ours?
			delete(s.countdowns, g.Name())
		}
	}()

	for _, left := range warnAt {
		// see if it gets cancelled or time runs out first
		select {
		case <-time.After(time.Until(c.stoppingAt.Add(-left))):

		case <-ctx.Done():
			return // cancelled
		}

		// check if game status has stopped
		status, err := g.Status(ctx)
		if err != nil {
			s.logger.Warn("countdown status check failed", "game", g.Name(), "error", err)
		}
		if status.State == games.Stopped {
			s.logger.Info("countdown ended: server already stopped", "game", g.Name())
			return
		}

		// else, announce to game that there is limited time remaining
		if a, ok := g.(games.Announcer); ok {
			if err := a.Announce(ctx, warning(left)); err != nil {
				s.logger.Warn("countdown announce failed", "game", g.Name(), "error", err)
			}
		}
	}

	select {
	case <-time.After(time.Until(c.stoppingAt)):

	case <-ctx.Done():
		return // cancelled
	}

	// stop server
	stopCtx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()

	err := g.Stop(stopCtx)
	if err != nil {
		s.logger.Error("countdown stop failed", "game", g.Name(), "error", err)
	} else {
		s.logger.Info("countdown stop", "game", g.Name())
	}
}

// cancelStop answers POST /games/{name}/stop/cancel (admins only).
func (s *server) cancelStop(w http.ResponseWriter, r *http.Request) {
	game, err := s.lookup(r.PathValue("name"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	// only admins can cancel the stop
	actor := actorFrom(r.Context())
	if !actor.Admin {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "admins only"})
		return
	}

	// cancel the stop
	s.mu.Lock()
	count, hadCountdown := s.countdowns[game.Name()] // cancel the stop
	if hadCountdown {
		count.cancel()
		delete(s.countdowns, game.Name())
	}
	s.mu.Unlock()

	// log the cancel
	if hadCountdown {
		s.logger.Info("countdown cancelled", "game", game.Name(), "actor", actor.ID)
		if a, ok := game.(games.Announcer); ok {
			if err := a.Announce(r.Context(), cancelledMessage); err != nil {
				s.logger.Warn("countdown announce failed", "game", game.Name(), "error", err)
			}
		}
	}

	// reply 200
	status, err := game.Status(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, gameResponse{
		Name:  game.Name(),
		State: status.State,
	})
}
