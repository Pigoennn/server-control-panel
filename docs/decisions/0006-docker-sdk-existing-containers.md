# 0006: Control existing containers with the Docker SDK, not Compose

- **Date:** 2026-11-15
- **Status:** Accepted

## Context

Each game is a Compose project (see 0003), and `mc.sh` drives it with `docker compose`. The API needs to start, stop and check the same servers from Go, in a way that's easy to test without Docker and fits the socket proxy's small allowlist (0005).

## Decision

The Minecraft adapter uses the official Docker Go SDK (`github.com/moby/moby/client`) on the container Compose already created, found by its `container_name`. It needs three calls: inspect (for state and health), start and stop. Tests use a fake that implements the same small `docker.Client` interface. If the container doesn't exist, `Start` returns `ErrNotCreated` and the API answers `409` with the `docker compose ... up --no-start` command to run.

## Alternatives

Running `docker compose up -d` / `stop` from Go: works even after `docker compose down`, but shells out, needs the Compose CLI and the compose files wherever the API runs, needs far more Docker permissions (create, networks, volumes) and is hard to test.

Creating the container from Go: a second copy of everything in `compose.yaml` to keep in sync. Ruled out.

## Consequences

The compose file stays the single source of truth for how a server is configured, the API only needs start, stop and inspect, and the adapter is unit-tested with a fake. The cost: after `docker compose down` or a compose file change, someone has to run `up --no-start` once before the API can start that game. Changing a compose file also doesn't reach the container until it's recreated.
