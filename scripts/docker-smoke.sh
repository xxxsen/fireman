#!/usr/bin/env bash
# Smoke-test the Docker Compose stack: build, start, verify web→backend API proxy.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT}/docker/docker-compose.yml"
WEB_URL="${WEB_URL:-http://127.0.0.1:3000}"
BACKEND_URL="${BACKEND_URL:-http://127.0.0.1:8080}"
MAX_WAIT="${MAX_WAIT:-180}"

cd "${ROOT}"

echo "==> Building and starting compose stack"
docker compose -f "${COMPOSE_FILE}" up -d --build

wait_http() {
  local url="$1"
  local label="$2"
  local elapsed=0
  while [ "${elapsed}" -lt "${MAX_WAIT}" ]; do
    if curl -sf "${url}" >/dev/null 2>&1; then
      echo "OK: ${label} (${url})"
      return 0
    fi
    sleep 3
    elapsed=$((elapsed + 3))
  done
  echo "FAIL: ${label} not ready after ${MAX_WAIT}s (${url})" >&2
  docker compose -f "${COMPOSE_FILE}" ps >&2 || true
  return 1
}

echo "==> Waiting for backend health"
wait_http "${BACKEND_URL}/api/v1/plans" "backend API"

echo "==> Waiting for web health"
wait_http "${WEB_URL}/" "web home"
wait_http "${WEB_URL}/api/v1/plans" "web API proxy"

echo "==> Checking web build proxy target"
if docker exec fireman-web test -f .next/routes-manifest.json; then
  if docker exec fireman-web grep -q 'backend:8080' .next/routes-manifest.json; then
    echo "OK: routes-manifest.json references backend:8080"
  else
    echo "WARN: routes-manifest.json exists but backend:8080 not found; checking rewrites"
    docker exec fireman-web cat .next/routes-manifest.json | head -c 2000 || true
    echo
  fi
else
  echo "NOTE: routes-manifest.json not in standalone image; proxy verified via live /api request"
fi

echo "==> Smoke test passed"
