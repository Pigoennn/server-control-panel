# Deploying to the server

How to get the whole stack running on the game server (`ssh gameserver`), starting from an Ubuntu Server LTS install with Docker. This **moves** everything from the dev machine instead of copying it: the same playit agent, the same Discord bot and the same secrets. Friends keep the same join address (`scooter-swizzle.tun.ply.gg`). The playit agent and the bot must only ever run in one place, so step 1 stops them on the dev machine.

What runs where on the server:

| What | How | Starts on boot? |
| --- | --- | --- |
| playit agent | Docker Compose (`deploy/playit`) | yes, `restart: unless-stopped` |
| Docker socket proxy | Docker Compose (`deploy/docker-proxy`) | yes, `restart: unless-stopped` |
| Minecraft (modded and vanilla) | Docker Compose, containers created but stopped | no, started from Discord |
| Control API | systemd `panel-api.service`, binary `/usr/local/bin/panel-api` | yes |
| Discord bot | systemd `panel-bot.service`, binary `/usr/local/bin/panel-bot` | yes |

Secrets live in four untracked files: the three `deploy/*/.env` files in the server's checkout, and the repo-root `.env`, which becomes `/etc/server-control-panel/panel.env` (root-only) for the two services. Why systemd for the Go services: [0010](decisions/0010-systemd-for-go-services.md).

Commands are marked **dev** (run in the repo on the dev machine) or **server** (run after `ssh gameserver`).

## 0. Check the server

**server.** Most of milestone 0 may already be done. Check, and do what's missing (see [MILESTONE-0.md](MILESTONE-0.md)):

```sh
docker run --rm hello-world        # Docker works without sudo (you're in the docker group)
docker compose version             # the Compose plugin is installed
sudo ufw status                    # active, allows only OpenSSH
free -h                            # note the RAM: see "Memory" below
```

Nothing needs opening in ufw: every game and API port is published on `127.0.0.1` only, and playit, the bot and Discord all connect outwards.

**Memory.** `deploy/minecraft/compose.yaml` gives the modded server a 6 GB heap and an 8 GB container limit. That fits a 16 GB machine, but not the 8 GB laptop: there, lower `MEMORY` and `mem_limit` together first (for example `4G` and `5500m`) and watch the first boot.

## 1. Stop things on the dev machine

**dev.** Stop the bot and the API if they're running (Ctrl-C in their terminals). Then stop the playit agent, which would otherwise fight the server's agent for the same tunnel, and any game server:

```sh
docker compose -f deploy/playit/compose.yaml down
deploy/mc.sh stop
docker compose -f deploy/minecraft-vanilla/compose.yaml stop
```

From now on, don't run the bot or playit on the dev machine.

## 2. Get the repo onto the server

**server.** The repo is public, so clone it over HTTPS (no GitHub key needed on the server). The clone is where Compose finds the compose files and the modpack zip.

```sh
git clone https://github.com/Pigoennn/server-control-panel.git ~/server-control-panel
```

## 3. Copy the secrets

**dev.** Copy the four `.env` files over `scp`. They never go through git.

```sh
scp deploy/playit/.env            gameserver:server-control-panel/deploy/playit/.env
scp deploy/minecraft/.env         gameserver:server-control-panel/deploy/minecraft/.env
scp deploy/minecraft-vanilla/.env gameserver:server-control-panel/deploy/minecraft-vanilla/.env
scp .env                          gameserver:panel.env
```

**server.** Lock them down, and move the API/bot config to `/etc`, readable by root only:

```sh
chmod 600 ~/server-control-panel/deploy/*/.env
sudo install -d -m 700 /etc/server-control-panel
sudo install -m 600 -o root -g root ~/panel.env /etc/server-control-panel/panel.env
rm ~/panel.env
```

Check that `/etc/server-control-panel/panel.env` has `DISCORD_GUILD_ID`, `ALLOWED_IDS`, `ADMIN_IDS` and `JOIN_ADDRESS` set: `sudo grep -E '^(DISCORD_GUILD_ID|ALLOWED_IDS|ADMIN_IDS|JOIN_ADDRESS)=' /etc/server-control-panel/panel.env`.

## 4. Start the always-on containers

**server.**

