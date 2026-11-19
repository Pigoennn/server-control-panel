# Runbook: running the game servers

Day-to-day jobs on the game server: watching logs, adding mods, changing the modpack, resetting a world, and adding friends or a whole new game. The first-time setup is in [DEPLOY.md](DEPLOY.md).

The examples use `minecraft-create`. For another game, swap in its name: the folder `deploy/<game>/`, the container `<game>` and the world volume `<game>-data` all share it.

Each step says where to run it: **dev** (the repo on your PC) or **server** (`ssh gameserver`, then `cd ~/server-control-panel`). The server always runs whatever is on `main`, so a change made on dev reaches it as: commit → push → merge to `main` → `git pull` on the server.

## Contents

- [Watch the logs](#watch-the-logs)
- [Add or remove a mod](#add-or-remove-a-mod)
- [Replace the whole modpack zip](#replace-the-whole-modpack-zip)
- [Blocked mods](#blocked-mods)
- [Reset the world](#reset-the-world)
- [Back up and restore a world](#back-up-and-restore-a-world)
- [Add a friend](#add-a-friend)
- [Add a new modpack server](#add-a-new-modpack-server)
- [Rules worth remembering](#rules-worth-remembering)

## Watch the logs

**server.**

| What | Command |
| --- | --- |
| A game server booting, live | `docker logs -f --tail 50 minecraft-create` |
| Is it up yet? | `docker inspect -f '{{.State.Health.Status}}' minecraft-create` |
| The Discord bot and the API, live | `journalctl -u panel-bot -u panel-api -f` |
| The playit tunnel | `docker logs --tail 50 playit` |
| What's running at all | `docker ps --format '{{.Names}} {{.Status}}'` |

`-f` follows the log as it's written. **Ctrl-C** stops following, not the server.

**What a boot looks like.** A boot runs through these stages in order:

1. The image checks the modpack and installs or updates mods. On a first boot, or after a change, it lists every mod it downloads.
2. NeoForge loads the mods. This is the long, quiet part: a minute or several for a big pack.
3. The world loads and generates `Preparing spawn area`.
4. `Done (42.123s)! For help, type "help"`. Players can join now, and the health check turns `healthy` within a minute, which is when Discord says it's up.

**Finding problems in a long log:**
```sh
docker logs minecraft-create 2>&1 | grep -iE "error|exception|missing|blocked|crash" | tail -30
```
- **A crash:** the full report is in the world volume. To see the latest:
  ```sh
  docker run --rm -v minecraft-create-data:/data:ro alpine:3.22 sh -c 'ls -t /data/crash-reports | head -1'
  ```
- **The container keeps getting killed:** check whether it ran out of memory:
  ```sh
  docker inspect -f '{{.State.OOMKilled}}' minecraft-create
  ```
  `true` means raise `MEMORY` and `mem_limit` together in its compose file.

**The server console** (run commands as the server): `docker attach minecraft-create`. **Detach with Ctrl-P then Ctrl-Q.** Ctrl-C stops the server. For a single command, this is safer:
```sh
docker exec minecraft-create rcon-cli list
```

## Add or remove a mod

For one extra mod, list it in the compose file. You don't need to touch the zip. The image installs it on top of the pack, chooses the file that matches the pack's Minecraft version and loader, and removes it again if you take it off the list.

**1. dev.** In `deploy/minecraft-create/compose.yaml`, under `environment:` (next to `CF_EXCLUDE_MODS`), add or edit:
```yaml
      # Mods added on top of the pack: CurseForge slugs, space separated.
      # The slug is the last part of the mod's URL:
      # curseforge.com/minecraft/mc-mods/<slug>
      CURSEFORGE_FILES: "jei appleskin"
```
- **Pin a version:** write `slug:fileID`, for example `jei:4593548`. The file ID is the number at the end of the mod's file page URL.
- **Remove a mod:** delete it from the list. It's uninstalled on the next start.

Commit, push and merge to `main`.

**2. server.** The game must be stopped: check with `/status` in Discord, or run `docker ps`. Then:
```sh
git pull
docker compose -f deploy/minecraft-create/compose.yaml up --no-start
```
This recreates the container with the new list. The world and the other mods are kept.

**3.** Run `/start minecraft-create` in Discord, and check that the mod was installed:
```sh
docker logs minecraft-create 2>&1 | grep -i <slug>
```

**Friends' clients.** A mod that adds content (blocks, items, mobs) has to be on every player's client too, or they can't join. For those, add it to the pack instead ([next section](#replace-the-whole-modpack-zip)) so friends get it with the pack. The list above suits server-only mods: performance, backups, admin tools.

**A mod that crashes the server** (usually a client-only mod): add its slug to `CF_EXCLUDE_MODS` instead, one per line with a comment saying why. Then do the forced sync from the next section, steps 3–5.

## Replace the whole modpack zip

The server only re-reads the zip when it's told to, by starting it once with `CF_FORCE_SYNCHRONIZE=true`.

**1. dev.** Export the pack from the CurseForge app and save it over `deploy/minecraft-create/modpack/create-pack.zip`. Keep that filename: a different name means changing `CF_MODPACK_ZIP` in the compose file. Check the zip is a real export:
```sh
unzip -l deploy/minecraft-create/modpack/create-pack.zip | grep manifest.json
```
Commit, push and merge to `main`.

**2. server.** Check that nothing is running except `playit` and `docker-proxy`: `docker ps --format '{{.Names}}'`. The next step starts the server by hand, which skips the API's one-game-at-a-time check. Then [back up the world](#back-up-and-restore-a-world), because removing a mod deletes its blocks from the world.

**3. server.** Pull, then start once with the forced sync and watch it:
```sh
git pull
CF_FORCE_SYNCHRONIZE=true docker compose -f deploy/minecraft-create/compose.yaml up -d
docker logs -f minecraft-create
```
Wait for `Done (...)!`, then press Ctrl-C.

**4. server.** Stop it, and recreate the container with the forced sync off again:
```sh
docker compose -f deploy/minecraft-create/compose.yaml stop
docker compose -f deploy/minecraft-create/compose.yaml up --no-start
```

**5.** Friends update the pack in their CurseForge app. Then run `/start minecraft-create` in Discord as normal.

## Blocked mods

Some authors block automatic downloads. The boot log then lists the mods and stops. **server:** download each listed file by hand from the link in the log, and put it in `deploy/minecraft-create/downloads/`. You can do this from the dev machine:
```sh
scp mod.jar gameserver:server-control-panel/deploy/minecraft-create/downloads/
```
Then start again. That folder is gitignored, because those mods can't be redistributed.

## Reset the world

This deletes only the `world` folder. The mods, configs, whitelist and ops in the same volume stay, so nothing is downloaded again. **This can't be undone,** so [back up](#back-up-and-restore-a-world) first if there's anything worth keeping.

**server.**
```sh
docker compose -f deploy/minecraft-create/compose.yaml stop                    # or /stop in Discord
docker ps --format '{{.Names}}'                                                # check: not listed
docker run --rm -v minecraft-create-data:/data alpine:3.22 rm -rf /data/world
```
Then `/start minecraft-create`. The new world gets a random seed, and the first boot takes a little longer while it builds the spawn area.

**A particular seed:** add `SEED: "<seed>"` under `environment:` in the compose file, then do the commit → `git pull` → `up --no-start` steps from [Add or remove a mod](#add-or-remove-a-mod) before starting. Remove it again afterwards if you want the next reset to be random.

## Back up and restore a world

Until milestone 6 adds scheduled backups, take one by hand before anything risky. **server**, with the game stopped:

```sh
# Back up: world, mods and configs, about 1 GB for a big pack
mkdir -p ~/backups
docker run --rm -v minecraft-create-data:/data:ro -v ~/backups:/backup alpine:3.22 \
  tar -C /data -czf /backup/minecraft-create-$(date +%F-%H%M).tgz .

# Restore: replaces everything in the volume with the backup
docker run --rm -v minecraft-create-data:/data -v ~/backups:/backup alpine:3.22 \
  sh -c 'rm -rf /data/* && tar -C /data -xzf /backup/minecraft-create-<date>.tgz'
```
`ls ~/backups` lists the backups. Delete old ones now and then: they're stored on the server's own disk.

## Add a friend

There are two lists, and a friend usually needs both:

**To join the game:** the whitelist. While the game is running, **server:**
```sh
docker exec minecraft-create rcon-cli whitelist add <minecraft name>
```
Or type `/whitelist add <name>` in-game as an op. The whitelist is per game, so do it for each one they play. The names under `WHITELIST:` in the compose file are only the starting list.

**To use the Discord bot:** the allowed list. In Discord, turn on Developer Mode, right-click the friend, and choose **Copy User ID**. Then, **server:**
```sh
sudo nano /etc/server-control-panel/panel.env      # add the ID to ALLOWED_IDS, comma separated
sudo systemctl restart panel-bot
```

## Add a new modpack server

This is how `minecraft-create` was added. Call the new one `minecraft-<name>`: the name shows up in Discord's `/start` list.

**1. dev.** Copy an existing server's compose file and `.env`. The `.env` has the shared RCON password:
```sh
mkdir -p deploy/minecraft-<name>/modpack
cp deploy/minecraft-create/compose.yaml deploy/minecraft-create/.env deploy/minecraft-<name>/
```
Put the exported pack in `deploy/minecraft-<name>/modpack/`. In the new `compose.yaml`, change these:

| Field | Change it to |
| --- | --- |
| `name:` and `container_name:` | `minecraft-<name>` |
| The volume's `name:` and key | `minecraft-<name>-data`. This is what gives it its own world. |
| `CF_MODPACK_ZIP` | `/modpacks/<file>.zip` |
| `image:` tag | The Java version the pack's Minecraft version needs: 1.20.5+ → `java21`, 1.18–1.20.4 → `java17`, 1.16.5 and older → `java8` |
| `MOTD`, the header comment | Describe the new pack |

Add `deploy/minecraft-<name>/downloads/` to `.gitignore`.

**2. dev.** Register it in `cmd/api/main.go`: copy one of the `minecraft.New(...)` entries and change `Name` and `Container`. Make `StopTimeout` match the compose file's `stop_grace_period`. Then check, build and copy:
```sh
go vet ./... && go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/ ./cmd/api ./cmd/bot
scp bin/api bin/bot gameserver:
scp deploy/minecraft-<name>/.env gameserver:server-control-panel/deploy/minecraft-<name>/.env
```
That last `scp` fails if the folder doesn't exist on the server yet. If it does, run it again after the `git pull` in step 3. Commit the compose file, the zip, `main.go` and `.gitignore`, then push and merge to `main`.

**3. server.**
```sh
git pull
chmod 600 deploy/minecraft-<name>/.env
docker compose -f deploy/minecraft-<name>/compose.yaml up --no-start
sudo install -m 755 ~/api /usr/local/bin/panel-api
sudo install -m 755 ~/bot /usr/local/bin/panel-bot
rm ~/api ~/bot
sudo systemctl restart panel-api panel-bot
journalctl -u panel-bot -n 5          # "bot ready" lists the new game
```
Then run `/start minecraft-<name>` and [watch the first boot](#watch-the-logs). It downloads every mod, so it's slow.

## Rules worth remembering

- **Only one game runs at a time.** They all share the ports and the playit tunnel. Discord enforces this; the by-hand `docker compose up` steps above don't, so check `docker ps` first.
- **A compose file change takes effect only after `up --no-start`,** with that game stopped. `git pull` alone changes nothing.
- **Don't run `docker compose down -v`.** The `-v` deletes the world volume.
- **Secrets never go in git:** every `.env`, and `/etc/server-control-panel/panel.env`.
