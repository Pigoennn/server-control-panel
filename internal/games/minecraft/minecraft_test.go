package minecraft

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// fakeDocker implements docker.Client in memory. inspect is what
// ContainerInspect reports (nil plus inspectErr == notFound means "no such
// container"); started and stopped record which containers were started or
// stopped, so tests can check a call did (or didn't) happen.
type fakeDocker struct {
	inspect     *container.State
	inspectErr  error
	started     []string
	stopped     []string
	stopTimeout *int
}

func (f *fakeDocker) ContainerInspect(ctx context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if f.inspectErr != nil {
		return client.ContainerInspectResult{}, f.inspectErr
	}
	return client.ContainerInspectResult{Container: container.InspectResponse{ID: id, State: f.inspect}}, nil
}

func (f *fakeDocker) ContainerStart(ctx context.Context, id string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	f.started = append(f.started, id)
	return client.ContainerStartResult{}, nil
}

func (f *fakeDocker) ContainerStop(ctx context.Context, id string, opts client.ContainerStopOptions) (client.ContainerStopResult, error) {
	f.stopped = append(f.stopped, id)
	f.stopTimeout = opts.Timeout
	return client.ContainerStopResult{}, nil
}

// notFound looks like the SDK's "No such container" error to docker.IsNotFound.
var notFound = fmt.Errorf("no such container: minecraft: %w", cerrdefs.ErrNotFound)

var (
	running = &container.State{Running: true, Health: &container.Health{Status: container.Healthy}}
	exited  = &container.State{Running: false}
)

// newServer builds an adapter around d. Query fails the test if called,
// unless a test replaces it.
func newServer(t *testing.T, d *fakeDocker) *Server {
	t.Helper()
	return New(Config{
		Name:        "minecraft",
		Container:   "minecraft",
		Docker:      d,
		StopTimeout: 2 * time.Minute,
		Query: func(cmd string) (string, error) {
			t.Fatalf("unexpected RCON query %q", cmd)
			return "", nil
		},
	})
}

func TestStatusRunning(t *testing.T) {
	s := newServer(t, &fakeDocker{inspect: running})
	got, err := s.Status(context.Background())
	if err != nil || got.State != games.Running {
		t.Errorf("Status() = %v, %v; want running, nil", got, err)
	}
}

func TestStatusNotCreated(t *testing.T) {
	s := newServer(t, &fakeDocker{inspectErr: notFound})
	got, err := s.Status(context.Background())
	if err != nil || got.State != games.Stopped {
		t.Errorf("Status() = %v, %v; want stopped, nil", got, err)
	}
}

func TestStatusDockerError(t *testing.T) {
	proxyDown := errors.New("proxy down")
	s := newServer(t, &fakeDocker{inspectErr: proxyDown})
	_, err := s.Status(context.Background())
	if !errors.Is(err, proxyDown) {
		t.Errorf("Status() error = %v, want it to wrap %v", err, proxyDown)
	}
}

func TestStartStopped(t *testing.T) {
	d := &fakeDocker{inspect: exited}
	if err := newServer(t, d).Start(context.Background()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if !reflect.DeepEqual(d.started, []string{"minecraft"}) {
		t.Errorf("started = %v, want [minecraft]", d.started)
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	d := &fakeDocker{inspect: running}
	if err := newServer(t, d).Start(context.Background()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if len(d.started) != 0 {
		t.Errorf("started = %v, want no calls", d.started)
	}
}

func TestStartNotCreated(t *testing.T) {
	err := newServer(t, &fakeDocker{inspectErr: notFound}).Start(context.Background())
	if !errors.Is(err, games.ErrNotCreated) {
		t.Errorf("Start() = %v, want games.ErrNotCreated", err)
	}
}

func TestStopRunning(t *testing.T) {
	d := &fakeDocker{inspect: running}
	if err := newServer(t, d).Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if !reflect.DeepEqual(d.stopped, []string{"minecraft"}) {
		t.Errorf("stopped = %v, want [minecraft]", d.stopped)
	}
	if d.stopTimeout == nil || *d.stopTimeout != 120 {
		t.Errorf("stop timeout = %v, want 120 seconds", d.stopTimeout)
	}
}

func TestStopAlreadyStopped(t *testing.T) {
	d := &fakeDocker{inspect: exited}
	if err := newServer(t, d).Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if len(d.stopped) != 0 {
		t.Errorf("stopped = %v, want no calls", d.stopped)
	}
}

func TestStopNotCreated(t *testing.T) {
	d := &fakeDocker{inspectErr: notFound}
	if err := newServer(t, d).Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if len(d.stopped) != 0 {
		t.Errorf("stopped = %v, want no calls", d.stopped)
	}
}

func TestPlayersNotRunning(t *testing.T) {
	// newServer's Query fails the test if it's called.
	got, err := newServer(t, &fakeDocker{inspect: exited}).Players(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("Players() = %#v, %v; want empty non-nil slice, nil", got, err)
	}
}

func TestPlayersRunning(t *testing.T) {
	s := newServer(t, &fakeDocker{inspect: running})
	s.cfg.Query = func(cmd string) (string, error) {
		if cmd != "list" {
			t.Errorf("RCON command = %q, want list", cmd)
		}
		return "There are 1 of a max of 20 players online: Pigoenn", nil
	}
	got, err := s.Players(context.Background())
	if err != nil || !reflect.DeepEqual(got, []games.Player{{Name: "Pigoenn"}}) {
		t.Errorf("Players() = %v, %v; want [Pigoenn], nil", got, err)
	}
}

func TestPlayersQueryError(t *testing.T) {
	refused := errors.New("connection refused")
	s := newServer(t, &fakeDocker{inspect: running})
	s.cfg.Query = func(string) (string, error) { return "", refused }
	if _, err := s.Players(context.Background()); !errors.Is(err, refused) {
		t.Errorf("Players() error = %v, want it to wrap %v", err, refused)
	}
}

func TestAnnounceSendsSay(t *testing.T) {
	s := newServer(t, &fakeDocker{inspect: running})
	var got string
	s.cfg.Query = func(cmd string) (string, error) {
		got = cmd
		return "", nil
	}

	msg := "Server stopping in 5 minutes (requested by an admin)."
	if err := s.Announce(context.Background(), msg); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if want := "say " + msg; got != want {
		t.Errorf("RCON command = %q, want %q", got, want)
	}
}

func TestAnnounceQueryError(t *testing.T) {
	s := newServer(t, &fakeDocker{inspect: running})
	refused := errors.New("connection refused")
	s.cfg.Query = func(string) (string, error) { return "", refused }

	err := s.Announce(context.Background(), "hello")
	if !errors.Is(err, refused) {
		t.Errorf("err = %v, want it to wrap %v", err, refused)
	}
}
