#!/bin/bash
# Test 12: Pluggable destinations.
#
# Verifies:
#   - Default mode (remarkable only): file destination type is rejected
#   - With -destinations=remarkable,file: file destination type is accepted
#   - Unknown destination type in -destinations flag causes fatal error
source "$(dirname "$0")/helpers.sh"
echo "Test 12: Pluggable destinations"

build

# --- Default: only remarkable ---
fresh_db
cli user add --email=user@test.com --password=password123 >/dev/null 2>&1

# Start server with default destinations (remarkable only — no helper, which enables all).
SERVER_PORT=$((20000 + RANDOM % 10000))
"$BINARY" serve -port "$SERVER_PORT" -db-dsn="$TEST_DB" >/dev/null 2>&1 &
SERVER_PID=$!
sleep 2

api POST /auth/login "" '{"email":"user@test.com","password":"password123"}'
TOKEN=$(echo "$HTTP_BODY" | json_field "['token']")

# 12a: File destination rejected (not enabled). Expected: 400.
api POST /destinations "$TOKEN" '{"name":"Files","type":"file","config":{"path":"/tmp/x"}}'
assert_status "400" "File destination rejected when not enabled"
assert_contains "$HTTP_BODY" "nknown destination" "Error mentions unknown type"

kill "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null
SERVER_PID=""
rm -f "$TEST_DB"

# --- With file enabled ---
fresh_db
cli user add --email=user@test.com --password=password123 >/dev/null 2>&1
SERVER_PORT=$((20000 + RANDOM % 10000))
"$BINARY" serve -port "$SERVER_PORT" -db-dsn="$TEST_DB" \
  -destinations=remarkable,file >/dev/null 2>&1 &
SERVER_PID=$!
sleep 2

api POST /auth/login "" '{"email":"user@test.com","password":"password123"}'
TOKEN=$(echo "$HTTP_BODY" | json_field "['token']")

# 12b: File destination accepted when enabled. Expected: 201.
api POST /destinations "$TOKEN" '{"name":"Files","type":"file","config":{"path":"/tmp/x"}}'
assert_status "201" "File destination accepted when enabled"

kill "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null
SERVER_PID=""
rm -f "$TEST_DB"

# --- Unknown type in flag ---
# 12c: Expected: process exits with error.
OUTPUT=$("$BINARY" serve -destinations=remarkable,faketype -db-dsn=/tmp/nonexistent.db 2>&1 || true)
assert_contains "$OUTPUT" "Unknown destination type" "Unknown type in flag causes error"

results
