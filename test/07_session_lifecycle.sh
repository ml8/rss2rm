#!/bin/bash
# Test 7: Session lifecycle.
#
# Verifies:
#   - /auth/me returns user info for valid token
#   - /health is accessible without auth (public)
#   - Logout returns 204 and invalidates the token
#   - Subsequent requests with invalidated token return 401
source "$(dirname "$0")/helpers.sh"
echo "Test 7: Session lifecycle"

build
fresh_db
cli user add --email=user@test.com --password=password123 >/dev/null 2>&1
start_server

api POST /auth/login "" '{"email":"user@test.com","password":"password123"}'
TOKEN=$(echo "$HTTP_BODY" | json_field "['token']")

# 7a: /auth/me returns user info. Expected: 200, JSON with email.
api GET /auth/me "$TOKEN"
assert_status "200" "/auth/me with valid token"
assert_contains "$HTTP_BODY" "user@test.com" "/auth/me returns email"

# 7b: Health is public. Expected: 200, {"status":"ok"}.
api GET /health ""
assert_status "200" "/health without auth"
assert_contains "$HTTP_BODY" '"ok"' "Health returns ok"

# 7c: Logout. Expected: 204.
api POST /auth/logout "$TOKEN"
assert_status "204" "Logout returns 204"

# 7d: Token invalidated. Expected: 401.
api GET /feeds "$TOKEN"
assert_status "401" "Token rejected after logout"

# 7e: /auth/me also rejects. Expected: 401.
api GET /auth/me "$TOKEN"
assert_status "401" "/auth/me rejected after logout"

results
