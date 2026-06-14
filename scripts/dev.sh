#!/usr/bin/env bash
# Start the three Fireman dev processes (Go backend, Next.js web, Python
# sidecar) in parallel without requiring a Dev Container CLI. This is a
# plain shell script that delegates lifecycle to the host shell.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_DIR="$ROOT/web"
PROVIDER_DIR="$ROOT/sidecars/market-provider"
CONFIG_PATH="${FIREMAN_CONFIG:-$ROOT/config.json}"
DEV_DATA_DIR="${FIREMAN_DEV_DATA_DIR:-$ROOT/.dev-data}"
mkdir -p "$DEV_DATA_DIR"

export MARKET_PROVIDER_RESOLVE_DEADLINE="${MARKET_PROVIDER_RESOLVE_DEADLINE:-70}"

pids=()
cleanup() {
  trap - INT TERM EXIT
  for pid in "${pids[@]:-}"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

echo "[fireman] starting market-provider on :18081"
(
  cd "$PROVIDER_DIR"
  MARKET_PROVIDER_RESOLVE_TIMEOUT="${MARKET_PROVIDER_RESOLVE_TIMEOUT:-60}" \
  MARKET_PROVIDER_FETCH_TIMEOUT="${MARKET_PROVIDER_FETCH_TIMEOUT:-240}" \
  uv run uvicorn fireman_market_provider.app:app --host 0.0.0.0 --port 18081 --reload
) &
pids+=("$!")

echo "[fireman] starting backend with config=$CONFIG_PATH"
(
  cd "$ROOT"
  MARKET_PROVIDER_RESOLVE_TIMEOUT="${MARKET_PROVIDER_RESOLVE_TIMEOUT:-90}" \
  MARKET_PROVIDER_FETCH_TIMEOUT="${MARKET_PROVIDER_FETCH_TIMEOUT:-300}" \
  go run ./cmd/fireman run --config="$CONFIG_PATH"
) &
pids+=("$!")

echo "[fireman] starting web on :3000"
( cd "$WEB_DIR" && npm run dev ) &
pids+=("$!")

wait -n
