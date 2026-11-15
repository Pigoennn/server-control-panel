# Milestone 3: Discord Bot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A user-installed Discord bot that lets allowlisted friends run `/status`, `/start` and `/stop` in a DM. Behind it, the API gains stop permissions, an admin countdown, and a one-game-at-a-time rule.

**Architecture:** The API keeps every rule: who may stop, the 5-minute countdown, cancel, one game at a time. It learns who is asking from `X-Actor-ID` and `X-Actor-Admin` headers. The bot (`internal/bot`) holds the command logic and talks to the API through an `APIClient` interface. It talks to Discord through a `Responder` interface, so it's tested with fakes. A thin `discordgo` layer (`internal/bot/discord.go`, `cmd/bot`) connects the two.

**Tech Stack:** Go 1.27.3, `net/http`, `log/slog`, `testing/synctest` (fake time for countdown and polling tests), `github.com/bwmarrin/discordgo` (only new dependency).

**Spec:** `docs/superpowers/specs/2026-11-16-milestone-3-discord-bot-design.md`

## Global Constraints

- **Who writes what.** Tasks marked **Writer: Jonathan** get a skeleton plus tests from Claude: types, signatures, `// TODO(jonathan):` comments that explain the Go idioms. Jonathan writes the bodies and Claude reviews. Tasks marked **Writer: Claude** are written by Claude in full. Jonathan runs every `git commit`; Claude gives the commands. Conventional Commits. Work in the repo root on `milestone-3-discord-bot`, with no worktrees.
- **Secrets.** Secrets live only in the untracked repo-root `.env`. `DISCORD_TOKEN` and `API_TOKEN` must never be logged or printed.
- **Testing.**
  - Unit tests need no Docker, no Minecraft and no Discord. The API is tested against `games.FakeGame`; the bot against a fake `APIClient` and a fake `Responder`.
  - `go vet ./... && go test -race ./...` stays green after every task.
- **Copy.** Exact copy from the spec:
  - `You're not on the list for this bot — ask Jonathan.`
  - `minecraft is already running; stop it first` (as `<other> is already running; stop it first`)
  - `Server stopping in 5 minutes (requested by an admin).`
  - `Can't stop: Pigoenn and 2 others are online. Ask an admin.`
  - `<game> stops in 5 minutes. Run /stop again to cancel.`
  - `Still starting — check with /status.`
- **Countdown.** It runs 5 minutes in total. Warnings go out at 5 min, 1 min, 30 s and 10 s before the stop.
- **Polling.** The bot polls every 5 s and gives up after 14 min, because Discord only allows a reply to be edited for 15 min.
- **Discord.**
  - Production: commands are global, with integration type user install and context bot DM.
  - Development: commands are registered to `DISCORD_DEV_GUILD_ID`.
  - Every reply is ephemeral.
  - The bot never posts in a channel or opens a DM on its own.

**Planning ruling: fake time instead of a clock in config.** The spec says the countdown "takes its durations and a clock from config, so tests run it in milliseconds". This plan uses `testing/synctest` instead (Go 1.25+). Tests run inside a bubble where `time.Sleep(5*time.Minute)` returns instantly and `time.Now()` starts at 2000-01-01 00:00 UTC. The countdown and polling use plain constants and `time.Now`, with no injected clock. That is the same result with less code, and a modern Go idiom worth explaining in an interview.

## Review Focus

1. **Discord's 3-second rule.** Discord drops an interaction that isn't acknowledged within 3 s, but RCON can take 5 s and a stop up to 2 min. Every command and button must be acknowledged straight away (a deferred response) before any API call. All bot output is then an edit of that reply. Covered in Task 9's manual step: `/status` while RCON is down still answers.
2. **API down.** If the control API is down or unreachable, every command ends with `The control panel isn't answering right now — tell Jonathan.`, never a reply stuck on "thinking…". Test: `TestAPIDown` in Task 8.
3. **Stale or replayed buttons.** A Confirm button can be pressed after the user was removed from `ADMIN_IDS` or `ALLOWED_IDS`, or after the server already stopped. The bot re-checks the lists on every button, and the API makes the real decision. Tests: `TestConfirmAfterDemotion` and `TestButtonNotAllowed` in Task 8.
4. **Server stopped (and maybe restarted) by other means mid-countdown,** for example with `deploy/mc.sh stop`. If the countdown finds the game already stopped at any warning, it ends without announcing or stopping. Otherwise it would cut short a fresh session someone started later. Test: `TestCountdownEndsIfStoppedElsewhere` in Task 5.
5. **Unknown game name.** The game choices are fixed when the bot starts. If the API's config changes and a name goes stale, the API's `404` becomes `I don't know a game called "x".` Test: `TestUnknownGame` in Task 8.

---

