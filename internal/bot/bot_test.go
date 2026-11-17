package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// friend and admin (Actors) are declared in apiclient_test.go.
const stranger = "999999999999999999"

const join = "scooter-swizzle.tun.ply.gg"

// harness is one bot wired to fakes.
type harness struct {
	bot  *Bot
	api  *fakeAPI
	r    *fakeResponder
	logs *syncBuffer
}

// newHarness builds a bot whose API holds games. friend and admin are
// allowed; admin is an admin.
func newHarness(games ...Game) *harness {
	h := &harness{
		api:  &fakeAPI{games: games, errs: map[string]error{}},
		r:    &fakeResponder{},
		logs: &syncBuffer{},
	}
	h.bot = New(Config{
		API:         h.api,
		Allowed:     map[string]bool{friend.ID: true, admin.ID: true},
		Admins:      map[string]bool{admin.ID: true},
		JoinAddress: join,
		Logger:      slog.New(slog.NewJSONHandler(h.logs, nil)),
	})
	return h
}

func running(players ...string) Game {
	if players == nil {
		players = []string{} // nobody, as opposed to unknown
	}
	return Game{Name: "minecraft", State: "running", Players: players}
}

func stopped(name string) Game {
	return Game{Name: name, State: "stopped"}
}

// wantEdits fails unless the bot's edits had exactly these contents.
func wantEdits(t *testing.T, r *fakeResponder, want ...string) {
	t.Helper()
	if got := r.contents(); !slices.Equal(got, want) {
		t.Errorf("edits:\n got %q\nwant %q", got, want)
	}
}

// wantButtons fails unless e has exactly these buttons, in order.
func wantButtons(t *testing.T, e edit, want ...Button) {
	t.Helper()
	if !slices.Equal(e.Buttons, want) {
		t.Errorf("buttons:\n got %+v\nwant %+v", e.Buttons, want)
	}
}

// --- who may use the bot ---

func TestNotAllowed(t *testing.T) {
	h := newHarness(running())
	h.bot.HandleCommand(context.Background(), stranger, "status", "", h.r)

	wantEdits(t, h.r, notAllowed)
	if calls := h.api.allCalls(); len(calls) != 0 {
		t.Errorf("API called for a stranger: %+v", calls)
	}
	if logs := h.logs.String(); !strings.Contains(logs, stranger) || !strings.Contains(logs, `"level":"WARN"`) {
		t.Errorf("refusal not logged at Warn with the user ID: %s", logs)
	}
}

func TestButtonNotAllowed(t *testing.T) {
	h := newHarness(running("Steve"))
	h.bot.HandleButton(context.Background(), stranger, confirmPrefix+"minecraft", h.r)

	wantEdits(t, h.r, notAllowed)
	if calls := h.api.allCalls(); len(calls) != 0 {
		t.Errorf("API called for a stranger's button: %+v", calls)
	}
}

func TestAdminIsAllowed(t *testing.T) {
	h := newHarness(stopped("minecraft"))
	onlyAdmin := "444444444444444444"
	h.bot.admins[onlyAdmin] = true // in Admins but not Allowed

	h.bot.HandleCommand(context.Background(), onlyAdmin, "status", "", h.r)

	if got := h.r.last().Content; got == notAllowed || !strings.Contains(got, "minecraft") {
		t.Errorf("admin not in Allowed got %q, want a status reply", got)
	}
}

// --- /status ---

func TestStatus(t *testing.T) {
	h := newHarness(running("Steve"), stopped("minecraft-vanilla"))
	h.bot.HandleCommand(context.Background(), friend.ID, "status", "", h.r)

	got := h.r.last().Content
	for _, want := range []string{"minecraft", "running", "Steve", "minecraft-vanilla", "stopped"} {
		if !strings.Contains(got, want) {
			t.Errorf("status %q doesn't contain %q", got, want)
		}
	}
	if calls := h.api.callsTo("Game"); len(calls) != 1 || calls[0].Game != "minecraft" {
		t.Errorf("Game calls = %+v, want one, for the running game's players", calls)
	}
}

func TestStatusCountdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := running("Steve")
		g.StoppingAt = time.Now().Add(3*time.Minute + 20*time.Second)
		h := newHarness(g)

		h.bot.HandleCommand(context.Background(), friend.ID, "status", "", h.r)

		if got := h.r.last().Content; !strings.Contains(got, "stopping in 3m20s") {
			t.Errorf("status %q doesn't contain %q", got, "stopping in 3m20s")
		}
	})
}

// --- /start ---

func TestStartBecomesReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(stopped("minecraft"))
		h.api.readyAfter = 30 * time.Second
		start := time.Now()

		h.bot.HandleCommand(context.Background(), friend.ID, "start", "minecraft", h.r)

		wantEdits(t, h.r,
			"Starting minecraft, usually ready in a few minutes.",
			"minecraft is up - join at `scooter-swizzle.tun.ply.gg`",
		)
		if took := h.r.last().At.Sub(start); took != 30*time.Second {
			t.Errorf("ready edit after %v, want 30s (the first poll that sees it running)", took)
		}
	})
}

func TestStartReadyWithoutAddress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(stopped("minecraft"))
		h.api.readyAfter = 30 * time.Second
		h.bot.joinAddress = ""

		h.bot.HandleCommand(context.Background(), friend.ID, "start", "minecraft", h.r)

		if got := h.r.last().Content; got != "minecraft is up." {
			t.Errorf("last edit = %q, want %q", got, "minecraft is up.")
		}
	})
}

func TestStartTimesOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(stopped("minecraft")) // readyAfter 0: never ready
		start := time.Now()

		h.bot.HandleCommand(context.Background(), friend.ID, "start", "minecraft", h.r)

		last := h.r.last()
		if last.Content != "minecraft: Still starting - check with /status." {
			t.Errorf("last edit = %q", last.Content)
		}
		if took := last.At.Sub(start); took < editWindow-pollEvery || took > editWindow {
			t.Errorf("gave up after %v, want %v (Discord stops accepting edits at 15m)", took, editWindow)
		}
		if n := len(h.r.all()); n != 2 {
			t.Errorf("%d edits, want 2 (starting, then timed out): %q", n, h.r.contents())
		}
	})
}

func TestStartAlreadyRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(running("Steve"))
		h.bot.HandleCommand(context.Background(), friend.ID, "start", "minecraft", h.r)

		if got := h.r.last().Content; got != "minecraft is already running." {
			t.Errorf("last edit = %q, want %q", got, "minecraft is already running.")
		}
	})
}

func TestStartOtherRunning(t *testing.T) {
	h := newHarness(stopped("minecraft"))
	h.api.errs["Start"] = &APIError{Status: http.StatusConflict, Message: "minecraft-vanilla is already running; stop it first"}

	h.bot.HandleCommand(context.Background(), friend.ID, "start", "minecraft", h.r)

	if got := h.r.last().Content; got != "minecraft-vanilla is already running; stop it first" {
		t.Errorf("last edit = %q, want the API's message as given", got)
	}
}

// --- /stop ---

func TestStopNobodyOnline(t *testing.T) {
	for _, who := range []Actor{friend, admin} {
		t.Run(fmt.Sprintf("admin=%v", who.Admin), func(t *testing.T) {
			h := newHarness(running())
			h.bot.HandleCommand(context.Background(), who.ID, "stop", "minecraft", h.r)

			wantEdits(t, h.r,
				"Stopping minecraft - saving the world can take up to 2 minutes.",
				"minecraft stopped.",
			)
			stops := h.api.callsTo("Stop")
			if len(stops) != 1 || stops[0].Actor != who {
				t.Errorf("Stop calls = %+v, want one as %+v", stops, who)
			}
		})
	}
}

