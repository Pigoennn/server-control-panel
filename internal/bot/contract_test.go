package bot

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"

	"github.com/Pigoennn/server-control-panel/internal/api"
	"github.com/Pigoennn/server-control-panel/internal/games"
)

// TestClientAgainstRealAPI runs the client against the real API handler
// (with a FakeGame instead of Docker). The other client tests use JSON
// written by hand; this one fails if the API and the client ever disagree
// about paths, headers or field names.
func TestClientAgainstRealAPI(t *testing.T) {
	mc := &games.FakeGame{
		GameName:   "minecraft",
		State:      games.Running,
		PlayerList: []games.Player{{Name: "Steve"}},
	}
	srv := httptest.NewServer(api.New(api.Config{
		Games:   []games.Game{mc},
		Token:   testToken,
		Logger:  slog.New(slog.DiscardHandler),
		Limiter: rate.NewLimiter(rate.Inf, 0),
	}))
	t.Cleanup(srv.Close)
	c := NewHTTPClient(srv.URL, testToken)
	ctx := context.Background()

	g, err := c.Game(ctx, friend, "minecraft")
	if err != nil {
		t.Fatal(err)
	}
	if g.State != "running" || len(g.Players) != 1 || g.Players[0] != "Steve" {
		t.Errorf("Game = %+v, want running with Steve", g)
	}

	// A friend can't stop it: the API's 409 arrives as an *APIError.
	_, _, err = c.Stop(ctx, friend, "minecraft")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || len(apiErr.Players) != 1 {
		t.Fatalf("friend Stop err = %v, want 409 with one player", err)
	}

	// A friend can't cancel either: X-Actor-Admin is absent.
	_, err = c.CancelStop(ctx, friend, "minecraft")
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("friend CancelStop err = %v, want 403", err)
	}

	// An admin starts the countdown: X-Actor-Admin reached the API.
	g, countdown, err := c.Stop(ctx, admin, "minecraft")
	if err != nil || !countdown || g.StoppingAt.IsZero() {
		t.Fatalf("admin Stop = %+v, %v, %v; want a countdown", g, countdown, err)
	}

	gs, err := c.Games(ctx, friend)
	if err != nil || len(gs) != 1 || !gs[0].StoppingAt.Equal(g.StoppingAt) {
		t.Fatalf("Games = %+v, %v; want stopping_at %v", gs, err, g.StoppingAt)
	}

	// Cancel, so the countdown goroutine ends before the test does.
	if _, err := c.CancelStop(ctx, admin, "minecraft"); err != nil {
		t.Fatal(err)
	}
	if g, _ := c.Game(ctx, friend, "minecraft"); !g.StoppingAt.IsZero() {
		t.Errorf("stopping_at still set after cancel: %v", g.StoppingAt)
	}

	// An unknown name is a 404 *APIError, not "API down".
	_, err = c.Game(ctx, friend, "hytale")
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Errorf("unknown game err = %v, want 404", err)
	}
}
