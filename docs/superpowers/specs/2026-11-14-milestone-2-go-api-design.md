# Milestone 2: Go control API — Design

- **Date:** 2026-11-14
- **Status:** Draft, awaiting review
- **Milestone:** 2 (see `docs/PLAN.md`)

## Why

Milestone 1 made the Minecraft server startable with `deploy/mc.sh`, but only by someone with a shell on the machine. Milestone 2 puts start, stop and status behind a small authenticated HTTP API, so the Discord bot (milestone 3) and later the dashboard (milestone 4) can control the server for friends without touching Docker. It is also where Jonathan learns Go on real code: he fills in the core pieces from skeleton code that Claude provides, and must be able to explain every decision in an interview.

## Goals

- A `Game` interface in Go, and a Minecraft adapter behind it, so a later game (Hytale, milestone 5) is one more adapter.
- An HTTP API: list games, show one game's status and players, start a game, stop a game.
- Every request except the health check needs a shared secret token.
- The API never touches the raw Docker socket. It goes through a socket proxy that allows only the few calls it needs.
- Requests are rate-limited and logged.
- Tests run without Docker or a Minecraft server, using a fake `Game` and a fake Docker client.

## Non-goals

- Discord OAuth, users and roles (milestone 4). The single token stands in for "the bot".
- SQLite, the audit log and usage metrics (milestone 3 onwards). Request logging to stdout is enough for now.
- `Backup` on the interface (milestone 6).
- Enforcing one game at a time (milestone 5). Today both Minecraft servers share port 25565, so starting a second one fails with Docker's own error.
- Running the API in a container, or on the laptop (milestone 7). In milestone 2 it is a binary run on the dev machine.
- Public access: the API listens on `127.0.0.1` only. Cloudflare Tunnel comes with the dashboard.

## Anti-goals (a failure even if it works)

- The token, or the RCON password, committed to git.
- The API able to do anything to Docker beyond list, inspect, start and stop containers.
- Jonathan holding code he can't explain. Skeletons come with comments that explain the Go idioms they use.

## Constraints

- `CLAUDE.md` rules: secrets in untracked `.env`; an allowlist of actions; a limited user or a socket proxy; every request authenticated, rate-limited and logged; tests against a fake `Game`.
- Go 1.27 (from `go.mod`). Docker Engine API 1.56 on the dev machine.
- The modded server takes minutes to boot and up to 2 minutes to stop (`stop_grace_period: 2m`). The vanilla test server boots in about 25 seconds.
- Keep dependencies few: Jonathan is learning the standard library first.

## Who writes what

Claude writes skeleton code for every Go file: the types, the function signatures, `// TODO(jonathan):` comments saying what each body must do, and the tests. Jonathan fills in the bodies of the core pieces (the `Game` interface methods, the Minecraft adapter, the HTTP handlers and the auth check), and Claude reviews and helps debug. Claude writes the non-Go parts outright (Compose files, `.env.example`, README) and drafts the decision records for Jonathan to edit. Jonathan runs every `git commit`.

## Approaches considered

### Controlling the container

| | Approach | Trade-off |
|---|---|---|
| **A (chosen)** | Docker Go SDK on the existing container, found by name: inspect, start, stop | Idiomatic, easy to fake in tests, needs only four Docker calls. After `docker compose down` there is no container to start, and the API says so. |
| B | Run `docker compose up/stop` from Go | Works even after `down`, but shells out, needs the CLI and compose files wherever the API runs, and is hard to test. |
| C | Create the container from Go | A second copy of `compose.yaml` to keep in sync. Ruled out. |

### Player list

| | Approach | Trade-off |
|---|---|---|
| **A (chosen)** | A Go RCON client on `127.0.0.1:25575`, password from `.env` | A real network protocol, and no need for Docker's `exec` permission (close to root). |
| B | `docker exec rcon-cli list` through the SDK | No new port, but needs the `exec` permission. |

### Docker socket

