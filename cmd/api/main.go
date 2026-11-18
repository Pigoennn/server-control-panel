// Command api runs the control API: start, stop and status for the game
// servers, over HTTP on localhost. Configuration comes from environment
// variables; see .env.example in the repo root.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"github.com/Pigoennn/server-control-panel/internal/api"
	"github.com/Pigoennn/server-control-panel/internal/docker"
	"github.com/Pigoennn/server-control-panel/internal/games"
	"github.com/Pigoennn/server-control-panel/internal/games/minecraft"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// main only sets up the logger and turns an error into an exit code.
	// Everything else is in run, which returns errors like any other
	// function; that keeps os.Exit (which skips deferred calls) in one place.
	if err := run(logger); err != nil {
		logger.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}

	dc, err := docker.New()
	if err != nil {
		return err
	}
	defer dc.Close()

	// Both servers share the RCON port; only one runs at a time.
	query := minecraft.NewRCONQuery(cfg.RCONAddr, cfg.RCONPassword)
	gs := []games.Game{
		minecraft.New(minecraft.Config{
			Name:        "minecraft",
			Container:   "minecraft",
			Docker:      dc,
			Query:       query,
			StopTimeout: 2 * time.Minute, // stop_grace_period in deploy/minecraft
		}),
		minecraft.New(minecraft.Config{
			Name:        "minecraft-vanilla",
			Container:   "minecraft-vanilla",
			Docker:      dc,
			Query:       query,
			StopTimeout: time.Minute, // stop_grace_period in deploy/minecraft-vanilla
		}),
		minecraft.New(minecraft.Config{
			Name:        "minecraft-create",
			Container:   "minecraft-create",
			Docker:      dc,
			Query:       query,
			StopTimeout: 2 * time.Minute, // stop_grace_period in deploy/minecraft-create
		}),
	}

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: api.New(api.Config{
			Games:   gs,
			Token:   cfg.Token,
			Logger:  logger,
			Limiter: rate.NewLimiter(5, 10), // 5 a second, bursts of 10
		}),
		// Slow clients can't hold a connection open by trickling headers.
		ReadHeaderTimeout: 10 * time.Second,
		// Long enough for a blocking stop of the modded server.
		WriteTimeout: 3 * time.Minute,
	}

	// ctx is cancelled on Ctrl-C (SIGINT) or `kill`/systemd stop (SIGTERM).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ListenAndServe blocks, so it runs in its own goroutine and reports
	// back over a channel. The channel has room for one value, so the
	// goroutine can always send and exit even if nobody is receiving.
	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		errc <- srv.ListenAndServe()
	}()

	// select waits for whichever happens first.
	select {
	case err := <-errc:
		// The server failed to start (port in use, say). Shutdown makes
		// ListenAndServe return ErrServerClosed, but we haven't called it.
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	// Stop accepting connections and wait for in-flight requests. Ten
	// seconds, not three minutes: a stop request in progress keeps running
	// in Docker regardless, because the adapter passed it to the daemon.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Requests still running after 10 s are cut off. Not a failure of
		// the API itself, so log it and exit cleanly.
		logger.Warn("shutdown timed out; dropped in-flight requests", "err", err)
	}
	return nil
}
