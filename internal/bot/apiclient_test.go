package bot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "s3cret-test-token"

var (
	friend = Actor{ID: "111111111111111111"}
	admin  = Actor{ID: "222222222222222222", Admin: true}
)

// fakeAPI starts a test HTTP server that answers every request with status
// and body. The function it returns gives the last request the server saw.
//
// The handler runs on the server's goroutine and the test reads the request
// on its own, so the request sits behind a mutex. The race detector doesn't
// count "the response arrived over TCP" as synchronisation.
func fakeAPI(t *testing.T, status int, body string) (APIClient, func() *http.Request) {
	t.Helper()
	var (
		mu   sync.Mutex
		last *http.Request
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		last = r.Clone(context.Background())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewHTTPClient(srv.URL, testToken), func() *http.Request {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

func TestClientSendsHeaders(t *testing.T) {
	c, last := fakeAPI(t, http.StatusOK, `{"name":"minecraft","state":"stopped"}`)

	if _, err := c.Game(context.Background(), admin, "minecraft"); err != nil {
		t.Fatal(err)
	}
	if got := last().Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("Authorization = %q", got)
	}
	if got := last().Header.Get("X-Actor-ID"); got != admin.ID {
		t.Errorf("X-Actor-ID = %q, want %q", got, admin.ID)
	}
	if got := last().Header.Get("X-Actor-Admin"); got != "true" {
		t.Errorf("X-Actor-Admin = %q, want true", got)
	}
	if last().Method != http.MethodGet || last().URL.Path != "/games/minecraft" {
		t.Errorf("request = %s %s, want GET /games/minecraft", last().Method, last().URL.Path)
	}

	if _, err := c.Game(context.Background(), friend, "minecraft"); err != nil {
		t.Fatal(err)
	}
	if _, ok := last().Header["X-Actor-Admin"]; ok {
		t.Errorf("non-admin request sent X-Actor-Admin: %q", last().Header.Get("X-Actor-Admin"))
	}
	if got := last().Header.Get("X-Actor-ID"); got != friend.ID {
		t.Errorf("X-Actor-ID = %q, want %q", got, friend.ID)
	}
}

func TestClientRoutes(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		call       func(c APIClient) error
		wantMethod string
		wantPath   string
	}{
		{"games", func(c APIClient) error { _, err := c.Games(ctx, friend); return err }, "GET", "/games"},
		{"game", func(c APIClient) error { _, err := c.Game(ctx, friend, "minecraft"); return err }, "GET", "/games/minecraft"},
		{"start", func(c APIClient) error { _, err := c.Start(ctx, friend, "minecraft"); return err }, "POST", "/games/minecraft/start"},
		{"stop", func(c APIClient) error { _, _, err := c.Stop(ctx, friend, "minecraft"); return err }, "POST", "/games/minecraft/stop"},
		{"cancel", func(c APIClient) error { _, err := c.CancelStop(ctx, admin, "minecraft"); return err }, "POST", "/games/minecraft/stop/cancel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"name":"minecraft","state":"running"}`
			if tt.name == "games" {
				body = `[` + body + `]`
			}
			c, last := fakeAPI(t, http.StatusOK, body)
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
			if last().Method != tt.wantMethod || last().URL.Path != tt.wantPath {
				t.Errorf("request = %s %s, want %s %s", last().Method, last().URL.Path, tt.wantMethod, tt.wantPath)
			}
		})
	}
}

func TestClientEscapesName(t *testing.T) {
	c, last := fakeAPI(t, http.StatusNotFound, `{"error":"unknown game"}`)
	c.Game(context.Background(), friend, "../healthz")
	if last().URL.Path != "/games/../healthz" || last().URL.RawPath != "/games/..%2Fhealthz" {
		t.Errorf("path = %q (raw %q), want the slash escaped", last().URL.Path, last().URL.RawPath)
	}
}

