#!/usr/bin/env bash
set -euo pipefail

[[ ${EUID} -eq 0 ]] || { echo "Run with sudo." >&2; exit 1; }
root=/opt/reeftank-hub/thread
pending=${root}/pending/dataset.tlv
[[ -f ${pending} ]] || { echo "No pending Thread dataset." >&2; exit 2; }
trap 'rm -f "${pending}"' EXIT
tlv=$(tr -d '\r\n[:space:]' < "${pending}")
[[ ${tlv} =~ ^[0-9A-Fa-f]+$ && $((${#tlv} % 2)) -eq 0 && ${#tlv} -ge 40 && ${#tlv} -le 2048 ]] || { echo "Invalid Thread dataset TLV." >&2; exit 3; }

install -d -m 0711 "${root}/datasets"
if current=$(docker exec reeftank-otbr ot-ctl dataset active -x 2>/dev/null | awk '/^[0-9A-Fa-f]+$/ {print; exit}') && [[ -n ${current} ]]; then
  backup="${root}/datasets/active-$(date -u +%Y%m%dT%H%M%SZ).tlv"
  umask 077
  printf '%s\n' "${current}" > "${backup}"
fi
docker exec reeftank-otbr ot-ctl dataset set active "${tlv}"
docker exec reeftank-otbr ot-ctl ifconfig up
docker exec reeftank-otbr ot-ctl thread start
state=disabled
for _ in {1..15}; do
  sleep 2
  state=$(docker exec reeftank-otbr ot-ctl state | head -1 | tr -d '\r')
  [[ ${state} == router || ${state} == leader || ${state} == child ]] && break
done
[[ ${state} == router || ${state} == leader || ${state} == child ]] || { echo "Thread failed to attach; state=${state}" >&2; exit 4; }
network_name=$(docker exec reeftank-otbr ot-ctl networkname | head -1 | tr -d '\r')
channel=$(docker exec reeftank-otbr ot-ctl channel | head -1 | tr -d '\r')
printf 'applied_at=%s\nstate=%s\nnetwork_name=%s\nchannel=%s\n' "$(date -u +%FT%TZ)" "${state}" "${network_name}" "${channel}" > "${root}/datasets/current"
chmod 0644 "${root}/datasets/current"
echo "Thread dataset applied; state=${state}"
