# Home Server Control Panel

A self-hosted platform that lets Jonathan's friends start, stop and check his home game servers (Minecraft first, then Hytale) from Discord or a web dashboard, with backups, monitoring and CI/CD behind it. It is a resume project for graduate software engineering / Linux engineer roles, and it has real users (about 10 friends).

The full plan is in `docs/PLAN.md`. The current milestone checklist is in `docs/MILESTONE-0.md`. Read both before starting work.

## Working with Jonathan

- Jonathan is learning Go with this project. Explain Go idioms and design choices as you go, and let him write the core logic (the `Game` interface, adapters, API handlers) himself. Help with planning, reviews, debugging, boilerplate and tests.
- He must be able to explain every design decision in an interview. When a non-obvious choice comes up, write it down as a short decision record in `docs/decisions/`.
- Work milestone by milestone (see `docs/PLAN.md`). Don't jump ahead to later milestones unless asked.
- Commit in small, clear steps.

## Fixed decisions

- **Backend:** Go (API + Discord bot). **Dashboard:** React + TypeScript.
- **Games:** Minecraft (itzg/minecraft-server Docker image) first, Hytale second. Palworld deferred until better hardware, but it must stay one adapter away.
- **Only one game server runs at a time.** Switching games stops the current one first.
- **Host:** a laptop running Ubuntu Server LTS, 8GB RAM, Intel Core i7 7th gen. About 6GB is available for the active game. Hytale is capped at about 6GB (workable for 2–4 vanilla players).
- **Networking:** the home connection is behind CGNAT and will stay that way (no paid ISP changes).
  - Game traffic: playit.gg now, a self-run Oracle Cloud Always Free VM + WireGuard relay later (milestone 8).
  - Dashboard/API: Cloudflare Tunnel.
  - The Discord bot connects outbound, so it needs no tunnel.
- **Storage:** SQLite.
- **Auth:** Discord OAuth. Friends can start/stop; only the admin can restore or delete.

## Repo layout (planned)

```
cmd/api/          control API
cmd/bot/          Discord bot
internal/games/   Game interface + one adapter per game
internal/docker/  Docker Engine API wrapper
web/              React + TypeScript dashboard
deploy/           Docker Compose files, Ansible
docs/             plan, milestones, decisions/
```

## Rules

- **Never commit secrets.** The Discord bot token, OAuth secret and playit key go in an untracked `.env` (listed in `.gitignore`).
- **The API only exposes an allowlist of actions.** Docker socket access is root-equivalent: run as a limited user or behind a socket proxy, and authenticate, rate-limit and log every request.
- **Test against a fake `Game` adapter,** so the API and bot can be tested without Docker.
- **Log usage metrics from milestone 4 onwards:** starts, unique players, hours played, uptime, and idle hours saved (moved from milestone 3; see `docs/decisions/0007-metrics-in-milestone-4.md`).