func TestClientDecodesGame(t *testing.T) {
	c, _ := fakeAPI(t, http.StatusOK, `[
		{"name":"minecraft","state":"running","players":[],"stopping_at":"2000-01-01T00:05:00Z"},
		{"name":"vanilla","state":"stopped"}
	]`)

	gs, err := c.Games(context.Background(), friend)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 2 {
		t.Fatalf("got %d games, want 2", len(gs))
	}

	mc := gs[0]
	if mc.Name != "minecraft" || mc.State != "running" {
		t.Errorf("game 0 = %+v", mc)
	}
	if mc.Players == nil || len(mc.Players) != 0 {
		t.Errorf(`players:[] decoded to %#v, want an empty non-nil slice`, mc.Players)
	}
	want := time.Date(2000, 1, 1, 0, 5, 0, 0, time.UTC)
	if !mc.StoppingAt.Equal(want) {
		t.Errorf("StoppingAt = %v, want %v", mc.StoppingAt, want)
	}

	v := gs[1]
	if v.Players != nil {
		t.Errorf("missing players decoded to %#v, want nil (unknown)", v.Players)
	}
	if !v.StoppingAt.IsZero() {
		t.Errorf("missing stopping_at decoded to %v, want zero", v.StoppingAt)
	}
}

func TestClientStopCountdown(t *testing.T) {
	c, _ := fakeAPI(t, http.StatusAccepted, `{"name":"minecraft","state":"running","stopping_at":"2000-01-01T00:05:00Z"}`)
	g, countdown, err := c.Stop(context.Background(), admin, "minecraft")
	if err != nil {
		t.Fatal(err)
	}
	if !countdown {
		t.Error("202 gave countdown = false")
	}
	if g.StoppingAt.IsZero() {
		t.Error("StoppingAt not decoded")
	}

	c, _ = fakeAPI(t, http.StatusOK, `{"name":"minecraft","state":"stopped"}`)
	_, countdown, err = c.Stop(context.Background(), friend, "minecraft")
	if err != nil {
		t.Fatal(err)
	}
	if countdown {
		t.Error("200 gave countdown = true")
	}
}

func TestClientPlayersOnlineError(t *testing.T) {
	c, _ := fakeAPI(t, http.StatusConflict, `{"error":"players are online","players":["Steve","Alex"]}`)
	_, _, err := c.Stop(context.Background(), friend, "minecraft")

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v (%T), want *APIError", err, err)
	}
	if apiErr.Status != http.StatusConflict || apiErr.Message != "players are online" {
		t.Errorf("APIError = %+v", apiErr)
	}
	if strings.Join(apiErr.Players, ",") != "Steve,Alex" {
		t.Errorf("Players = %v, want [Steve Alex]", apiErr.Players)
	}
}

func TestClientErrorWithoutJSON(t *testing.T) {
	c, _ := fakeAPI(t, http.StatusBadGateway, `<html>bad gateway</html>`)
	_, err := c.Games(context.Background(), friend)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v (%T), want *APIError", err, err)
	}
	if apiErr.Status != http.StatusBadGateway || apiErr.Message != "Bad Gateway" {
		t.Errorf("APIError = %+v, want 502 with the status text", apiErr)
	}
}

func TestClientAPIDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens on url any more

	c := NewHTTPClient(url, testToken)
	_, err := c.Games(context.Background(), friend)
	if err == nil {
		t.Fatal("no error from a closed server")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("err = %v is an *APIError; want a transport error, so the bot says the API is down", err)
	}
}

func TestClientNeverLogsToken(t *testing.T) {
	c, _ := fakeAPI(t, http.StatusConflict, `{"error":"players are online"}`)
	_, _, err := c.Stop(context.Background(), friend, "minecraft")
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Errorf("409 error %v contains the token", err)
	}

	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	_, err = NewHTTPClient(srv.URL, testToken).Games(context.Background(), friend)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Errorf("transport error %v contains the token", err)
	}
}