### Task 1: Actor headers and audit logging

**Writer: Claude.**

**Files:**
- Create: `internal/api/actor.go`
- Modify: `internal/api/server.go` (wrap the outermost handler), `internal/api/middleware.go` (`logRequests` adds the actor fields)
- Test: `internal/api/actor_test.go`; add a `doAs` helper to `internal/api/handlers_test.go`

**Interfaces:**
- Produces:
  ```go
  type Actor struct { ID string; Admin bool }
  func actorFrom(ctx context.Context) Actor   // zero Actor if none
  func withActor(next http.Handler) http.Handler
  // test helper:
  func doAs(t *testing.T, h http.Handler, method, path string, a Actor) *httptest.ResponseRecorder
  ```
- Header rules:
  - `X-Actor-ID` is copied as-is.
  - `X-Actor-Admin` sets `Admin` only when its value is exactly `true`.
- `New` returns `withActor(logRequests(...))`. `withActor` sits outside `logRequests` so that `logRequests` can read the actor from `r.Context()`.
- Each request's log line gains `"actor"` (the ID) and `"admin"` (a bool).

- [ ] **Step 1: Write the failing tests.**
  - `TestActorLogged`: send a request with `X-Actor-ID: 123` and `X-Actor-Admin: true`. The JSON log line has `"actor":"123"` and `"admin":true`.
  - `TestNoActorIsNonAdmin`: send a request with no actor headers. The log line has `"actor":""` and `"admin":false`.
  - `TestAdminHeaderOnlyTrue`: for each of `TRUE`, `1` and `yes`, `actorFrom` gives `Admin == false`.
  - Use the logger-to-buffer pattern from `TestLogDoesNotContainToken`.
- [ ] **Step 2: Run the tests and watch them fail.** `go test ./internal/api/ -run 'Actor|Admin'` should fail with undefined `actorFrom`/`withActor`.
- [ ] **Step 3: Implement `actor.go`.**
  - The context key is an unexported `type actorKey struct{}`.
  - The godoc explains why a custom key type: it avoids collisions with other packages' keys.
  - Wire `withActor` into `New` and add the fields to `logRequests`.
- [ ] **Step 4: Run the tests and watch them pass.** `go vet ./... && go test -race ./...` should pass.
- [ ] **Step 5: Commit.**
  ```bash
  git add internal/api
  git commit -m "feat(api): read the acting Discord user from headers and log it on every request"
  ```

### Task 2: One game at a time

**Writer: Jonathan** (`startGame` body).

**Files:**
- Modify: `internal/api/handlers.go` (`startGame`), `internal/api/server.go` (add `startMu sync.Mutex` to `server`)
- Test: `internal/api/handlers_test.go`

**Interfaces:**
- Consumes: `games.Game`, `writeJSON`, `errorResponse`.
- Rule: under `s.startMu`, check every *other* game's `Status`.
  - If one isn't `stopped`, refuse with `409 {"error":"<other name> is already running; stop it first"}` and don't call `Start`.
  - If another game's `Status` returns an error, go through `writeError` (500).
  - Starting the game that is itself already running stays a no-op `200`.

- [ ] **Step 1: Write the failing tests.**
  - `TestStartRefusedWhileOtherRunning`: `minecraft` is running and `minecraft-vanilla` is stopped. `POST /games/minecraft-vanilla/start` gives 409 `{"error":"minecraft is already running; stop it first"}`, and vanilla's `Starts == 0`.
  - `TestStartRefusedWhileOtherStarting`: same, with the other game in state `starting`.
  - `TestStartAllowedWhenOthersStopped`: gives 200 and `Starts == 1`.
  - `TestStartSelfAlreadyRunning`: gives 200 and `{"name":"minecraft","state":"running"}`.
  - `TestStartOtherStatusError`: the other game's `Err` is set. Gives 500 `{"error":"internal error"}`.
  - `TestConcurrentStartsOnlyOneWins`: two stopped games, each started from its own goroutine 50 times at once with a fresh pair each round. In every round, the two `Starts` counts sum to exactly 1. Run with `-race`.
- [ ] **Step 2: Run them and watch them fail.** `go test ./internal/api/ -run Start` should fail: the first test gets 200, not 409.
- [ ] **Step 3: Claude adds the skeleton** (the `startMu` field and a TODO in `startGame`). **Jonathan implements.**
- [ ] **Step 4: Run the tests and watch them pass.** `go vet ./... && go test -race ./...` should pass.
- [ ] **Step 5: Commit.**
  ```bash
  git add internal/api
  git commit -m "feat(api): refuse to start a game while another one is running"
  ```

### Task 3: Announcer

**Writer: Jonathan** (Minecraft `Announce`). **Writer: Claude** (the interface and `FakeGame`).