func TestNameList(t *testing.T) {
	tests := []struct {
		players []string
		want    string
	}{
		{[]string{"Steve"}, "Steve"},
		{[]string{"Steve", "Alex"}, "Steve and Alex"},
		{[]string{"Pigoenn", "Steve", "Alex"}, "Pigoenn and 2 others"},
		{[]string{"Pigoenn", "Steve", "Alex", "Notch"}, "Pigoenn and 3 others"},
	}
	for _, tt := range tests {
		if got := nameList(tt.players); got != tt.want {
			t.Errorf("nameList(%q) = %q, want %q", tt.players, got, tt.want)
		}
	}
}

func TestStopRefusedNames(t *testing.T) {
	tests := []struct {
		name    string
		players []string
		want    string
	}{
		{"one", []string{"Steve"}, "Can't stop: Steve is online. Ask an admin."},
		{"two", []string{"Steve", "Alex"}, "Can't stop: Steve and Alex are online. Ask an admin."},
		{"three", []string{"Pigoenn", "Steve", "Alex"}, "Can't stop: Pigoenn and 2 others are online. Ask an admin."},
		{"unknown", nil, "Can't stop: the server didn't say who's online, so I'm assuming someone is. Ask an admin."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(running(tt.players...))
			h.api.errs["Stop"] = &APIError{Status: http.StatusConflict, Message: "players are online", Players: tt.players}

			h.bot.HandleCommand(context.Background(), friend.ID, "stop", "minecraft", h.r)

			if got := h.r.last().Content; got != tt.want {
				t.Errorf("last edit = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAdminStopPrompt(t *testing.T) {
	h := newHarness(running("Steve", "Alex"))
	h.bot.HandleCommand(context.Background(), admin.ID, "stop", "minecraft", h.r)

	last := h.r.last()
	if want := "Steve and Alex online on minecraft. Stop anyway? Players get a 5-minute warning."; last.Content != want {
		t.Errorf("prompt = %q, want %q", last.Content, want)
	}
	wantButtons(t, last,
		Button{ID: "stop-confirm:minecraft", Label: "Stop in 5 minutes", Danger: true},
		Button{ID: "stop-dismiss:minecraft", Label: "Never mind"},
	)
	if stops := h.api.callsTo("Stop"); len(stops) != 0 {
		t.Errorf("Stop called before the admin confirmed: %+v", stops)
	}
}

func TestAdminStopUnknownPlayers(t *testing.T) {
	g := running()
	g.Players = nil // running, but RCON didn't answer
	h := newHarness(g)

	h.bot.HandleCommand(context.Background(), admin.ID, "stop", "minecraft", h.r)

	last := h.r.last()
	if !strings.Contains(last.Content, "Stop anyway?") {
		t.Errorf("prompt = %q, want the confirm prompt", last.Content)
	}
	if len(last.Buttons) != 2 || last.Buttons[0].ID != "stop-confirm:minecraft" {
		t.Errorf("buttons = %+v, want confirm and dismiss", last.Buttons)
	}
	if stops := h.api.callsTo("Stop"); len(stops) != 0 {
		t.Errorf("Stop called before the admin confirmed: %+v", stops)
	}
}

func TestStopDuringCountdownAdmin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := running("Steve")
		g.StoppingAt = time.Now().Add(3*time.Minute + 20*time.Second)
		h := newHarness(g)

		h.bot.HandleCommand(context.Background(), admin.ID, "stop", "minecraft", h.r)

		last := h.r.last()
		if last.Content != "minecraft is stopping in 3m20s." {
			t.Errorf("last edit = %q", last.Content)
		}
		wantButtons(t, last,
			Button{ID: "countdown-cancel:minecraft", Label: "Cancel shutdown"},
			Button{ID: "stop-dismiss:minecraft", Label: "Never mind"},
		)
		if stops := h.api.callsTo("Stop"); len(stops) != 0 {
			t.Errorf("Stop called during a countdown: %+v", stops)
		}
	})
}

func TestStopDuringCountdownNonAdmin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := running("Steve")
		g.StoppingAt = time.Now().Add(3*time.Minute + 20*time.Second)
		h := newHarness(g)

		h.bot.HandleCommand(context.Background(), friend.ID, "stop", "minecraft", h.r)

		last := h.r.last()
		if last.Content != "minecraft is already stopping in 3m20s." {
			t.Errorf("last edit = %q", last.Content)
		}
		if len(last.Buttons) != 0 {
			t.Errorf("non-admin got buttons: %+v", last.Buttons)
		}
	})
}

