package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// These tests use testing/synctest (Go 1.25+). Inside synctest.Test the
// test runs in a "bubble" with a fake clock: it starts at
// 2000-01-01 00:00:00 UTC, and time only moves when every goroutine in the
// bubble is blocked. So time.Sleep(5*time.Minute) returns instantly, after
// the countdown goroutine has done everything it would do in those five
// minutes. synctest.Wait() waits until every other goroutine in the bubble
// is blocked, i.e. has finished reacting.
//
// The bubble also checks for leaks: synctest.Test fails if a goroutine
// started inside it is still blocked when the test function returns. So
// every test lets its countdown finish or cancels it.

// noCountdown fails if GET /games/minecraft still shows a countdown.
func noCountdown(t *testing.T, h http.Handler) {
	t.Helper()
	if body := do(t, h, "GET", "/games/minecraft").Body.String(); strings.Contains(body, "stopping_at") {
		t.Errorf("countdown still listed after it ended: %s", body)
	}
}

// wantSaid are the four warnings, in order.
var wantSaid = []string{
	"Server stopping in 5 minutes (requested by an admin).",
	"Server stopping in 1 minute (requested by an admin).",
	"Server stopping in 30 seconds (requested by an admin).",
	"Server stopping in 10 seconds (requested by an admin).",
}

// busy returns a running game with two players on it.
func busy() *games.FakeGame {
	return &games.FakeGame{GameName: "minecraft", State: games.Running,
		PlayerList: []games.Player{{Name: "Steve"}, {Name: "Alex"}}}
}

func TestWarning(t *testing.T) {
	for i, d := range warnAt {
		if got := warning(d); got != wantSaid[i] {
			t.Errorf("warning(%v) = %q, want %q", d, got, wantSaid[i])
		}
	}
}

func TestAdminStopStartsCountdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := busy()
		h := newTestAPI(g)
		wantJSON(t, doAs(t, h, "POST", "/games/minecraft/stop", admin), 202,
			`{"name":"minecraft","state":"running","stopping_at":"2000-01-01T00:05:00Z"}`)
		synctest.Wait()
		if n := g.StopCount(); n != 0 {
			t.Errorf("Stops = %d straight away, want 0", n)
		}
		time.Sleep(shutdownDelay + time.Second) // let it finish
	})
}

func TestCountdownWarnsThenStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := busy()
		h := newTestAPI(g)
		doAs(t, h, "POST", "/games/minecraft/stop", admin)

		time.Sleep(5*time.Minute + time.Second)
		synctest.Wait()

		if got := g.Said(); !slices.Equal(got, wantSaid) {
			t.Errorf("said %q, want %q", got, wantSaid)
		}
		if n := g.StopCount(); n != 1 {
			t.Errorf("Stops = %d, want 1", n)
		}
		noCountdown(t, h)
	})
}

// Pressing /stop again during a countdown reports it; it never starts a
// second one, whoever asks.
func TestStopDuringCountdownSameTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := busy()
		h := newTestAPI(g)
		doAs(t, h, "POST", "/games/minecraft/stop", admin)

		time.Sleep(time.Minute)
		want := `{"name":"minecraft","state":"running","stopping_at":"2000-01-01T00:05:00Z"}`
		wantJSON(t, doAs(t, h, "POST", "/games/minecraft/stop", admin), 202, want)
		wantJSON(t, doAs(t, h, "POST", "/games/minecraft/stop", friend), 202, want)

		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if got := g.Said(); len(got) != 4 {
			t.Errorf("said %d messages, want 4 (one countdown): %q", len(got), got)
		}
		if n := g.StopCount(); n != 1 {
			t.Errorf("Stops = %d, want 1", n)
		}
	})
}

func TestCancelByAdmin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := busy()
		h := newTestAPI(g)
		doAs(t, h, "POST", "/games/minecraft/stop", admin)

		time.Sleep(2 * time.Minute)
		wantJSON(t, doAs(t, h, "POST", "/games/minecraft/stop/cancel", admin), 200,
			`{"name":"minecraft","state":"running"}`)

		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if n := g.StopCount(); n != 0 {
			t.Errorf("Stops = %d after cancel, want 0", n)
		}
		said := g.Said()
		if len(said) == 0 || said[len(said)-1] != cancelledMessage {
			t.Errorf("said %q, want it to end with %q", said, cancelledMessage)
		}
		if len(said) != 2 {
			t.Errorf("said %q, want the 5-minute warning then the cancel", said)
		}
	})
}

