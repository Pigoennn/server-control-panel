# Server Control Panel

A self-hosted control panel that lets my friends start, stop and check my home game servers. The plan is in [`docs/PLAN.md`](docs/PLAN.md), and the decisions behind it are in [`docs/decisions/`](docs/decisions/).

## Running the Minecraft server

The server runs in Docker with the [`itzg/minecraft-server`](https://github.com/itzg/docker-minecraft-server) image. It runs the custom "Create Banger" CurseForge pack (Minecraft 1.21.1, NeoForge). Friends reach it through a [playit.gg](https://playit.gg) tunnel, because the home connection is behind CGNAT.

There are four Compose projects:

| Project | File | Runs |
|---|---|---|
| `playit` | `deploy/playit/compose.yaml` | Tunnel agent. Always on, and restarts with Docker. |
| `docker-proxy` | `deploy/docker-proxy/compose.yaml` | Docker socket proxy for the control API. Always on, localhost only. |
| `minecraft` | `deploy/minecraft/compose.yaml` | The game server. Only runs when started on purpose. |
| `minecraft-vanilla` | `deploy/minecraft-vanilla/compose.yaml` | A vanilla test server. Only runs when started on purpose. |

### First-time setup

1. **playit key.** On playit.gg, add a new agent and choose Docker. Copy the secret key into `deploy/playit/.env`, using `.env.example` as the template. `.env` is gitignored and must never be committed. Then start the agent:
   ```sh
   docker compose -f deploy/playit/compose.yaml up -d
   ```
2. **Tunnel.** On the playit.gg dashboard, add a *Minecraft Java* tunnel to the agent, with local address **`127.0.0.1`** and port `25565`. Use `127.0.0.1`, not `localhost`. On this machine `localhost` resolves to IPv6 `::1` first, and the server only listens on IPv4.
3. **Modpack.** In the CurseForge app, export the pack and save it as `deploy/minecraft/modpack/create-banger.zip`. The export holds the manifest and the configs, not the mod jars. The server downloads the mods by their CurseForge IDs on first start.
4. **CurseForge API key.** Nothing to do: the image ships with its own key. To use your own key instead, see the comment in `deploy/minecraft/compose.yaml`. Every `$` in the key must be doubled to `$$` in `.env`, or Compose mangles it.
5. **RCON password.** Copy `deploy/minecraft/.env.example` to `deploy/minecraft/.env` and set `MC_RCON_PASSWORD` (see [Running the API](#running-the-api) for a command that generates it). Compose refuses to start the server without it.
6. **Blocked mods.** Some mod authors block automatic downloads. If the logs list any, download those mods by hand into `deploy/minecraft/downloads/` (gitignored, because the mods can't be redistributed) and start again.

### Day to day

```sh
deploy/mc.sh start    # boot the server (a modpack boot takes a few minutes)
deploy/mc.sh stop     # stop cleanly; saving the world can take up to 2 minutes
deploy/mc.sh status   # stopped | starting | running | unhealthy, plus who's online
```

- **Connecting:** use the playit address, `scooter-swizzle.tun.ply.gg`. On this machine you can also use `127.0.0.1:25565`.
- **Whitelist:** the whitelist is on, and ops add friends in-game with `/whitelist add <name>`. Additions survive restarts.
- **World storage:** the world lives in the Docker volume `minecraft-data`. Neither `docker compose down` nor a container rebuild deletes it.
- **Server console:** `docker attach minecraft` opens it. Detach with Ctrl-P Ctrl-Q; Ctrl-C stops the server.

### Vanilla test server

`deploy/minecraft-vanilla/compose.yaml` runs a plain vanilla server (26.3) with its own world volume. Friends can join it without installing the modpack. It uses the same port, so the same playit address reaches it. Only one of the two servers can run at a time:

```sh
deploy/mc.sh stop
docker compose -f deploy/minecraft-vanilla/compose.yaml up -d
docker compose -f deploy/minecraft-vanilla/compose.yaml stop
```

## Running the API

The control API (`cmd/api`) starts, stops and checks the game servers over HTTP, so the Discord bot can do it for friends. It listens on `127.0.0.1:8080` only. It never touches the Docker socket directly: it goes through the socket proxy, which allows listing, inspecting, starting and stopping containers and refuses everything else. It asks who's online over RCON.

### First-time setup

1. **Socket proxy.** Start it once; it restarts with Docker:
   ```sh
   docker compose -f deploy/docker-proxy/compose.yaml up -d
   ```
2. **Secrets.** Three untracked `.env` files, each copied from the `.env.example` beside it: the repo root (API token, addresses, RCON password), `deploy/minecraft/` and `deploy/minecraft-vanilla/` (the same RCON password, which Compose passes to the server). This generates a token and a password and writes all three without printing them. Run it once: it overwrites existing `.env` files, and a new RCON password only reaches a server after its container is recreated (step 3):
   ```sh
   bash -c 'pw=$(openssl rand -hex 24); tok=$(openssl rand -hex 32)
   sed "s/^API_TOKEN=$/API_TOKEN=$tok/; s/^MC_RCON_PASSWORD=$/MC_RCON_PASSWORD=$pw/" .env.example > .env
   for d in minecraft minecraft-vanilla; do sed "s/^MC_RCON_PASSWORD=$/MC_RCON_PASSWORD=$pw/" deploy/$d/.env.example > deploy/$d/.env; done'
   ```
3. **Create the containers.** The API starts and stops existing containers; it doesn't create them. After a first install, a `docker compose down`, or a change to a compose file, create them once without starting:
   ```sh
   docker compose -f deploy/minecraft/compose.yaml up --no-start
   docker compose -f deploy/minecraft-vanilla/compose.yaml up --no-start
   ```
   Until then, starting that game returns `409` with this hint.

### Run it

```sh
env $(grep -v '^#' .env | grep .) go run ./cmd/api
```

This works in bash and fish. It logs one JSON line per request to stdout (never the token). Ctrl-C shuts it down gracefully.

### Endpoints

Every endpoint except `/healthz` needs `Authorization: Bearer <API_TOKEN>`. In fish, load the token with `set -x API_TOKEN (grep '^API_TOKEN=' .env | cut -d= -f2)`; in bash, `export API_TOKEN=$(grep '^API_TOKEN=' .env | cut -d= -f2)`.

```sh
curl -s 127.0.0.1:8080/healthz
# {"status":"ok"}

curl -s -H "Authorization: Bearer $API_TOKEN" 127.0.0.1:8080/games
# [{"name":"minecraft","state":"stopped"},{"name":"minecraft-vanilla","state":"stopped"}]

curl -s -H "Authorization: Bearer $API_TOKEN" 127.0.0.1:8080/games/minecraft-vanilla
# {"name":"minecraft-vanilla","state":"running","players":["Pigoenn"]}

curl -s -X POST -H "Authorization: Bearer $API_TOKEN" 127.0.0.1:8080/games/minecraft-vanilla/start
# {"name":"minecraft-vanilla","state":"starting"}   (poll the status until "running")

curl -s -X POST -H "Authorization: Bearer $API_TOKEN" 127.0.0.1:8080/games/minecraft-vanilla/stop
# {"name":"minecraft-vanilla","state":"stopped"}    (waits for the world to save, up to 2 minutes)
```

States are `stopped`, `starting`, `running` and `unhealthy`. `players` is `[]` unless the server is running, and missing if RCON didn't answer.

**Who is asking.** The bot sends two headers with every request: `X-Actor-ID` (the Discord user ID) and `X-Actor-Admin: true` for admins. Both appear in the API's log line for the request, which makes the log the audit trail of who started, stopped or cancelled what. A request without them (like the `curl` above) counts as a non-admin with no ID. The API trusts these headers because only a holder of `API_TOKEN` can send requests at all ([0009](docs/decisions/0009-api-trusts-actor-headers.md)).

**Stopping with people online.** Only one game runs at a time, and nobody gets kicked off by accident:

| Who | Players online | `POST /games/{name}/stop` |
| --- | --- | --- |
| anyone | none | stops now, `200` |
| non-admin | some, or unknown (RCON didn't answer) | `409 {"error":"players are online","players":[...]}` |
| admin | some, or unknown | `202` with `stopping_at`: players are warned in-game at 5 min, 1 min, 30 s and 10 s, then it stops |
| anyone | a countdown is already running | `202` with the same `stopping_at` |

```sh
# As an admin: starts the 5-minute countdown
curl -s -X POST -H "Authorization: Bearer $API_TOKEN" -H "X-Actor-ID: 123456789012345678" -H "X-Actor-Admin: true" \
  127.0.0.1:8080/games/minecraft-vanilla/stop
# {"name":"minecraft-vanilla","state":"running","stopping_at":"2026-11-17T09:05:00Z"}

# Cancel it (admins only), which also says "Shutdown cancelled." in-game
curl -s -X POST -H "Authorization: Bearer $API_TOKEN" -H "X-Actor-Admin: true" \
  127.0.0.1:8080/games/minecraft-vanilla/stop/cancel
# {"name":"minecraft-vanilla","state":"running"}
```

`GET /games` and `GET /games/{name}` include `stopping_at` while a countdown runs. If the API restarts mid-countdown, the countdown is lost and the server keeps running.

Errors are `{"error": "..."}`:
- `401`: bad or missing token.
- `403`: a non-admin tried to cancel a countdown.
- `404`: unknown game.
- `409`: container not created; another game is already running (`"minecraft is already running; stop it first"`); or players are online (above).
- `429`: too many requests (5 a second, bursts of 10).
- `500`: anything else (details in the API's log).

### Tests

```sh
go vet ./... && go test ./...        # unit tests: no Docker or Minecraft needed
env $(grep -v '^#' .env | grep .) go test -tags integration -v ./internal/games/minecraft/
```

The integration test starts and stops the real `minecraft-vanilla` container through the proxy (about 30 seconds). It needs the proxy running and the container created. Stop the modded server first: both use the same ports.

## Running the bot

The Discord bot (`cmd/bot`) lets friends run `/status`, `/start` and `/stop` from Discord. It lives in one Discord server, connects out to Discord (so it needs no open port or tunnel), and calls the control API on localhost for everything. Being in the server isn't enough: only people on its allowed list can use it ([0008](docs/decisions/0008-server-based-bot.md)), and every reply is visible only to the person who asked.

### First-time setup

1. **Create the app.** On the [Discord Developer Portal](https://discord.com/developers/applications), click **New Application**.
2. **Installation.** Tick **Guild Install**, with scopes `applications.commands` and `bot` and no permissions. Leave **User Install** unticked. Under **Bot**, you can turn **Public Bot** off, so only you can add it to a server.
3. **Bot token.** Under **Bot**, click **Reset Token** and put it straight into the repo-root `.env` as `DISCORD_TOKEN`. Leave all three privileged gateway intents off: the bot reads no messages.
4. **Add it to the server.** Open the install link from the **Installation** page and choose the server. In Discord, turn on **Settings → Advanced → Developer Mode**, right-click the server → **Copy Server ID**, and put it in `.env` as `DISCORD_GUILD_ID`.
5. **Who may use it.** Right-click a person → **Copy User ID**, and put IDs in `ALLOWED_IDS` and `ADMIN_IDS` in `.env` (see `.env.example`).

Leaving `DISCORD_GUILD_ID` empty registers the commands globally instead, for a DM with the bot (a user-installed app; tick **User Install** too). That was the original design and still works, but commands then take up to an hour to update.

### Run it

Start the socket proxy and the API first (see [Running the API](#running-the-api)): the bot asks the API which games exist when it starts, and refuses to start if it can't reach it. Then, in a second terminal:

```sh
env $(grep -v '^#' .env | grep .) go run ./cmd/bot
```

It logs `"bot ready"` once the commands are registered, then one JSON line per command or button press, with the Discord user ID. It never logs either token. Ctrl-C disconnects it.

### Commands

- **`/status`**: each game's state, who's online, and "stopping in 3m20s" during a countdown.
- **`/start <game>`**: refused if another game is running. Otherwise the reply says it's starting, then changes to "is up — join at …" once the server is ready (or "Still starting — check with /status." after 14 minutes, Discord's limit for editing a reply).
- **`/stop <game>`**: stops it if nobody is online. With people online:
  - a friend is refused, with the names of who's on;
  - an admin is asked to confirm. On confirm, players are warned in-game and the server stops 5 minutes later. Running `/stop` again during the countdown offers a **Cancel shutdown** button.

The API makes these decisions, not the bot (see the table under [Endpoints](#endpoints)); the bot only chooses which buttons to show. If the API is down, every command answers "The control panel isn't answering right now — tell Jonathan."

Changing the allowed or admin lists means editing `.env` and restarting the bot.
