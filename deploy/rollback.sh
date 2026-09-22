#!/usr/bin/env bash
# Triggered by systemd `OnFailure=` when reeftank-hub crash-loops after an OTA
# update. Repoints `current` at the PREVIOUS release and restarts once.
#
# Guards against ping-ponging: only rolls back if the *running* tag hasn't been
# CONFIRMED healthy and differs from PREVIOUS, and writes ROLLED_BACK so a
# second failure doesn't try again.
set -euo pipefail

ROOT=/opt/reeftank-hub
STATE="$ROOT/state"
CUR_LINK="$ROOT/current"

log() { logger -t reeftank-hub-rollback "$*"; echo "reeftank-hub-rollback: $*" >&2; }

[[ -L "$CUR_LINK" ]] || { log "no current symlink; nothing to roll back"; exit 0; }

running="$(basename "$(readlink -f "$CUR_LINK")")"
previous="$(cat "$STATE/PREVIOUS" 2>/dev/null || true)"
confirmed="$(cat "$STATE/CONFIRMED" 2>/dev/null || true)"
lastrb="$(cat "$STATE/ROLLED_BACK" 2>/dev/null || true)"

if [[ "$running" == "$confirmed" ]]; then
  log "running $running is confirmed-healthy; failure is not update-related, leaving it"
  exit 0
fi
if [[ -z "$previous" ]]; then
  log "no PREVIOUS recorded; cannot roll back"
  exit 0
fi
if [[ "$running" == "$previous" ]]; then
  log "already on $previous; not rolling back further"
  exit 0
fi
if [[ "$lastrb" == "$running->$previous" ]]; then
  log "already rolled $running->$previous once; giving up to avoid a loop"
  exit 0
fi
if [[ ! -x "$ROOT/releases/$previous/reeftank-hub" ]]; then
  log "previous release $previous binary missing; cannot roll back"
  exit 0
fi

log "rolling back $running -> $previous"
ln -sfn "$ROOT/releases/$previous" "$CUR_LINK.tmp"
mv -Tf "$CUR_LINK.tmp" "$CUR_LINK"
echo "$running->$previous" > "$STATE/ROLLED_BACK"
systemctl restart reeftank-hub
