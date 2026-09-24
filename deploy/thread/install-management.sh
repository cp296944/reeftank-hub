#!/usr/bin/env bash
set -euo pipefail
[[ ${EUID} -eq 0 ]] || { echo "Run with sudo." >&2; exit 1; }
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
for unit in reeftank-thread-flash.service reeftank-thread-install.service reeftank-thread-restart.service reeftank-thread-rollback.service; do
  install -m 0644 "${here}/${unit}" "/etc/systemd/system/${unit}"
done
install -m 0644 "${here}/51-reeftank-thread.rules" /etc/polkit-1/rules.d/51-reeftank-thread.rules
systemctl daemon-reload
echo "Thread management units installed."