**Files:**
- Modify: `internal/games/game.go`, `internal/games/fake.go`, `internal/games/minecraft/minecraft.go`
- Test: `internal/games/fake_test.go`, `internal/games/minecraft/minecraft_test.go`

**Interfaces:**
- Produces:
  ```go
  // internal/games
  type Announcer interface { Announce(ctx context.Context, message string) error }
  // FakeGame gains:  AnnounceErr error; announcements []string (guarded by mu)
  //                  func (g *FakeGame) Said() []string  // a copy, taken under the lock
  // minecraft.Server: func (s *Server) Announce(ctx context.Context, message string) error  → s.cfg.Query("say " + message)
  ```
- Compile-time checks: `var _ games.Announcer = (*FakeGame)(nil)` and `var _ games.Announcer = (*Server)(nil)`.
- `FakeGame.Announce` records the message even when `AnnounceErr` is set, so a test can see what was attempted, and then returns `AnnounceErr`.
- Minecraft `Announce` wraps errors as `fmt.Errorf("announce %s: %w", name, err)`.

- [ ] **Step 1: Write the failing tests.**
  - `TestFakeAnnounce`: `Said()` returns the messages in order.
  - `TestAnnounceSendsSay`: Query receives exactly `say Server stopping in 5 minutes (requested by an admin).`.
  - `TestAnnounceQueryError`: the error wraps the Query error (`errors.Is`).
- [ ] **Step 2: Run them and watch them fail.** `go test ./internal/games/...` should fail with undefined `Announce`.
- [ ] **Step 3: Claude writes the interface and the FakeGame part; Jonathan writes `Server.Announce`.**
  - The godoc on `Announcer` explains optional interfaces and the `g.(games.Announcer)` type assertion.
- [ ] **Step 4: Run the tests and watch them pass.** `go vet ./... && go test -race ./...` should pass.
- [ ] **Step 5: Commit.**
  ```bash
  git add internal/games
  git commit -m "feat(games): add optional Announcer interface, implemented with RCON say"
  ```

### Task 4: Stop permissions

**Writer: Jonathan** (`stopGame` body). This task covers stop-now and refusals only. A 202 is a placeholder until Task 5.

**Files:**
- Modify: `internal/api/handlers.go`
- Test: `internal/api/stop_test.go` (new; the old `TestStop*` tests move here unchanged)

**Interfaces:**
- Consumes: `actorFrom` (Task 1).
- Produces:
  ```go
  // 409 body when players are online:
  type playersOnlineResponse struct {
      Error   string   `json:"error"`            // "players are online"
      Players []string `json:"players,omitzero"` // nil = unknown (RCON failed)
  }
  ```
- Rules, applied in this order after the lookup:
  1. If `Status` is `stopped`, return 200 now, as today.
  2. Call `Players`. If it returns a non-empty list, or an error ("unknown"):
     - non-admin: 409 `playersOnlineResponse`, with `Players` nil when unknown;
     - admin: hand over to the countdown (Task 5). Until Task 5 lands, return `501`.
  3. Otherwise (empty list) stop now, as today: 200.

  The Minecraft adapter reports `[]` for a server that is `starting` or `unhealthy`, so a server still booting can be stopped by anyone.

- [ ] **Step 1: Write the failing tests** with `doAs`.
  - `TestStopNobodyOnlineNonAdmin`: running, `PlayerList` empty. Gives 200 `{"name":"minecraft","state":"stopped"}` and `Stops == 1`.
  - `TestStopPlayersOnlineNonAdmin`: players `Steve`, `Alex`. Gives 409 `{"error":"players are online","players":["Steve","Alex"]}` and `Stops == 0`.
  - `TestStopPlayersUnknownNonAdmin`: `PlayersErr` is set. Gives 409 `{"error":"players are online"}` and `Stops == 0`.
  - `TestStopAlreadyStopped`: gives 200, and `Players` is never consulted. Set `PlayersErr` so a call would show.
  - `TestStopNobodyOnlineAdmin`: gives 200 and `Stops == 1`.
- [ ] **Step 2: Run them and watch them fail.** `go test ./internal/api/ -run Stop` should fail: `TestStopPlayersOnlineNonAdmin` gets 200.
- [ ] **Step 3: Claude writes the skeleton (the response type and a TODO); Jonathan implements.**
- [ ] **Step 4: Run the tests and watch them pass.** `go vet ./... && go test -race ./...` should pass.
- [ ] **Step 5: Commit.**
  ```bash
  git add internal/api
  git commit -m "feat(api): only stop a server with players online when an admin asks"
  ```

### Task 5: Countdown and cancel

**Writer: Jonathan** (the countdown goroutine and handlers). **Writer: Claude** (types, routes, tests).

