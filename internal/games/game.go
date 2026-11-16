// Package games defines what the API can do with a game server, independent
// of which game it is. Each game (Minecraft now, Hytale later) is an
// "adapter": a type that implements the Game interface. The API, the bot
// and the dashboard only ever see Game, so adding a game means writing one
// adapter and touching nothing else.
package games

import (
	"context"
	"errors"
)

// Game is one controllable game server.
//
// In Go, a type implements an interface just by having its methods: there is
// no "implements" keyword. That keeps this package free of any knowledge of
// Docker or Minecraft, and lets tests pass a FakeGame instead.
//
// Every method takes a context.Context first. The context carries a deadline
// and a cancel signal from the caller (for example, an HTTP request that was
// abandoned), so slow calls to Docker or RCON can be stopped early. Passing
// ctx as the first parameter is a Go-wide convention.
type Game interface {
	// Name is the game's ID in URLs, e.g. "minecraft".
	Name() string

	// Start boots the server. It returns once the server has been told to
	// start, not when players can join (watch Status for that). Starting a
	// server that isn't stopped does nothing and returns nil.
	Start(ctx context.Context) error

	// Stop shuts the server down cleanly and waits until it has stopped.
	// Stopping a stopped server does nothing and returns nil.
	Stop(ctx context.Context) error

	// Status reports what the server is doing right now.
	Status(ctx context.Context) (Status, error)

	// Players lists who is online. It's empty unless the server is running.
	Players(ctx context.Context) ([]Player, error)
}

// Announcer is a game that can show a message to everyone in-game. The API
// uses it to warn players before an admin's countdown stops the server.
//
// It's an "optional interface": a separate, small interface that some
// Games also implement, instead of a sixth method on Game. Code that has a
// Game asks whether it can also announce with a type assertion:
//
//	if a, ok := g.(games.Announcer); ok {
//		a.Announce(ctx, "Server stopping in 1 minute")
//	}
//
// ok is false for a game that can't, and the caller just skips the
// warnings. That way a future game without in-game chat (or without a way
// to reach it) still fits Game unchanged. The standard library does the
// same: io.Copy checks whether a Writer is also an io.ReaderFrom.
type Announcer interface {
	// Announce shows message to every player on the server.
	Announce(ctx context.Context, message string) error
}

// State is one of the four states a server can be in. It's a named string
// type rather than a plain string: the compiler then stops you passing any
// random string where a State is expected, while it still prints and
// encodes to JSON as text ("running").
type State string

// The same four states as deploy/mc.sh.
const (
	Stopped   State = "stopped"   // not running (or the container doesn't exist)
	Starting  State = "starting"  // running, but players can't join yet
	Running   State = "running"   // players can join
	Unhealthy State = "unhealthy" // running, but its health check is failing
)

// Status is a struct, not just a State, so fields can be added later (uptime,
// version, memory) without changing the Game interface.
type Status struct {
	State State
}

// Player is someone online. A struct for the same reason as Status.
type Player struct {
	Name string
}

// Sentinel errors: fixed values that callers compare against with
// errors.Is(err, games.ErrNotCreated). errors.Is also sees through wrapping,
// so an adapter can return fmt.Errorf("start minecraft: %w", ErrNotCreated)
// to add context and the API can still recognise it and pick the right
// HTTP status code.
var (
	// ErrNotFound means there is no game with that name.
	ErrNotFound = errors.New("game not found")

	// ErrNotCreated means the game's container doesn't exist, usually after
	// `docker compose down`. Fix: `docker compose -f … up --no-start` once.
	ErrNotCreated = errors.New("container not created")
)
