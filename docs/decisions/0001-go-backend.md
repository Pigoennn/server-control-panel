# 0001: Go for the backend (API + Discord bot)

- **Date:** 2026-11-13
- **Status:** Accepted

## Context

Backend needs to be able to talk to Docker, run 24/7 (unless shut down temporarily) and not run too hard on the laptop so the limited hardware can focus on hosting the servers.

Hardware is i7 7th Gen Intel CPU along with 8GB of RAM. May be upgraded in the future.
## Decision

Go, for the control API (to talk to the Docker server) and the Discord bot.
## Alternatives

Python - yes but too heavy on memory. Since I only have 8GB of memory I want to squeeze everything I have out of it. I've also already used Python for a lot of projects so wouldn't hurt to improve my skills in another one.

Node is fine too but I don't like it. Doesn't have its own Docker SDK and needs its own runtime on the server. Would prefer over Python but not over Go.
## Consequences

Docker's official SDK is in Go, since Docker itself is written in Go. Go also compiles to a single binary, so the server needs nothing else installed. Discord bots have discordgo, a community-maintained library.

Downside is that development may be slightly slower as I am not the most versed in Go as I am in Python, however it should be fine.