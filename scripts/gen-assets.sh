#!/usr/bin/env bash
# gen-assets.sh — regenerate Peek's README / ProductHunt launch assets.
#
# Builds the binaries, boots a throwaway Peek server, seeds a demo "report"
# (scripts/demo-report.html) plus review comments, drives headless Chrome to
# capture 1270x760 (ProductHunt ratio) 2x screenshots + a screencast, and uses
# ffmpeg to produce assets/demo.mp4 and assets/demo.gif. Rerun it any time the
# app changes to refresh every asset.
#
# Requires: go, node (>=20), ffmpeg, and Google Chrome / Chromium.
# Override Chrome with:  CHROME="/path/to/chrome" scripts/gen-assets.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUT="$ROOT/assets"
PORT="${PEEK_ASSET_PORT:-7799}"
DBG_PORT="${PEEK_ASSET_DBG_PORT:-9355}"
TOKEN=""
DATA="$(mktemp -d)"
FRAMES="$(mktemp -d)"
CHROME_PROFILE="$(mktemp -d)"
COOKIE="$DATA/cookies.txt"
SRV_PID=""
CHROME_PID=""

cleanup() {
  [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null || true
  [ -n "$CHROME_PID" ] && kill "$CHROME_PID" 2>/dev/null || true
  sleep 0.3
  chmod -R u+w "$DATA" "$FRAMES" "$CHROME_PROFILE" 2>/dev/null || true
  rm -rf "$DATA" "$FRAMES" "$CHROME_PROFILE" 2>/dev/null || true
}
trap cleanup EXIT

# --- locate tools ---
command -v go >/dev/null || { echo "error: go not found"; exit 1; }
command -v node >/dev/null || { echo "error: node not found"; exit 1; }
command -v ffmpeg >/dev/null || { echo "error: ffmpeg not found"; exit 1; }
if [ -z "${CHROME:-}" ]; then
  for c in \
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
    "$(command -v google-chrome || true)" \
    "$(command -v chromium || true)" \
    "$(command -v chromium-browser || true)"; do
    if [ -n "$c" ] && [ -x "$c" ]; then CHROME="$c"; break; fi
  done
fi
[ -n "${CHROME:-}" ] || { echo "error: Chrome/Chromium not found (set CHROME=…)"; exit 1; }

mkdir -p "$OUT"

echo "› building binaries"
go build -o bin/peekd ./cmd/peekd
go build -o bin/peek  ./cmd/peek

echo "› starting peekd on :$PORT"
./bin/peekd --addr ":$PORT" --data "$DATA" \
  --base-url "http://localhost:$PORT" >/dev/null 2>&1 &
SRV_PID=$!
for _ in $(seq 1 50); do
  curl -fsS "http://localhost:$PORT/" >/dev/null 2>&1 && break
  sleep 0.1
done

csrf_from_html() {
  sed -n 's/.*name="csrf" value="\([^"]*\)".*/\1/p' | head -n1
}

json_value() {
  key="$1"
  sed -n "s/.*\"$key\":\"\\([^\"]*\\)\".*/\\1/p"
}

echo "› creating first admin + CLI token"
SETUP_CODE="$(tr -d '\n\r' < "$DATA/setup.key")"
SETUP_HTML="$(curl -fsS -c "$COOKIE" "http://localhost:$PORT/setup?code=$SETUP_CODE")"
SETUP_CSRF="$(printf '%s' "$SETUP_HTML" | csrf_from_html)"
curl -fsS -b "$COOKIE" -c "$COOKIE" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "email=admin@example.com" \
  --data-urlencode "name=Admin" \
  --data-urlencode "password=asset-admin-password" \
  --data-urlencode "code=$SETUP_CODE" \
  --data-urlencode "csrf=$SETUP_CSRF" \
  "http://localhost:$PORT/setup" >/dev/null
START="$(curl -fsS -X POST "http://localhost:$PORT/api/cli/login/start")"
DEVICE_CODE="$(printf '%s' "$START" | json_value device_code)"
USER_CODE="$(printf '%s' "$START" | json_value user_code)"
APPROVE_HTML="$(curl -fsS -b "$COOKIE" -c "$COOKIE" "http://localhost:$PORT/cli-login/$USER_CODE")"
APPROVE_CSRF="$(printf '%s' "$APPROVE_HTML" | csrf_from_html)"
curl -fsS -b "$COOKIE" -c "$COOKIE" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "csrf=$APPROVE_CSRF" \
  --data-urlencode "decision=approve" \
  "http://localhost:$PORT/cli-login/$USER_CODE" >/dev/null
POLL="$(curl -fsS -H "Content-Type: application/json" \
  -d "{\"device_code\":\"$DEVICE_CODE\"}" \
  "http://localhost:$PORT/api/cli/login/poll")"
TOKEN="$(printf '%s' "$POLL" | json_value token)"
[ -n "$TOKEN" ] || { echo "error: token setup failed: $POLL"; exit 1; }

echo "› seeding demo report + comments"
api() { curl -fsS -H "Authorization: Bearer $TOKEN" "$@"; }
UP=$(api --data-binary @scripts/demo-report.html \
  -H "Content-Type: text/html" \
  "http://localhost:$PORT/api/upload?filename=codebase-health-report.html")
SLUG=$(printf '%s' "$UP" | sed -n 's/.*"slug":"\([^"]*\)".*/\1/p')
[ -n "$SLUG" ] || { echo "error: upload failed: $UP"; exit 1; }
# a second upload so the dashboard list looks real
printf '<!doctype html><h1>Release checklist</h1><p>v2 freeze tasks.</p>' | \
  api --data-binary @- -H "Content-Type: text/html" \
  "http://localhost:$PORT/api/upload?filename=release-checklist.html" >/dev/null

comment() { api -H "Content-Type: application/json" -d "$1" \
  "http://localhost:$PORT/api/uploads/$SLUG/comments" >/dev/null; }
comment '{"selector":"#latency-claim","element_text":"p95 request latency regressed by 38% after the caching layer refactor","name":"Priya","body":"Can we link the trace for this? Want to confirm it is the cache key and not GC."}'
comment '{"selector":"#r1","element_text":"Fix the cache key derivation in session.Store.Get and add a regression benchmark.","name":"Devin","body":"Prioritizing this for the next sprint — Ill take it."}'
comment '{"selector":"","element_text":"","name":"Sam","body":"Great write-up. Looping in the platform team for the backoff change."}'

echo "› launching headless Chrome"
"$CHROME" --headless --disable-gpu --hide-scrollbars --force-device-scale-factor=1 \
  --remote-debugging-port="$DBG_PORT" --user-data-dir="$CHROME_PROFILE" about:blank \
  >/dev/null 2>&1 &
CHROME_PID=$!
for _ in $(seq 1 50); do
  curl -fsS "http://localhost:$DBG_PORT/json/version" >/dev/null 2>&1 && break
  sleep 0.1
done

echo "› capturing screenshots + screencast"
DEBUG="http://localhost:$DBG_PORT" BASE="http://localhost:$PORT" SLUG="$SLUG" \
  TOKEN="$TOKEN" OUT="$OUT" FRAMES="$FRAMES" node scripts/gen-assets.mjs

echo "› encoding video (mp4 + gif)"
ffmpeg -y -framerate 10 -i "$FRAMES/f_%04d.png" \
  -c:v libx264 -pix_fmt yuv420p -vf "scale=1270:760,format=yuv420p" -movflags +faststart \
  "$OUT/demo.mp4" >/dev/null 2>&1
ffmpeg -y -i "$OUT/demo.mp4" -vf "fps=12,scale=1100:-1:flags=lanczos,palettegen=stats_mode=diff" \
  "$FRAMES/pal.png" >/dev/null 2>&1
ffmpeg -y -i "$OUT/demo.mp4" -i "$FRAMES/pal.png" \
  -lavfi "fps=12,scale=1100:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=3" \
  "$OUT/demo.gif" >/dev/null 2>&1

echo "✓ assets written to assets/:"
ls -1 "$OUT"
