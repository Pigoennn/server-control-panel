# Milestone 3: Discord bot — Design

- **Date:** 2026-11-16
- **Status:** Draft, awaiting review
- **Milestone:** 3 (see `docs/PLAN.md`)

## Why

Milestone 2 put start, stop and status behind an HTTP API on `127.0.0.1`. Nobody but Jonathan can reach it. Milestone 3 gives friends a way in: a Discord bot they message directly. It is the "resume-ready" milestone (target 6 Dec), the point where the project has real users.

Jonathan's friends come from several different Discord servers, so the bot belongs to no server. Each friend adds it to their own Discord account and talks to it in a direct message. Nothing they do shows up in any channel; Jonathan doesn't want "Xxx started minecraft" posted for everyone.

## Goals

- A Go bot (`cmd/bot`) that friends add to their own Discord account ("Add to my apps") and use in a direct message with the bot.
- Three slash commands: `/status`, `/start <game>`, `/stop <game>`.
- Only Discord users on an allowed list can use any command. Admins are a second list, and are allowed automatically.
- Stopping protects people who are playing:
  - a non-admin can stop a server only when nobody is online;
  - an admin who stops a server with players online confirms first, then the server warns players in-game and stops after 5 minutes. The admin can cancel during the countdown.
- Only one game server runs at a time: the API refuses to start a second one.
- Every start, stop and cancel is logged with the Discord user who asked for it (an audit trail, not metrics).
- The bot is tested without Discord, and the new API rules without Docker.

## Non-goals

- **Usage metrics** (starts, unique players, hours played, uptime, idle hours saved). Moved to milestone 4, alongside SQLite and the dashboard where they'll be shown. `CLAUDE.md` and `docs/PLAN.md` are updated, and a decision record explains the move. Cost, accepted: player and playtime data from milestone 3 is never collected.
- **Managing the allowed and admin lists** from Discord. For now they are lists of user IDs in `.env`; editing them means restarting the bot.
- **The full one-game switch** of milestone 5 (stopping the current game for you and starting the other). Milestone 3 only refuses.
- **Server channels.** The bot joins no server and posts in no channel.
- **Running the bot as a service** (milestone 7). It runs with `go run ./cmd/bot`, like the API.
- **Surviving restarts mid-countdown.** If the API restarts during a countdown, the countdown is lost and the server keeps running. The admin runs `/stop` again.

## Anti-goals (a failure even if it works)

- Someone not on the allowed list starting, stopping or even seeing the status of the server.
- A non-admin kicking playing friends off the server.
- Anything posted where people other than the person who asked can see it.
- The Discord bot token or the API token committed to git.
- Jonathan holding code he can't explain.

## Constraints

- `CLAUDE.md` rules: secrets in untracked `.env`; the API exposes an allowlist of actions; every request authenticated, rate-limited and logged; tests against a fake `Game`.
- The bot connects out to Discord and needs no inbound port or tunnel.
- Go 1.27 (`go.mod`). Keep dependencies few: `discordgo` is the only new one.
- Discord limits:
  - Commands registered for all of Discord ("global") can take up to an hour to update. Commands registered for one server update instantly.
  - A reply to a command can be edited for 15 minutes after the command. After that the bot can't reach the user.
  - A bot can't start a direct message with a user who shares no server with it. So every message the bot sends is a reply, or an edit of a reply, to a command the user ran.
- The modded server takes a few minutes to boot and up to 2 minutes to stop. The vanilla server boots in about 25 seconds.

## Who writes what

