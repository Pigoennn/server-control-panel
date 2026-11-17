package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Button is a button under a reply. ID comes back in HandleButton when the
// user presses it.
type Button struct {
	ID     string
	Label  string
	Danger bool // red, for actions that kick people off
}

// Reply is what the user sees. Every reply replaces the one before, and
// empty Buttons removes any buttons it had.
type Reply struct {
	Content string
	Buttons []Button
}

// Responder edits the user's reply. The Discord layer has already
// acknowledged the command or button (Discord drops it after 3 seconds
// otherwise), so everything the bot says is an Edit of that reply.
type Responder interface {
	Edit(ctx context.Context, r Reply) error
}

// Config is what New needs.
type Config struct {
	API APIClient
	// Allowed and Admins are sets of Discord user IDs. Admins may use the
	// bot even if they're not in Allowed.
	Allowed     map[string]bool
	Admins      map[string]bool
	JoinAddress string // shown when a server is up; empty to leave it out
	Logger      *slog.Logger
}

// Bot is the command logic, with no Discord code in it.
type Bot struct {
	api         APIClient
	allowed     map[string]bool
	admins      map[string]bool
	joinAddress string
	logger      *slog.Logger
}

// New returns a Bot. The maps are used as given, not copied: the bot only
// reads them.
func New(cfg Config) *Bot {
	return &Bot{
		api:         cfg.API,
		allowed:     cfg.Allowed,
		admins:      cfg.Admins,
		joinAddress: cfg.JoinAddress,
		logger:      cfg.Logger,
	}
}

// pollEvery is how often /start and a confirmed stop check on the server.
const pollEvery = 5 * time.Second

// editWindow is how long the bot keeps polling. Discord accepts edits to
// a reply for 15 minutes after the command; 14 leaves a margin.
const editWindow = 14 * time.Minute

// Button IDs are "<prefix><game>", e.g. "stop-confirm:minecraft".
// strings.Cut(id, ":") splits one back into its two halves.
const (
	confirmPrefix = "stop-confirm:"
	dismissPrefix = "stop-dismiss:"
	cancelPrefix  = "countdown-cancel:"
)

// Button labels.
const (
	labelConfirm = "Stop in 5 minutes" // Danger
	labelDismiss = "Never mind"
	labelCancel  = "Cancel shutdown"
)

// What the bot says. The ones with %s and %q go through fmt.Sprintf.
const (
	notAllowed     = "You're not on the list for this bot - ask Jonathan."
	apiDown        = "The control panel isn't answering right now - tell Jonathan."
	msgUnknownGame = "I don't know a game called %q." // game

	msgStarting       = "Starting %s, usually ready in a few minutes." // game
	msgReadyAt        = "%s is up - join at `%s`"                      // game, JoinAddress
	msgReady          = "%s is up."                                    // game (no JoinAddress)
	msgAlreadyRunning = "%s is already running."                       // game
	msgStillStarting  = "%s: Still starting - check with /status."     // game

	msgStopping = "Stopping %s - saving the world can take up to 2 minutes." // game
	msgStopped  = "%s stopped."                                              // game

	msgRefused = "Can't stop: %s. Ask an admin." // whoIsOnline
	// The middle of msgRefused when the API didn't say who's on:
	whoUnknown = "the server didn't say who's online, so I'm assuming someone is"

	msgConfirm        = "%s online on %s. Stop anyway? Players get a 5-minute warning." // nameList, game
	msgConfirmUnknown = "The server didn't say who's online on %s. Stop anyway? Players get a 5-minute warning."
	msgDismissed      = "OK, leaving %s running." // game

	msgCountdownStarted = "%s stops in 5 minutes. Run /stop again to cancel." // game
	msgCountdownAdmin   = "%s is stopping in %s."                             // game, until
	msgCountdownFriend  = "%s is already stopping in %s."                     // game, until
	msgCancelled        = "Shutdown cancelled; %s keeps running."             // game

	bugFound = "ERROR: Bug found, please report to Jonathan"
)

// States
const (
	stateRunning = "running"
	stateStopped = "stopped"
)

// until formats the time left before t the way replies show it, e.g.
// "3m20s".
func until(t time.Time) string {
	return time.Until(t).Round(time.Second).String()
}

// edit sends r and logs a failure. There's nothing else to do when an edit
// fails (the user dismissed the reply, or the 15 minutes ran out), so
// callers carry on without checking.
func (b *Bot) edit(ctx context.Context, resp Responder, r Reply) {
	if err := resp.Edit(ctx, r); err != nil {
		b.logger.Warn("edit reply", "error", err)
	}
}

// say is edit for a reply with only text, which also clears any buttons.
func (b *Bot) say(ctx context.Context, resp Responder, content string) {
	b.edit(ctx, resp, Reply{Content: content})
}

