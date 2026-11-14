# 0003: playit.gg as its own always-on Compose project

- **Date:** 2026-11-14
- **Status:** Accepted

## Context

My home internet is behind CGNAT, so friends can't connect directly and game traffic goes through a playit.gg tunnel. Only one game server runs at a time, and the plan is to switch between Minecraft and Hytale (and maybe Palworld later), so the tunnel has to work for whichever game is running.

## Decision

The playit agent runs as its own Compose project (`deploy/playit/`) with `restart: unless-stopped`, so it's always on. Each game is a separate Compose project with `restart: "no"`, only started on purpose. The agent uses host networking and forwards to `127.0.0.1:25565`, where the game publishes its port, localhost only.

## Alternatives

One Compose file with both Minecraft and playit: the simplest to begin with, but `docker compose down` would kill the tunnel along with the game, and the file would have to be split anyway once a second game comes along.

Installing playit on the host as a system service: one less container, but it installs differently on CachyOS (dev machine) and Ubuntu (laptop), and it's harder to rebuild with Ansible later. Keeping everything in containers means both machines run the same thing.

## Consequences

Switching games doesn't touch the tunnel, and each game being one Compose project is the same unit the milestone 2 API will start and stop. A new game only needs a tunnel added on the playit dashboard.

The cost is two projects to manage instead of one, and the agent always uses a little memory even when no game is running. playit is also temporary: milestone 8 replaces it with my own relay (Oracle Cloud VM and WireGuard), and keeping it separate makes that swap easy.
