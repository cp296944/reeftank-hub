#!/usr/bin/env bash
set -euo pipefail
root=/opt/reeftank-hub/thread
if docker compose version >/dev/null 2>&1; then
  docker compose --project-directory "${root}" -f "${root}/compose.yaml" up -d --force-recreate
elif command -v docker-compose >/dev/null 2>&1; then
  docker-compose --project-directory "${root}" -f "${root}/compose.yaml" up -d --force-recreate
else
  echo "Docker Compose is unavailable." >&2
  exit 1
fi
for _ in {1..60}; do
  sleep 2
  state=$(docker exec reeftank-otbr ot-ctl state 2>/dev/null | head -1 | tr -d '\r' || true)
  [[ ${state} == router || ${state} == leader || ${state} == child ]] && break
done
if [[ ${state:-} == router || ${state:-} == leader || ${state:-} == child ]]; then
  network_name=$(docker exec reeftank-otbr ot-ctl networkname | head -1 | tr -d '\r')
  channel=$(docker exec reeftank-otbr ot-ctl channel | head -1 | tr -d '\r')
  install -d -m 0700 "${root}/datasets"
  printf 'observed_at=%s\nstate=%s\nnetwork_name=%s\nchannel=%s\n' "$(date -u +%FT%TZ)" "${state}" "${network_name}" "${channel}" > "${root}/datasets/current"
  chmod 0644 "${root}/datasets/current"
fi
