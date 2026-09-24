#!/usr/bin/env bash
set -euo pipefail
[[ ${EUID} -eq 0 ]] || { echo "Run with sudo." >&2; exit 1; }
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
target=/opt/reeftank-hub/thread/bin
install -d -m 0755 "${target}"
for script in flash-c6.sh rollback-c6.sh install.sh restart.sh; do
  install -m 0755 "${here}/${script}" "${target}/${script}"
done
install -m 0644 "${here}/compose.yaml" "${target}/compose.yaml"
for unit in reeftank-thread-flash.service reeftank-thread-install.service reeftank-thread-restart.service reeftank-thread-rollback.service; do
  install -m 0644 "${here}/${unit}" "/etc/systemd/system/${unit}"
done
install -m 0644 "${here}/51-reeftank-thread.rules" /etc/polkit-1/rules.d/51-reeftank-thread.rules
systemctl daemon-reload
echo "Thread management scripts and units installed."
