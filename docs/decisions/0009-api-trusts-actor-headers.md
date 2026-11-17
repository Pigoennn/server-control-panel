# 0009: The stop rules live in the API, which trusts actor headers from the bot

- **Date:** 2026-11-16
- **Status:** Accepted

## Context

Milestone 3 adds rules about who may do what: a friend can't stop a server while people are playing, an admin can (after a 5-minute in-game warning), and only admins can cancel that countdown. Those rules need to know who is asking. Until now the API only knew "this request has the bot's token" (0004).

## Decision

The rules and the countdown live in the API, not the bot. The bot tells the API who is asking with two headers on every request: `X-Actor-ID` (the Discord user ID) and `X-Actor-Admin: true` for admins. The API trusts them because only a holder of `API_TOKEN` can send requests at all, and today that's the bot and me. Both headers are logged with every request, which makes the API's log the audit trail. A request without them counts as a non-admin with no ID, so forgetting them fails safe.

## Alternatives

The rules in the bot: no API change, but every future client (the milestone 4 dashboard) would have to repeat them, and a rule enforced only in the bot is bypassed by anything else that holds the token.

The API checking with Discord itself who is an admin: the right answer for the dashboard, where people log in with Discord (milestone 4), but the bot already knows who's asking, and doing it again for each request adds a Discord API call and its failures to every stop.

## Consequences

One place makes the decision, so the dashboard gets the same rules and countdown for free, and the tests for them run against a fake game, with no Discord involved. The cost: anyone with the token can claim to be an admin. That's acceptable while the token lives only in my `.env` on the same machine, but it means the token must never reach a browser. The milestone 4 dashboard will check admin status itself through Discord login, not send these headers.
