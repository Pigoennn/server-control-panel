# 0005: Reach Docker through a socket proxy, not the docker group

- **Date:** 2026-11-15
- **Status:** Accepted

## Context

The API starts and stops game containers through the Docker Engine API. Access to the Docker socket is root-equivalent: anything that can talk to it can start a privileged container that mounts the host's disk. If the API had a bug, or its token leaked, that would be root on the laptop.

## Decision

The API talks to `lscr.io/linuxserver/socket-proxy` (`deploy/docker-proxy/`, always on, published on `127.0.0.1:2375`) via `DOCKER_HOST=tcp://127.0.0.1:2375`. The proxy mounts the socket read-only and forwards only listing and inspecting containers, start and stop (`CONTAINERS`, `ALLOW_START`, `ALLOW_STOP`, plus `PING` and `VERSION` for version negotiation). Everything else is off, including `POST` in general, `EXEC`, images, volumes and networks; I checked that create and exec requests are refused.

## Alternatives

Running the API as a user in the `docker` group: one less container, but the `docker` group is effectively root, so it doesn't contain anything.

Rootless Docker: a real improvement for the whole host, but it changes how every container runs (ports, volumes, the playit host network) and is a bigger project than this milestone.

## Consequences

A compromised API can at worst start or stop existing containers, which is what it's for anyway. The cost is one more always-on container, a second network hop, and an allowlist to update if the API ever needs another Docker call (backups in milestone 6 may). The proxy listens on localhost with no auth of its own, so anything on the machine can use it; that's acceptable because it can only do the same few things.