```sh
cd ~/server-control-panel
docker compose -f deploy/playit/compose.yaml up -d
docker compose -f deploy/docker-proxy/compose.yaml up -d
docker logs playit --tail 20     # should show the agent connected and the tunnel
```

The playit dashboard should now show the agent online. The tunnel already points to `127.0.0.1:25565`, which is right on the server too.

## 5. Create the game containers

**server.** The API starts and stops containers but doesn't create them (0006). This pulls the images (about 1.3 GB each) and creates the containers and their volumes, stopped:

```sh
docker compose -f deploy/minecraft/compose.yaml up --no-start
docker compose -f deploy/minecraft-vanilla/compose.yaml up --no-start
```

## 6. Move the worlds (optional)

Skip this to start with fresh worlds. Otherwise, with both servers stopped on **both** machines, stream each volume across. The modded volume (about 1 GB) also holds the downloaded mods, so the first start on the server doesn't download them again.

**dev.**

```sh
for v in minecraft-data minecraft-vanilla-data; do
  docker run --rm -v $v:/data:ro alpine:3.22 tar -C /data -czf - . \
    | ssh gameserver "docker run --rm -i -v $v:/data alpine:3.22 tar -C /data -xzf -"
done
```

`tar` keeps the file owners (uid 1000, the image's `minecraft` user), so the server can write to the copied files. Check one: `ssh gameserver docker run --rm -v minecraft-data:/data alpine:3.22 ls -ln /data`.

## 7. Install the API and the bot

**dev.** Build static Linux binaries (no C dependencies, so they run on any x86-64 Linux) and copy them over:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/ ./cmd/api ./cmd/bot
scp bin/api bin/bot gameserver:
```

**server.**

```sh
sudo install -m 755 ~/api /usr/local/bin/panel-api
sudo install -m 755 ~/bot /usr/local/bin/panel-bot
rm ~/api ~/bot
sudo cp ~/server-control-panel/deploy/systemd/panel-*.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now panel-api panel-bot
```

`enable` starts them on every boot; `--now` also starts them now.

## 8. Check it works

**server.**

```sh
systemctl status panel-api panel-bot      # both "active (running)"
curl -s 127.0.0.1:8080/healthz            # {"status":"ok"}
journalctl -u panel-bot -n 20             # ends with "bot ready"
```

**Discord.** Run `/status` (both games `stopped`), then `/start minecraft-vanilla`. When the reply changes to "is up", join `scooter-swizzle.tun.ply.gg` from a Minecraft client, then `/stop minecraft-vanilla` after leaving. Watch it live with `journalctl -u panel-api -u panel-bot -f`.

**Reboot test.** `sudo reboot`, wait a minute, then `/status` from Discord. Everything except the games should be back on its own.

## Updating

When `main` changes:

**dev.** `git switch main && git pull`, then rebuild and copy the binaries as in step 7.

**server.**

```sh
cd ~/server-control-panel && git pull
sudo install -m 755 ~/api /usr/local/bin/panel-api && sudo install -m 755 ~/bot /usr/local/bin/panel-bot && rm ~/api ~/bot
sudo systemctl restart panel-api panel-bot
```

Also:

- **A changed unit file:** copy it to `/etc/systemd/system/` again and run `sudo systemctl daemon-reload` before restarting.
- **A changed compose file:** recreate that game's container with `up --no-start` while it's stopped (step 5).
- **A changed `.env` (for example a new friend in `ALLOWED_IDS`):** edit it with `sudo nano /etc/server-control-panel/panel.env`, then `sudo systemctl restart panel-bot`.

Milestone 7 replaces this with CI building the binaries and Ansible doing the rest.

## Troubleshooting

| Symptom | Look at |
| --- | --- |
| Bot answers "The control panel isn't answering" | `systemctl status panel-api`, `journalctl -u panel-api -n 50` |
| Bot doesn't answer at all | `journalctl -u panel-bot -n 50`. Is the dev machine still running the bot? |
| `/start` says the container isn't created | Step 5 |
| Server starts but friends can't join | `docker logs playit`; the playit dashboard tunnel points to `127.0.0.1:25565` |
| Player list missing | RCON password mismatch: `MC_RCON_PASSWORD` must be the same in `panel.env` and both `deploy/minecraft*/.env`; recreate the container after changing it |
| Modded server killed while booting | `docker inspect minecraft -f '{{.State.OOMKilled}}'`; see Memory in step 0 |
