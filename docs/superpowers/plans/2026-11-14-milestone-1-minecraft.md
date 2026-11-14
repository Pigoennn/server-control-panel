# Milestone 1: Containerise Minecraft Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Jonathan's custom CurseForge modpack runs in Docker, a script starts, stops and checks it, and a friend joins through playit.gg.

**Architecture:** Two separate Compose projects: `deploy/minecraft/` (the game, stopped and started on purpose) and `deploy/playit/` (the tunnel, always on, host networking). A bash script `deploy/mc.sh` wraps `docker compose` and `docker inspect` for start, stop and status.

**Tech Stack:** Docker Compose, `itzg/minecraft-server` (`TYPE=AUTO_CURSEFORGE`), playit.gg agent image, bash.

**Spec:** `docs/superpowers/specs/2026-11-14-milestone-1-minecraft-design.md`

**Who does what:** tasks marked **[Claude]** are written by Claude, with each setting explained. Tasks marked **[Jonathan]** are written by Jonathan, with Claude reviewing. Jonathan runs every `git commit` himself, from the repo root, on branch `milestone-1-minecraft`.

## Global Constraints

- No secrets in git. The CurseForge key and playit key live only in `deploy/*/.env` (already ignored). `.env.example` files hold names only.
- Every `$` in the CurseForge key is written `$$` in `deploy/minecraft/.env`.
- Image tags are pinned. Never `latest`.
- Minecraft port is published as `127.0.0.1:25565:25565` only.
- Minecraft `restart: "no"`. playit `restart: unless-stopped`.
- Fixed names: Compose project `minecraft`, container `minecraft`, volume `minecraft-data`.
- `MEMORY=6G`, container memory limit `7g`, both with a "shrink for the laptop" comment.
- `stop_grace_period: 2m`. Health check `start_period: 5m`.
- `deploy/minecraft/downloads/` is never committed (public repo, redistribution forbidden).
- Testing is manual. There is no Go code in this milestone.

## Review Focus

1. **Unescaped `$` in the CurseForge key:** Compose silently mangles it and downloads fail with a vague 403. Pinned in Task 2, Step 4.
2. **Port open to the LAN:** a plain `25565:25565` bypasses `ufw`. Pinned in Task 2, Step 6 (`ss` shows only `127.0.0.1`).
3. **World cut off mid-save on stop:** without the grace period Docker kills Java after 10s. Pinned in Task 4, Step 3 (exit code is not 137).
4. **`mc.sh` run from another folder, with no argument or a wrong one:** it should still find its Compose file, or print usage and exit 1. Pinned in Task 3, Step 1.
5. **`mc.sh start` while already running, and Docker restarting:** start should be harmless, and after a Docker restart playit returns while Minecraft stays off. Pinned in Task 3, Step 1 and Task 4, Step 4.

---

## File Map

| File | Responsibility | Task |
|---|---|---|
| `.gitignore` | add `deploy/minecraft/downloads/` (`.claude/` already added) | 1 |
| `deploy/playit/compose.yaml`, `deploy/playit/.env.example` | the tunnel | 1 |
| `deploy/minecraft/compose.yaml`, `deploy/minecraft/.env.example` | the game | 2 |
| `deploy/minecraft/modpack/pack.zip` | Jonathan's CurseForge export | 2 |
| `deploy/mc.sh` | start / stop / status | 3 |
| `README.md` | "Running the Minecraft server" section | 6 |
| `docs/decisions/0002-custom-curseforge-pack.md`, `0003-playit-separate-compose.md` | decision records | 6 |
| `deploy/.gitkeep` | delete (Task 1) | 1 |

---

### Task 0: Commit the spec and plan [Jonathan]

- [ ] **Step 1: Commit**

```bash
git add docs/superpowers .gitignore
git commit -m "Add milestone 1 design and implementation plan; ignore .claude/"
```

---

### Task 1: Housekeeping and the playit tunnel [Claude writes, Jonathan claims the agent]

**Files:**
- Modify: `.gitignore`
- Delete: `deploy/.gitkeep`
- Create: `deploy/playit/compose.yaml`, `deploy/playit/.env.example`

