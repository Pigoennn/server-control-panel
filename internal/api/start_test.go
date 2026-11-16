package api

import (
	"errors"
	"sync"
	"testing"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// Both Minecraft servers use the same ports, so only one game may run at
// a time. These two build the usual pair.
func pair(modded, vanilla games.State) (*games.FakeGame, *games.FakeGame) {
	return &games.FakeGame{GameName: "minecraft", State: modded},
		&games.FakeGame{GameName: "minecraft-vanilla", State: vanilla}
}

func TestStartRefusedWhileOtherRunning(t *testing.T) {
	mc, van := pair(games.Running, games.Stopped)
	wantJSON(t, do(t, newTestAPI(mc, van), "POST", "/games/minecraft-vanilla/start"), 409,
		`{"error":"minecraft is already running; stop it first"}`)
	if van.Starts != 0 {
		t.Errorf("vanilla Starts = %d, want 0", van.Starts)
	}
}

func TestStartRefusedWhileOtherStarting(t *testing.T) {
	mc, van := pair(games.Starting, games.Stopped)
	wantJSON(t, do(t, newTestAPI(mc, van), "POST", "/games/minecraft-vanilla/start"), 409,
		`{"error":"minecraft is already running; stop it first"}`)
	if van.Starts != 0 {
		t.Errorf("vanilla Starts = %d, want 0", van.Starts)
	}
}

func TestStartAllowedWhenOthersStopped(t *testing.T) {
	mc, van := pair(games.Stopped, games.Stopped)
	wantJSON(t, do(t, newTestAPI(mc, van), "POST", "/games/minecraft-vanilla/start"), 200,
		`{"name":"minecraft-vanilla","state":"starting"}`)
	if van.Starts != 1 {
		t.Errorf("vanilla Starts = %d, want 1", van.Starts)
	}
}

// The check is about the *other* games: starting one that's already
// running is still the harmless no-op it was.
func TestStartSelfAlreadyRunning(t *testing.T) {
	mc, van := pair(games.Running, games.Stopped)
	wantJSON(t, do(t, newTestAPI(mc, van), "POST", "/games/minecraft/start"), 200,
		`{"name":"minecraft","state":"running"}`)
}

func TestStartOtherStatusError(t *testing.T) {
	mc, van := pair(games.Running, games.Stopped)
	mc.Err = errors.New("proxy down")
	wantJSON(t, do(t, newTestAPI(mc, van), "POST", "/games/minecraft-vanilla/start"), 500,
		`{"error":"internal error"}`)
	if van.Starts != 0 {
		t.Errorf("vanilla Starts = %d, want 0", van.Starts)
	}
}

// Two friends press /start for different games at the same moment. Without
// a lock, both handlers could check "is anything else running?", both see
// "no", and both start. Run it many times so a missing lock shows up.
func TestConcurrentStartsOnlyOneWins(t *testing.T) {
	for round := range 50 {
		mc, van := pair(games.Stopped, games.Stopped)
		h := newTestAPI(mc, van)

		var wg sync.WaitGroup
		for _, name := range []string{"minecraft", "minecraft-vanilla"} {
			wg.Go(func() { do(t, h, "POST", "/games/"+name+"/start") })
		}
		wg.Wait()

		if got := mc.Starts + van.Starts; got != 1 {
			t.Fatalf("round %d: %d games started, want exactly 1", round, got)
		}
	}
}
