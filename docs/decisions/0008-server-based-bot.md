# 0008: server based bot

- **Date:** 2026-11-16
- **Status:** Accepted

## Context

All friends who play will use the same server, so the bot will stay on one server and be used there. I still don't want their actions announced to everyone in it ("Xxx started minecraft" in a channel), and not everyone in the server should be able to start or stop the game servers.

## Decision

The bot lives in one Discord server (`DISCORD_GUILD_ID` in `.env`), and its slash commands are registered to that server only. Every reply is ephemeral, so only the person who ran the command sees it. Being in the server isn't enough to use it: the permission check is an allowed list of Discord user IDs (`ALLOWED_IDS`), checked before every command and button press. Admins (`ADMIN_IDS`) are allowed automatically and are the only ones who can stop a server with people on it. The bot connects out over Discord's gateway with `discordgo`, so it needs no public URL.

## Alternatives

A user-installed app that friends use in a direct message (the original milestone 3 design): works for friends spread across different servers, but global commands take up to an hour to update, and once everyone is in one server it adds an install step for each friend for no benefit. The code for it is still there: leaving `DISCORD_GUILD_ID` empty registers the commands globally for a DM instead.

Server membership (or a Discord role) as the permission: simpler, but anyone invited to the server for chatting could then shut down someone's game.

Receiving commands as HTTP requests from Discord instead of over the gateway: easy to test, but needs a public URL, which would pull the Cloudflare Tunnel forward from milestone 4.

Managing the lists from Discord (an `/allow` command): nicer, but needs storage and its own permission rules. Editing `.env` is enough for about 10 friends.

## Consequences

Commands update instantly, friends need nothing installed, and nothing they do is seen by anyone else in the server. Someone in the server but not on the list gets "You're not on the list", and the refusal is logged with their ID. The costs: everyone who plays has to join that server; adding someone means editing `.env` and restarting the bot; and a bot can only reply to commands, never message someone first, so every message must be a reply to a command they ran.
