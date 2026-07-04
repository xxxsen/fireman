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
PID_DIR="${FIREMAN_DEV_PID_DIR:-${TMPDIR:-/tmp}}"
mkdir -p "$PID_DIR"
ROOT_HASH="$(printf '%s' "$ROOT" | cksum | awk '{print $1}')"
PID_FILE="${FIREMAN_DEV_PID_FILE:-$PID_DIR/fireman-dev-$ROOT_HASH.pids}"

export MARKET_PROVIDER_RESOLVE_DEADLINE="${MARKET_PROVIDER_RESOLVE_DEADLINE:-70}"

pids=()

proc_start_time() {
  local pid="$1"
  awk '{print $22}' "/proc/$pid/stat" 2>/dev/null || true
}

record_pid() {
  local pid="$1"
  local label="$2"
  local started
  started="$(proc_start_time "$pid")"
  printf '%s %s %s\n' "$pid" "$started" "$label" >>"$PID_FILE"
  pids+=("$pid")
}

kill_tree() {
  local pid="$1"
  local child
  while read -r child; do
    [[ -n "$child" ]] && kill_tree "$child"
  done < <(pgrep -P "$pid" 2>/dev/null || true)
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
  fi
}

kill_recorded_pid() {
  local pid="$1"
  local expected_start="$2"
  local label="$3"
  local current_start

  [[ -n "$pid" ]] || return 0
  kill -0 "$pid" 2>/dev/null || return 0

  current_start="$(proc_start_time "$pid")"
  if [[ -n "$expected_start" && -n "$current_start" && "$current_start" != "$expected_start" ]]; then
    echo "[fireman] skip stale $label pid=$pid; process id was reused"
    return 0
  fi

  echo "[fireman] stopping stale $label pid=$pid"
  kill_tree "$pid"
}

cleanup_previous() {
  [[ -f "$PID_FILE" ]] || return 0

  echo "[fireman] cleaning previous dev processes from $PID_FILE"
  while read -r pid started label; do
    kill_recorded_pid "$pid" "$started" "${label:-process}"
  done <"$PID_FILE"
  rm -f "$PID_FILE"
}

cleanup() {
  trap - INT TERM EXIT
  for pid in "${pids[@]:-}"; do
    kill_recorded_pid "$pid" "$(proc_start_time "$pid")" "process"
  done
  wait 2>/dev/null || true
  rm -f "$PID_FILE"
}

cleanup_previous
: >"$PID_FILE"
trap cleanup INT TERM EXIT

echo "[fireman] starting market-provider on :18081"
(
  cd "$PROVIDER_DIR"
  MARKET_PROVIDER_RESOLVE_TIMEOUT="${MARKET_PROVIDER_RESOLVE_TIMEOUT:-60}" \
  MARKET_PROVIDER_FETCH_TIMEOUT="${MARKET_PROVIDER_FETCH_TIMEOUT:-240}" \
  uv run uvicorn fireman_market_provider.app:app --host 0.0.0.0 --port 18081 --reload
) &
record_pid "$!" "market-provider"

echo "[fireman] starting backend with config=$CONFIG_PATH"
(
  cd "$ROOT"
  MARKET_PROVIDER_RESOLVE_TIMEOUT="${MARKET_PROVIDER_RESOLVE_TIMEOUT:-90}" \
  MARKET_PROVIDER_FETCH_TIMEOUT="${MARKET_PROVIDER_FETCH_TIMEOUT:-300}" \
  go run ./cmd/fireman run --config="$CONFIG_PATH"
) &
record_pid "$!" "backend"

echo "[fireman] starting web on :3000"
( cd "$WEB_DIR" && npm run dev ) &
record_pid "$!" "web"

wait -n
