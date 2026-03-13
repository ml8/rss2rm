#!/bin/bash
# Test 3: Authentication flows.
#
# Verifies:
#   - Login with correct credentials returns 200 + token
#   - Login with wrong password returns 401
#   - Login with non-existent email returns 401 (no user enumeration)
#   - Registration returns 201 + token (auto-login)
#   - Duplicate registration returns 409
#   - Empty/short password rejected with 400
#   - Malformed JSON returns 400
source "$(dirname "$0")/helpers.sh"
echo "Test 3: Authentication flows"

build
fresh_db
cli user add --email=existing@test.com --password=password123 >/dev/null 2>&1
start_server

# 3a: Login success.
# Expected: 200, JSON with "token" field.
api POST /auth/login "" '{"email":"existing@test.com","password":"password123"}'
assert_status "200" "Login with correct credentials"
assert_contains "$HTTP_BODY" '"token"' "Response contains token"

# 3b: Wrong password.
# Expected: 401, generic error (no info leak about whether email exists).
api POST /auth/login "" '{"email":"existing@test.com","password":"wrongpass"}'
assert_status "401" "Login with wrong password"

# 3c: Non-existent email.
# Expected: 401, same generic error as wrong password.
api POST /auth/login "" '{"email":"nobody@test.com","password":"password123"}'
assert_status "401" "Login with non-existent email"

# 3d: Registration.
# Expected: 201, JSON with token (auto-login after registration).
api POST /auth/register "" '{"email":"newuser@test.com","password":"password123"}'
assert_status "201" "Registration succeeds"
assert_contains "$HTTP_BODY" '"token"' "Registration returns token"

# 3e: Duplicate registration.
# Expected: 409.
api POST /auth/register "" '{"email":"existing@test.com","password":"password123"}'
assert_status "409" "Duplicate registration rejected"

# 3f: Short password.
# Expected: 400.
api POST /auth/register "" '{"email":"short@test.com","password":"abc"}'
assert_status "400" "Short password rejected"

# 3g: Malformed JSON.
# Expected: 400.
api POST /auth/login "" 'not-json'
assert_status "400" "Malformed JSON rejected"

results
