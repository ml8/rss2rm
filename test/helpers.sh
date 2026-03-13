#!/bin/bash
# Shared helpers for e2e tests.
# Source this file from test scripts: source "$(dirname "$0")/helpers.sh"

set -euo pipefail

BINARY="$(cd "$(dirname "$0")/.." && pwd)/rss2rm"
TEST_DB=""
SERVER_PID=""
SERVER_PORT=""
PASS_COUNT=0
FAIL_COUNT=0
HTTP_CODE=""
HTTP_BODY=""

# Colors (disabled if not a terminal)
if [ -t 1 ]; then
  GREEN='\033[0;32m'; RED='\033[0;31m'; RESET='\033[0m'
else
  GREEN=''; RED=''; RESET=''
fi

# Build the binary if needed.
build() {
  (cd "$(dirname "$0")/.." && go build -o rss2rm ./cmd/rss2rm) 2>&1
}

# Create a fresh temp database. Sets TEST_DB.
fresh_db() {
  TEST_DB=$(mktemp /tmp/rss2rm-e2e-XXXXXX.db)
}

# Start the server on a random high port. Sets SERVER_PID and SERVER_PORT.
start_server() {
  SERVER_PORT=$((20000 + RANDOM % 10000))
  "$BINARY" serve -port "$SERVER_PORT" -db-dsn="$TEST_DB" -destinations=remarkable,file,email,gcp,gmail,dropbox,notion >/dev/null 2>&1 &
  SERVER_PID=$!
  sleep 2
  # Verify server started
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "FATAL: server failed to start" >&2
    exit 1
  fi
}

# Stop the server and remove the temp database.
cleanup() {
  if [ -n "$SERVER_PID" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
    SERVER_PID=""
  fi
  if [ -n "$TEST_DB" ] && [ -f "$TEST_DB" ]; then
    rm -f "$TEST_DB"
    TEST_DB=""
  fi
}
trap cleanup EXIT

# Run a CLI command against the test database.
cli() {
  "$BINARY" -db-dsn="$TEST_DB" "$@" 2>/dev/null
}

# Make an HTTP request. Sets HTTP_CODE and HTTP_BODY globals.
# Always call directly (not in $(...) subshell) so globals are visible.
# Usage: api GET /feeds [auth_token] [json_body]
api() {
  local method="$1" path="$2" token="${3:-}" body="${4:-}"
  local url="http://localhost:${SERVER_PORT}/api/v1${path}"
  local tmpfile
  tmpfile=$(mktemp)
  local args=(-s -o "$tmpfile" -w '%{http_code}' -X "$method")

  if [ -n "$token" ]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  if [ -n "$body" ]; then
    args+=(-H 'Content-Type: application/json' -d "$body")
  fi

  HTTP_CODE=$(curl "${args[@]}" "$url")
  HTTP_BODY=$(cat "$tmpfile")
  rm -f "$tmpfile"
}

# Extract a JSON field from stdin. Uses python3 (available on macOS and most Linux).
json_field() {
  python3 -c "import sys,json; print(json.load(sys.stdin)$1)"
}

# Assert HTTP status code equals expected.
# Usage: assert_status 200 "description"
assert_status() {
  local expected="$1" desc="$2"
  if [ "$HTTP_CODE" = "$expected" ]; then
    echo -e "  ${GREEN}PASS${RESET} $desc (HTTP $HTTP_CODE)"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo -e "  ${RED}FAIL${RESET} $desc (expected HTTP $expected, got $HTTP_CODE)"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

# Assert a string contains a substring.
# Usage: assert_contains "$output" "substring" "description"
assert_contains() {
  local haystack="$1" needle="$2" desc="$3"
  if echo "$haystack" | grep -q "$needle"; then
    echo -e "  ${GREEN}PASS${RESET} $desc"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo -e "  ${RED}FAIL${RESET} $desc (expected to contain '$needle')"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

# Assert a string equals expected.
# Usage: assert_equals "$actual" "expected" "description"
assert_equals() {
  local actual="$1" expected="$2" desc="$3"
  if [ "$actual" = "$expected" ]; then
    echo -e "  ${GREEN}PASS${RESET} $desc"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo -e "  ${RED}FAIL${RESET} $desc (expected '$expected', got '$actual')"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

# Print test results summary and exit with appropriate code.
results() {
  echo ""
  local total=$((PASS_COUNT + FAIL_COUNT))
  if [ "$FAIL_COUNT" -eq 0 ]; then
    echo -e "${GREEN}All $total tests passed.${RESET}"
  else
    echo -e "${RED}$FAIL_COUNT of $total tests failed.${RESET}"
  fi
  exit "$FAIL_COUNT"
}