Same split as milestone 2. Claude writes skeletons (types, signatures, `// TODO(jonathan):` comments), the tests, the `discordgo` wiring (connecting, registering commands, turning Discord interactions into calls on the bot's logic), config, `.env.example`, README and draft decision records. Jonathan writes the core logic: the new API rules (stop permissions, countdown, cancel, one game at a time) and the bot's command handlers. Claude reviews. Jonathan runs every `git commit`.

## Approaches considered

### Connecting to Discord

| | Approach | Trade-off |
|---|---|---|
| **A (chosen)** | Gateway (a connection the bot holds open to Discord) with `discordgo` | No inbound port or tunnel, as `docs/PLAN.md` always planned. The most-used Go Discord library, so help is easy to find. Loosely typed. |
| B | Gateway with `disgo` | Stricter types and first-class user-installed apps, but a smaller community. No benefit big enough. |
| C | Discord sends commands to an HTTP endpoint | Simple to test, but needs a public URL: the Cloudflare Tunnel would move forward from milestone 4. |

### Who may use the bot

Because the bot is in no server, "being in the friends' server" can't be the permission: anyone with the install link can add it. Chosen: an allowed list of user IDs, checked before every command, with admins allowed automatically.

### Where the stop countdown lives

| | Approach | Trade-off |
|---|---|---|
| **A (chosen)** | In the API | The API already owns stopping, so the dashboard (milestone 4) gets the same rules and countdown for free. The API has to trust the bot about who is an admin (below). |
| B | In the bot | The API stays unchanged, but every client would have to re-implement the rules, and a non-admin rule enforced only in the bot is bypassed by any other client. |

### Telling the API who is asking

Chosen: two request headers on every API call from the bot, `X-Actor-ID` (the Discord user ID) and `X-Actor-Admin` (`true` or absent). The API trusts them because only a holder of the API token can send requests at all, and today that is only the bot (and Jonathan's `curl`). The headers are logged on every request, which makes the request log the audit trail. Milestone 4's dashboard will check admin status itself through Discord login.

## Design

### Layout

```
cmd/bot/main.go             config, connect to Discord, register commands, run
cmd/bot/config.go           config from env
internal/bot/bot.go         command logic: allowlist check, /status, /start, /stop, buttons
internal/bot/apiclient.go   HTTP client for the control API
internal/bot/discord.go     discordgo glue (thin: no logic worth testing)
internal/api/...            new stop rules, countdown, cancel, one-game check, actor headers
internal/games/game.go      optional Announcer interface
```

### Bot configuration (`.env`, repo root, shared with the API)

| Variable | Meaning |
|---|---|
| `DISCORD_TOKEN` | Bot token. Required. |
| `DISCORD_DEV_GUILD_ID` | If set, commands are registered to this private test server only (instant updates). If unset, they are registered globally. |
| `ALLOWED_IDS` | Comma-separated Discord user IDs who may use the bot. |
| `ADMIN_IDS` | Comma-separated Discord user IDs who are admins. Allowed automatically. |
| `API_URL` | Default `http://127.0.0.1:8080`. |
| `API_TOKEN` | The same token the API uses. Required. |

The bot refuses to start if a required variable is missing, if an ID isn't a number, or if both lists are empty.

### Installing and registering

- The Discord application allows **user install** only. Its commands are registered with install type "user" and context "bot DM", so they appear only in a direct message with the bot.
- Development uses a private test server with only Jonathan and the bot in it, via `DISCORD_DEV_GUILD_ID`. Friends never see it.
- `/start` and `/stop` take a `game` option with fixed choices. The bot fetches them from `GET /games` at startup, so it needs the API running first, and says so if it isn't.

### Commands

The allowlist check runs first for every command and button press. Someone not on the list gets "You're not on the list for this bot — ask Jonathan." and the refusal is logged with their user ID.

**`/status`:** each game's state, and who is online when it's running. Shows "stopping in 3m 20s" during a countdown.

**`/start <game>`:**
1. The bot calls `POST /games/{game}/start`.
2. It replies "Starting minecraft, usually ready in a few minutes."
3. It polls `GET /games/{game}` every 5 seconds and edits that reply to "minecraft is up — join at `scooter-swizzle.tun.ply.gg`" once the server is running.
4. If the server isn't running after 14 minutes (just inside Discord's 15-minute edit window), the reply becomes "Still starting — check with /status."
5. Refusals are shown as given: another game running (`409`), container not created (`409`).

**`/stop <game>`:**
- **Non-admin:** the bot calls `POST /games/{game}/stop`.
  - With nobody online, the API stops the server, and the reply says "minecraft stopped."
  - With players online, the API refuses with `409`, and the bot replies "Can't stop: Pigoenn and 2 others are online. Ask an admin."
- **Admin, nobody online:** stops straight away, as for a non-admin.
- **Admin, players online:**
  1. The bot replies with the player list and Confirm and Cancel buttons. Only that admin sees them, since it's their direct message.
  2. On Confirm, the bot calls `POST /games/{game}/stop` as an admin. The API answers `202` and starts the countdown.
  3. The reply becomes "minecraft stops in 5 minutes. Run /stop again to cancel." Later it is edited to "minecraft stopped." All of this happens inside the 15-minute window.
- **Admin, countdown already running:** the bot offers a Cancel-shutdown button. It calls `POST /games/{game}/stop/cancel`.

The bot finds out which case applies by calling `GET /games/{game}` first. The API makes the real decision; the bot's first look only chooses which buttons to show.

### API changes

**Actor headers.** A middleware reads `X-Actor-ID` and `X-Actor-Admin` into the request context, and `logRequests` adds both to each request's log line. Requests without them (Jonathan's `curl`) count as a non-admin with no ID.

