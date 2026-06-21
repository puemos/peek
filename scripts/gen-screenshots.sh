#!/usr/bin/env bash
# gen-screenshots.sh - regenerate Peek's static app screenshots.
#
# Builds the binaries, boots a throwaway Peek server, seeds a demo report,
# comments, visits, and dashboard data, then drives headless Chromium to write
# exact 1270x760 PNG screenshots to assets/screenshots/.
#
# Requires: go, node (>=20), pnpm install, sqlite3, and Google Chrome /
# Chromium. Override Chrome with:
#   CHROME="/path/to/chrome" scripts/gen-screenshots.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUT="$ROOT/assets/screenshots"
PORT="${PEEK_SCREENSHOT_PORT:-7798}"
BASE="http://localhost:$PORT"
DATA="$(mktemp -d)"
COOKIE="$DATA/cookies.txt"
SRV_PID=""

cleanup() {
  [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null || true
  sleep 0.3
  chmod -R u+w "$DATA" 2>/dev/null || true
  rm -rf "$DATA" 2>/dev/null || true
}
trap cleanup EXIT

command -v go >/dev/null || { echo "error: go not found"; exit 1; }
command -v node >/dev/null || { echo "error: node not found"; exit 1; }
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
[ -n "${CHROME:-}" ] || { echo "error: Chrome/Chromium not found (set CHROME=...)"; exit 1; }

mkdir -p "$OUT"
rm -f "$OUT"/*.png

echo "-> building binaries"
go build -o bin/peekd ./cmd/peekd
go build -o bin/peek ./cmd/peek

echo "-> starting peekd on :$PORT"
./bin/peekd --addr ":$PORT" --data "$DATA" --base-url "$BASE" >/dev/null 2>&1 &
SRV_PID=$!
for _ in $(seq 1 50); do
  curl -fsS "$BASE/" >/dev/null 2>&1 && break
  sleep 0.1
done

csrf_from_html() {
  sed -n 's/.*name="csrf" value="\([^"]*\)".*/\1/p' | head -n1
}

json_value() {
  key="$1"
  sed -n "s/.*\"$key\":\"\\([^\"]*\\)\".*/\\1/p"
}

echo "-> creating first admin + CLI token"
SETUP_CODE="$(tr -d '\n\r' < "$DATA/setup.key")"
SETUP_HTML="$(curl -fsS -c "$COOKIE" "$BASE/setup?code=$SETUP_CODE")"
SETUP_CSRF="$(printf '%s' "$SETUP_HTML" | csrf_from_html)"
curl -fsS -b "$COOKIE" -c "$COOKIE" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "email=admin@example.com" \
  --data-urlencode "name=Admin" \
  --data-urlencode "password=asset-admin-password" \
  --data-urlencode "code=$SETUP_CODE" \
  --data-urlencode "csrf=$SETUP_CSRF" \
  "$BASE/setup" >/dev/null

START="$(curl -fsS -X POST "$BASE/api/cli/login/start")"
DEVICE_CODE="$(printf '%s' "$START" | json_value device_code)"
USER_CODE="$(printf '%s' "$START" | json_value user_code)"
APPROVE_HTML="$(curl -fsS -b "$COOKIE" -c "$COOKIE" "$BASE/cli-login/$USER_CODE")"
APPROVE_CSRF="$(printf '%s' "$APPROVE_HTML" | csrf_from_html)"
curl -fsS -b "$COOKIE" -c "$COOKIE" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "csrf=$APPROVE_CSRF" \
  --data-urlencode "decision=approve" \
  "$BASE/cli-login/$USER_CODE" >/dev/null
POLL="$(curl -fsS -H "Content-Type: application/json" \
  -d "{\"device_code\":\"$DEVICE_CODE\"}" \
  "$BASE/api/cli/login/poll")"
TOKEN="$(printf '%s' "$POLL" | json_value token)"
[ -n "$TOKEN" ] || { echo "error: token setup failed: $POLL"; exit 1; }

api() { curl -fsS -H "Authorization: Bearer $TOKEN" "$@"; }

echo "-> seeding uploads"
UP="$(api --data-binary @scripts/demo-report.html \
  -H "Content-Type: text/html" \
  "$BASE/api/upload?filename=codebase-health-report.html")"
SLUG="$(printf '%s' "$UP" | json_value slug)"
[ -n "$SLUG" ] || { echo "error: upload failed: $UP"; exit 1; }