**Files:**
- Create: `internal/api/countdown.go`
- Modify: `internal/api/handlers.go` (`stopGame` admin branch; `stopping_at` in `listGames`/`getGame`), `internal/api/server.go` (route, fields)
- Test: `internal/api/countdown_test.go`

**Interfaces:**
- Produces:
  ```go
  const shutdownDelay = 5 * time.Minute
  var warnAt = []time.Duration{5 * time.Minute, time.Minute, 30 * time.Second, 10 * time.Second}
  func warning(left time.Duration) string // "Server stopping in 5 minutes (requested by an admin)." / "1 minute" / "30 seconds" / "10 seconds"
  const cancelledMessage = "Shutdown cancelled."

  type countdown struct { stoppingAt time.Time; cancel context.CancelFunc }
  // server gains: mu sync.Mutex; countdowns map[string]*countdown  (keyed by game name)
  func (s *server) startCountdown(g games.Game, a Actor) time.Time // idempotent: returns the running one's stoppingAt; a is logged
  func (s *server) stoppingAt(name string) time.Time        // zero if none
  func (s *server) cancelStop(w http.ResponseWriter, r *http.Request) // POST /games/{name}/stop/cancel

  // gameResponse gains:  StoppingAt time.Time `json:"stopping_at,omitzero"`
  ```
