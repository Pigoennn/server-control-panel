# Milestone 0: Setup

Target: Sun 15 Nov (Tour of Go can finish by Sun 22 Nov). Tick items off as you go.

## Laptop (Ubuntu Server)

- [ ] Update everything: `sudo apt update && sudo apt full-upgrade`
- [ ] Give the laptop a fixed local IP (a DHCP reservation on the router, or netplan)
- [ ] SSH: copy your key over (`ssh-copy-id`), then set `PasswordAuthentication no` and `PermitRootLogin no` in `/etc/ssh/sshd_config`, and restart ssh. **Test logging in with your key in a second terminal before closing the first.**
- [ ] Firewall: `sudo ufw allow OpenSSH && sudo ufw enable`
- [ ] Automatic security updates: `sudo apt install unattended-upgrades && sudo dpkg-reconfigure unattended-upgrades`
- [ ] Lid closed without suspending: set `HandleLidSwitch=ignore` (and `HandleLidSwitchExternalPower=ignore`) in `/etc/systemd/logind.conf`, then `sudo systemctl restart systemd-logind`
- [ ] Battery charge cap at about 80%, if the laptop supports it (check `/sys/class/power_supply/BAT*/charge_control_end_threshold`)

## Docker

- [ ] Install Docker Engine + the Compose plugin from Docker's official apt repository (not the snap)
- [ ] `sudo usermod -aG docker $USER`, then log out and back in
- [ ] `docker run hello-world` works
- [ ] Check the free RAM baseline with `free -h`, and note it in `docs/decisions/`

## Repo

- [x] Create a public GitHub repo on the personal account (Pigoennn)
- [x] Add `CLAUDE.md`, `docs/PLAN.md` and this file
- [x] `go mod init github.com/Pigoennn/<repo-name>`
- [x] `.gitignore` covering `.env`, binaries and `node_modules/`
- [x] Create the folder skeleton: `cmd/api`, `cmd/bot`, `internal/games`, `internal/docker`, `web`, `deploy`, `docs/decisions`
- [x] Write the first decision record: `docs/decisions/0001-go-backend.md`

## Learning Go

- [ ] Install Go (latest stable) on your dev machine, plus the Go extension for your editor
- [ ] Tour of Go: basics, methods and interfaces (the most important part for the `Game` interface), concurrency
- [ ] Mini exercise: a tiny Go program that lists running containers with the Docker Go SDK

## Done when

You can SSH into the hardened laptop with a key, Docker runs, the repo exists with the skeleton and docs, and you can write and run a small Go program.