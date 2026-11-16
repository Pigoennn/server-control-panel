package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/time/rate"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

const testToken = "test-token-0123456789abcdef0123456789"

// newTestAPI builds the API around the given games with no rate limit
// (rate.Inf) and a logger that throws everything away.
func newTestAPI(gs ...games.Game) http.Handler {
	return New(Config{
		Games:   gs,
		Token:   testToken,
		Logger:  slog.New(slog.DiscardHandler),
		Limiter: rate.NewLimiter(rate.Inf, 0),
	})
}

// do sends one request with a valid token and returns the recorded
// response. httptest.ResponseRecorder is an http.ResponseWriter that keeps
// what's written to it, so no real network is involved.
func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// doAs is do, sent on behalf of a Discord user: it adds the headers the
// bot sends. X-Actor-Admin is only set for admins, like the bot does.
func doAs(t *testing.T, h http.Handler, method, path string, a Actor) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Actor-ID", a.ID)
	if a.Admin {
		req.Header.Set("X-Actor-Admin", "true")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// wantJSON fails unless the response has the given status code and its
// body is the same JSON as want (ignoring spacing and key order).
func wantJSON(t *testing.T, rec *httptest.ResponseRecorder, code int, want string) {
	t.Helper()
	if rec.Code != code {
		t.Errorf("status = %d, want %d (body %s)", rec.Code, code, rec.Body)
	}
	var got, exp any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %q", rec.Body)
	}
	if err := json.Unmarshal([]byte(want), &exp); err != nil {
		t.Fatalf("bad want JSON: %v", err)
	}
	gotB, _ := json.Marshal(got)
	expB, _ := json.Marshal(exp)
	if string(gotB) != string(expB) {
		t.Errorf("body = %s, want %s", gotB, expB)
	}
}

func TestHealthz(t *testing.T) {
	wantJSON(t, do(t, newTestAPI(), "GET", "/healthz"), 200, `{"status":"ok"}`)
}

func TestListGames(t *testing.T) {
	h := newTestAPI(
		&games.FakeGame{GameName: "minecraft", State: games.Running},
		&games.FakeGame{GameName: "minecraft-vanilla", State: games.Stopped},
	)
	wantJSON(t, do(t, h, "GET", "/games"), 200,
		`[{"name":"minecraft","state":"running"},{"name":"minecraft-vanilla","state":"stopped"}]`)
}

func TestGetGameRunning(t *testing.T) {
	h := newTestAPI(&games.FakeGame{GameName: "minecraft", State: games.Running,
		PlayerList: []games.Player{{Name: "Pigoenn"}}})
	wantJSON(t, do(t, h, "GET", "/games/minecraft"), 200,
		`{"name":"minecraft","state":"running","players":["Pigoenn"]}`)
}

func TestGetGameStoppedHasEmptyPlayers(t *testing.T) {
	h := newTestAPI(&games.FakeGame{GameName: "minecraft", State: games.Stopped})
	wantJSON(t, do(t, h, "GET", "/games/minecraft"), 200,
		`{"name":"minecraft","state":"stopped","players":[]}`)
}

func TestGetGamePlayersError(t *testing.T) {
	h := newTestAPI(&games.FakeGame{GameName: "minecraft", State: games.Running,
		PlayersErr: errors.New("rcon timeout")})
	wantJSON(t, do(t, h, "GET", "/games/minecraft"), 200,
		`{"name":"minecraft","state":"running"}`)
}

func TestGetUnknownGame(t *testing.T) {
	wantJSON(t, do(t, newTestAPI(), "GET", "/games/nope"), 404, `{"error":"game not found"}`)
}

func TestStart(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", State: games.Stopped}
	wantJSON(t, do(t, newTestAPI(g), "POST", "/games/minecraft/start"), 200,
		`{"name":"minecraft","state":"starting"}`)
	if g.Starts != 1 {
		t.Errorf("Starts = %d, want 1", g.Starts)
	}
}

func TestStartNotCreated409(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", Err: games.ErrNotCreated}
	rec := do(t, newTestAPI(g), "POST", "/games/minecraft/start")
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "up --no-start") {
		t.Errorf("got %d %s; want 409 with the up --no-start hint", rec.Code, rec.Body)
	}
}

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

func TestInternalErrorIsGeneric(t *testing.T) {
	g := &games.FakeGame{GameName: "minecraft", Err: errors.New("proxy secret detail")}
	rec := do(t, newTestAPI(g), "GET", "/games/minecraft")
	wantJSON(t, rec, 500, `{"error":"internal error"}`)
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("500 body leaks the internal error: %s", rec.Body)
	}
}

func TestStartWrongMethod(t *testing.T) {
	h := newTestAPI(&games.FakeGame{GameName: "minecraft"})
	if rec := do(t, h, "GET", "/games/minecraft/start"); rec.Code != 405 {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
