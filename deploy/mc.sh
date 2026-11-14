#!/usr/bin/env bash
# Start, stop and check the Minecraft server.
#
#   mc.sh start    start the server (a modpack boot takes a few minutes)
#   mc.sh stop     stop it cleanly (waits for the world to save)
#   mc.sh status   print one of: stopped, starting, running, unhealthy
#                  ("running" is followed by who is online)
#
# This is the rough draft of the milestone 2 Minecraft adapter: start, stop
# and status are the Game interface's methods, and the four states are the
# start of its Status type.

# -e: exit on any failing command. -u: using an unset variable is an error.
# -o pipefail: a pipeline fails if any part of it fails, not just the last.
set -euo pipefail
# Make $(...) command substitutions follow -e too (bash doesn't by default),
# so a failing docker call inside a function still stops the script.
shopt -s inherit_errexit

# The script's own folder, wherever it's run from (BASH_SOURCE[0] is this
# file's path; cd + pwd turns it into an absolute path).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/minecraft/compose.yaml"
CONTAINER=minecraft

usage() {
  echo "usage: $(basename "$0") start|stop|status" >&2
  exit 1
}

compose() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

# Prints the server's state: stopped, starting, running or unhealthy.
state() {
  # `docker ps` prints nothing (and succeeds) when the container doesn't
  # exist, but fails with Docker's own error if Docker itself is down, which
  # -e turns into an exit. `docker inspect` can't tell those two apart.
  local id
  id=$(docker ps -aq --filter "name=^${CONTAINER}$")
  if [[ -z "$id" ]]; then
    echo stopped
    return
  fi

  # Health is only reported while running: starting, healthy or unhealthy.
  # The image's health check says "healthy" only once players can join.
  local s
  s=$(docker inspect -f '{{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$CONTAINER")
  case "$s" in
    "true healthy")   echo running ;;
    "true starting")  echo starting ;;
    "true unhealthy") echo unhealthy ;;
    *)                echo stopped ;;
  esac
}

cmd_start() {
  local st
  st=$(state)
  # `compose up -d` on a running server is mostly harmless, but it recreates
  # the container if compose.yaml changed, which would be a surprise restart.
  if [[ "$st" != stopped ]]; then
    echo "already $st"
    return
  fi
  compose up -d
  echo "starting: a modpack boot takes a few minutes, check with: $(basename "$0") status"
}

cmd_stop() {
  local st
  st=$(state)
  if [[ "$st" == stopped ]]; then
    echo "already stopped"
    return
  fi
  echo "stopping (saving the world can take up to 2 minutes)..."
  # `stop`, not `down`: down removes the container. The world volume would
  # survive either way, but stop keeps the container's logs for debugging.
  compose stop
  echo stopped
}

cmd_status() {
  local st
  st=$(state)
  echo "$st"
  case "$st" in
    running)
      # e.g. "There are 1 of a max of 20 players online: Pigoenn".
      # rcon-cli adds terminal colour codes and a blank line; sed strips both.
      # A failed query shouldn't make a running server look broken.
      docker exec "$CONTAINER" rcon-cli list \
        | sed -e 's/\x1b\[[0-9;]*m//g' -e '/^[[:space:]]*$/d' \
        || echo "(couldn't query players)" >&2
      ;;
    unhealthy)
      echo "health check failing; see: docker compose -f $COMPOSE_FILE logs --tail 50" >&2
      ;;
  esac
}

# ${1:-} means "the first argument, or empty if there isn't one". A bare $1
# with no argument would trip set -u and crash instead of printing usage.
case "${1:-}" in
  start)  cmd_start ;;
  stop)   cmd_stop ;;
  status) cmd_status ;;
  *)      usage ;;
esac