| | Approach | Trade-off |
|---|---|---|
| **A (chosen)** | A socket proxy container allowing only list, inspect, start and stop | Meets the rule properly. One more Compose project. |
| B | Run the API as a user in the `docker` group | Simpler, but the `docker` group is effectively root. |

### Auth

A shared bearer token from `.env` (chosen). Discord OAuth now would pull milestone 4 forward; no auth breaks the rules.

## Design

### Layout

```
cmd/api/main.go                 config from env, build adapters, start server
internal/games/game.go          Game interface, State, Status, Player, errors
internal/games/fake.go          FakeGame for tests (exported: the bot will reuse it)
internal/games/minecraft/       Minecraft adapter
  minecraft.go                  Start, Stop, Status, Players
  state.go                      Docker container state -> games.State
  list.go                       parse the RCON "list" reply
internal/docker/client.go       the small Docker interface the adapter needs
internal/api/                   HTTP layer
  server.go                     routes (net/http patterns)
  handlers.go                   the four game endpoints
  middleware.go                 auth, rate limit, request logging
deploy/docker-proxy/compose.yaml  socket proxy, always on
.env.example                    API_TOKEN, MC_RCON_PASSWORD, ... (no values)
```

### The `Game` interface

```go
type Game interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Status(ctx context.Context) (Status, error)
    Players(ctx context.Context) ([]Player, error)
}
```

