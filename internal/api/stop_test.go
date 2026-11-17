package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

var (
	friend = Actor{ID: "111111111111111111"}
	admin  = Actor{ID: "222222222222222222", Admin: true}
)

func TestStop(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", State: games.Running}
	wantJSON(t, do(t, newTestAPI(g), "POST", "/games/minecraft/stop"), 200,
		`{"name":"minecraft","state":"stopped"}`)
	if g.Stops != 1 {
		t.Errorf("Stops = %d, want 1", g.Stops)
	}
}

// If the caller hangs up mid-stop, its context is cancelled. The stop must
// carry on anyway, so the world isn't cut off mid-save.
func TestStopSurvivesClientCancel(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", State: games.Running}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the caller has already gone

	req := httptest.NewRequestWithContext(ctx, "POST", "/games/minecraft/stop", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	newTestAPI(g).ServeHTTP(httptest.NewRecorder(), req)

	if g.Stops != 1 || g.StopCtxErr != nil {
		t.Errorf("Stops = %d, ctx error seen by Stop = %v; want 1, nil", g.Stops, g.StopCtxErr)
	}
}

// Stopping protects people who are playing: anyone may stop an empty
// server, but only an admin may stop one with players on it.

func TestStopNobodyOnlineNonAdmin(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", State: games.Running, PlayerList: []games.Player{}}
	wantJSON(t, doAs(t, newTestAPI(g), "POST", "/games/minecraft/stop", friend), 200,
		`{"name":"minecraft","state":"stopped"}`)
	if g.Stops != 1 {
		t.Errorf("Stops = %d, want 1", g.Stops)
	}
}

func TestStopPlayersOnlineNonAdmin(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", State: games.Running,
		PlayerList: []games.Player{{Name: "Steve"}, {Name: "Alex"}}}
	wantJSON(t, doAs(t, newTestAPI(g), "POST", "/games/minecraft/stop", friend), 409,
		`{"error":"players are online","players":["Steve","Alex"]}`)
	if g.Stops != 0 {
		t.Errorf("Stops = %d, want 0", g.Stops)
	}
}

// When RCON doesn't answer, nobody knows who's on. Assume someone is: the
// cost of guessing wrong is a friend's unsaved progress.
func TestStopPlayersUnknownNonAdmin(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", State: games.Running,
		PlayersErr: errors.New("rcon timeout")}
	wantJSON(t, doAs(t, newTestAPI(g), "POST", "/games/minecraft/stop", friend), 409,
		`{"error":"players are online"}`)
	if g.Stops != 0 {
		t.Errorf("Stops = %d, want 0", g.Stops)
	}
}

// A stopped server has nobody on it, so the API mustn't ask. PlayersErr is
// set so that asking would turn the answer into a 409.
func TestStopAlreadyStopped(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", State: games.Stopped,
		PlayersErr: errors.New("should not be called")}
	wantJSON(t, doAs(t, newTestAPI(g), "POST", "/games/minecraft/stop", friend), 200,
		`{"name":"minecraft","state":"stopped"}`)
}

func TestStopNobodyOnlineAdmin(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", State: games.Running, PlayerList: []games.Player{}}
	wantJSON(t, doAs(t, newTestAPI(g), "POST", "/games/minecraft/stop", admin), 200,
		`{"name":"minecraft","state":"stopped"}`)
	if g.Stops != 1 {
		t.Errorf("Stops = %d, want 1", g.Stops)
	}
}
