package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/time/rate"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// send makes one request with the given Authorization header ("" sends
// none) and returns the recorded response.
func send(h http.Handler, method, path, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func oneGame() *games.FakeGame {
	return &games.FakeGame{GameName: "minecraft", State: games.Stopped}
}

// wantUnauthorized checks the 401 is the same whatever was wrong, so a
// caller can't tell a missing token from a wrong one.
func wantUnauthorized(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	wantJSON(t, rec, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want %q", got, "Bearer")
	}
}

func TestMissingToken(t *testing.T) {
	g := oneGame()
	wantUnauthorized(t, send(newTestAPI(g), "GET", "/games", ""))
}

func TestWrongToken(t *testing.T) {
	g := oneGame()
	wantUnauthorized(t, send(newTestAPI(g), "GET", "/games", "Bearer not-the-token"))
}

func TestBasicScheme(t *testing.T) {
	g := oneGame()
	wantUnauthorized(t, send(newTestAPI(g), "GET", "/games", "Basic "+testToken))
}

// Without a token, an unknown game must still be 401, not 404: otherwise
// anyone could probe which game names exist.
func TestUnknownGameWithoutToken(t *testing.T) {
	g := oneGame()
	wantUnauthorized(t, send(newTestAPI(g), "GET", "/games/nope", ""))
}

// The token check must guard start, not just reads, and must stop the
// request before it reaches the game.
func TestStartWithoutTokenDoesNothing(t *testing.T) {
	g := oneGame()
	wantUnauthorized(t, send(newTestAPI(g), "POST", "/games/minecraft/start", ""))
	if g.Starts != 0 {
		t.Errorf("Starts = %d, want 0", g.Starts)
	}
}

func TestHealthzNeedsNoToken(t *testing.T) {
	wantJSON(t, send(newTestAPI(), "GET", "/healthz", ""), 200, `{"status":"ok"}`)
}

// newLimitedAPI uses a limiter that never refills (rate 0) and holds 2
// tokens, so exactly 2 requests get through.
func newLimitedAPI(gs ...games.Game) http.Handler {
	return New(Config{
		Games:   gs,
		Token:   testToken,
		Logger:  slog.New(slog.DiscardHandler),
		Limiter: rate.NewLimiter(0, 2),
	})
}

func TestRateLimit(t *testing.T) {
	h := newLimitedAPI(oneGame())
	for i := 1; i <= 2; i++ {
		if rec := do(t, h, "GET", "/games"); rec.Code != 200 {
			t.Fatalf("request %d: status = %d, want 200", i, rec.Code)
		}
	}
	wantJSON(t, do(t, h, "GET", "/games"), http.StatusTooManyRequests,
		`{"error":"too many requests"}`)
}

func TestHealthzNotRateLimited(t *testing.T) {
	h := newLimitedAPI()
	for i := 1; i <= 5; i++ {
		if rec := send(h, "GET", "/healthz", ""); rec.Code != 200 {
			t.Fatalf("request %d: status = %d, want 200", i, rec.Code)
		}
	}
}

func TestLogDoesNotContainToken(t *testing.T) {
	var buf bytes.Buffer
	h := New(Config{
		Games:   []games.Game{oneGame()},
		Token:   testToken,
		Logger:  slog.New(slog.NewJSONHandler(&buf, nil)),
		Limiter: rate.NewLimiter(rate.Inf, 0),
	})
	do(t, h, "GET", "/games")

	log := buf.String()
	if !strings.Contains(log, `"status":200`) {
		t.Errorf("log has no status 200 line:\n%s", log)
	}
	if !strings.Contains(log, `"path":"/games"`) {
		t.Errorf("log has no path:\n%s", log)
	}
	if strings.Contains(log, testToken) {
		t.Errorf("log contains the token:\n%s", log)
	}
}

// A rejected request is still logged, with its real status.
func TestLogRecordsRejectedStatus(t *testing.T) {
	var buf bytes.Buffer
	h := New(Config{
		Games:   []games.Game{oneGame()},
		Token:   testToken,
		Logger:  slog.New(slog.NewJSONHandler(&buf, nil)),
		Limiter: rate.NewLimiter(rate.Inf, 0),
	})
	send(h, "GET", "/games", "")

	if !strings.Contains(buf.String(), `"status":401`) {
		t.Errorf("log has no status 401 line:\n%s", buf.String())
	}
}
