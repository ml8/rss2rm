#!/bin/bash
# Test 10: Registration control.
#
# Verifies:
#   - registration=closed rejects all public registration
#   - registration=allowlist rejects emails not on the list
#   - registration=allowlist accepts emails on the list
#   - registration=open (default) accepts anyone
source "$(dirname "$0")/helpers.sh"
echo "Test 10: Registration control"

build

# --- Test closed mode ---
fresh_db
CLOSED_PORT=$((20000 + RANDOM % 10000))
"$BINARY" serve -port "$CLOSED_PORT" -db-dsn="$TEST_DB" \
  -registration=closed -destinations=remarkable >/dev/null 2>&1 &
CLOSED_PID=$!
sleep 2

# 10a: Registration rejected when closed. Expected: 403.
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://localhost:${CLOSED_PORT}/api/v1/auth/register" \
  -H 'Content-Type: application/json' -d '{"email":"anyone@test.com","password":"password123"}')
assert_equals "$HTTP_CODE" "403" "Closed mode rejects registration"

kill "$CLOSED_PID" 2>/dev/null; wait "$CLOSED_PID" 2>/dev/null
rm -f "$TEST_DB"

# --- Test allowlist mode ---
fresh_db
ALLOW_PORT=$((20000 + RANDOM % 10000))
"$BINARY" serve -port "$ALLOW_PORT" -db-dsn="$TEST_DB" \
  -registration=allowlist -registration-allowlist="allowed@test.com,also@test.com" \
  -destinations=remarkable >/dev/null 2>&1 &
ALLOW_PID=$!
sleep 2

# 10b: Rejected email not on allowlist. Expected: 403.
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://localhost:${ALLOW_PORT}/api/v1/auth/register" \
  -H 'Content-Type: application/json' -d '{"email":"stranger@test.com","password":"password123"}')
assert_equals "$HTTP_CODE" "403" "Allowlist rejects non-listed email"

# 10c: Accepted email on allowlist. Expected: 201.
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://localhost:${ALLOW_PORT}/api/v1/auth/register" \
  -H 'Content-Type: application/json' -d '{"email":"allowed@test.com","password":"password123"}')
assert_equals "$HTTP_CODE" "201" "Allowlist accepts listed email"

kill "$ALLOW_PID" 2>/dev/null; wait "$ALLOW_PID" 2>/dev/null
rm -f "$TEST_DB"

# --- Test open mode (default) ---
fresh_db
SERVER_PORT=$((20000 + RANDOM % 10000))
start_server

# 10d: Anyone can register in open mode. Expected: 201.
api POST /auth/register "" '{"email":"anyone@test.com","password":"password123"}'
assert_status "201" "Open mode accepts registration"

results