**`POST /games/{name}/start`:** if any other game is not `stopped`, it refuses with `409 {"error":"minecraft is already running; stop it first"}`. A mutex around "check the others, then start" stops two simultaneous starts from both getting through.

**`POST /games/{name}/stop`:**

| Who | Players online | Result |
|---|---|---|
| anyone | none | stops now, `200` (as today) |
| non-admin | some, or unknown (RCON didn't answer) | `409 {"error":"players are online","players":[...]}` |
| admin | some, or unknown | starts the 5-minute countdown, `202 {"name":..., "state":..., "stopping_at":"<RFC 3339 time>"}` |
| anyone | a countdown is already running | `202` with the same `stopping_at` (no second countdown) |

"Unknown" counts as "some" for non-admins: when the API can't tell, it protects the players.

**`POST /games/{name}/stop/cancel`:** admin only. Non-admins get `403`. It cancels a running countdown and announces "Shutdown cancelled" in-game, returning `200`. With no countdown running it is a no-op, also `200`.

**`GET /games` and `GET /games/{name}`:** gain `stopping_at` while a countdown runs.

**The countdown.** It runs as one goroutine per game, started by the stop handler, with a cancel function the cancel handler calls. Its context is not tied to the request.
- In-game warnings go out at 5 min, 1 min, 30 s and 10 s: "Server stopping in 5 minutes (requested by an admin)."
- Then it calls `Stop`.
- The rules apply when the countdown starts. If everyone leaves early, it still runs to the end.
- If a warning fails to send (RCON down), that's logged, and the countdown carries on.

**Announcer.** Warnings need RCON `say`, which not every future game may support. So it is an optional interface in `internal/games`:

```go
type Announcer interface {
    Announce(ctx context.Context, message string) error
}
```

The Minecraft adapter implements it. The API checks for it with a type assertion (`if a, ok := g.(games.Announcer); ok`) and skips the warnings for games without it. The `Game` interface itself is unchanged.

### Testing

- **API:** table tests against `FakeGame` (extended to record announcements and to implement `Announcer`):
  - the stop table above;
  - cancel for an admin and a non-admin;
  - a start refused while another game runs;
  - simultaneous starts;
  - the actor headers appearing in the log.
- **Countdown timing:** the countdown takes its durations and a clock from config, so tests run it in milliseconds.
- **Bot:** the command logic in `internal/bot` depends on an `APIClient` interface, not on `discordgo`. Tests use a fake API client and check the reply for each case:
  - not on the list;
  - each `/stop` branch;
  - `/start` reaching running, and timing out;
  - every API error.
  `discord.go` stays thin enough that a manual test covers it.
- **Manual end-to-end:** in the private test server, run every command once as an admin and once as a non-admin (a second Discord account, or temporarily removing yourself from `ADMIN_IDS`), with someone on the vanilla server.

### Decision records

- **0007** Metrics move to milestone 4.
- **0008** User-installed bot with an allowed list, not a server bot.
- **0009** The API trusts actor headers from the bot, and the stop rules live in the API.

## Left to the builder

- Exact reply wording beyond what is quoted above.
- How the bot formats `/status` (plain text or an embed).
- Polling details within the limits above.
