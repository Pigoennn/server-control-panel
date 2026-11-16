// Package minecraft is the Game adapter for Minecraft servers run with the
// itzg/minecraft-server image. It controls an existing container through
// the Docker API (via the socket proxy) and asks the server who's online
// over RCON.
package minecraft

import (
	"context"
	"fmt"
	"time"

	"github.com/Pigoennn/server-control-panel/internal/docker"
	"github.com/Pigoennn/server-control-panel/internal/games"
	"github.com/moby/moby/client"
)

// Config is everything one Minecraft server needs. Passing a struct rather
// than five parameters keeps New's call sites readable (fields are named)
// and lets fields be added later without breaking callers.
type Config struct {
	// Name is the game's ID in the API, e.g. "minecraft".
	Name string
	// Container is the Docker container name, e.g. "minecraft".
	Container string
	// Docker talks to the Docker API. In tests it's a fake.
	Docker docker.Client
	// Query sends one RCON command and returns the reply. A function value
	// instead of an interface: it has one job, and a test can pass a
	// closure. In production it comes from NewRCONQuery.
	Query func(cmd string) (string, error)
	// StopTimeout is how long Docker waits for a clean shutdown before
	// killing Java. Match the compose file's stop_grace_period.
	StopTimeout time.Duration
}

// Server is one Minecraft server, and implements games.Game.
type Server struct {
	cfg Config
}

// Compile-time check that *Server implements games.Game.
var _ games.Game = (*Server)(nil)

// ...and games.Announcer, so the API can warn players before a countdown
// stop. If Announce were missing or had the wrong signature, this line
// would stop the build.
var _ games.Announcer = (*Server)(nil)

// New returns an adapter for the server described by cfg.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Name() string { return s.cfg.Name }

func (s *Server) state(ctx context.Context) (st games.State, exists bool, err error) {
	res, err := s.cfg.Docker.ContainerInspect(ctx, s.cfg.Container, client.ContainerInspectOptions{})

	if docker.IsNotFound(err) {
		return games.Stopped, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inspect %s: %w", s.cfg.Container, err)
	}

	state := res.Container.State

	return stateFrom(state), true, nil
}

// Status reports the server's state. A container that doesn't exist counts
// as stopped.
func (s *Server) Status(ctx context.Context) (games.Status, error) {
	st, _, err := s.state(ctx)
	if err != nil {
		return games.Status{}, err
	}

	return games.Status{State: st}, nil
}

// Start starts the container if it's stopped. Docker returns as soon as the
// container is running; the server then takes a while to boot, which shows
// up in Status as "starting".
func (s *Server) Start(ctx context.Context) error {
	st, exists, err := s.state(ctx)

	switch {
	case err != nil:
		return err

	// no container
	case !exists:
		return fmt.Errorf("start %s: %w", s.cfg.Name, games.ErrNotCreated)

	case st != games.Stopped:
		return nil
	}

	// start it
	_, err = s.cfg.Docker.ContainerStart(ctx, s.cfg.Container, client.ContainerStartOptions{})
	if err != nil {
		return fmt.Errorf("start %s: %w", s.cfg.Container, err)
	}

	return nil
}

// Stop stops the container if it's running, waiting for the world to save.
func (s *Server) Stop(ctx context.Context) error {

	st, exists, err := s.state(ctx)

	switch {
	case err != nil:
		return err

	case !exists, st == games.Stopped:
		return nil
	}

	// stop it
	secs := int(s.cfg.StopTimeout.Seconds())
	_, err = s.cfg.Docker.ContainerStop(ctx, s.cfg.Container, client.ContainerStopOptions{Timeout: &secs})
	if err != nil {
		return fmt.Errorf("stop %s: %w", s.cfg.Container, err)
	}

	return nil
}

// Players lists who's online. It only asks the server when it's running;
// otherwise the list is empty.
func (s *Server) Players(ctx context.Context) ([]games.Player, error) {
	// check the game is running and no error occurred
	st, _, err := s.state(ctx)
	switch {
	case err != nil:
		return nil, err

	case st != games.Running:
		return make([]games.Player, 0), nil
	}

	reply, err := s.cfg.Query("list")
	if err != nil {
		return nil, fmt.Errorf("players %s: %w", s.cfg.Name, err)
	}
	return parseList(reply)
}

// Announce shows message in the in-game chat, using the RCON command
// "say <message>". Players see it as "[Server] <message>".
func (s *Server) Announce(ctx context.Context, message string) error {
	_, err := s.cfg.Query("say " + message)
	if err != nil {
		return fmt.Errorf("announce %s: %w", s.cfg.Name, err)
	}

	return nil
}