// HandleCommand runs a slash command for the Discord user userID. command
// is "status", "start" or "stop"; game is the chosen game ("" for status).
// It returns when the bot has nothing more to say, which for /start can be
// up to editWindow later: the Discord layer runs each call in its own
// goroutine.
func (b *Bot) HandleCommand(ctx context.Context, userID, command, game string, r Responder) {
	// only allow whitelisted people to run a command
	if !b.allow(ctx, userID, r) {
		return
	}

	actor := Actor{ID: userID, Admin: b.admins[userID]}

	// run the given command
	switch command {
	case "status":
		b.status(ctx, actor, r)
	case "start":
		b.start(ctx, actor, game, r)
	case "stop":
		b.stop(ctx, actor, game, r)
	default:
		b.logger.Error("unknown command", "user", userID, "command", command)
		b.say(ctx, r, bugFound)
	}
}

// HandleButton runs a button press. buttonID is a Button.ID the bot sent
// earlier, e.g. "stop-confirm:minecraft".
func (b *Bot) HandleButton(ctx context.Context, userID, buttonID string, r Responder) {
	// only register inputs of whitelisted people
	if !b.allow(ctx, userID, r) {
		return
	}

	actor := Actor{ID: userID, Admin: b.admins[userID]}

	// check which button was pressed and run command
	prefix, game, ok := strings.Cut(buttonID, ":")
	if !ok {
		b.logger.Error("malformed button ID", "id", buttonID)
		b.say(ctx, r, bugFound)
		return
	}

	switch prefix + ":" {
	case confirmPrefix:
		b.confirmStop(ctx, actor, game, r)
	case dismissPrefix:
		b.say(ctx, r, fmt.Sprintf(msgDismissed, game))
	case cancelPrefix:
		b.cancelCountdown(ctx, actor, game, r)
	default:
		b.logger.Error("unknown button", "id", buttonID)
		b.say(ctx, r, bugFound)
	}
}

// allow reports whether userID may use the bot; if not, it has already replied.
func (b *Bot) allow(ctx context.Context, userID string, r Responder) bool {
	if b.allowed[userID] || b.admins[userID] {
		return true
	}

	b.logger.Warn("user not on allowlist", "user", userID)
	b.say(ctx, r, notAllowed)
	return false
}

// status answers /status: one line per game.
func (b *Bot) status(ctx context.Context, a Actor, r Responder) {
	// get all games to write status on
	allGames, err := b.api.Games(ctx, a)
	if err != nil {
		b.apiFailure(ctx, r, "", err)
		return
	}

	// build response string based on each game
	lines := make([]string, 0, len(allGames))
	for _, game := range allGames {
		line := fmt.Sprintf("%s: %s", game.Name, game.State)

		// if game is currently running, also get player names
		if game.State == stateRunning {
			details, err := b.api.Game(ctx, a, game.Name)

			if err != nil {
				b.apiFailure(ctx, r, game.Name, err)
				return
			}

			line += " - " + playersText(details.Players)
		}

		// if the server is stopping soon, include said info
		if !game.StoppingAt.IsZero() {
			line += fmt.Sprintf(" (stopping in %s)", until(game.StoppingAt))
		}

		lines = append(lines, line)
	}

	final := strings.Join(lines, "\n")
	b.say(ctx, r, final)
}

// start answers /start <game>: start it, then keep the reply up to date
// until the server is up or editWindow runs out.
func (b *Bot) start(ctx context.Context, a Actor, game string, r Responder) {
	// start said game
	g, err := b.api.Start(ctx, a, game)
	if err != nil {
		b.apiFailure(ctx, r, game, err)
		return
	}

	// game might be already running
	if g.State == stateRunning {
		b.say(ctx, r, fmt.Sprintf(msgAlreadyRunning, game))
		return
	}

	b.say(ctx, r, fmt.Sprintf(msgStarting, game))

	// wait until game is ready to run
	_, ok := b.waitUntil(ctx, a, game, func(g Game) bool { return g.State == stateRunning })
	if !ok {
		b.say(ctx, r, fmt.Sprintf(msgStillStarting, game))
		return
	}

	msg := fmt.Sprintf(msgReady, game)
	if b.joinAddress != "" {
		msg = fmt.Sprintf(msgReadyAt, game, b.joinAddress)
	}
	b.say(ctx, r, msg)
}

// stop answers /stop <game>. It looks at the game first only to decide
// which reply to show; the API makes the real decision.
func (b *Bot) stop(ctx context.Context, a Actor, game string, r Responder) {
	g, err := b.api.Game(ctx, a, game)
	if err != nil {
		b.apiFailure(ctx, r, game, err)
		return
	}

	// countdown is already running
	if !g.StoppingAt.IsZero() {
		if a.Admin {
			b.edit(ctx, r, Reply{
				Content: fmt.Sprintf(msgCountdownAdmin, game, until(g.StoppingAt)),
				Buttons: []Button{
					{ID: cancelPrefix + game, Label: labelCancel},
					{ID: dismissPrefix + game, Label: labelDismiss},
				},
			})
		} else {
			b.say(ctx, r, fmt.Sprintf(msgCountdownFriend, game, until(g.StoppingAt)))
		}

		return
	}

	// if admin tried to stop a running game, give them the stop in 5 minutes button
	players := g.Players
	if a.Admin && g.State == stateRunning && (players == nil || len(players) > 0) {

		content := fmt.Sprintf(msgConfirmUnknown, game)
		if len(players) > 0 {
			content = fmt.Sprintf(msgConfirm, nameList(g.Players), game)
		}

		b.edit(ctx, r, Reply{
			Content: content,
			Buttons: []Button{
				{ID: confirmPrefix + game, Label: labelConfirm, Danger: true},
				{ID: dismissPrefix + game, Label: labelDismiss},
			},
		})

		return
	}

	// otherwise, just stop it
	b.say(ctx, r, fmt.Sprintf(msgStopping, game))

	g, countdown, err := b.api.Stop(ctx, a, game)
	if err != nil {
		b.apiFailure(ctx, r, game, err)
		return
	}

	if countdown {
		b.say(ctx, r, fmt.Sprintf(msgCountdownFriend, game, until(g.StoppingAt)))
		return
	}
	b.say(ctx, r, fmt.Sprintf(msgStopped, game))
}

