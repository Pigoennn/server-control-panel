# 0004: A shared bearer token for the API until Discord OAuth

- **Date:** 2026-11-15
- **Status:** Accepted

## Context

The control API can start and stop game servers, so every request must be authenticated (a `CLAUDE.md` rule). In milestone 2 its only caller is my own Discord bot, which runs on the same machine. Real per-user login (Discord OAuth, with roles) belongs to the dashboard in milestone 4.

## Decision

One shared secret, `API_TOKEN`, sent as `Authorization: Bearer <token>`. It lives in the untracked `.env`, must be at least 32 characters (the API refuses to start otherwise), is compared with `crypto/subtle.ConstantTimeCompare` so response timing doesn't leak how much of a guess was right, and is never logged. A missing and a wrong token get the same `401`, so a caller learns nothing about which it was.

## Alternatives

Discord OAuth now: the end goal, but it pulls milestone 4 forward (sessions, a callback URL, a public hostname) before there's a dashboard to log into.

No auth, relying on the API listening on `127.0.0.1` only: breaks the rule, and anything else running on the machine could start or stop servers. 

## Consequences

Simple to build, test and explain, and enough for one trusted caller on localhost. The cost: the token identifies "the bot", not a person, so there are no per-user permissions or per-user audit log yet, and rotating it means editing `.env` and restarting both the API and the bot. Milestone 4 adds OAuth for people; the token can stay for the bot.