- Stop table (the spec's), checked in this order:
  1. A countdown is already running for this game: anyone gets 202 with the current `stopping_at`.
  2. `stopped`: 200.
  3. Players online or unknown: a non-admin gets 409 (Task 4); an admin gets `startCountdown` and 202 `{"name","state","stopping_at"}`.
  4. Nobody online: 200.
- Countdown goroutine:
  - It uses `context.Background()` with its own cancel, never the request's context.
  - At each `warnAt` point (measured back from `stoppingAt`) it first checks `Status`. If the game is `stopped`, it removes itself from the map and returns, with no announcement and no stop.
  - Otherwise it announces if `g` implements `games.Announcer`. A failed announcement is logged at Warn and the countdown carries on.
  - At `stoppingAt` it calls `Stop` with `stopTimeout`, logs any error, and removes itself from the map.
  - Log lines: `"countdown started"`, `"countdown cancelled"`, `"countdown stop"`, each with `game` and the actor.
- Cancel:
  - Admin only; a non-admin gets 403 `{"error":"admins only"}`.
  - With a countdown running: cancel it, announce `Shutdown cancelled.` (if the game is an Announcer) and return 200 `gameResponse`.
  - With none running: 200 `gameResponse`, a no-op.
  - An unknown game gets 404.

- [ ] **Step 1: Write the failing tests.** Every test that involves time runs inside `synctest.Test(t, func(t *testing.T) {...})`, starting at 2000-01-01 00:00 UTC.
  - `TestAdminStopStartsCountdown`: players online, as admin. Gives 202 `{"name":"minecraft","state":"running","stopping_at":"2000-01-01T00:05:00Z"}` and `Stops == 0`.
  - `TestCountdownWarnsThenStops`: after `time.Sleep(5*time.Minute + time.Second); synctest.Wait()`:
    - `Said()` equals the four `warning(...)` strings in order (5 min, 1 min, 30 s, 10 s);
    - `Stops == 1`;
    - `GET /games/minecraft` has no `stopping_at`.
  - `TestStopDuringCountdownSameTime`: a second stop as admin, and a third as non-admin, both get 202 with the same `stopping_at`. Exactly 4 warnings total, so no second countdown ran.
  - `TestCancelByAdmin`: cancel at the 2-minute mark gives 200. After 10 more minutes, `Stops == 0`, and the last message in `Said()` is `Shutdown cancelled.`.
  - `TestCancelNonAdmin403`: gives 403 `{"error":"admins only"}`, and the countdown still stops the server.
  - `TestCancelNoCountdown`: gives 200 and no announcements.
  - `TestStoppingAtInStatus`: during a countdown, both `GET /games` and `GET /games/minecraft` include `"stopping_at":"2000-01-01T00:05:00Z"`.
  - `TestCountdownAnnounceErrorStillStops`: `AnnounceErr` is set, and `Stops == 1` after 5 minutes.
  - `TestCountdownWithoutAnnouncer`: wrap the FakeGame in `struct{ games.Game }`, which hides `Announce`. It stops after 5 minutes.
  - `TestCountdownEndsIfStoppedElsewhere` (Review Focus 4): at the 2-minute mark, set the fake's state to `stopped`; at 3 minutes, set it back to `running`. After 10 minutes, `Stops == 0` and only the first warning was said.
- [ ] **Step 2: Run them and watch them fail.** `go test ./internal/api/ -run 'Countdown|Cancel|StoppingAt'` should fail to compile on the undefined `warning` / `cancelStop`.
- [ ] **Step 3: Claude writes `countdown.go`** (the constants, `warning`, the types, signatures with TODOs, the route `POST /games/{name}/stop/cancel`, and `StoppingAt` on `gameResponse`). **Jonathan implements** the goroutine, `startCountdown`, `cancelStop`, and the 202 branch in `stopGame`.
- [ ] **Step 4: Run the tests and watch them pass.** `go vet ./... && go test -race ./...` should pass.
- [ ] **Step 5: Commit.**
  ```bash
  git add internal/api
  git commit -m "feat(api): admin stop with players online starts a 5-minute in-game countdown, cancellable"
  ```

### Task 6: Bot API client

**Writer: Claude.**

**Files:**
- Create: `internal/bot/apiclient.go`
- Test: `internal/bot/apiclient_test.go` (`httptest.NewServer`)

**Interfaces:**
- Produces:
  ```go
  type Actor struct { ID string; Admin bool }
  type Game struct {
      Name       string
      State      string     // "stopped" | "starting" | "running" | "unhealthy"
      Players    []string   // nil = unknown
      StoppingAt time.Time  // zero = no countdown
  }
  type APIError struct { Status int; Message string; Players []string } // implements error
  type APIClient interface {
      Games(ctx context.Context, a Actor) ([]Game, error)
      Game(ctx context.Context, a Actor, name string) (Game, error)
      Start(ctx context.Context, a Actor, name string) (Game, error)
      Stop(ctx context.Context, a Actor, name string) (g Game, countdown bool, err error) // countdown = 202
      CancelStop(ctx context.Context, a Actor, name string) (Game, error)
  }
  func NewHTTPClient(baseURL, token string) APIClient
  ```
- Every request sets:
  - `Authorization: Bearer <token>`;
  - `X-Actor-ID`;
  - `X-Actor-Admin: true`, only when `a.Admin`.
- A non-2xx response decodes `{"error","players"}` into `*APIError`.
- A transport failure is returned wrapped, so the bot sees it as "API down" (`errors.As` fails for `*APIError`).
- Each call uses an `http.Client` with a 3-minute timeout, the same as the API's `WriteTimeout`, so a blocking stop fits.

- [ ] **Step 1: Write the failing tests.**
  - `TestClientSendsHeaders`: as admin, the token, `X-Actor-ID` and `X-Actor-Admin: true` all arrive. As non-admin, the admin header is absent.
  - `TestClientDecodesGame`: `players:[]` gives an empty non-nil slice; a missing `players` gives nil; `stopping_at` is parsed.
  - `TestClientStopCountdown`: 202 gives `countdown == true`.
  - `TestClientPlayersOnlineError`: 409 gives an `*APIError` with `Status 409`, `Message "players are online"` and `Players`.
  - `TestClientAPIDown`: a closed server gives an error that is not an `*APIError`.
  - `TestClientNeverLogsToken`: the error strings don't contain the token.
- [ ] **Step 2: Run them and watch them fail.** `go test ./internal/bot/` should fail with undefined `NewHTTPClient`.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the tests and watch them pass.** `go vet ./... && go test -race ./...` should pass.
- [ ] **Step 5: Commit.**
  ```bash
  git add internal/bot
  git commit -m "feat(bot): add HTTP client for the control API"
  ```

### Task 7: Bot config

**Writer: Claude.**

**Files:**
- Create: `cmd/bot/config.go`
- Test: `cmd/bot/config_test.go`. Same `env(map)` and `wantErr` pattern as `cmd/api/config_test.go`.

**Interfaces:**
- Produces:
  ```go
  type config struct {
      DiscordToken string           // DISCORD_TOKEN, required
      DevGuildID   string           // DISCORD_DEV_GUILD_ID, optional
      Allowed      map[string]bool  // ALLOWED_IDS ∪ ADMIN_IDS
      Admins       map[string]bool  // ADMIN_IDS
      APIURL       string           // API_URL, default http://127.0.0.1:8080
      APIToken     string           // API_TOKEN, required
      JoinAddress  string           // JOIN_ADDRESS, optional, e.g. scooter-swizzle.tun.ply.gg
  }
  func loadConfig(getenv func(string) string) (config, error)
  ```
- Error messages, exact:
  - `DISCORD_TOKEN is required`
  - `API_TOKEN is required`
  - `ALLOWED_IDS and ADMIN_IDS are both empty: nobody could use the bot`
  - `ALLOWED_IDS: "abc" is not a Discord user ID` (and likewise for `ADMIN_IDS`)
- IDs are trimmed of spaces, and empty items are skipped, so `1, 2,` is valid. A Discord user ID is 17–20 digits.

- [ ] **Step 1: Write the failing tests.**
  - `TestLoadConfigDefaults`
  - `TestAdminsAreAllowed`
  - `TestIDListSpaces`
  - `TestMissingDiscordToken`
  - `TestMissingAPIToken`
  - `TestBothListsEmpty`
  - `TestBadID` (`abc`, and `123`, which is too short)
- [ ] **Step 2: Run them and watch them fail.** `go test ./cmd/bot/` should fail with undefined `loadConfig`.
- [ ] **Step 3: Implement.** `cmd/bot/main.go` holds only `package main` and an empty `func main() {}` until Task 9.
- [ ] **Step 4: Run the tests and watch them pass.** `go vet ./... && go test -race ./...` should pass.
- [ ] **Step 5: Commit.**
  ```bash
  git add cmd/bot
  git commit -m "feat(bot): load bot config from the environment"
  ```

### Task 8: Bot command logic

**Writer: Jonathan** (handler bodies). **Writer: Claude** (types, copy constants, fakes, tests).

**Files:**
- Create: `internal/bot/bot.go`, `internal/bot/fakes_test.go`
- Test: `internal/bot/bot_test.go`

**Interfaces:**
- Consumes: `APIClient`, `Actor`, `Game`, `APIError` (Task 6).
- Produces:
  ```go
  type Button struct { ID, Label string; Danger bool }
  type Reply  struct { Content string; Buttons []Button }
  // Responder edits the user's (already deferred) reply. Every output is an Edit.
  type Responder interface { Edit(ctx context.Context, r Reply) error }

  type Config struct {
      API         APIClient
      Allowed     map[string]bool
      Admins      map[string]bool
      JoinAddress string
      Logger      *slog.Logger
  }
  func New(cfg Config) *Bot
  func (b *Bot) HandleCommand(ctx context.Context, userID, command, game string, r Responder) // command: "status" | "start" | "stop"
  func (b *Bot) HandleButton(ctx context.Context, userID, buttonID string, r Responder)

  const pollEvery = 5 * time.Second
  const editWindow = 14 * time.Minute
  ```
- Button IDs:
  - `stop-confirm:<game>`
  - `stop-dismiss:<game>`
  - `countdown-cancel:<game>`
- Copy constants (in `bot.go`; Claude writes them):
  - `notAllowed = "You're not on the list for this bot — ask Jonathan."`
  - `apiDown = "The control panel isn't answering right now — tell Jonathan."`
  - unknown game: `I don't know a game called %q.`
  - start:
    - `Starting %s, usually ready in a few minutes.`
    - ready, with an address: `%s is up — join at ` + "`%s`"
    - ready, without one: `%s is up.`
    - already running: `%s is already running.`
    - timed out: `%s: Still starting — check with /status.`
  - stop, straight away:
    - `Stopping %s — saving the world can take up to 2 minutes.`
    - `%s stopped.`
  - refused: `Can't stop: %s. Ask an admin.`, where the middle is one of:
    - `Steve is online`
    - `Steve and Alex are online`
    - `Pigoenn and 2 others are online`
    - unknown: `the server didn't say who's online, so I'm assuming someone is`
  - admin confirm prompt: `%s online on %s. Stop anyway? Players get a 5-minute warning.`, with buttons:
    - `Stop in 5 minutes`, Danger, `stop-confirm:<game>`
    - `Never mind`, `stop-dismiss:<game>`
  - dismiss: `OK, leaving %s running.`
  - countdown started: `%s stops in 5 minutes. Run /stop again to cancel.`
  - countdown running, admin: `%s is stopping in %s.`, with buttons:
    - `Cancel shutdown`, `countdown-cancel:<game>`
    - `Never mind`, `stop-dismiss:<game>`
  - countdown running, non-admin: `%s is already stopping in %s.`
  - cancelled: `Shutdown cancelled; %s keeps running.`
  - durations: `time.Until(stoppingAt).Round(time.Second).String()`, e.g. `3m20s`.
- Behaviour:
  - The allowlist check comes first for every command and button. A refusal is logged at Warn with the user ID.
  - The actor is `Actor{ID: userID, Admin: b.Admins[userID]}`, recomputed on every call.
  - `/status`: one line per game from `Games`, plus `Game` for any game that is running, to get its players. The format is the builder's call, but it must contain the state, the player names, and `stopping in <d>` when a countdown is running.
  - `/start`:
    1. Edit the reply to the starting message.
    2. Poll `Game` every `pollEvery`.
    3. On `running`, edit to the ready message.
    4. After `editWindow`, edit to the timed-out message.
    - A 409 or 404 from the API goes into the reply. For a 404, use the unknown-game copy; for a 409, the API's message as given.
  - `/stop`: first call `Game`.
    - Countdown running: an admin gets the countdown-running buttons; a non-admin gets the countdown-running message.
    - Admin with players non-empty or unknown (nil while running): the confirm prompt.
    - Otherwise: edit to the stopping message, call `Stop`, then:
      - 200: `%s stopped.`
      - 409 players: the refused message;
      - 202: the countdown-running message for a non-admin.
  - `stop-confirm`:
    1. Call `Stop` as the current actor.
    2. On 202, edit to the countdown-started message.
    3. Poll `Game` until `stopped`, then edit to `%s stopped.`. Give up silently at `editWindow`.
    - If the user is no longer an admin, the API's 409 gives the refused message (Review Focus 3).
  - `countdown-cancel`: call `CancelStop`, then show the cancelled message, or the API's message on 403.
  - Any non-`*APIError` error from the API gives `apiDown`, and the error is logged.
- Fakes (`fakes_test.go`, Claude):
  - `fakeAPI`: in-memory games, with scripted results per method. It records the calls with their actors.
  - `fakeResponder`: records every `Reply` it receives.

- [ ] **Step 1: Write the failing tests.** Tests involving polling use `synctest.Test`.
  - `TestNotAllowed`: a stranger's `/status` gives one edit, `notAllowed`, and no API calls.
  - `TestButtonNotAllowed`: a stranger presses `stop-confirm:minecraft`. Gives `notAllowed`, and `Stop` is not called.
  - `TestAdminIsAllowed`: a user only in `Admins` gets a status reply.
  - `TestStatus`: one game running with `[Steve]` and one stopped. The reply contains `minecraft`, `running`, `Steve`, `minecraft-vanilla` and `stopped`.
  - `TestStatusCountdown`: `StoppingAt` is now+3m20s. The reply contains `stopping in 3m20s`.
  - `TestStartBecomesReady`: the state goes to running after 30 s. The edits are `Starting minecraft, usually ready in a few minutes.`, then ``minecraft is up — join at `scooter-swizzle.tun.ply.gg` ``.
  - `TestStartTimesOut`: never running. The last edit is `minecraft: Still starting — check with /status.`, sent at 14 minutes.
  - `TestStartOtherRunning`: a 409 `minecraft is already running; stop it first` is shown as given.
  - `TestStopNobodyOnline`: the edits are the stopping message, then `minecraft stopped.`, and the actor's `Admin == false`.
  - `TestStopRefusedNames`: a table of 1, 2 and 3 players, plus unknown, against the exact refused strings.
  - `TestAdminStopPrompt`: an admin with `[Steve, Alex]` gets the confirm prompt with both buttons, and `Stop` is not called.
  - `TestAdminStopUnknownPlayers`: running with nil players gives the confirm prompt.
  - `TestConfirmStartsCountdown`: `stop-confirm` gives a 202, then the countdown-started message. The fake goes to stopped at 5 min, and the last edit is `minecraft stopped.`.
  - `TestConfirmAfterDemotion` (Review Focus 3): the user is removed from `Admins` before pressing. The actor has `Admin == false`, and a 409 gives the refused message.
  - `TestStopDuringCountdownAdmin`: gives the `Cancel shutdown` button.
  - `TestStopDuringCountdownNonAdmin`: gives `minecraft is already stopping in 3m20s.`.
  - `TestCountdownCancel`: `CancelStop` is called as admin, and the reply is `Shutdown cancelled; minecraft keeps running.`.
  - `TestDismiss`: gives `OK, leaving minecraft running.`, with no API calls.
  - `TestAPIDown` (Review Focus 2): for each command, a transport error gives `apiDown` as the final edit.
  - `TestUnknownGame` (Review Focus 5): a 404 gives `I don't know a game called "minecraft-old".`.
- [ ] **Step 2: Run them and watch them fail.** `go test ./internal/bot/` should fail with undefined `New`/`HandleCommand`.
- [ ] **Step 3: Claude writes the skeleton and fakes; Jonathan implements** `HandleCommand`, `HandleButton` and their helpers.
- [ ] **Step 4: Run the tests and watch them pass.** `go vet ./... && go test -race ./...` should pass.
- [ ] **Step 5: Commit.**
  ```bash
  git add internal/bot
  git commit -m "feat(bot): add /status, /start and /stop logic with admin confirm and countdown"
  ```

### Task 9: Discord wiring and `cmd/bot`

**Writer: Claude.**

**Files:**
- Create: `internal/bot/discord.go`
- Modify: `cmd/bot/main.go`, `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `bot.New`, `HandleCommand`, `HandleButton`, `Responder`, `Reply`, `Button` (Task 8), `NewHTTPClient` (Task 6), `loadConfig` (Task 7).
- Produces:
  ```go
  func Commands(games []string, devGuild bool) []*discordgo.ApplicationCommand
  func Attach(s *discordgo.Session, b *Bot, logger *slog.Logger) // adds the InteractionCreate handler
  ```
- Dependency: `go get github.com/bwmarrin/discordgo@<latest release>`. Check that its `ApplicationCommand` has `IntegrationTypes` and `Contexts`, and use `ApplicationIntegrationUserInstall` and `InteractionContextBotDM`. If the latest release lacks them, stop and rule in the ledger.
- Behaviour:
  - Commands:
    - production (`devGuild == false`): global, `IntegrationTypes{UserInstall}`, `Contexts{BotDM}`;
    - development: registered to the guild, `Contexts{Guild}`.
    - `start` and `stop` take a required string option `game` with one choice per name from `GET /games`.
  - Every interaction is answered within 3 s, before any API call (Review Focus 1):
    - a command gets `InteractionResponseDeferredChannelMessageWithSource`;
    - a button gets `InteractionResponseDeferredMessageUpdate`;
    - both set the `MessageFlagsEphemeral` flag.
  - The handler then runs the bot's logic with a `Responder` whose `Edit` calls `InteractionResponseEdit`. It maps `Reply.Buttons` to one action row; `Danger` maps to `DangerButton`, and the rest to `SecondaryButton`. An empty `Buttons` clears the components.
  - The user ID is taken from `i.User` in a DM, or `i.Member.User` in a guild.
  - `main`:
    1. JSON slog to stdout; `loadConfig`.
    2. `GET /games` with a 5-second timeout. On failure: `control API not reachable at <url>: start it first (go run ./cmd/api)`.
    3. Open a session with no privileged intents and `Attach`.
    4. Register the commands: `ApplicationCommandBulkOverwrite(appID, guildID or "", cmds)`.
    5. Wait for SIGINT/SIGTERM, then `Close`.
    - Never log the token.

- [ ] **Step 1: Build and vet.** `go vet ./... && go test -race ./... && go build ./cmd/bot` should pass. There's no unit test here: the layer only translates, and Task 8 covers the logic.
- [ ] **Step 2: Manual check in the private test server** with `DISCORD_DEV_GUILD_ID` set. The API is running and the vanilla server is created.
  - `/status`, `/start minecraft-vanilla` (the reply should change to "is up"), and `/start minecraft` should be refused because vanilla is running.
  - Join the vanilla server, then `/stop minecraft-vanilla` as an admin: the prompt appears. Confirm, check the in-game warning, run `/stop` again, and cancel.
  - Remove yourself from `ADMIN_IDS`, restart the bot, and `/stop`: refused, with your name.
  - Remove yourself from `ALLOWED_IDS` too: `notAllowed`.
  - Stop the API: `apiDown`.
  - Stop the server's RCON by stopping the container through Docker directly while the API runs, then `/status`. It still answers (Review Focus 1).
- [ ] **Step 3: Commit.**
  ```bash
  git add internal/bot cmd/bot go.mod go.sum
  git commit -m "feat(bot): connect to Discord, register user-install DM commands and route interactions"
  ```

### Task 10: Docs, metrics move, decision records

**Writer: Claude** (drafts; Jonathan edits the decision records).

**Files:**
- Modify:
  - `.env.example`: add `DISCORD_TOKEN`, `DISCORD_DEV_GUILD_ID`, `ALLOWED_IDS`, `ADMIN_IDS`, `API_URL`, `JOIN_ADDRESS`, with comments.
  - `README.md`: a new "Running the bot" section:
    - Developer Portal setup: create the app; under Installation, allow user install, and guild install only while developing; bot token; no privileged intents.
    - The install link for friends.
    - Getting a Discord user ID (Developer Mode → Copy User ID).
    - The run order (proxy → API → bot).
    - The commands and the stop rules.
    - The new API status codes: 202, 403, and 409 for "players are online" and "already running"; the `X-Actor-*` headers; and curl examples with `-H "X-Actor-Admin: true"`.
  - `CLAUDE.md`: the metrics rule becomes "from milestone 4 onwards".
  - `docs/PLAN.md`: move "Start logging usage metrics" from milestone 3 to milestone 4.
- Create:
  - `docs/decisions/0007-metrics-in-milestone-4.md`
  - `docs/decisions/0008-user-installed-bot-allowlist.md`
  - `docs/decisions/0009-api-trusts-actor-headers.md`

  These follow `0000-template.md`, with the reasoning and trade-offs from the spec's "Approaches considered" and "Non-goals".

- [ ] **Step 1: Write the docs.** Check that every command in the README runs as written, in bash and fish.
- [ ] **Step 2: Run the full suite.** `go vet ./... && go test -race ./...` should pass.
- [ ] **Step 3: Commit.**
  ```bash
  git add README.md .env.example CLAUDE.md docs
  git commit -m "docs: document the bot, move usage metrics to milestone 4, add decision records 0007-0009"
  ```
