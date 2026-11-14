# Milestone 1: Containerise Minecraft — Design

- **Date:** 2026-11-14
- **Status:** Draft, awaiting review
- **Milestone:** 1 (see `docs/PLAN.md`)

## Why

Jonathan and about 10 friends play a modded Java Minecraft world. Milestone 1 runs that server in Docker so it can be started, stopped and checked with one command, and so friends can join from outside a CGNAT home connection. It is also the groundwork for milestone 2: the script's `start` / `stop` / `status` are a rough draft of the Minecraft adapter behind the Go `Game` interface.

## Goals

- The server runs Jonathan's **custom CurseForge modpack**, so players' game clients and the server have the same mods.
- The world survives container restarts and re-creation (persistent volume).
- One script starts, stops and reports the server's status, including who is online.
- A friend outside the home network joins through **playit.gg**.
- The existing friends' world is moved onto the server before friends start playing.
- Build and test on the dev machine (CachyOS, 31GB RAM). The same files run unchanged on the laptop (Ubuntu Server) later.

## Non-goals

- Fitting on the laptop's 8GB. Memory is configurable, but tuning it for the laptop comes later (Jonathan's call).
- The Go API, Discord bot, backups and monitoring (milestones 2–7).
- Multiple worlds or mod sets. Only one world for now. The "other worlds with other modpacks" idea comes back at milestone 2's `Game` design (see Notes for milestone 2).
- Paper/Bukkit plugins. Mod loaders and plugin servers don't mix reliably. Plugin-style features come from server-side mods.

## Anti-goals (would make this a failure even if it works)

- Committing a secret (the CurseForge API key or the playit secret key).
- An open server that anyone who finds the playit address can join.
- Losing or corrupting the real world during setup.

## Constraints

- `CLAUDE.md` rules apply. Secrets live in untracked `.env` files. Docker socket access is root-equivalent. Only one game server runs at a time.
- Home connection is behind CGNAT, so there are no inbound ports. playit.gg's agent connects outbound.
- The laptop will be Ubuntu and the dev machine is CachyOS. Anything installed on the host would differ between the two, so everything here runs in containers.

## Approaches considered

| | Approach | Trade-off |
|---|---|---|
| **A (chosen)** | Separate Compose projects for Minecraft and playit, plus a bash script | Two files to manage. The tunnel stays up while games come and go. Each game is one Compose project, the same unit the milestone 2 API will control. |
| B | One Compose file with both services | Simplest now. `docker compose down` would kill the tunnel too, and the file must be split once a second game arrives. |
| C | playit installed on the host as a system service | Fewer containers. Installed differently on CachyOS vs Ubuntu, and harder to rebuild with Ansible. |

Writing the script in Go was dropped. That is milestone 2, and the bash version is where Jonathan learns what start/stop/status really mean for this server.

## Design

### Layout

```
deploy/
  minecraft/
    compose.yaml
    .env.example          committed: variable names, no values
    .env                  untracked: real CurseForge key
    modpack/pack.zip      the CurseForge export (manifest + overrides)
    downloads/            mods CurseForge blocks from automatic download (if any); gitignored
  playit/
    compose.yaml
    .env.example
    .env                  untracked: playit secret key
  mc.sh                   start | stop | status
```

The existing `.gitignore` already ignores `.env` at any depth and keeps `.env.example`. Add `deploy/minecraft/downloads/` to it, because those are mod jars whose authors forbid redistribution, and this is a public repo. Remove `deploy/.gitkeep` once real files land.

### Minecraft service (`deploy/minecraft/compose.yaml`)

- **Image:** `itzg/minecraft-server`, pinned to a **Java tag that matches the pack's Minecraft version** (e.g. `java21` for 1.20.5 and later, `java17` for 1.18–1.20.4, `java8` for older Forge packs). The builder reads the version from the pack's `manifest.json`. Never use `latest`, because an image update could change Java under a working pack.
- **Names:** top-level `name: minecraft` (Compose project name) and `container_name: minecraft`, so the project, container and volume names are stable whatever folder Compose runs from. The script and later the API refer to them by name.
- **Pack install:** `TYPE=AUTO_CURSEFORGE`, `CF_MODPACK_ZIP` pointing at the mounted `modpack/pack.zip` (mounted read-only). The image installs the right loader (Forge, NeoForge or Fabric) and downloads every mod.
- **CurseForge key:** `CF_API_KEY` comes from `deploy/minecraft/.env`. CurseForge keys contain `$` characters, which Compose treats as variable references. In `.env` every `$` must be written as `$$`. Put this warning in `.env.example`.
- **Blocked mods:** mount `./downloads` to the image's downloads folder. If startup logs list mods that can't be downloaded automatically, Jonathan downloads those files by hand into that folder and restarts.
- **Client-only mods:** the image skips well-known client-only mods by itself. Any others that crash the server go into `CF_EXCLUDE_MODS` (comma-separated), with a comment saying why.
- **World data:** named volume `minecraft-data` mounted at `/data`. A named volume, not a bind mount, keeps world files owned by the container's user and away from the repo.
- **Memory:** one `MEMORY` setting (Java heap), starting at `6G` on the dev machine, plus a container memory limit about 1GB above it, since Java needs room beyond its heap. These are the only two numbers to change for the laptop later. Put a comment next to them saying so.
- **Access:** `EULA=TRUE`, `ENABLE_WHITELIST=TRUE`, `ENFORCE_WHITELIST=TRUE`, `WHITELIST` and `OPS` listing Minecraft usernames (Jonathan as op). Usernames aren't secret, so they live in `compose.yaml`. Jonathan supplies the list.
- **Port:** publish `127.0.0.1:25565:25565` (TCP, localhost only). The playit agent on the host network reaches it, and so does Jonathan's own client on the dev machine. Binding to localhost matters because Docker-published ports bypass `ufw` on the laptop: a plain `25565:25565` would quietly open the port to the whole LAN despite the firewall. Everyone outside this machine joins through playit.
- **Health check timing:** override the image's health check `start_period` to about `5m`. A modpack can take several minutes to boot, and with the default the container would report `unhealthy` while it is still loading.
- **Shutdown:** `stop_grace_period: 2m`. A modded world can take a while to save, and Docker's default 10 seconds would kill it mid-save.
- **Console:** `stdin_open: true` and `tty: true`, so `docker attach minecraft` gives a live server console when debugging.
- **Restart policy:** `restart: "no"`. Starting and stopping are deliberate, done by the script now and by the API later. A server that restarts itself would fight the "one game at a time" rule.
- **RCON:** leave the image's built-in RCON on (its default). It is reachable only inside the container (do not publish its port). The script uses it through `rcon-cli`.

