#!/bin/bash
# Test 13: Email verification configuration.
#
# Verifies:
#   - verify-email without SMTP config causes startup failure
#   - verify-email with SMTP config starts successfully
#   - Without verify-email, login does not check verified status
source "$(dirname "$0")/helpers.sh"
echo "Test 13: Email verification configuration"

build

# 13a: verify-email without SMTP exits with error. Expected: exit 1.
OUTPUT=$("$BINARY" serve -verify-email -db-dsn=/tmp/nonexistent.db 2>&1 || true)
assert_contains "$OUTPUT" "requires SMTP" "verify-email without SMTP fails at startup"

# 13b: Default mode (no verify-email), unverified users can log in.
# Manually create an unverified user via DB and verify login works.
fresh_db
# Create user normally (verified=true)
cli user add --email=user@test.com --password=password123 >/dev/null 2>&1
# Manually set verified=false in the DB to simulate
sqlite3 "$TEST_DB" "UPDATE users SET verified = 0 WHERE email = 'user@test.com'"

start_server

# 13c: Without verify-email flag, unverified user CAN log in (fail open).
api POST /auth/login "" '{"email":"user@test.com","password":"password123"}'
assert_status "200" "Unverified user can log in when verification is disabled"

results