**Interfaces:**
- Produces: a running container `playit` on the host network, tunnelling to `127.0.0.1:25565` once a tunnel is created in Task 5.

- [ ] **Step 1: Add the ignore rule and check it**

Append `deploy/minecraft/downloads/` (with a comment) to `.gitignore`.
Run: `git check-ignore -v --no-index deploy/minecraft/downloads/x.jar deploy/playit/.env .claude/x; git check-ignore --no-index deploy/playit/.env.example || echo "example tracked"`
Expected: the first three each print a matching rule, then `example tracked`.

- [ ] **Step 2: Write `deploy/playit/compose.yaml` and `.env.example`**

Service `playit`: image `ghcr.io/playit-cloud/playit-agent:<current version tag>`, `network_mode: host`, `restart: unless-stopped`, `environment: SECRET_KEY: ${SECRET_KEY}`. `.env.example` contains `SECRET_KEY=` plus a comment on where to get it. Delete `deploy/.gitkeep`.

- [ ] **Step 3: Jonathan gets the secret key**

Follow playit.gg's Docker setup to create an agent and get its secret key. Put it in `deploy/playit/.env`.

- [ ] **Step 4: Verify the agent connects**

Run: `cd deploy/playit && docker compose up -d && docker compose logs --tail 20`
Expected: logs show the agent connected/authenticated, and the agent shows as online on the playit.gg dashboard.

- [ ] **Step 5: Commit**

```bash
git add .gitignore deploy/
git status   # confirm deploy/playit/.env is NOT listed
git commit -m "feat(playit): add playit.gg tunnel as its own Compose project"
```

---

### Task 2: Minecraft Compose project with the custom pack [Claude writes compose, Jonathan supplies pack + key]

**Files:**
- Create: `deploy/minecraft/compose.yaml`, `deploy/minecraft/.env.example`, `deploy/minecraft/modpack/pack.zip`, `deploy/minecraft/downloads/` (empty, ignored)

