# 0007: Usage metrics move from milestone 3 to milestone 4

- **Date:** 2026-11-16
- **Status:** Accepted

## Context

The plan said to start logging usage metrics (server starts, unique players, hours played, uptime, idle hours saved) in milestone 3, alongside the Discord bot. But there is nowhere to store them yet (SQLite arrives with the dashboard) and nowhere to show them, and milestone 3 already carries the bot plus new API rules for stopping safely.

## Decision

Metrics move to milestone 4, where SQLite and the dashboard that displays them are built. Milestone 3 keeps an audit trail instead: every API request is logged with the Discord user who asked (see 0009), so who started, stopped or cancelled what is still recorded.

## Alternatives

Log metrics as JSON lines in milestone 3 and import them into SQLite later: real data sooner, but a throwaway format and an import step, for numbers nobody can see until milestone 4 anyway.

Build a minimal SQLite store in milestone 3: pulls part of milestone 4 forward and grows a milestone that's already the resume-ready deadline (6 Dec).

## Consequences

Milestone 3 stays focused on what friends actually use, and the metrics get designed once, together with where they're shown. The cost: player and playtime data from the weeks between milestones 3 and 4 is never collected, so the first numbers on the dashboard start from zero.
