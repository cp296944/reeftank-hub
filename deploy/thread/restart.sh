#!/usr/bin/env bash
set -euo pipefail
root=/opt/reeftank-hub/thread
if docker compose version >/dev/null 2>&1; then
  docker compose --project-directory "${root}" -f "${root}/compose.yaml" restart
elif command -v docker-compose >/dev/null 2>&1; then
  docker-compose --project-directory "${root}" -f "${root}/compose.yaml" restart
else
  echo "Docker Compose is unavailable." >&2
  exit 1
fi
