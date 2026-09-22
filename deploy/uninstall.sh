#!/usr/bin/env bash
# Remove the K7 Pi Bridge. Leaves /opt/reeftank-hub/data (config, profiles,
# backups) unless --purge is given. Never touches networking or sshd.
set -euo pipefail
ROOT=/opt/reeftank-hub
[[ $EUID -eq 0 ]] || { echo "run with sudo" >&2; exit 1; }

PURGE=0
[[ "${1:-}" == "--purge" ]] && PURGE=1

systemctl disable --now reeftank-hub.service 2>/dev/null || true
rm -f /etc/systemd/system/reeftank-hub.service \
      /etc/systemd/system/reeftank-hub-rollback.service \
      /etc/polkit-1/rules.d/49-reeftank-hub.rules
systemctl daemon-reload

if [[ $PURGE -eq 1 ]]; then
  rm -rf "$ROOT"
  userdel reefhub 2>/dev/null || true
  echo "purged $ROOT and the reefhub user"
else
  rm -rf "$ROOT/releases" "$ROOT/current" "$ROOT/current.tmp" "$ROOT/rollback.sh" "$ROOT/state"
  echo "removed binaries; kept $ROOT/data (use --purge to remove everything)"
fi
