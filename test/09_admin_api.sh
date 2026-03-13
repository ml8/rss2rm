#!/bin/bash
# Test 9: Admin API.
#
# Verifies:
#   - Admin index page returns 200
#   - Admin can create a user (verified by default)
#   - Admin can list users
#   - Admin-created user can log in via public API
#   - Admin can delete a user (cascade)
#   - Deleted user can no longer log in
source "$(dirname "$0")/helpers.sh"
echo "Test 9: Admin API"

build
fresh_db

# Start server with custom admin port.
SERVER_PORT=$((20000 + RANDOM % 10000))
ADMIN_PORT=$((SERVER_PORT + 1))
"$BINARY" serve -port "$SERVER_PORT" -admin-port "$ADMIN_PORT" \
  -db-dsn="$TEST_DB" -destinations=remarkable,file >/dev/null 2>&1 &
SERVER_PID=$!
sleep 2

# Helper for admin API calls (no auth, different port).
api_admin() {
  local method="$1" path="$2" body="${3:-}"
  local tmpfile=$(mktemp)
  local args=(-s -o "$tmpfile" -w '%{http_code}' -X "$method")
  if [ -n "$body" ]; then
    args+=(-H 'Content-Type: application/json' -d "$body")
  fi
  HTTP_CODE=$(curl "${args[@]}" "http://localhost:${ADMIN_PORT}${path}")
  HTTP_BODY=$(cat "$tmpfile"); rm -f "$tmpfile"
}

# 9a: Admin index page. Expected: 200.
api_admin GET /admin/
assert_status "200" "Admin index page accessible"

# 9b: Create user via admin. Expected: 201 with UUID.
api_admin POST /admin/users '{"email":"admin-created@test.com","password":"password123"}'
assert_status "201" "Admin creates user"
USER_ID=$(echo "$HTTP_BODY" | json_field "['id']")
assert_contains "$USER_ID" "[0-9a-f]" "Created user has UUID"

# 9c: List users. Expected: 200, user is verified.
api_admin GET /admin/users
assert_status "200" "Admin lists users"
VERIFIED=$(echo "$HTTP_BODY" | python3 -c "
import sys,json
users=json.load(sys.stdin)
u = [x for x in users if x['email']=='admin-created@test.com']
print(u[0]['verified'] if u else 'NOT_FOUND')")
assert_equals "$VERIFIED" "True" "Admin-created user is verified"

# 9d: Created user can log in via public API.
api POST /auth/login "" '{"email":"admin-created@test.com","password":"password123"}'
assert_status "200" "Admin-created user can log in"

# 9e: Delete user via admin. Expected: 204.
api_admin DELETE "/admin/users/$USER_ID"
assert_status "204" "Admin deletes user"

# 9f: Deleted user can no longer log in.
api POST /auth/login "" '{"email":"admin-created@test.com","password":"password123"}'
assert_status "401" "Deleted user cannot log in"

results
