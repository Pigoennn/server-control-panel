package games

import (
	"context"
	"sync"
)

// FakeGame is a Game that keeps its state in memory, for tests of code that
// uses Game (the API now, the bot later) without Docker or Minecraft.
// It's in a normal (non _test.go) file so other packages' tests can use it.
//
// Set the exported fields to arrange a scenario, then read Starts, Stops
// and StopCtxErr afterwards to check what happened.
type FakeGame struct {
	GameName   string
	State      State
	PlayerList []Player

	// Err, when set, is returned by Start, Stop and Status.
	Err error
	// PlayersErr, when set, is returned by Players.
	PlayersErr error

	// Starts and Stops count calls that changed the state.
	Starts, Stops int
	// StopCtxErr records ctx.Err() as Stop saw it, so a test can check the
	// caller didn't pass an already-cancelled context.
	StopCtxErr error
	// AnnounceErr, when set, is returned by Announce (after recording the
	// message, so a test can see what was attempted).
	AnnounceErr error

	// announcements is what Announce was asked to say. Unexported, because
	// the countdown writes it from its own goroutine: tests read it through
	// Said, which takes the lock.
	announcements []string

	// mu guards the fields above, because HTTP handlers run concurrently.
	// The zero value of sync.Mutex is ready to use: no constructor needed.
	mu sync.Mutex
}

// Compile-time check that *FakeGame implements Game. It assigns a nil
// *FakeGame to a Game variable named _ (discarded); if a method were
// missing, this line would fail to compile.
var _ Game = (*FakeGame)(nil)

// FakeGame can announce too, like the Minecraft adapter.
var _ Announcer = (*FakeGame)(nil)

func (g *FakeGame) Name() string { return g.GameName }

func (g *FakeGame) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Err != nil {
		return g.Err
	}
	if g.State == Stopped {
		g.State = Starting
		g.Starts++
	}
	return nil
}

func (g *FakeGame) Stop(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.StopCtxErr = ctx.Err()
	if g.Err != nil {
		return g.Err
	}
	if g.State != Stopped {
		g.State = Stopped
		g.Stops++
	}
	return nil
}

func (g *FakeGame) Status(ctx context.Context) (Status, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Err != nil {
		return Status{}, g.Err
	}
	return Status{State: g.State}, nil
}

func (g *FakeGame) Players(ctx context.Context) ([]Player, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.PlayersErr != nil {
		return nil, g.PlayersErr
	}
	return g.PlayerList, nil
}

func (g *FakeGame) Announce(ctx context.Context, message string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.announcements = append(g.announcements, message)
	return g.AnnounceErr
}

// Said returns a copy of every message passed to Announce, in order. A copy,
// so the caller can't race with a later Announce appending to the same
// slice.
func (g *FakeGame) Said() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.announcements...)
}
