#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_DIR="$ROOT/web"
CACHE_DIR="${FIREMAN_WEB_NM_CACHE:-/tmp/fireman-web-modules}"

mkdir -p "$CACHE_DIR"
LOCK_HASH="$(sha256sum "$WEB_DIR/package-lock.json" | awk '{print $1}')"

if [ ! -f "$CACHE_DIR/.lock-hash" ] || [ "$(cat "$CACHE_DIR/.lock-hash")" != "$LOCK_HASH" ] || [ ! -f "$CACHE_DIR/node_modules/.bin/next" ]; then
  cp "$WEB_DIR/package.json" "$WEB_DIR/package-lock.json" "$CACHE_DIR/"
  rm -rf "$CACHE_DIR/node_modules"
  (cd "$CACHE_DIR" && npm ci)
  echo "$LOCK_HASH" > "$CACHE_DIR/.lock-hash"
fi

# Invalidate cache when package manifest changes even if lock hash is unchanged.
if ! cmp -s "$WEB_DIR/package.json" "$CACHE_DIR/package.json"; then
  cp "$WEB_DIR/package.json" "$WEB_DIR/package-lock.json" "$CACHE_DIR/"
  rm -rf "$CACHE_DIR/node_modules"
  (cd "$CACHE_DIR" && npm ci)
  echo "$LOCK_HASH" > "$CACHE_DIR/.lock-hash"
fi

if [ -e "$WEB_DIR/node_modules" ]; then
  rm -rf "$WEB_DIR/node_modules" 2>/dev/null || mv "$WEB_DIR/node_modules" "/tmp/fireman-nm-leftover-$(date +%s)"
fi
mkdir -p "$WEB_DIR/node_modules"
rsync -a "$CACHE_DIR/node_modules/" "$WEB_DIR/node_modules/"
