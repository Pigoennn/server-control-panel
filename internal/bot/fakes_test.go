package bot

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"sync"
	"time"
)

// apiCall is one call the bot made on fakeAPI.
type apiCall struct {
	Method string // "Games", "Game", "Start", "Stop", "CancelStop"
	Actor  Actor
	Game   string // empty for Games
}

// fakeAPI is an in-memory APIClient. It behaves enough like the real API
// for the bot's polling to work against fake time:
//
//   - Start moves a stopped game to "starting", and Game reports it
//     "running" once readyAfter has passed (never, if readyAfter is 0).
//   - Stop stops a game straight away (200), or, with stopCountdown set,
//     starts a 5-minute countdown (202). Game reports the game "stopped"
//     once its StoppingAt has passed.
//   - Games leaves Players out, like the real GET /games, so the bot has
//     to call Game for a running game's players.
//   - An unknown name gives a 404 *APIError.
//
// errs scripts a failure per method name: when errs["Stop"] is set, Stop
// returns it and changes nothing. Every call is recorded in calls.
type fakeAPI struct {
	mu            sync.Mutex
	games         []Game
	readyAfter    time.Duration
	stopCountdown bool
	errs          map[string]error
	calls         []apiCall

	startedAt time.Time
}

var _ APIClient = (*fakeAPI)(nil)

// record notes the call and returns the scripted error for method, if any.
// The caller must hold f.mu.
func (f *fakeAPI) record(method string, a Actor, game string) error {
	f.calls = append(f.calls, apiCall{Method: method, Actor: a, Game: game})
	return f.errs[method]
}

// find returns a pointer to the named game, so callers can change it in
// place, or a 404 *APIError. The caller must hold f.mu.
func (f *fakeAPI) find(name string) (*Game, error) {
	for i := range f.games {
		if f.games[i].Name == name {
			f.advance(&f.games[i])
			return &f.games[i], nil
		}
	}
	return nil, &APIError{Status: http.StatusNotFound, Message: "unknown game"}
}

// advance applies what fake time has done to g: a start finishing, or a
// countdown reaching its end.
func (f *fakeAPI) advance(g *Game) {
	now := time.Now()
	if g.State == "starting" && f.readyAfter > 0 && now.Sub(f.startedAt) >= f.readyAfter {
		g.State = "running"
		g.Players = []string{}
	}
	if !g.StoppingAt.IsZero() && !now.Before(g.StoppingAt) {
		g.State = "stopped"
		g.Players = nil
		g.StoppingAt = time.Time{}
	}
}

// copyGame returns g with its own Players slice, so the bot can't change
// the fake's state through the slice it was handed.
func copyGame(g Game) Game {
	g.Players = slices.Clone(g.Players)
	return g
}

func (f *fakeAPI) Games(ctx context.Context, a Actor) ([]Game, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("Games", a, ""); err != nil {
		return nil, err
	}
	var out []Game
	for i := range f.games {
		f.advance(&f.games[i])
		g := copyGame(f.games[i])
		g.Players = nil // GET /games doesn't list players
		out = append(out, g)
	}
	return out, nil
}

func (f *fakeAPI) Game(ctx context.Context, a Actor, name string) (Game, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("Game", a, name); err != nil {
		return Game{}, err
	}
	g, err := f.find(name)
	if err != nil {
		return Game{}, err
	}
	return copyGame(*g), nil
}

func (f *fakeAPI) Start(ctx context.Context, a Actor, name string) (Game, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("Start", a, name); err != nil {
		return Game{}, err
	}
	g, err := f.find(name)
	if err != nil {
		return Game{}, err
	}
	if g.State == "stopped" {
		g.State = "starting"
		f.startedAt = time.Now()
	}
	return copyGame(*g), nil
}

func (f *fakeAPI) Stop(ctx context.Context, a Actor, name string) (Game, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("Stop", a, name); err != nil {
		return Game{}, false, err
	}
	g, err := f.find(name)
	if err != nil {
		return Game{}, false, err
	}
	if f.stopCountdown {
		if g.StoppingAt.IsZero() {
			g.StoppingAt = time.Now().Add(5 * time.Minute)
		}
		return copyGame(*g), true, nil
	}
	g.State = "stopped"
	g.Players = nil
	return copyGame(*g), false, nil
}

func (f *fakeAPI) CancelStop(ctx context.Context, a Actor, name string) (Game, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("CancelStop", a, name); err != nil {
		return Game{}, err
	}
	g, err := f.find(name)
	if err != nil {
		return Game{}, err
	}
	g.StoppingAt = time.Time{}
	return copyGame(*g), nil
}

// callsTo returns the recorded calls to method, in order.
func (f *fakeAPI) callsTo(method string) []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []apiCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// allCalls returns every recorded call.
func (f *fakeAPI) allCalls() []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// edit is one Reply the bot sent, and when (in fake time).
type edit struct {
	Reply
	At time.Time
}

// fakeResponder records every edit instead of sending it to Discord.
type fakeResponder struct {
	mu    sync.Mutex
	edits []edit
}

var _ Responder = (*fakeResponder)(nil)

func (r *fakeResponder) Edit(ctx context.Context, reply Reply) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edits = append(r.edits, edit{Reply: reply, At: time.Now()})
	return nil
}

// all returns every edit so far.
func (r *fakeResponder) all() []edit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.edits)
}

// contents returns just the text of every edit so far.
func (r *fakeResponder) contents() []string {
	var out []string
	for _, e := range r.all() {
		out = append(out, e.Content)
	}
	return out
}

// last returns the newest edit, or a zero edit if there are none.
func (r *fakeResponder) last() edit {
	all := r.all()
	if len(all) == 0 {
		return edit{}
	}
	return all[len(all)-1]
}

// syncBuffer is a bytes.Buffer safe to write from several goroutines, for
// capturing log output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
