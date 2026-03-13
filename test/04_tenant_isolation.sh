#!/bin/bash
# Test 4: Tenant isolation.
#
# Verifies:
#   - Two users each see only their own feeds
#   - Two users each see only their own destinations
#   - Unauthenticated requests are rejected with 401
#   - Bogus token is rejected with 401
source "$(dirname "$0")/helpers.sh"
echo "Test 4: Tenant isolation"

build
fresh_db
cli user add --email=alice@test.com --password=password123 >/dev/null 2>&1
cli user add --email=bob@test.com --password=password456 >/dev/null 2>&1
start_server

# Get tokens for both users.
api POST /auth/login "" '{"email":"alice@test.com","password":"password123"}'
ALICE_TOKEN=$(echo "$HTTP_BODY" | json_field "['token']")
api POST /auth/login "" '{"email":"bob@test.com","password":"password456"}'
BOB_TOKEN=$(echo "$HTTP_BODY" | json_field "['token']")

# Each user adds a feed.
api POST /feeds "$ALICE_TOKEN" '{"url":"http://example.com/alice-feed"}'
api POST /feeds "$BOB_TOKEN" '{"url":"http://example.com/bob-feed"}'

# 4a: Alice sees only her feed.
# Expected: 1 feed with URL containing "alice".
api GET /feeds "$ALICE_TOKEN"
assert_status "200" "Alice can list feeds"
COUNT=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
assert_equals "$COUNT" "1" "Alice sees exactly 1 feed"
assert_contains "$HTTP_BODY" "alice-feed" "Alice sees her own feed"

# 4b: Bob sees only his feed.
# Expected: 1 feed with URL containing "bob".
api GET /feeds "$BOB_TOKEN"
COUNT=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
assert_equals "$COUNT" "1" "Bob sees exactly 1 feed"
assert_contains "$HTTP_BODY" "bob-feed" "Bob sees his own feed"

# 4c: Alice adds a destination. Bob can't see it.
# Expected: Alice has 1 destination, Bob has 0.
api POST /destinations "$ALICE_TOKEN" '{"name":"Alice Dest","type":"file","config":{"path":"/tmp/a"}}'

api GET /destinations "$ALICE_TOKEN"
ALICE_DESTS=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
api GET /destinations "$BOB_TOKEN"
BOB_DESTS=$(echo "$HTTP_BODY" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if d else 0)")
assert_equals "$ALICE_DESTS" "1" "Alice sees her destination"
assert_equals "$BOB_DESTS" "0" "Bob sees no destinations"

# 4d: No auth header → 401.
api GET /feeds ""
assert_status "401" "No auth header rejected"

# 4e: Bogus token → 401.
api GET /feeds "bogus-token-12345"
assert_status "401" "Bogus token rejected"

results