// confirmStop runs when an admin presses "Stop in 5 minutes".
func (b *Bot) confirmStop(ctx context.Context, a Actor, game string, r Responder) {
	_, countdown, err := b.api.Stop(ctx, a, game)
	if err != nil {
		b.apiFailure(ctx, r, game, err)
		return
	}

	// everyone left meanwhile, so it stopped straight away
	if !countdown {
		b.say(ctx, r, fmt.Sprintf(msgStopped, game))
		return
	}

	// start countdown
	b.say(ctx, r, fmt.Sprintf(msgCountdownStarted, game))

	// Wait for the countdown to end either way: the server stopped, or an
	// admin cancelled (no StoppingAt any more, but still not stopped).
	g, ok := b.waitUntil(ctx, a, game, func(g Game) bool {
		return g.State == stateStopped || g.StoppingAt.IsZero()
	})
	switch {
	case !ok:
		// timed out: Discord won't take more edits, so fail quietly
	case g.State == stateStopped:
		b.say(ctx, r, fmt.Sprintf(msgStopped, game))
	default:
		// cancelled (from another reply), so this one stops saying
		// "stops in 5 minutes"
		b.say(ctx, r, fmt.Sprintf(msgCancelled, game))
	}
}

// cancelCountdown runs when an admin presses "Cancel shutdown".
func (b *Bot) cancelCountdown(ctx context.Context, a Actor, game string, r Responder) {
	_, err := b.api.CancelStop(ctx, a, game)
	if err != nil {
		b.apiFailure(ctx, r, game, err)
		return
	}

	b.say(ctx, r, fmt.Sprintf(msgCancelled, game))
}

// apiFailure turns an error from the API client into a reply, for game
// (empty for /status).
func (b *Bot) apiFailure(ctx context.Context, r Responder, game string, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == http.StatusNotFound:
			b.say(ctx, r, fmt.Sprintf(msgUnknownGame, game))

		case apiErr.Status == http.StatusConflict && apiErr.Message == "players are online":
			b.say(ctx, r, fmt.Sprintf(msgRefused, whoIsOnline(apiErr.Players)))

		default:
			b.say(ctx, r, apiErr.Message)
		}
		return
	}

	b.logger.Error("control API unreachable", "game", game, "error", err)
	b.say(ctx, r, apiDown)
}

// waitUntil polls game until done reports true for it, and returns the Game
// that satisfied it. It reports false on timeout or cancellation; the
// caller decides what to say.
//
// done is a function value: the caller passes in what "finished" means,
// e.g. func(g Game) bool { return g.State == stateRunning }. The same idea
// as sort.Slice's less function, or what other languages use lambdas for.
func (b *Bot) waitUntil(ctx context.Context, a Actor, game string, done func(Game) bool) (Game, bool) {
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	timeout := time.After(editWindow)

	for {
		select {
		case <-ctx.Done():
			return Game{}, false
		case <-timeout:
			return Game{}, false
		case <-ticker.C:
			g, err := b.api.Game(ctx, a, game)
			if err != nil {
				b.logger.Warn("poll failed", "game", game, "error", err)
				continue
			}

			if done(g) {
				return g, true
			}
		}
	}
}

// nameList joins player names for a reply: "Steve", "Steve and Alex",
// "Pigoenn and 2 others". players has at least one name.
func nameList(players []string) string {
	switch len(players) {
	case 0:
		return bugFound // players should not be empty
	case 1:
		return players[0]
	case 2:
		return fmt.Sprintf("%s and %s", players[0], players[1])
	default:
		return fmt.Sprintf("%s and %d others", players[0], len(players)-1)
	}
}

// playersText is who's online for a /status line. nil means the API
// couldn't ask the server, which is different from nobody being on.
func playersText(players []string) string {
	switch {
	case players == nil:
		return "players unknown"
	case len(players) == 0:
		return "nobody online"
	default:
		// every name: /status is where people look to see who's on
		return strings.Join(players, ", ")
	}
}

// whoIsOnline is the middle of msgRefused: "Steve is online",
// "Steve and Alex are online", or whoUnknown when players is nil.
func whoIsOnline(players []string) string {
	switch len(players) {
	case 0:
		return whoUnknown
	case 1:
		return nameList(players) + " is online"
	default:
		return nameList(players) + " are online"
	}
}
