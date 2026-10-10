#!/usr/bin/env bash
# Records the terminal session shown on the start page of the Pages site:
# starts upwatch, registers two URLs, waits for the first checks and prints
# the results. Commands are shown with :8080 (curl's short form for
# localhost); the script itself uses a spare port. Output (with ANSI colours) goes to site/src/hero-session.ansi.
#
#   scripts/hero-session.sh
set -euo pipefail
cd "$(dirname "$0")/.."

tmp="$(mktemp -d)"
trap 'kill "$pid" 2>/dev/null || true; rm -rf "$tmp"' EXIT

go build -o "$tmp/upwatch" ./cmd/upwatch
UPWATCH_ADDR=127.0.0.1:18089 UPWATCH_DB="$tmp/upwatch.db" UPWATCH_TICK=1s \
  "$tmp/upwatch" >"$tmp/log.txt" 2>&1 &
pid=$!
sleep 1

api=http://127.0.0.1:18089
prompt() { printf '\e[32m$\e[0m %s\n' "$1"; }

{
  prompt "curl -sd '{\"url\":\"https://example.com\"}' \\"
  printf '    :8080/targets | jq -c '"'"'{id, url}'"'"'\n'
  curl -s -d '{"url":"https://example.com"}' "$api/targets" | jq -c '{id, url}'
  prompt "curl -sd '{\"url\":\"https://nope.invalid\"}' \\"
  printf '    :8080/targets | jq -c '"'"'{id, url}'"'"'\n'
  curl -s -d '{"url":"https://nope.invalid"}' "$api/targets" | jq -c '{id, url}'
  sleep 2
  prompt "curl -s :8080/targets/1/results \\"
  printf '    | jq -c '"'"'.[0] | {status_code, duration_ms}'"'"'\n'
  curl -s "$api/targets/1/results" | jq -c '.[0] | {status_code, duration_ms}'
  prompt "curl -s :8080/targets/2/results | jq '.[0].up'"
  curl -s "$api/targets/2/results" | jq '.[0].up'
} >site/src/hero-session.ansi

cat site/src/hero-session.ansi
