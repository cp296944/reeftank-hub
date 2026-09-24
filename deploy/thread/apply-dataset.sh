#!/usr/bin/env bash
set -euo pipefail

[[ ${EUID} -eq 0 ]] || { echo "Run with sudo." >&2; exit 1; }
root=/opt/reeftank-hub/thread
pending=${root}/pending/dataset.tlv
[[ -f ${pending} ]] || { echo "No pending Thread dataset." >&2; exit 2; }
trap 'rm -f "${pending}"' EXIT
tlv=$(tr -d '\r\n[:space:]' < "${pending}")
[[ ${tlv} =~ ^[0-9A-Fa-f]+$ && $((${#tlv} % 2)) -eq 0 && ${#tlv} -ge 40 && ${#tlv} -le 2048 ]] || { echo "Invalid Thread dataset TLV." >&2; exit 3; }

mkdir -p "${root}/datasets"
if current=$(docker exec reeftank-otbr ot-ctl dataset active -x 2>/dev/null | awk '/^[0-9A-Fa-f]+$/ {print; exit}') && [[ -n ${current} ]]; then
  backup="${root}/datasets/active-$(date -u +%Y%m%dT%H%M%SZ).tlv"
  umask 077
  printf '%s\n' "${current}" > "${backup}"
fi
printf 'dataset set active %s\nifconfig up\nthread start\n' "${tlv}" | docker exec -i reeftank-otbr ot-ctl
sleep 5
state=$(docker exec reeftank-otbr ot-ctl state | head -1)
printf 'applied_at=%s\nstate=%s\n' "$(date -u +%FT%TZ)" "${state}" > "${root}/datasets/current"
chmod 0644 "${root}/datasets/current"
echo "Thread dataset applied; state=${state}"
