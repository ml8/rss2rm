#!/bin/bash
# Test 11: Password change.
#
# Verifies:
#   - Authenticated user can change their password
#   - Old password stops working after change
#   - New password works
#   - Wrong current password is rejected
#   - Short new password is rejected
source "$(dirname "$0")/helpers.sh"
echo "Test 11: Password change"

build
fresh_db
cli user add --email=user@test.com --password=oldpassword1 >/dev/null 2>&1
start_server

api POST /auth/login "" '{"email":"user@test.com","password":"oldpassword1"}'
TOKEN=$(echo "$HTTP_BODY" | json_field "['token']")

# 11a: Wrong current password rejected. Expected: 401.
api POST /auth/change-password "$TOKEN" '{"current_password":"wrongpass","new_password":"newpassword1"}'
assert_status "401" "Wrong current password rejected"

# 11b: Short new password rejected. Expected: 400.
api POST /auth/change-password "$TOKEN" '{"current_password":"oldpassword1","new_password":"short"}'
assert_status "400" "Short new password rejected"

# 11c: Successful password change. Expected: 200.
api POST /auth/change-password "$TOKEN" '{"current_password":"oldpassword1","new_password":"newpassword1"}'
assert_status "200" "Password changed successfully"

# 11d: Old password no longer works. Expected: 401.
api POST /auth/login "" '{"email":"user@test.com","password":"oldpassword1"}'
assert_status "401" "Old password rejected after change"

# 11e: New password works. Expected: 200.
api POST /auth/login "" '{"email":"user@test.com","password":"newpassword1"}'
assert_status "200" "New password accepted"

results
