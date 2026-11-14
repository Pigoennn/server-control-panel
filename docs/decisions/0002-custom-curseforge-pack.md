# 0002: Run Minecraft as a mod loader server with a custom CurseForge pack

- **Date:** 2026-11-14
- **Status:** Accepted

## Context

My friends and I play Java Edition with mods (Create and about 360 others), not vanilla with server-side tweaks. The first world runs on "Create Banger", a custom pack we put together in CurseForge, and later worlds might use other packs from CurseForge or Modrinth.

## Decision

The server runs the pack's own mod loader (NeoForge for Minecraft 1.21.1). It uses the `itzg/minecraft-server` image with `TYPE: AUTO_CURSEFORGE`, pointed at the pack exported from the CurseForge app, so the server installs the same loader and the same mod versions as our clients.

## Alternatives

Paper with plugins: lighter and well supported, but plugins only change the server. Mods like Create add new blocks and items that the client must also have, which plugins can't do.

A hybrid server (mods and plugins together, like Mohist or Arclight): it gives both, but it's known for breaking mods and lags behind new versions. I don't need plugins yet, so it isn't worth the instability.

Installing mods by hand into a mods folder: it works, but it's easy to get versions out of sync with the clients. The exported pack is the single source of truth.

## Consequences

Updating the server means re-exporting the pack and restarting. The image downloads the mods by their CurseForge IDs, so the repo only stores a 10MB zip and no mod jars, which aren't allowed to be redistributed anyway.

The cost is memory: this pack uses 7–8GB on the server, more than the laptop's ~6GB budget, so it'll need trimming (or better hardware) before it moves to the laptop. Boots are also slow (about a minute, a few minutes on first install).