func TestCancelNonAdmin403(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := busy()
		h := newTestAPI(g)
		doAs(t, h, "POST", "/games/minecraft/stop", admin)

		wantJSON(t, doAs(t, h, "POST", "/games/minecraft/stop/cancel", friend), 403,
			`{"error":"admins only"}`)

		time.Sleep(shutdownDelay + time.Second)
		synctest.Wait()
		if n := g.StopCount(); n != 1 {
			t.Errorf("Stops = %d, want 1 (the countdown carries on)", n)
		}
	})
}

func TestCancelNoCountdown(t *testing.T) {
	g := busy()
	wantJSON(t, doAs(t, newTestAPI(g), "POST", "/games/minecraft/stop/cancel", admin), 200,
		`{"name":"minecraft","state":"running"}`)
	if said := g.Said(); len(said) != 0 {
		t.Errorf("said %q, want nothing", said)
	}
}

func TestCancelUnknownGame(t *testing.T) {
	wantJSON(t, doAs(t, newTestAPI(), "POST", "/games/nope/stop/cancel", admin), 404,
		`{"error":"game not found"}`)
}

func TestStoppingAtInStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := busy()
		h := newTestAPI(g)
		doAs(t, h, "POST", "/games/minecraft/stop", admin)

		wantJSON(t, do(t, h, "GET", "/games"), 200,
			`[{"name":"minecraft","state":"running","stopping_at":"2000-01-01T00:05:00Z"}]`)
		wantJSON(t, do(t, h, "GET", "/games/minecraft"), 200,
			`{"name":"minecraft","state":"running","players":["Steve","Alex"],"stopping_at":"2000-01-01T00:05:00Z"}`)

		time.Sleep(shutdownDelay + time.Second)
	})
}

// RCON being down mustn't save the server from an admin's stop.
func TestCountdownAnnounceErrorStillStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := busy()
		g.AnnounceErr = errors.New("rcon down")
		h := newTestAPI(g)
		doAs(t, h, "POST", "/games/minecraft/stop", admin)

		time.Sleep(shutdownDelay + time.Second)
		synctest.Wait()
		if n := g.StopCount(); n != 1 {
			t.Errorf("Stops = %d, want 1", n)
		}
	})
}

// A game that can't announce still gets stopped, just without warnings.
// Embedding games.Game in a struct gives a value whose method set is only
// Game's methods, so the type assertion to games.Announcer fails.
func TestCountdownWithoutAnnouncer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := busy()
		h := newTestAPI(struct{ games.Game }{g})
		wantJSON(t, doAs(t, h, "POST", "/games/minecraft/stop", admin), 202,
			`{"name":"minecraft","state":"running","stopping_at":"2000-01-01T00:05:00Z"}`)

		time.Sleep(shutdownDelay + time.Second)
		synctest.Wait()
		if n := g.StopCount(); n != 1 {
			t.Errorf("Stops = %d, want 1", n)
		}
		if said := g.Said(); len(said) != 0 {
			t.Errorf("said %q through a game that can't announce", said)
		}
	})
}

// Someone stops the server another way (deploy/mc.sh stop) mid-countdown.
// The countdown notices at its next warning and ends, so it can't later
// kill a session someone starts afterwards.
func TestCountdownEndsIfStoppedElsewhere(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := busy()
		h := newTestAPI(g)
		doAs(t, h, "POST", "/games/minecraft/stop", admin)

		time.Sleep(2 * time.Minute)
		g.SetState(games.Stopped) // mc.sh stop
		time.Sleep(2*time.Minute + 30*time.Second)
		synctest.Wait()
		g.SetState(games.Running) // a friend starts it again at 4m30s

		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if n := g.StopCount(); n != 0 {
			t.Errorf("Stops = %d, want 0: the countdown killed a fresh session", n)
		}
		if said := g.Said(); !slices.Equal(said, wantSaid[:1]) {
			t.Errorf("said %q, want only the first warning", said)
		}
		noCountdown(t, h)
	})
}