// The bot's first look said "no countdown", but by the time it called
// Stop an admin had started one: the API answers 202 to the friend.
func TestStopRaceIntoCountdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(running())
		h.api.stopCountdown = true

		h.bot.HandleCommand(context.Background(), friend.ID, "stop", "minecraft", h.r)

		if got := h.r.last().Content; got != "minecraft is already stopping in 5m0s." {
			t.Errorf("last edit = %q, want the non-admin countdown message", got)
		}
	})
}

// --- buttons ---

func TestConfirmStartsCountdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(running("Steve"))
		h.api.stopCountdown = true

		h.bot.HandleButton(context.Background(), admin.ID, "stop-confirm:minecraft", h.r)

		edits := h.r.all()
		if len(edits) == 0 || edits[0].Content != "minecraft stops in 5 minutes. Run /stop again to cancel." {
			t.Fatalf("edits = %q, want the countdown-started message first", h.r.contents())
		}
		for _, e := range edits {
			if len(e.Buttons) != 0 {
				t.Errorf("edit %q kept buttons %+v: the old Confirm could be pressed twice", e.Content, e.Buttons)
			}
		}
		if got := h.r.last().Content; got != "minecraft stopped." {
			t.Errorf("last edit = %q, want %q", got, "minecraft stopped.")
		}
		stops := h.api.callsTo("Stop")
		if len(stops) != 1 || stops[0].Actor != admin {
			t.Errorf("Stop calls = %+v, want one as admin", stops)
		}
	})
}

func TestConfirmAfterDemotion(t *testing.T) {
	h := newHarness(running("Steve"))
	delete(h.bot.admins, admin.ID) // demoted after the prompt was shown
	h.api.errs["Stop"] = &APIError{Status: http.StatusConflict, Message: "players are online", Players: []string{"Steve"}}

	h.bot.HandleButton(context.Background(), admin.ID, "stop-confirm:minecraft", h.r)

	stops := h.api.callsTo("Stop")
	if len(stops) != 1 || stops[0].Actor.Admin {
		t.Errorf("Stop calls = %+v, want one with Admin false", stops)
	}
	if got := h.r.last().Content; got != "Can't stop: Steve is online. Ask an admin." {
		t.Errorf("last edit = %q, want the refused message", got)
	}
}

func TestCountdownCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := running("Steve")
		g.StoppingAt = time.Now().Add(3 * time.Minute)
		h := newHarness(g)

		h.bot.HandleButton(context.Background(), admin.ID, "countdown-cancel:minecraft", h.r)

		cancels := h.api.callsTo("CancelStop")
		if len(cancels) != 1 || cancels[0].Actor != admin {
			t.Errorf("CancelStop calls = %+v, want one as admin", cancels)
		}
		last := h.r.last()
		if last.Content != "Shutdown cancelled; minecraft keeps running." {
			t.Errorf("last edit = %q", last.Content)
		}
		if len(last.Buttons) != 0 {
			t.Errorf("buttons kept after cancel: %+v", last.Buttons)
		}
	})
}

func TestCountdownCancelForbidden(t *testing.T) {
	h := newHarness(running("Steve"))
	delete(h.bot.admins, admin.ID)
	h.api.errs["CancelStop"] = &APIError{Status: http.StatusForbidden, Message: "admins only"}

	h.bot.HandleButton(context.Background(), admin.ID, "countdown-cancel:minecraft", h.r)

	if got := h.r.last().Content; got != "admins only" {
		t.Errorf("last edit = %q, want the API's message", got)
	}
}

