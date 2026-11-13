# Home Server Control Panel — Project Plan

Exported from the planning doc on 11 Nov 2026. The live version is https://claude.ai/artifact/AnnCyGJBGkX31DEEtZo2Qc

A self-hosted platform that lets friends start, stop and check the home game servers (Minecraft, Hytale, possibly Palworld) from Discord or a web dashboard, with backups, monitoring and CI/CD behind it.

## What it does (v1)

Friends can, from Discord or a web page:

- start and stop any of the game servers
- see whether it's up and who's playing
- trigger a backup

The admin also gets resource monitoring, alerts and restores. Anything beyond that waits for v2.

## Architecture

- **Game servers in Docker.** Containers give the controller a clean API (start, stop, logs, stats) and make the setup reproducible. Each game gets its own Compose service and volume.
- **Control API in Go.** A small service that talks to the Docker Engine API through the official Go SDK.
- **Discord bot.** Also in Go (e.g. discordgo). Slash commands like `/start minecraft`, `/status`, `/backup`. It calls the API rather than touching Docker itself.
- **Web dashboard.** React + TypeScript, with Discord OAuth login. Roles: friends can start/stop; only the admin can restore or delete.
- **Storage.** SQLite: users, roles, an audit log ("who started it at 2am") and backup records.
- **Remote access.** The dashboard and API go through a Cloudflare Tunnel: no open router ports, free TLS, and nothing for friends to install. The Discord bot connects out to Discord, so it needs no inbound access.
- **Game traffic.** See Networking.
- **Monitoring.** Prometheus + Grafana (or just Docker stats to start), with alerts posted to a Discord channel.

## Multi-game design

Each game is a plug-in behind one Go interface. Adding a game means writing one adapter, without touching the API, bot or dashboard.

```go
type Game interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Status(ctx context.Context) (Status, error)
    Players(ctx context.Context) ([]Player, error)
    Backup(ctx context.Context, dest string) error
}
```

| Game | Running it | Player count / control | Priority |
| --- | --- | --- | --- |
| Minecraft | itzg/minecraft-server Docker image | RCON or the server query protocol | First: build everything against this |
| Hytale | Check the official dedicated-server tooling and Linux support | Depends on what the server exposes | Second |
| Palworld | Linux dedicated server via SteamCMD; community Docker images exist | RCON or its REST API | Deferred: needs 8GB+ RAM on its own |

**One server at a time.** The laptop has 8GB RAM. After Ubuntu Server and Docker (~1GB), the active game gets roughly 6GB. The API enforces a single running game: switching games stops the current one first, with a clear message in Discord.

- Minecraft fits comfortably (3–4GB for 10 players).
- Hytale uses 4–8GB. Capped at about 6GB it is workable for 2–4 players on vanilla.
- Palworld waits for better hardware.

## Networking

The home connection is behind CGNAT, so port forwarding won't work, and there will be no paid ISP changes.

- **Game traffic:** playit.gg first. Later, an Oracle Cloud Always Free VM (Sydney/Melbourne) running WireGuard as a self-run relay.
- **Dashboard/API:** Cloudflare Tunnel.
- **Fallback:** Tailscale, if every friend is happy to install it.

| Option | How it works | Friends install anything? | Notes |
| --- | --- | --- | --- |
| playit.gg | Agent on the laptop connects out to playit's relays | No | Free tier, TCP + UDP; third-party relay |
| Tailscale | Friends join the tailnet or get the laptop shared with them | Yes | Usually peer-to-peer, lowest latency |
| Free VPS + WireGuard | VM holds a public IP and forwards game ports over WireGuard | No | Most setup; best resume story |
| Cloudflare Tunnel | Used for the dashboard | Would need cloudflared on every client | Not for game traffic on the free plan |

## Host

Ubuntu Server LTS on the laptop (Intel Core i7 7th gen, 8GB RAM), possibly replaced by a better laptop later.

- Ignore the lid switch so the laptop doesn't suspend.
- Keep it ventilated.
- Cap battery charge at around 80% if supported.

**Why not Pterodactyl or Crafty?** Built to learn Go and infrastructure, Discord-first, auto-shutdown, and designed around constrained hardware.

## Security

Docker socket access is effectively root on the machine. To lock it down:

- The API only exposes an allowlist of actions.
- It runs as a limited user, or behind a socket proxy.
- Every request is authenticated, rate-limited and logged.

## Project setup

- **Repo:** one public repo on the personal GitHub account (Pigoennn), using the layout in `CLAUDE.md`.
- **Secrets:** kept in an untracked `.env` (or encrypted with sops), never in the repo. CI runs a secret scanner.
- **Testing:** a fake `Game` adapter for unit tests; a small integration suite against a real container in CI.
- **Decision log:** a short `docs/decisions/` file per big choice.
- **Metrics:** server starts, unique players, hours played, uptime, idle hours saved.
- **README:** an architecture diagram, a GIF of the bot and dashboard, and a "why not Pterodactyl" paragraph.

## Milestones

0. **Setup.** Harden the laptop (SSH keys only, ufw, automatic security updates), install Docker, create the repo, and work through the Tour of Go.
1. **Containerise Minecraft.** Docker Compose with a persistent volume, plus a tiny script that starts it, stops it and reports status. A friend joins through playit.gg.
2. **Go API.** The `Game` interface, the Minecraft adapter, and start/stop/status as authenticated endpoints, with tests.
3. **Discord bot.** Friends start using it. Start logging usage metrics.
4. **Dashboard + public access.** Discord login, roles, live status, player count and logs, served through Cloudflare Tunnel.
5. **More games.** The Hytale adapter and the one-game-at-a-time switch.
6. **Backups.** Scheduled snapshots per game, retention rules and an off-machine copy, with a tested restore.
7. **Ops polish.** GitHub Actions CI, automated deploys, Ansible to rebuild the box from scratch, and alerts.
8. **Own relay.** Replace playit.gg with an Oracle Cloud VM forwarding game ports over WireGuard, set up with the same Ansible.

## Timeline

About 18 hours a week (3 hours a day, Sundays off).

| Target (week ending) | Milestones | Hours (approx.) |
| --- | --- | --- |
| Sun 15 Nov | 0: setup, start Tour of Go | 9 |
| Sun 22 Nov | 0 finished, 1: Minecraft in Docker + playit.gg, start 2 | 18 |
| Sun 29 Nov | 2: Go API + Minecraft adapter + tests | 18 |
| **Sun 6 Dec** | **3: Discord bot, friends using it (resume-ready)** | 18 |
| Sun 20 Dec | 4: dashboard + Discord login + Cloudflare Tunnel | 36 |
| Sun 10 Jan | 5: Hytale adapter; 6: backups | 30 |
| Sun 24 Jan | 7: CI/CD, Ansible, alerts | 30 |
| Sun 7 Feb | 8: own Oracle + WireGuard relay | 25 |

**Stretch goals:** auto-shutdown after 30 idle minutes, a Minecraft mod/plugin manager, and media-server stats on the dashboard.

## Resume line to aim for

> Built and operate a self-hosted game-server control platform in Go (Docker, Discord OAuth, Cloudflare Tunnel) running Minecraft and Hytale servers for N users. Automated backups, monitoring and CI/CD deploys, and cut idle power use X% with auto-shutdown.

## Open questions

1. Does playit.gg's free tier handle Hytale's network traffic well? Test during milestone 5.
