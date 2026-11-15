//go:build integration

// This file only compiles with `go test -tags integration`. The build tag
// on the first line (before the package clause, followed by a blank line)
// keeps it out of plain `go test ./...`, which must pass with no Docker.
//
// It drives the real minecraft-vanilla container through the socket proxy,
// so it needs the proxy running, the container created
// (`docker compose -f deploy/minecraft-vanilla/compose.yaml up --no-start`)
// and the repo-root .env loaded:
//
//	env $(grep -v '^#' .env | grep .) go test -tags integration -run TestVanillaStartStop -v ./internal/games/minecraft/
package minecraft

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Pigoennn/server-control-panel/internal/docker"
	"github.com/Pigoennn/server-control-panel/internal/games"
)

func TestVanillaStartStop(t *testing.T) {
	password := os.Getenv("MC_RCON_PASSWORD")
	if password == "" {
		t.Skip("MC_RCON_PASSWORD unset; load the repo-root .env first")
	}
	rconAddr := os.Getenv("MC_RCON_ADDR")
	if rconAddr == "" {
		rconAddr = "127.0.0.1:25575"
	}
	if os.Getenv("DOCKER_HOST") == "" {
		t.Fatal("DOCKER_HOST unset; point it at the socket proxy, tcp://127.0.0.1:2375")
	}

	dc, err := docker.New()
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	s := New(Config{
		Name:        "minecraft-vanilla",
		Container:   "minecraft-vanilla",
		Docker:      dc,
		Query:       NewRCONQuery(rconAddr, password),
		StopTimeout: time.Minute,
	})

	// t.Context() (Go 1.24+) is cancelled when the test ends.
	ctx := t.Context()

	// If the test fails half-way, still stop the server. Cleanup runs after
	// t.Context() is cancelled, so it needs its own context.
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.Stop(stopCtx); err != nil {
			t.Errorf("cleanup stop: %v", err)
		}
	})

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for {
		st, err := s.Status(ctx)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st.State == games.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still %q after 2 minutes", st.State)
		}
		time.Sleep(2 * time.Second)
	}

	players, err := s.Players(ctx)
	if err != nil {
		t.Fatalf("Players (RCON): %v", err)
	}
	t.Logf("players online: %v", players)

	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status after stop: %v", err)
	}
	if st.State != games.Stopped {
		t.Errorf("state after stop = %q, want %q", st.State, games.Stopped)
	}
}