**Interfaces:**
- Produces: project `minecraft`, container `minecraft`, volume `minecraft-data` (set with `volumes: minecraft-data: name: minecraft-data` so it isn't prefixed), health states `starting` / `healthy` / `unhealthy`, `rcon-cli` inside the container. Task 3 relies on all of these.

- [ ] **Step 1: Jonathan exports the pack and gets the key**

Export the pack from the CurseForge app ("Export profile", CurseForge format) to `deploy/minecraft/modpack/pack.zip`. Get an API key from the CurseForge developer console and put it in `deploy/minecraft/.env` as `CF_API_KEY=...` with every `$` doubled to `$$`. Tell Claude the pack's Minecraft version (from `manifest.json`) and the whitelist usernames.

- [ ] **Step 2: Write `deploy/minecraft/compose.yaml`**

Values from the spec:
- top-level `name: minecraft`. Service `minecraft`, `container_name: minecraft`.
- image `itzg/minecraft-server:<javaNN matching the pack>` (`java21` for 1.20.5+, `java17` for 1.18–1.20.4, `java8` for older Forge).
- env: `EULA=TRUE`, `TYPE=AUTO_CURSEFORGE`, `CF_API_KEY=${CF_API_KEY}`, `CF_MODPACK_ZIP=/modpacks/pack.zip`, `MEMORY=6G`, `ENABLE_WHITELIST=TRUE`, `ENFORCE_WHITELIST=TRUE`, `WHITELIST=<names>`, `OPS=<Jonathan>`, and `CF_EXCLUDE_MODS` left empty with a comment.
- volumes: `minecraft-data:/data`, `./modpack:/modpacks:ro`, `./downloads:/downloads` (confirm `/downloads` is the image's default `CF_DOWNLOADS_REPO` in the itzg docs).
- `ports: ["127.0.0.1:25565:25565"]`, `mem_limit: 7g`, `stop_grace_period: 2m`, `restart: "no"`, `stdin_open: true`, `tty: true`.
- `healthcheck: { test: ["CMD", "mc-health"], start_period: 5m }`.

`.env.example`: `CF_API_KEY=` plus the `$$` warning.

- [ ] **Step 3: Validate the file**

Run: `cd deploy/minecraft && docker compose config --quiet && echo OK`
Expected: `OK`.

- [ ] **Step 4: Check the key survived Compose (Review Focus 1)**

Run: `docker compose config | grep CF_API_KEY`
Expected: the key appears with single `$` characters, exactly as on the CurseForge console. A shortened or `$`-less key means the escaping is wrong.

- [ ] **Step 5: First boot**

Run: `docker compose up -d && docker compose logs -f`
Expected: loader installs, mods download, and eventually a `Done (...)! For help, type "help"` line. If it lists mods that can't be downloaded automatically, download those files into `downloads/` and run it again. If a client-only mod crashes it, add the mod to `CF_EXCLUDE_MODS` with a comment and run it again.

- [ ] **Step 6: Check health and port binding (Review Focus 2)**

Run: `docker inspect -f '{{.State.Health.Status}}' minecraft; ss -ltn | grep 25565`
Expected: `healthy`, and the only listener is `127.0.0.1:25565` (no `0.0.0.0` or `*`).

- [ ] **Step 7: Jonathan joins on `localhost`**

Join with the same pack in the CurseForge app. Expected: joins with no mod-mismatch screen.

- [ ] **Step 8: Commit**

```bash
git add deploy/minecraft
git status   # confirm .env and downloads/ are NOT listed
git commit -m "feat(minecraft): add Compose project running the custom CurseForge pack"
```

---

### Task 3: `deploy/mc.sh` [Jonathan writes, Claude reviews]

**Files:**
- Create: `deploy/mc.sh` (executable)

**Interfaces:**
- Consumes: from Task 2, project `minecraft`, container `minecraft`, health status from `docker inspect -f '{{.State.Health.Status}}' minecraft`, `docker exec minecraft rcon-cli list`.
- Produces: `mc.sh start|stop|status`. `status` prints a first line of exactly one of `stopped`, `starting`, `running`, `unhealthy`. Milestone 2's `Status` type copies these.

- [ ] **Step 1: Write the expected behaviour down first (the test)**

Before writing the script, read this table. It is the test: Step 4 runs every row.

| Run | Expected |
|---|---|
| `mc.sh` | usage on stderr, exit 1 |
| `mc.sh bogus` | usage on stderr, exit 1 |
| `mc.sh status` (server down) | `stopped`, exit 0 |
| `mc.sh start` | says it's starting and to check `status`, exit 0 |
| `mc.sh status` (within ~1 min) | `starting` |
| `mc.sh status` (after boot) | `running` and `0 players` (or the online names) |
| `mc.sh start` (already running) | harmless, exit 0, server not restarted |
| `cd /tmp && <repo>/deploy/mc.sh status` | same answer as from the repo |
| `mc.sh stop` | waits, then says stopped, exit 0 |

- [ ] **Step 2: Run it before the script exists**

Run: `deploy/mc.sh status`
Expected: `No such file or directory`, which shows the table is checking something real.

- [ ] **Step 3: Write `deploy/mc.sh`**

`#!/usr/bin/env bash`, `set -euo pipefail`. Find the Compose file from the script's own location (`${BASH_SOURCE[0]}`), not the current folder. `start` is `docker compose up -d`, and `stop` is `docker compose stop` (not `down`). `status` maps "no container or not running" to `stopped` and maps the health values to the other three states, querying `rcon-cli list` only when healthy. One `case` on `$1`. `chmod +x`.

Things to look up for this step: why `set -u` makes `$1` fail when there's no argument (use `${1:-}`), and how `docker inspect` behaves when the container doesn't exist (non-zero exit, which `set -e` would turn into a crash).

- [ ] **Step 4: Run every row of the table**

Expected: every row matches. Fix and re-run until it does.

- [ ] **Step 5: Claude reviews**

Ask Claude to review `mc.sh`. Apply the agreed fixes and re-run Step 4.

- [ ] **Step 6: Commit**

```bash
git add deploy/mc.sh
git commit -m "feat(minecraft): add mc.sh to start, stop and check the server"
```

---

### Task 4: Persistence and shutdown checks [Jonathan runs, Claude helps debug]

**Files:** none (verification only)

**Interfaces:**
- Consumes: `mc.sh` (Task 3), the volume `minecraft-data` (Task 2).

- [ ] **Step 1: The world survives a stop**

Place a recognisable block, run `mc.sh stop`, run `mc.sh start`, and rejoin. Expected: the block is there.

- [ ] **Step 2: The world survives removing the container**

Run: `cd deploy/minecraft && docker compose down && ../mc.sh start` and rejoin once `running`. Expected: the block is there and `docker volume ls` still shows `minecraft-data`.

- [ ] **Step 3: Clean shutdown (Review Focus 3)**

Run: `time deploy/mc.sh stop; docker inspect -f '{{.State.ExitCode}}' minecraft; docker logs --tail 15 minecraft`
Expected: exit code `0` or `143`, **not** `137` (137 means it was killed after the grace period). Logs end with the world being saved.

- [ ] **Step 4: Restart policies (Review Focus 5)**

Run: `sudo systemctl restart docker; sleep 15; docker ps --format '{{.Names}}'`
Expected: `playit` is listed and `minecraft` is not.

No commit (nothing changed).

---

### Task 5: Public access through playit [Jonathan, with a friend]

**Files:** none (playit.gg dashboard and testing)

**Interfaces:**
- Consumes: the `playit` agent (Task 1), Minecraft on `127.0.0.1:25565` (Task 2).

- [ ] **Step 1: Create the tunnel**

On the playit.gg dashboard, create a "Minecraft Java" tunnel for this agent pointing to local address `127.0.0.1`, port `25565` (not `localhost`: it resolves to IPv6 `::1` first, where nothing listens). Note the public address.

- [ ] **Step 2: A non-whitelisted account is refused**

Have someone not on the whitelist try the public address. Expected: "You are not white-listed on this server".

- [ ] **Step 3: A friend joins**

A whitelisted friend outside the home network joins through the public address. Run `deploy/mc.sh status`. Expected: `running` with their name listed.

No commit. The address goes into the README in Task 6.

---

### Task 6: Move the real world in, then docs and decision records [Jonathan, Claude drafts README]

**Files:**
- Modify: `README.md`
- Create: `docs/decisions/0002-custom-curseforge-pack.md`, `docs/decisions/0003-playit-separate-compose.md`

- [ ] **Step 1: Back up the original save**

Copy the existing world's save folder to somewhere outside the repo. Confirm the pack's mod versions match what it was last played on.

- [ ] **Step 2: Copy it into the volume**

Run `deploy/mc.sh stop`. Then replace the volume's `/data/world` with the save folder's contents, using a throwaway container that mounts `minecraft-data`, for example `docker run --rm -v minecraft-data:/data -v <save>:/src:ro alpine sh -c 'rm -rf /data/world && cp -a /src /data/world && chown -R 1000:1000 /data/world'`. Then run `deploy/mc.sh start`.
Expected: `status` reaches `running`. Logs show no "missing registry"/missing block warnings, and existing builds are there when Jonathan joins.

- [ ] **Step 3: README section (Claude drafts, Jonathan edits)**

Add "Running the Minecraft server", covering the CurseForge key (with the `$$` note), both `.env` files, exporting the pack to `deploy/minecraft/modpack/pack.zip`, claiming the playit agent and creating the tunnel, and `mc.sh start|stop|status`. Include the public address, but no secrets.

- [ ] **Step 4: Decision records**

Jonathan writes `0002` (mod loader through a custom CurseForge pack, not Paper plugins or a hybrid) and `0003` (playit as its own always-on Compose project, approach A vs B/C from the spec), using `docs/decisions/0000-template.md`.

- [ ] **Step 5: Final secret check**

Run: `git status --short; git ls-files | grep -E '\.env$|downloads/' || echo "clean"`
Expected: `clean`.

- [ ] **Step 6: Commit and merge to main**

```bash
git add README.md docs/decisions
git commit -m "docs: document running the Minecraft server, add decision records 0002 and 0003"
# then, from the main checkout:
cd ~/Projects/coding/server-control-panel
git merge --ff-only milestone-1-minecraft
```
