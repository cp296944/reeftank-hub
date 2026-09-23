#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Run with sudo." >&2
  exit 1
fi
if [[ "${1:-}" != "--confirm" ]]; then
  echo "Usage: sudo ./install.sh --confirm" >&2
  exit 2
fi

root=/opt/reeftank-hub/thread
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -n "${RCP_DEVICE:-}" ]]; then
  rcp_device="${RCP_DEVICE}"
else
  mapfile -t radios < <(find /dev/serial/by-id -maxdepth 1 -type l 2>/dev/null | sort)
  if [[ ${#radios[@]} -ne 1 ]]; then
    printf 'Expected exactly one USB serial radio, found %d:\n' "${#radios[@]}" >&2
    printf '  %s\n' "${radios[@]:-none}" >&2
    echo "Unplug other USB serial adapters or set RCP_DEVICE manually." >&2
    exit 3
  fi
  rcp_device="${radios[0]}"
fi
infra_if="${INFRA_IF:-$(ip route show default | awk 'NR==1 {print $5}')}"
if [[ -z "${infra_if}" || ! -e "${rcp_device}" ]]; then
  echo "Unable to resolve Ethernet interface or C6 serial device." >&2
  exit 4
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is not installed. Install Docker first, then rerun this command." >&2
  exit 5
fi
docker compose version >/dev/null

install -d -m 0750 "${root}/data"
install -m 0644 "${script_dir}/compose.yaml" "${root}/compose.yaml"
umask 077
printf 'RCP_DEVICE=%s\nINFRA_IF=%s\n' "${rcp_device}" "${infra_if}" > "${root}/.env"
sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null
printf 'net.ipv6.conf.all.forwarding=1\n' > /etc/sysctl.d/90-reeftank-thread.conf
docker compose --project-directory "${root}" -f "${root}/compose.yaml" pull
docker compose --project-directory "${root}" -f "${root}/compose.yaml" up -d
echo "OTBR installed: RCP=${rcp_device}, Ethernet=${infra_if}"
