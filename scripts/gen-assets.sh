#!/usr/bin/env bash
# gen-assets.sh — regenerate Peek's demo video and GIF.
#
# Builds the binaries, boots a throwaway Peek server, seeds a demo page
# (scripts/demo-report.html) plus review comments, drives headless Chrome to
# record the CLI-to-browser commenting story, and uses ffmpeg to produce
# assets/demo.mp4 and assets/demo.gif.
#
# Requires: go, node (>=20), pnpm install, ffmpeg, and Google Chrome / Chromium.
# Override Chrome with:  CHROME="/path/to/chrome" scripts/gen-assets.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUT="$ROOT/assets"
DOCS_OUT="$ROOT/docs/public/assets"
PORT="${PEEK_ASSET_PORT:-7799}"
TOKEN=""
DATA="$(mktemp -d)"
TMP_ASSETS="$(mktemp -d)"
COOKIE="$DATA/cookies.txt"
RAW_VIDEO="$TMP_ASSETS/demo.webm"
SRV_PID=""

cleanup() {
  [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null || true
  sleep 0.3
  chmod -R u+w "$DATA" "$TMP_ASSETS" 2>/dev/null || true
  rm -rf "$DATA" "$TMP_ASSETS" 2>/dev/null || true
}
trap cleanup EXIT

# --- locate tools ---
command -v go >/dev/null || { echo "error: go not found"; exit 1; }
command -v node >/dev/null || { echo "error: node not found"; exit 1; }
command -v ffmpeg >/dev/null || { echo "error: ffmpeg not found"; exit 1; }
command -v ffprobe >/dev/null || { echo "error: ffprobe not found"; exit 1; }
command -v sqlite3 >/dev/null || { echo "error: sqlite3 not found"; exit 1; }
node -e "import('playwright-core')" >/dev/null 2>&1 || { echo "error: Playwright not installed; run: pnpm install"; exit 1; }
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

echo "› seeding demo page"
api() { curl -fsS -H "Authorization: Bearer $TOKEN" "$@"; }
UP=$(api --data-binary @scripts/demo-report.html \
  -H "Content-Type: text/html" \
  "http://localhost:$PORT/api/upload?filename=demo.html&visibility=public")
SLUG=$(printf '%s' "$UP" | sed -n 's/.*"slug":"\([^"]*\)".*/\1/p')
[ -n "$SLUG" ] || { echo "error: upload failed: $UP"; exit 1; }
# a second upload so the dashboard list looks real
printf '<!doctype html><h1>Release checklist</h1><p>v2 freeze tasks.</p>' | \
  api --data-binary @- -H "Content-Type: text/html" \
  "http://localhost:$PORT/api/upload?filename=release-checklist&visibility=public" >/dev/null

UPLOAD_ID=$(sqlite3 "$DATA/peek.db" "SELECT id FROM uploads WHERE slug='$SLUG'")
NOW=$(date +%s)

echo "› seeding existing review comments"
sqlite3 "$DATA/peek.db" <<SQL
INSERT INTO comments(upload_id,element_selector,element_text,anchor_kind,author_name,author_cookie,body,created_at)
VALUES
  ($UPLOAD_ID,'#demo-standfirst','generated reports, prototypes, build artifacts, and one-off HTML pages','text','Maya','seed-maya','This names the exact artifacts we review every week.',$((NOW - 5400))),
  ($UPLOAD_ID,'#demo-diagram','Stores the artifact Wraps it in a safe viewer Adds review tools Leaves an audit trail','element','Jordan','seed-jordan','The review-layer framing is useful. Keep this section near the top.',$((NOW - 3600)));
SQL

echo "› seeding fake page views for sparkline"
{
  for d in 0 1 2 3 4 5 6; do
    DAY=$((NOW - (6 - d) * 86400))
    N=$(( (d + 1) * 2 ))
    for v in $(seq 1 $N); do
      TS=$((DAY + v * 3600 + RANDOM % 7200))
      echo "INSERT INTO visits(upload_id,visitor_cookie,ip,user_agent,visited_at) VALUES($UPLOAD_ID,'demo-$((RANDOM%20))','','',$TS);"
    done
  done
} | sqlite3 "$DATA/peek.db"

echo "› recording demo"
BASE="http://localhost:$PORT" SLUG="$SLUG" VIDEO_RAW="$RAW_VIDEO" \
  CHROME="$CHROME" node scripts/gen-assets.mjs

echo "› encoding video (mp4)"
ffmpeg -y -i "$RAW_VIDEO" \
  -c:v libx264 -preset slow -crf 16 \
  -pix_fmt yuv420p \
  -vf "fps=60,scale=1920:1080:flags=lanczos,format=yuv420p" \
  -color_primaries bt709 -color_trc bt709 -colorspace bt709 \
  -movflags +faststart \
  "$OUT/demo.mp4" >/dev/null 2>&1

if [ -d "$DOCS_OUT" ]; then
  mkdir -p "$DOCS_OUT"
  cp "$OUT/demo.mp4" "$DOCS_OUT/demo.mp4"
  echo "› mirrored video to docs/public/assets/demo.mp4"
fi

echo "› encoding README animation (gif)"
PALETTE="$TMP_ASSETS/demo-palette.png"
ffmpeg -y -i "$OUT/demo.mp4" \
  -vf "fps=15,scale=1100:-1:flags=lanczos,palettegen=stats_mode=diff" \
  "$PALETTE" >/dev/null 2>&1
ffmpeg -y -i "$OUT/demo.mp4" -i "$PALETTE" \
  -lavfi "fps=15,scale=1100:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=3:diff_mode=rectangle" \
  "$OUT/demo.gif" >/dev/null 2>&1

DURATION="$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$OUT/demo.mp4")"
echo "✓ wrote assets/demo.mp4 (${DURATION}s)"
echo "✓ wrote assets/demo.gif"
