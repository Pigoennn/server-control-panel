package games

import (
	"context"
	"errors"
	"testing"
)

func TestFakeGameStartStop(t *testing.T) {
	g := &FakeGame{GameName: "minecraft", State: Stopped}
	ctx := context.Background()

	if err := g.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st, _ := g.Status(ctx)
	if st.State != Starting || g.Starts != 1 {
		t.Errorf("after Start: state %q, starts %d; want starting, 1", st.State, g.Starts)
	}

	if err := g.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st, _ = g.Status(ctx)
	if st.State != Stopped || g.Stops != 1 {
		t.Errorf("after Stop: state %q, stops %d; want stopped, 1", st.State, g.Stops)
	}
}

func TestFakeGameErr(t *testing.T) {
	boom := errors.New("boom")
	g := &FakeGame{GameName: "minecraft", Err: boom}
	ctx := context.Background()

	if err := g.Start(ctx); !errors.Is(err, boom) {
		t.Errorf("Start = %v, want boom", err)
	}
	if err := g.Stop(ctx); !errors.Is(err, boom) {
		t.Errorf("Stop = %v, want boom", err)
	}
	if _, err := g.Status(ctx); !errors.Is(err, boom) {
		t.Errorf("Status = %v, want boom", err)
	}
}

func TestFakeAnnounce(t *testing.T) {
	g := &FakeGame{GameName: "minecraft", State: Running}
	ctx := context.Background()
	g.Announce(ctx, "one")
	g.AnnounceErr = errors.New("rcon down")
	if err := g.Announce(ctx, "two"); err == nil {
		t.Error("Announce returned nil with AnnounceErr set")
	}

	got := g.Said()
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("Said() = %q, want [one two] (failed attempts are recorded too)", got)
	}
}