func TestDismiss(t *testing.T) {
	h := newHarness(running("Steve"))
	h.bot.HandleButton(context.Background(), admin.ID, "stop-dismiss:minecraft", h.r)

	wantEdits(t, h.r, "OK, leaving minecraft running.")
	if len(h.r.last().Buttons) != 0 {
		t.Errorf("buttons kept after dismiss: %+v", h.r.last().Buttons)
	}
	if calls := h.api.allCalls(); len(calls) != 0 {
		t.Errorf("API called on dismiss: %+v", calls)
	}
}

// --- failures ---

func TestAPIDown(t *testing.T) {
	down := fmt.Errorf("control API GET /games: %w", errors.New("connect: connection refused"))
	tests := []struct {
		name string
		run  func(b *Bot, r Responder)
	}{
		{"status", func(b *Bot, r Responder) { b.HandleCommand(context.Background(), friend.ID, "status", "", r) }},
		{"start", func(b *Bot, r Responder) { b.HandleCommand(context.Background(), friend.ID, "start", "minecraft", r) }},
		{"stop", func(b *Bot, r Responder) { b.HandleCommand(context.Background(), friend.ID, "stop", "minecraft", r) }},
		{"confirm", func(b *Bot, r Responder) { b.HandleButton(context.Background(), admin.ID, "stop-confirm:minecraft", r) }},
		{"cancel", func(b *Bot, r Responder) {
			b.HandleButton(context.Background(), admin.ID, "countdown-cancel:minecraft", r)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(running())
			for _, m := range []string{"Games", "Game", "Start", "Stop", "CancelStop"} {
				h.api.errs[m] = down
			}

			tt.run(h.bot, h.r)

			if got := h.r.last().Content; got != apiDown {
				t.Errorf("last edit = %q, want %q", got, apiDown)
			}
			if !strings.Contains(h.logs.String(), "connection refused") {
				t.Errorf("transport error not logged: %s", h.logs.String())
			}
		})
	}
}

func TestUnknownGame(t *testing.T) {
	for _, command := range []string{"start", "stop"} {
		t.Run(command, func(t *testing.T) {
			h := newHarness(running()) // knows "minecraft" only

			h.bot.HandleCommand(context.Background(), friend.ID, command, "minecraft-old", h.r)

			if got, want := h.r.last().Content, `I don't know a game called "minecraft-old".`; got != want {
				t.Errorf("last edit = %q, want %q", got, want)
			}
		})
	}
}

// An admin confirms, then cancels from a second /stop. The confirm reply
// must stop saying "stops in 5 minutes", and its polling must end then,
// not 14 minutes later.
func TestConfirmThenCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(running("Steve"))
		h.api.stopCountdown = true
		start := time.Now()

		done := make(chan struct{})
		go func() {
			h.bot.HandleButton(context.Background(), admin.ID, "stop-confirm:minecraft", h.r)
			close(done)
		}()

		time.Sleep(2 * time.Minute)
		if _, err := h.api.CancelStop(context.Background(), admin, "minecraft"); err != nil {
			t.Fatal(err)
		}
		<-done

		last := h.r.last()
		if last.Content != "Shutdown cancelled; minecraft keeps running." {
			t.Errorf("confirm reply's last edit = %q, want the cancelled message", last.Content)
		}
		if took := last.At.Sub(start); took > 2*time.Minute+pollEvery {
			t.Errorf("noticed the cancel after %v, want within one poll of 2m", took)
		}
	})
}

func TestStatusListsEveryName(t *testing.T) {
	h := newHarness(running("Pigoenn", "Steve", "Alex"))
	h.bot.HandleCommand(context.Background(), friend.ID, "status", "", h.r)

	got := h.r.last().Content
	for _, name := range []string{"Pigoenn", "Steve", "Alex"} {
		if !strings.Contains(got, name) {
			t.Errorf("status %q doesn't name %s", got, name)
		}
	}
}