### playit service (`deploy/playit/compose.yaml`)

- **Image:** playit.gg's official agent image (`ghcr.io/playit-cloud/playit-agent`), pinned to a version tag.
- **Network:** `network_mode: host`, so the tunnel targets `127.0.0.1:25565` and works the same for any future game's port.
- **Secret:** `SECRET_KEY` from `deploy/playit/.env`.
- **Restart policy:** `restart: unless-stopped`. The tunnel is infrastructure and should always be up, unlike the game.
- **One-time setup (manual, on playit.gg's website):** claim the agent, then create a "Minecraft Java" tunnel pointing to local port 25565. The public address it gives is what friends type in. Record it in the README section below, but not the secret.

### Script (`deploy/mc.sh`)

Bash, with `set -euo pipefail`. It finds the Compose file relative to its own location, so it works from any directory. Three commands, matching the future `Game` methods:

| Command | Does | Prints |
|---|---|---|
| `start` | `docker compose up -d` for the Minecraft project | that it's starting, and to check `status` (a modpack boot can take minutes) |
| `stop` | `docker compose stop` (not `down`, which removes the container; the volume would survive either way) | when it has stopped |
| `status` | reads the container's state and health with `docker inspect`. When healthy, runs `docker exec minecraft rcon-cli list` | exactly one of the states below |

**Status states:**
- `stopped`: no container, or the container isn't running.
- `starting`: running, health is `starting`. The image's built-in health check only reports healthy once players can actually join.
- `running`: healthy, followed by the player count and names.
- `unhealthy`: running but the health check is failing. Suggest `docker compose logs`.

These four states are the start of milestone 2's `Status` type.

**Errors:** an unknown command or a missing argument prints usage and exits 1. Docker being unreachable produces Docker's own error and a non-zero exit, with no extra handling.

### Moving the real world in (last step of the milestone)

1. Confirm the pack's mod versions match what the world was last played with.
2. Keep an untouched copy of the original save outside the repo.
3. `mc.sh stop`. Copy the save folder's contents into the volume's world folder (`/data/world`, replacing the fresh one) with `docker cp`, or with a throwaway container that mounts `minecraft-data`. Forge, NeoForge and Fabric use the vanilla layout, so a single-player or LAN save's folder copies across as-is.
4. `mc.sh start`, join, and walk around the existing builds before telling friends.

This is a rehearsal for milestone 6's restore.

### Decision records (Jonathan writes these, 5-minute template)

- `0002-custom-curseforge-pack.md`: mod loader through a custom CurseForge pack, not Paper plugins or a hybrid server.
- `0003-playit-separate-compose.md`: playit as its own always-on container, separate from game projects (approach A).

### README

A short "Running the Minecraft server" section in `README.md` covers the setup steps: get a CurseForge API key, fill in both `.env` files, export the pack to `deploy/minecraft/modpack/pack.zip`, claim the playit agent, and use `mc.sh`.

## Testing (manual, there's no Go code in this milestone)

1. `mc.sh status` prints `stopped` before anything runs.
2. `mc.sh start`, then `status` shows `starting`, then `running` with 0 players.
3. Jonathan joins on `localhost` with his own client, and `status` lists him.
4. `mc.sh stop`, then `start` again: the world (a placed block, say) is still there.
5. `docker compose down` then `start`: the world is still there (the volume outlived the container).
6. A non-whitelisted account is refused.
7. A friend outside the home network joins through the playit address.
8. After moving the real world in, existing builds are present and the mods load without missing-block warnings.
9. `git status` shows no `.env` files and `git check-ignore` confirms both are ignored.

**Done when:** steps 1–9 pass on the dev machine, the decision records and README section are written, and the changes are committed.

## Who builds what

Following `CLAUDE.md`:
- **Claude:** the Compose files, `.env.example` files and README section, explaining each setting as it goes.
- **Jonathan:** `mc.sh`, with review from Claude. It is the rough draft of his Minecraft adapter. Also the decision records, the pack export, the playit claim and the world move.

## Notes for milestone 2 (not built now)

- "Several worlds with different modpacks" means one game with several profiles. The `Game` interface or its config will need a notion of which world or profile to start.
- The four status states above are a starting point for the Go `Status` type.
- The API will start and stop the `minecraft` Compose project through the Docker API, which is why the container name is fixed and the restart policy is `no`.

## Left to the builder

- Exact image tags (the Java tag that matches the pack, plus the current playit agent version).
- The image's exact mount path for manually downloaded mods, from the itzg docs.
- Exact wording of script output.
