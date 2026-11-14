# Milestone 2: Go Control API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An authenticated HTTP API in Go that lists, starts, stops and reports the status (and players) of the Minecraft servers, through a Docker socket proxy.

**Architecture:** `internal/games` defines the `Game` interface. `internal/games/minecraft` implements it with the Docker SDK (talking to a socket proxy) and RCON. `internal/api` serves the endpoints with `net/http` and wraps them in logging, rate-limit and token middleware. `cmd/api` reads config from the environment and wires everything up.

**Tech Stack:** Go 1.27, `github.com/moby/moby/client` v0.6.3 (Docker SDK), `github.com/containerd/errdefs` (not-found check), `github.com/gorcon/rcon` v1.4.0, `golang.org/x/time/rate` v0.16.0, `log/slog`, `lscr.io/linuxserver/socket-proxy`.

**Spec:** `docs/superpowers/specs/2026-11-14-milestone-2-go-api-design.md`

## How this plan is executed (read first)

- **Skeleton, then Jonathan.** Tasks marked **[J]** are core logic. For those, Claude writes the file with the types, the exact signatures, doc comments explaining the Go idioms, and bodies that return zero values or `errTODO` (never `panic`, so one unfinished function doesn't crash the whole test run). Each body has a `// TODO(jonathan): …` comment saying what it must do. Claude writes the tests in full. Claude runs the tests and shows them failing (RED), then **stops and hands over to Jonathan**. Jonathan fills in the bodies and says when he's done. Claude runs the tests (GREEN), reviews the code and explains any problems, and does not rewrite Jonathan's code unless he asks.
- Tasks not marked [J] are written by Claude in full.
- **Jonathan runs every `git commit`.** Claude gives the exact commands at each commit step.
- Work happens in the repo root on branch `milestone-2-go-api`. No worktrees, no subagents.
- Every commit gate: `go vet ./... && go test ./...` pass.

## Global Constraints

- Secrets (`API_TOKEN`, `MC_RCON_PASSWORD`) only in untracked `.env` files; `.env.example` files committed with names and no values.
- The API listens on `127.0.0.1` only (default `API_ADDR=127.0.0.1:8080`).
- The API reaches Docker only via the proxy: `DOCKER_HOST=tcp://127.0.0.1:2375`. The proxy allows `CONTAINERS`, `ALLOW_START`, `ALLOW_STOP` (+ default `PING`, `VERSION`) and nothing else; `POST=0`, `EXEC=0`.
- Error body is always `{"error": "<message>"}`. Status codes: 401 bad/missing token (identical body for both), 404 unknown game, 409 not created, 429 rate limited, 500 anything else (generic message; detail only in logs).
- Token compared with `crypto/subtle.ConstantTimeCompare`; API refuses to start if `API_TOKEN` is shorter than 32 characters; the token is never logged.
- Rate limit: one `rate.NewLimiter(5, 10)` for the whole API; `/healthz` is not rate-limited or authenticated.
- `http.Server`: `ReadHeaderTimeout: 10*time.Second`, `WriteTimeout: 3*time.Minute`; graceful shutdown on SIGINT/SIGTERM.
- Stop timeouts: `minecraft` 120 s, `minecraft-vanilla` 60 s (match each compose `stop_grace_period`).
- Dependencies: only those in Tech Stack. Routing via `http.ServeMux` patterns.
- Conventional Commits.

## Review Focus

1. **Bot's HTTP client gives up during a 2-minute stop.** The request context is cancelled, and the stop must still finish rather than abort mid-save. Handler uses `context.WithoutCancel` + its own 3-minute timeout → test `TestStopSurvivesClientCancel` (Task 5).
2. **RCON hangs or the port is closed while the server shows `running`.** Status must still return within seconds, without players. `NewRCONQuery` sets a 5 s dial timeout and deadline; handler omits `players` on error → `TestGetGamePlayersError` (Task 5).
3. **`.env` with a short, empty or missing token or password.** The API must refuse to start with a message naming the variable → `TestLoadConfig*` (Task 7).
4. **Container removed with `compose down`.** Start returns 409 with the `up --no-start` hint; status says `stopped` → `TestStartNotCreated` (Task 4), `TestStartNotCreated409` (Task 5).
5. **Docker/proxy down.** A clean 500 whose body does not leak the internal error → `TestInternalErrorIsGeneric` (Task 5).

---

### Task 1: Game interface and FakeGame (Jonathan reviews and edits)

`game.go` is mostly type declarations; Claude writes it with comments explaining interfaces, named string types and sentinel errors, and Jonathan reads and edits it rather than filling bodies. `fake.go` is test support, written by Claude.

**Files:**
- Create: `internal/games/game.go`, `internal/games/fake.go`, `internal/games/fake_test.go`
- Delete: `internal/games/.gitkeep`

**Interfaces:**
- Produces:
  - `type Game interface { Name() string; Start(ctx context.Context) error; Stop(ctx context.Context) error; Status(ctx context.Context) (Status, error); Players(ctx context.Context) ([]Player, error) }`
  - `type State string` with consts `Stopped = "stopped"`, `Starting = "starting"`, `Running = "running"`, `Unhealthy = "unhealthy"`
  - `type Status struct { State State }`, `type Player struct { Name string }`
  - `var ErrNotFound = errors.New("game not found")`, `var ErrNotCreated = errors.New("container not created")`
  - `type FakeGame struct { GameName string; State State; PlayerList []Player; Err error; PlayersErr error; Starts, Stops int; StopCtxErr error }` with a `sync.Mutex`. `Err` is returned by Start/Stop/Status when set. `Start` sets `State = Starting` if `Stopped`; `Stop` sets `State = Stopped` and records `ctx.Err()` into `StopCtxErr`. `Players` returns `PlayersErr` if set, else `PlayerList`. Compile-time check `var _ Game = (*FakeGame)(nil)`.

- [ ] **Step 1: Write `fake_test.go`**: `TestFakeGameStartStop` (stopped → Start → `Starting`, `Starts == 1`; Stop → `Stopped`, `Stops == 1`), `TestFakeGameErr` (with `Err` set, Start/Stop/Status return it).
- [ ] **Step 2: Run** `go test ./internal/games/` → Expected: FAIL (undefined: FakeGame).
- [ ] **Step 3: Write `game.go` and `fake.go`** per Interfaces.
- [ ] **Step 4: Run** `go vet ./... && go test ./internal/games/` → Expected: PASS.
- [ ] **Step 5: Jonathan commits**
```bash
git add internal/games
git commit -m "feat(games): add Game interface and FakeGame for tests"
```

### Task 2: Docker client wrapper and socket proxy

**Files:**
- Create: `internal/docker/client.go`, `deploy/docker-proxy/compose.yaml`
- Delete: `internal/docker/.gitkeep`
- Modify: `go.mod`, `go.sum` (`go get github.com/moby/moby/client@v0.6.3 github.com/containerd/errdefs`)

**Interfaces:**
- Produces (package `docker`):
  - `type Client interface { ContainerInspect(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error); ContainerStart(ctx context.Context, id string, opts client.ContainerStartOptions) (client.ContainerStartResult, error); ContainerStop(ctx context.Context, id string, opts client.ContainerStopOptions) (client.ContainerStopResult, error) }` — `*client.Client` satisfies it (compile-time check `var _ Client = (*client.Client)(nil)`).
  - `func New() (*client.Client, error)` → `client.New(client.FromEnv)`.
  - `func IsNotFound(err error) bool` → `cerrdefs.IsNotFound(err)`.

- [ ] **Step 1: Write `client.go`**, with a comment on why the interface is small (only what the adapter uses, so tests can fake it).
- [ ] **Step 2: Write `deploy/docker-proxy/compose.yaml`**: project `docker-proxy`, image `lscr.io/linuxserver/socket-proxy` pinned to a current version tag, `restart: unless-stopped`, `/var/run/docker.sock:/var/run/docker.sock:ro`, `read_only: true`, `tmpfs: /run`, port `"127.0.0.1:2375:2375"`, env `CONTAINERS=1`, `ALLOW_START=1`, `ALLOW_STOP=1`, `POST=0`, `EXEC=0`, all other sections 0. Comments explain each permission.
- [ ] **Step 3: Verify proxy**
  Run: `docker compose -f deploy/docker-proxy/compose.yaml up -d`, then
  `curl -s 127.0.0.1:2375/containers/minecraft-vanilla/json | head -c 80` → Expected: JSON starting `{"Id":`.
  `curl -s -o /dev/null -w '%{http_code}\n' -X POST 127.0.0.1:2375/containers/create` → Expected: `403`.
  `curl -s -o /dev/null -w '%{http_code}\n' -X POST 127.0.0.1:2375/containers/minecraft-vanilla/exec` → Expected: `403`.
  `curl -s -o /dev/null -w '%{http_code}\n' -X POST 127.0.0.1:2375/containers/minecraft-vanilla/kill` → Expected: `403`.
- [ ] **Step 4: Run** `go vet ./... && go build ./...` → Expected: no output.
- [ ] **Step 5: Jonathan commits**
```bash
git add internal/docker deploy/docker-proxy go.mod go.sum
git commit -m "feat(docker): add Docker client wrapper and socket proxy allowing only inspect, start and stop"
```

### Task 3: Minecraft state mapping and `list` parser [J]

**Files:**
- Create: `internal/games/minecraft/state.go`, `internal/games/minecraft/list.go`, `internal/games/minecraft/state_test.go`, `internal/games/minecraft/list_test.go`

**Interfaces:**
- Consumes: `games.State` consts, `games.Player` (Task 1); `container.State`, `container.Health` from `github.com/moby/moby/api/types/container`.
- Produces: `func stateFrom(s *container.State) games.State`; `func parseList(reply string) ([]games.Player, error)`.

- [ ] **Step 1: Write `state_test.go`**: table test `TestStateFrom`:

| input | want |
|---|---|
| `nil` | `Stopped` |
| `{Running: false}` | `Stopped` |
| `{Running: true, Health: nil}` | `Running` |
| `{Running: true, Health: {Status: container.NoHealthcheck}}` | `Running` |
| `{Running: true, Health: {Status: container.Starting}}` | `Starting` |
| `{Running: true, Health: {Status: container.Healthy}}` | `Running` |
| `{Running: true, Health: {Status: container.Unhealthy}}` | `Unhealthy` |

- [ ] **Step 2: Write `list_test.go`**: table test `TestParseList`:

| reply | want |
|---|---|
| `"There are 0 of a max of 20 players online: "` | `[]` (empty, non-nil) |
| `"There are 0 of a max of 20 players online:"` | `[]` |
| `"There are 1 of a max of 20 players online: Pigoenn"` | `[{Pigoenn}]` |
| `"There are 3 of a max of 20 players online: Pigoenn, probitbit, Steve"` | three players in order |
| `"There are 1 of a max of 20 players online: Pigoenn\n"` | `[{Pigoenn}]` |
| `"Unknown command"` | error |

- [ ] **Step 3: Write skeletons** of `stateFrom` (returns `""`) and `parseList` (returns `nil, errTODO`), with `var errTODO = errors.New("TODO(jonathan): not implemented")` in `list.go`. Comments point at `strings.Cut`, `strings.HasPrefix`, `strings.Split`, `strings.TrimSpace`.
- [ ] **Step 4: Run** `go test ./internal/games/minecraft/` → Expected: FAIL in every case. **Hand over to Jonathan.**
- [ ] **Step 5: Jonathan fills the bodies.** Then run `go vet ./... && go test ./internal/games/minecraft/` → Expected: PASS. Claude reviews.
- [ ] **Step 6: Jonathan commits**
```bash
git add internal/games/minecraft
git commit -m "feat(minecraft): map container state to game state and parse the RCON list reply"
```

### Task 4: Minecraft adapter [J]

**Files:**
- Create: `internal/games/minecraft/minecraft.go`, `internal/games/minecraft/rcon.go`, `internal/games/minecraft/minecraft_test.go`
- Modify: `go.mod`, `go.sum` (`go get github.com/gorcon/rcon@v1.4.0`)

**Interfaces:**
- Consumes: `docker.Client`, `docker.IsNotFound` (Task 2); `stateFrom`, `parseList` (Task 3); `games.*` (Task 1).
- Produces:
  - `type Config struct { Name string; Container string; Docker docker.Client; Query func(cmd string) (string, error); StopTimeout time.Duration }`
  - `type Server struct { cfg Config }`; `func New(cfg Config) *Server`; `var _ games.Game = (*Server)(nil)`
  - Methods per `games.Game`. Errors from Docker are wrapped with `fmt.Errorf("inspect %s: %w", …)` style.
  - `func NewRCONQuery(addr, password string) func(cmd string) (string, error)` in `rcon.go` (Claude writes in full): each call `rcon.Dial(addr, password, rcon.SetDialTimeout(5*time.Second), rcon.SetDeadline(5*time.Second))`, `Execute`, `Close`.

- [ ] **Step 1: Write `minecraft_test.go`** with a `fakeDocker` (fields: `inspect *container.State`, `inspectErr error`, `started []string`, `stopped []string`, `stopTimeout *int`) and a not-found error built as `fmt.Errorf("no such container: %w", cerrdefs.ErrNotFound)`. Tests:
  - `TestStatusRunning`: inspect `{Running: true, Health: Healthy}` → `Status{Running}`.
  - `TestStatusNotCreated`: not-found → `Status{Stopped}`, nil error.
  - `TestStatusDockerError`: `errors.New("proxy down")` → error, and `errors.Is` finds the original.
  - `TestStartStopped`: `{Running: false}` → nil; `started == ["minecraft"]`.
  - `TestStartAlreadyRunning`: running → nil; `started` empty.
  - `TestStartNotCreated`: not-found → `errors.Is(err, games.ErrNotCreated)`.
  - `TestStopRunning`: running, `StopTimeout: 2*time.Minute` → nil; `stopped == ["minecraft"]`, `*stopTimeout == 120`.
  - `TestStopAlreadyStopped` and `TestStopNotCreated`: nil; `stopped` empty.
  - `TestPlayersNotRunning`: stopped → empty list; `Query` must not be called (fake `Query` calls `t.Fatal`).
  - `TestPlayersRunning`: running, `Query` returns `"There are 1 of a max of 20 players online: Pigoenn"` and asserts `cmd == "list"` → `[{Pigoenn}]`.
  - `TestPlayersQueryError`: running, `Query` errors → error returned.
- [ ] **Step 2: Write skeletons** (`minecraft.go` bodies return zero values / `errTODO`; `Name()` returns `cfg.Name` in full) and `rcon.go` in full.
- [ ] **Step 3: Run** `go test ./internal/games/minecraft/` → Expected: Task 3 tests PASS, Task 4 tests FAIL. **Hand over to Jonathan.**
- [ ] **Step 4: Jonathan fills the bodies.** Hint in comments: a private `inspect(ctx) (*container.State, error)` helper that returns `nil, nil` on not-found keeps the four methods short. Run `go vet ./... && go test ./internal/games/minecraft/` → Expected: PASS. Claude reviews.
- [ ] **Step 5: Jonathan commits**
```bash
git add internal/games/minecraft go.mod go.sum
git commit -m "feat(minecraft): add Minecraft adapter using the Docker SDK and RCON"
```

### Task 5: HTTP handlers [J]

**Files:**
- Create: `internal/api/server.go`, `internal/api/handlers.go`, `internal/api/json.go`, `internal/api/handlers_test.go`

**Interfaces:**
- Consumes: `games.Game`, `games.FakeGame`, `games.ErrNotFound`, `games.ErrNotCreated` (Task 1).
- Produces:
  - `type Config struct { Games []games.Game; Token string; Logger *slog.Logger; Limiter *rate.Limiter }`
  - `func New(cfg Config) http.Handler` (in `server.go`, Claude writes in full): builds the mux; registers `GET /healthz`, `GET /games`, `GET /games/{name}`, `POST /games/{name}/start`, `POST /games/{name}/stop`; wraps the `/games` routes with `requireToken` and `rateLimit` (Task 6) and everything with `logRequests` (Task 6). Until Task 6 is filled the middleware are pass-throughs, so Task 5 tests use a valid token anyway.
  - `type server struct { games []games.Game; logger *slog.Logger }`; `func (s *server) lookup(name string) (games.Game, error)` returning `games.ErrNotFound`.
  - `type gameResponse struct { Name string \`json:"name"\`; State games.State \`json:"state"\`; Players []string \`json:"players,omitzero"\` }` — `omitzero` drops a nil slice but keeps `[]`.
  - Handlers (Jonathan): `healthz`, `listGames`, `getGame`, `startGame`, `stopGame` as `func (s *server) x(w http.ResponseWriter, r *http.Request)`.
  - `json.go` (Claude writes): `func writeJSON(w http.ResponseWriter, status int, v any)`. Jonathan writes `func (s *server) writeError(w http.ResponseWriter, r *http.Request, err error)`: `ErrNotFound` → 404 `"game not found"`, `ErrNotCreated` → 409 `"container not created: run docker compose -f deploy/<game>/compose.yaml up --no-start once"`, else log the error and 500 `"internal error"`.
  - `stopGame` calls `Stop` with `context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)`. `startGame` and `stopGame` log `slog` attr `game`. After a successful start/stop, the response state comes from a fresh `Status` call.
- [ ] **Step 1: Write `handlers_test.go`**, with a helper `do(t, h, method, path string) *httptest.ResponseRecorder` that sets a valid token, and `newTestServer(games ...games.Game)` using a big limiter (`rate.Inf`) and a discard logger. Tests:
  - `TestHealthz`: 200, body `{"status":"ok"}`.
  - `TestListGames`: two fakes (`minecraft` running, `minecraft-vanilla` stopped) → 200, `[{"name":"minecraft","state":"running"},{"name":"minecraft-vanilla","state":"stopped"}]`.
  - `TestGetGameRunning`: players Pigoenn → body `{"name":"minecraft","state":"running","players":["Pigoenn"]}`.
  - `TestGetGameStoppedHasEmptyPlayers`: body contains `"players":[]`.
  - `TestGetGamePlayersError`: `PlayersErr` set → 200, body has no `players` key, state present.
  - `TestGetUnknownGame`: `/games/nope` → 404, `{"error":"game not found"}`.
  - `TestStart`: stopped fake → 200, state `starting`, `Starts == 1`.
  - `TestStartNotCreated409`: `Err: games.ErrNotCreated` → 409, error contains `up --no-start`.
  - `TestStop`: running fake → 200, state `stopped`, `Stops == 1`.
  - `TestStopSurvivesClientCancel`: request built with an already-cancelled context → `StopCtxErr == nil`.
  - `TestInternalErrorIsGeneric`: `Err: errors.New("proxy secret detail")` on GET → 500, body `{"error":"internal error"}`, does not contain `secret`.
  - `TestStartWrongMethod`: `GET /games/minecraft/start` → 405.
- [ ] **Step 2: Write skeletons** (handlers write nothing; `writeError` writes nothing) plus `server.go`, `json.go` in full, and middleware stubs in `middleware.go` that just return `next` (filled in Task 6).
- [ ] **Step 3: Run** `go test ./internal/api/` → Expected: FAIL (wrong codes/bodies). **Hand over to Jonathan.**
- [ ] **Step 4: Jonathan fills the handlers and `writeError`.** Run `go vet ./... && go test ./internal/api/` → Expected: PASS. Claude reviews.
- [ ] **Step 5: Jonathan commits**
```bash
git add internal/api go.mod go.sum
git commit -m "feat(api): add game endpoints for list, status, start and stop"
```

### Task 6: Middleware: token, rate limit, logging [J for `requireToken` and `rateLimit`]

**Files:**
- Modify: `internal/api/middleware.go`
- Create: `internal/api/middleware_test.go`
- Modify: `go.mod`, `go.sum` (`golang.org/x/time@v0.16.0`, if not already added in Task 5)

**Interfaces:**
- Produces:
  - `func requireToken(token string, next http.Handler) http.Handler` (Jonathan): `Authorization` must be exactly `"Bearer " + token`, compared with `subtle.ConstantTimeCompare`; otherwise 401 `{"error":"unauthorized"}` and set `WWW-Authenticate: Bearer`.
  - `func rateLimit(l *rate.Limiter, next http.Handler) http.Handler` (Jonathan): `l.Allow()` false → 429 `{"error":"too many requests"}`.
  - `func logRequests(logger *slog.Logger, next http.Handler) http.Handler` (Claude, in full) with a `statusRecorder` that wraps `http.ResponseWriter` to capture the status. Logs `method`, `path`, `status`, `duration`, `remote`. Never logs headers.
- [ ] **Step 1: Write `middleware_test.go`**:
  - `TestMissingToken`, `TestWrongToken`, `TestBasicScheme` (`Basic <token>`): 401, identical body `{"error":"unauthorized"}`.
  - `TestHealthzNeedsNoToken`: 200 without a header.
  - `TestRateLimit`: limiter `rate.NewLimiter(0, 2)` → requests 1–2 to `/games` 200, request 3 429.
  - `TestHealthzNotRateLimited`: same limiter, 5× `/healthz` all 200.
  - `TestLogDoesNotContainToken`: logger writing to a `bytes.Buffer`; after a request, the buffer contains `"status":200` and not the token.
- [ ] **Step 2: Write `logRequests` in full and `requireToken`/`rateLimit` skeletons** (still pass-through).
- [ ] **Step 3: Run** `go test ./internal/api/` → Expected: auth and rate-limit tests FAIL, others PASS. **Hand over to Jonathan.**
- [ ] **Step 4: Jonathan fills both.** Run `go vet ./... && go test ./...` → Expected: PASS. Claude reviews.
- [ ] **Step 5: Jonathan commits**
```bash
git add internal/api go.mod go.sum
git commit -m "feat(api): require a bearer token, rate-limit and log every request"
```

### Task 7: `cmd/api` wiring, config and RCON in Compose

**Files:**
- Create: `cmd/api/main.go`, `cmd/api/config.go`, `cmd/api/config_test.go`, `.env.example`, `deploy/minecraft/.env.example`, `deploy/minecraft-vanilla/.env.example`
- Delete: `cmd/api/.gitkeep`
- Modify: `deploy/minecraft/compose.yaml`, `deploy/minecraft-vanilla/compose.yaml`

**Interfaces:**
- Consumes: `api.New`, `api.Config` (Task 5); `minecraft.New`, `minecraft.Config`, `minecraft.NewRCONQuery` (Task 4); `docker.New` (Task 2).
- Produces: `type config struct { Token, Addr, RCONAddr, RCONPassword string }`; `func loadConfig(getenv func(string) string) (config, error)` — defaults `Addr=127.0.0.1:8080`, `RCONAddr=127.0.0.1:25575`; errors name the variable: `API_TOKEN is required`, `API_TOKEN must be at least 32 characters`, `MC_RCON_PASSWORD is required`. `DOCKER_HOST` is read by the SDK; `main` logs a warning if it's unset (the SDK would fall back to the raw socket).

- [ ] **Step 1: Write `config_test.go`**: `TestLoadConfigDefaults`, `TestLoadConfigMissingToken`, `TestLoadConfigShortToken` (31 chars), `TestLoadConfigMissingRCONPassword`, each asserting the exact message above. Uses a `map[string]string` lookup as `getenv`.
- [ ] **Step 2: Run** `go test ./cmd/api/` → FAIL (undefined). **Write `config.go`** (Claude; it's boilerplate) → PASS.
- [ ] **Step 3: Write `main.go`**: `slog.New(slog.NewJSONHandler(os.Stdout, nil))`; `loadConfig(os.Getenv)` (exit 1 on error); `docker.New()`; register `minecraft` (container `minecraft`, `StopTimeout: 2*time.Minute`) and `minecraft-vanilla` (container `minecraft-vanilla`, `StopTimeout: time.Minute`), both with `NewRCONQuery(cfg.RCONAddr, cfg.RCONPassword)`; `api.New` with `rate.NewLimiter(5, 10)`; `http.Server` timeouts per Global Constraints; `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` and `srv.Shutdown` with a 10 s timeout.
- [ ] **Step 4: Compose and env files.** In both compose files add `RCON_PASSWORD: ${MC_RCON_PASSWORD:?set MC_RCON_PASSWORD in .env next to this file}` and port `"127.0.0.1:25575:25575"` (comment: RCON for the API's player list; localhost only). Each compose project reads its own `.env`, so `deploy/minecraft/.env.example` and `deploy/minecraft-vanilla/.env.example` hold `MC_RCON_PASSWORD=` with the `$$` note; the repo-root `.env.example` holds `API_TOKEN=` (with `openssl rand -hex 32`), `API_ADDR=127.0.0.1:8080`, `DOCKER_HOST=tcp://127.0.0.1:2375`, `MC_RCON_ADDR=127.0.0.1:25575`, `MC_RCON_PASSWORD=` (same value as the compose `.env` files). Verify the `.gitignore` already ignores all three `.env` files: `git check-ignore -v .env deploy/minecraft/.env deploy/minecraft-vanilla/.env` → Expected: three matches.
- [ ] **Step 5: Manual end-to-end** (Jonathan creates the three `.env` files with real values first):
  - `docker compose -f deploy/minecraft-vanilla/compose.yaml up --no-start` (recreates with RCON settings), proxy running.
  - `env $(grep -v '^#' .env | xargs) go run ./cmd/api` (works in bash and fish).
  - `curl -s 127.0.0.1:8080/healthz` → `{"status":"ok"}`.
  - `curl -s -o /dev/null -w '%{http_code}\n' 127.0.0.1:8080/games` → `401`.
  - With `-H "Authorization: Bearer $API_TOKEN"`: `POST /games/minecraft-vanilla/start` → state `starting`; poll `GET /games/minecraft-vanilla` until `running` with `"players":[]`; `POST …/stop` → `stopped`. Log lines appear as JSON with no token.
  - `docker compose -f deploy/minecraft/compose.yaml config -q` → no output (modded file still valid).
- [ ] **Step 6: Jonathan commits**
```bash
git add cmd/api .env.example deploy/minecraft deploy/minecraft-vanilla
git commit -m "feat(api): wire up the API binary with config from the environment and RCON in Compose"
```

### Task 8: Integration test, README, decision records

**Files:**
- Create: `internal/games/minecraft/integration_test.go`, `docs/decisions/0004-shared-token-auth.md`, `docs/decisions/0005-docker-socket-proxy.md`, `docs/decisions/0006-docker-sdk-existing-containers.md`
- Modify: `README.md`

- [ ] **Step 1: Write `integration_test.go`** with `//go:build integration`: `TestVanillaStartStop` builds a real adapter for `minecraft-vanilla` from env (`DOCKER_HOST`, `MC_RCON_ADDR`, `MC_RCON_PASSWORD`; `t.Skip` if the password is unset), Starts, polls `Status` every 2 s until `Running` (fail after 2 min), checks `Players` returns no error, Stops, and checks `Status` is `Stopped`.
- [ ] **Step 2: Run** `go test ./...` → Expected: PASS and the integration test not compiled. Then `env $(grep -v '^#' .env | xargs) go test -tags integration -run TestVanillaStartStop -v ./internal/games/minecraft/` → Expected: PASS (~30–60 s).
- [ ] **Step 3: README** — add "Running the API": the proxy project in the table, the three `.env` files, the run command, a curl example per endpoint, the `up --no-start` note, how to run unit and integration tests.
- [ ] **Step 4: Decision records 0004–0006** in the existing `docs/decisions/0000-template.md` format, drafted from the spec's Approaches tables, for Jonathan to edit.
- [ ] **Step 5: Final check** `go vet ./... && go test ./... && git status --short` → Expected: tests pass; no `.env` listed.
- [ ] **Step 6: Jonathan commits**
```bash
git add internal/games/minecraft/integration_test.go README.md docs/decisions
git commit -m "docs: document running the API, add integration test and decision records 0004-0006"
```