- `State` is a string type with the four `mc.sh` states: `stopped`, `starting`, `running`, `unhealthy`.
- `Status` holds `State`. `Player` holds `Name`.
- Sentinel errors in `internal/games`: `ErrNotFound` (no such game), `ErrNotCreated` (the container doesn't exist; run `docker compose -f … up --no-start` once). Handlers map these with `errors.Is`.
- `Start` on a game that isn't `stopped`, and `Stop` on one that is, do nothing and return nil, like `mc.sh`.

### Minecraft adapter

- Configured with a display name, a container name and an RCON address and password. `main.go` registers `minecraft` (container `minecraft`) and `minecraft-vanilla` (container `minecraft-vanilla`), so the fast vanilla server can be used for manual testing.
- Talks to Docker only through `internal/docker.Client`, an interface with just the methods it uses (`ContainerInspect`, `ContainerStart`, `ContainerStop`), so tests pass a fake. The real implementation is the official Docker SDK client pointed at the proxy via `DOCKER_HOST=tcp://127.0.0.1:2375`.
- State mapping, same as `mc.sh`: no container → `ErrNotCreated` from `Start`, `stopped` from `Status`; not running → `stopped`; running with health `starting`/`healthy`/`unhealthy` → `starting`/`running`/`unhealthy`; running with no health check → `running`.
- `Stop` passes a timeout matching the container's grace period (2 minutes), so Docker never kills Java mid-save.
- `Players` returns an empty list without calling RCON unless the state is `running`. It sends `list` over RCON and parses `There are N of a max of M players online: a, b`. An empty server ("...online: ") gives an empty list.
- RCON client: `github.com/gorcon/rcon` (small, maintained, no other dependencies). Each call dials, sends and closes; no long-lived connection to break when the server restarts.

### Compose changes

- `deploy/minecraft/compose.yaml` and `deploy/minecraft-vanilla/compose.yaml`: set `RCON_PASSWORD` from `.env` (doubling any `$`), and publish `127.0.0.1:25575:25575`. The image already enables RCON. Both share the port, which is fine because only one runs.
- `deploy/docker-proxy/compose.yaml`: `lscr.io/linuxserver/socket-proxy`, `restart: unless-stopped`, Docker socket mounted read-only, published on `127.0.0.1:2375`, with only `CONTAINERS=1`, `ALLOW_START=1`, `ALLOW_STOP=1` and everything else (including `POST`, `EXEC`, `IMAGES`, `VOLUMES`) off. `PING` and `VERSION` stay on (their defaults), because the SDK negotiates the API version through them. Builder: confirm these variable names against the image's README and that a container `create`, `kill` or `exec` through the proxy is refused.

### HTTP API

Listens on `API_ADDR` (default `127.0.0.1:8080`). JSON in and out. Errors are `{"error": "<message>"}`.

| Method and path | Auth | Success | Notes |
|---|---|---|---|
| `GET /healthz` | no | `200 {"status":"ok"}` | For monitoring later. Not rate-limited. |
| `GET /games` | yes | `200 [{"name":"minecraft","state":"stopped"}, …]` | |
| `GET /games/{name}` | yes | `200 {"name":…, "state":…, "players":["Pigoenn"]}` | `players` is `[]` unless running. If RCON fails, `players` is omitted and the state still returns. |
| `POST /games/{name}/start` | yes | `200 {"name":…, "state":…}` | Returns once Docker has started the container (state `starting`); the bot polls status. Already running → 200, no-op. |
| `POST /games/{name}/stop` | yes | `200 {"name":…, "state":"stopped"}` | Blocks until stopped (up to ~2 minutes). Already stopped → 200, no-op. |

Errors: unknown game → 404; missing or wrong token → 401 (same message for both, no hint which); not created → 409 with the `up --no-start` hint; rate limited → 429; anything else → 500 with a generic message, details only in the log.

Routing uses the standard library (`http.ServeMux` patterns like `POST /games/{name}/start`, `r.PathValue("name")`). No framework.

The server sets `ReadHeaderTimeout` and a `WriteTimeout` of 3 minutes, so a blocking stop finishes. It shuts down gracefully on SIGINT/SIGTERM (`signal.NotifyContext`, `srv.Shutdown`).

### Middleware

Applied outside-in: logging → rate limit → auth → handler (`/healthz` gets logging only).

- **Auth:** `Authorization: Bearer <token>`, compared with `crypto/subtle.ConstantTimeCompare`. The API refuses to start if `API_TOKEN` is empty or shorter than 32 characters. The token is never logged.
- **Rate limit:** one `golang.org/x/time/rate` limiter for the whole API (the only caller is the bot): 5 requests a second, burst 10. A per-client limiter waits for milestone 4, when there are real users.
- **Logging:** `log/slog`, JSON to stdout, one line per request: method, path, status, duration, remote address. Start and stop also log the game name.

### Config

All from environment variables: `API_TOKEN`, `API_ADDR`, `DOCKER_HOST`, `MC_RCON_PASSWORD`, `MC_RCON_ADDR` (default `127.0.0.1:25575`). A missing required one stops the API at startup with a message naming it. The repo-root `.env` (untracked; `.env.example` committed) holds them. The README shows how to load it for `go run ./cmd/api` in both bash and fish. Builder's call: no `.env` library unless loading it from the shell proves awkward.

### Tests

- `internal/games/minecraft`: table tests for the state mapping and the `list` parser (no Docker); adapter tests with a fake `docker.Client` for start/stop no-ops and `ErrNotCreated`.
- `internal/api`: `httptest` tests against `FakeGame`: each endpoint's status code and JSON, 401 for missing and wrong tokens, 404, 409, 429 when the burst is exceeded, `/healthz` without a token.
- An integration test behind the `integration` build tag that starts and stops the real `minecraft-vanilla` container through the proxy. Run by hand (`go test -tags integration ./...`); CI comes in milestone 7.
- `go vet ./...` and `go test ./...` must pass before each commit.

### Decision records

Claude drafts, Jonathan edits:

- `0004`: shared-token auth until Discord OAuth.
- `0005`: Docker socket proxy instead of the `docker` group.
- `0006`: Docker SDK on existing containers instead of driving Compose from Go.

## Left to the builder

- Order of work and commit boundaries (small Conventional Commits; Jonathan commits).
- Exact JSON field names beyond those above, log field names, and Go package-internal names.
- How the fake Docker client and `FakeGame` record calls for assertions.

## Done when

- `go vet ./...` and `go test ./...` pass.
- With the proxy and the API running: `curl` with the token starts the vanilla server, shows `starting` then `running` with players, and stops it; without the token it gets 401.
- A request to the proxy to create or exec in a container is refused.
- README has a "Running the API" section; decision records 0004–0006 exist.
