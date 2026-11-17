# 0010: Run the API and the bot as systemd services under a dynamic user

- **Date:** 2026-11-17
- **Status:** Accepted

## Context

So far the API and the bot ran with `go run` in a terminal on the dev machine. On the server they have to start on boot, restart after a crash, keep their logs, and get their secrets without those sitting in a world-readable file. The API must also reach Docker only through the socket proxy (0005).

## Decision

Both run as systemd services (`deploy/systemd/`) from static binaries in `/usr/local/bin`, built on the dev machine with `CGO_ENABLED=0`. Each runs as a `DynamicUser`: a user that exists only while the service runs and isn't in the `docker` group, so the proxy is the API's only way to reach Docker. The config is the repo-root `.env`, installed as `/etc/server-control-panel/panel.env` (root-only), which systemd reads as root and passes in as environment variables. Logs go to the journal.

## Alternatives

Containers in Docker Compose, like everything else: one tool for the whole stack, but a container on Docker's bridge network can't reach the proxy or the API on the host's `127.0.0.1` without host networking or publishing them more widely. It also adds an image build to every deploy.

Running them as my own user (in tmux, or with `systemctl --user`): the simplest, but my user is in the `docker` group, so a bug or leaked token in the API would have root-equivalent access through the raw socket, skipping the proxy.

Building on the server: needs a Go toolchain on the production box, and milestone 7's CI will build the binaries anyway.

## Consequences

A crash or a reboot recovers by itself, `journalctl -u panel-bot` shows what happened, and the sandbox (no capabilities, no `/home`, read-only system) limits what a compromised process could touch. The costs: a manual build-and-copy deploy until milestone 7, and two ways of running things (Compose for the games, playit and proxy; systemd for my own code). Milestone 4's SQLite database will need a `StateDirectory=`, which works with `DynamicUser`.