printf '<!doctype html><h1>Release checklist</h1><p>v2 freeze tasks for reviewers.</p>' | \
  api --data-binary @- -H "Content-Type: text/html" \
  "$BASE/api/upload?filename=release-checklist.html&password=review" >/dev/null
printf '<!doctype html><h1>Design RFC</h1><p>New dashboard navigation proposal.</p>' | \
  api --data-binary @- -H "Content-Type: text/html" \
  "$BASE/api/upload?filename=dashboard-navigation-rfc.html" >/dev/null
printf '<!doctype html><h1>Incident follow-up</h1><p>Action items from the cache incident.</p>' | \
  api --data-binary @- -H "Content-Type: text/html" \
  "$BASE/api/upload?filename=incident-follow-up.html" >/dev/null

UPLOAD_ID="$(sqlite3 "$DATA/peek.db" "SELECT id FROM uploads WHERE slug='$SLUG'")"
NOW="$(date +%s)"

echo "-> seeding comments, visits, accounts, and settings"
sqlite3 "$DATA/peek.db" <<SQL
INSERT INTO comments(upload_id,element_selector,element_text,anchor_kind,author_name,author_cookie,body,created_at)
VALUES
  ($UPLOAD_ID,'#summary','two regressions need attention before the next release','text','Maya','seed-maya','This is the right headline. Can we make the release blocker explicit for the infra team?',$((NOW - 5400))),
  ($UPLOAD_ID,'#callout','Agent note: the latency regression is fully reproducible. A targeted fix to the cache key derivation restores p95 to ~226ms in local benchmarks.','element','Jordan','seed-jordan','Good evidence. Please keep this note attached when the report is exported.',$((NOW - 3600))),
  ($UPLOAD_ID,'#issues','1 high severity','element','Sam','seed-sam','Can we add owner and due date here?',$((NOW - 1800)));

INSERT INTO accounts(email,name,password_hash,is_admin,disabled,created_at,updated_at)
VALUES
  ('maya@example.com','Maya Chen','',0,0,$((NOW - 86400)),$((NOW - 86400))),
  ('contractor@example.com','Contractor','',0,1,$((NOW - 172800)),$((NOW - 3600)));

INSERT INTO settings(key,value,updated_at) VALUES
  ('auth_token_login_enabled','true',$NOW),
  ('oauth_google_enabled','true',$NOW),
  ('oauth_google_client_id','peek-internal-google-client',$NOW),
  ('oauth_google_client_secret','configured',$NOW),
  ('oauth_github_enabled','true',$NOW),
  ('oauth_github_client_id','peek-internal-github-client',$NOW),
  ('oauth_github_client_secret','configured',$NOW),
  ('max_upload','8388608',$NOW),
  ('max_total_size','1073741824',$NOW),
  ('max_uploads_per_token','250',$NOW),
  ('max_storage_per_token','268435456',$NOW),
  ('retention_days','30',$NOW),
  ('storage','file',$NOW)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at;
SQL

{
  for d in 0 1 2 3 4 5 6; do
    AGE_DAYS=$((6 - d))
    N=$(((d + 2) * 3))
    for v in $(seq 1 "$N"); do
      TS=$((NOW - AGE_DAYS * 86400 - (N - v + 1) * 1800 - RANDOM % 900))
      NAME=""
      [ $((v % 5)) -eq 0 ] && NAME="Maya"
      HASH="$(printf '%012x' $((RANDOM * RANDOM)))"
      UA="Mozilla/5.0 internal reviewer"
      echo "INSERT INTO visits(upload_id,visitor_cookie,visitor_name,ip,user_agent,visited_at) VALUES($UPLOAD_ID,'demo-$((RANDOM%24))','$NAME','$HASH','$UA',$TS);"
    done
  done
} | sqlite3 "$DATA/peek.db"

DASH_HTML="$(curl -fsS -b "$COOKIE" -c "$COOKIE" "$BASE/dashboard")"
DASH_CSRF="$(printf '%s' "$DASH_HTML" | csrf_from_html)"
curl -fsS -b "$COOKIE" -c "$COOKIE" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "email=reviewer@example.com" \
  --data-urlencode "csrf=$DASH_CSRF" \
  "$BASE/dashboard/invites" >/dev/null

echo "-> capturing screenshots"
BASE="$BASE" SLUG="$SLUG" OUT="$OUT" COOKIE_FILE="$COOKIE" CHROME="$CHROME" \
  node scripts/gen-screenshots.mjs

echo "wrote screenshots to assets/screenshots/"
