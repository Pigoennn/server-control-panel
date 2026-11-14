# Server Control Panel

A self-hosted control panel that lets my friends start, stop and check my home game servers. The plan is in [`docs/PLAN.md`](docs/PLAN.md), and the decisions behind it are in [`docs/decisions/`](docs/decisions/).

## Running the Minecraft server

The server runs in Docker with the [`itzg/minecraft-server`](https://github.com/itzg/docker-minecraft-server) image. It runs the custom "Create Banger" CurseForge pack (Minecraft 1.21.1, NeoForge). Friends reach it through a [playit.gg](https://playit.gg) tunnel, because the home connection is behind CGNAT.

There are two Compose projects:

| Project | File | Runs |
|---|---|---|
| `playit` | `deploy/playit/compose.yaml` | Tunnel agent. Always on, and restarts with Docker. |
| `minecraft` | `deploy/minecraft/compose.yaml` | The game server. Only runs when started on purpose. |

### First-time setup

1. **playit key.** On playit.gg, add a new agent and choose Docker. Copy the secret key into `deploy/playit/.env`, using `.env.example` as the template. `.env` is gitignored and must never be committed. Then start the agent:
   ```sh
   docker compose -f deploy/playit/compose.yaml up -d
   ```
2. **Tunnel.** On the playit.gg dashboard, add a *Minecraft Java* tunnel to the agent, with local address **`127.0.0.1`** and port `25565`. Use `127.0.0.1`, not `localhost`. On this machine `localhost` resolves to IPv6 `::1` first, and the server only listens on IPv4.
3. **Modpack.** In the CurseForge app, export the pack and save it as `deploy/minecraft/modpack/create-banger.zip`. The export holds the manifest and the configs, not the mod jars. The server downloads the mods by their CurseForge IDs on first start.
4. **CurseForge API key.** Nothing to do: the image ships with its own key. To use your own key instead, see the comment in `deploy/minecraft/compose.yaml`. Every `$` in the key must be doubled to `$$` in `.env`, or Compose mangles it.
5. **Blocked mods.** Some mod authors block automatic downloads. If the logs list any, download those mods by hand into `deploy/minecraft/downloads/` (gitignored, because the mods can't be redistributed) and start again.

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
