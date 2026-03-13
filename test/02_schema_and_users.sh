#!/bin/bash
# Test 2: Schema migration and user management.
#
# Verifies:
#   - Fresh database runs all migrations (v2 UUID, v3 tenant, v4 retention)
#   - User creation via CLI returns a UUID
#   - User list shows the created user
#   - Duplicate user creation fails
source "$(dirname "$0")/helpers.sh"
echo "Test 2: Schema migration and user management"

build
fresh_db

# Creating a user on a fresh DB should trigger all migrations.
# Expected: migrations v2, v3, v4 run; user gets UUID ID.
OUTPUT=$(cli user add --email=test@example.com --password=testpassword123 2>&1)
assert_contains "$OUTPUT" "User created" "User creation succeeds"
assert_contains "$OUTPUT" "test@example.com" "Output contains email"
# UUID format: 8-4-4-4-12 hex chars
assert_contains "$OUTPUT" "[0-9a-f]\{8\}-[0-9a-f]\{4\}-" "ID is a UUID"

# User list should show the user.
# Expected: table with ID, Email, Created columns; one row with test@example.com.
OUTPUT=$(cli user list 2>&1)
assert_contains "$OUTPUT" "test@example.com" "User appears in list"

# Duplicate user should fail.
# Expected: error message about duplicate.
OUTPUT=$(cli user add --email=test@example.com --password=anotherpass1 2>&1)
assert_contains "$OUTPUT" "Failed\|UNIQUE" "Duplicate user rejected"

results
