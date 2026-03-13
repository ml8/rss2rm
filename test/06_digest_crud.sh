#!/bin/bash
# Test 6: Digest CRUD with retain.
#
# Verifies:
#   - Create digest with retain returns 201
#   - Digest appears in list with correct retain value
#   - Edit digest name, schedule, directory, retain
#   - Add/remove feed to/from digest
#   - Remove digest returns 204
source "$(dirname "$0")/helpers.sh"
echo "Test 6: Digest CRUD with retain"

build
fresh_db
cli user add --email=user@test.com --password=password123 >/dev/null 2>&1
start_server

api POST /auth/login "" '{"email":"user@test.com","password":"password123"}'
TOKEN=$(echo "$HTTP_BODY" | json_field "['token']")

# Add a feed for later association.
api POST /feeds "$TOKEN" '{"url":"http://example.com/rss"}'
api GET /feeds "$TOKEN"
FEED_ID=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")

# 6a: Create digest with retain. Expected: 201.
api POST /digests "$TOKEN" '{"name":"Morning","schedule":"07:00","retain":3}'
assert_status "201" "Create digest"
DIGEST_ID=$(echo "$HTTP_BODY" | json_field "['id']")
assert_contains "$DIGEST_ID" "[0-9a-f]" "Digest ID is a UUID"

# 6b: Digest in list with retain=3.
api GET /digests "$TOKEN"
assert_status "200" "List digests"
RETAIN=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['Retain'])")
assert_equals "$RETAIN" "3" "Retain is 3"

# 6c: Edit digest. Expected: 200, fields updated.
api PUT "/digests/$DIGEST_ID" "$TOKEN" '{"name":"Evening","schedule":"18:00","directory":"EveningDir","retain":7}'
assert_status "200" "Edit digest"

api GET /digests "$TOKEN"
NAME=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['Name'])")
SCHED=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['Schedule'])")
DIR=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['Directory'])")
RETAIN=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['Retain'])")
assert_equals "$NAME" "Evening" "Name updated"
assert_equals "$SCHED" "18:00" "Schedule updated"
assert_equals "$DIR" "EveningDir" "Directory updated"
assert_equals "$RETAIN" "7" "Retain updated"

# 6d: Add feed to digest. Expected: 200, 1 feed in digest.
api POST "/digests/$DIGEST_ID/feeds" "$TOKEN" "{\"feed_id\":\"$FEED_ID\",\"also_individual\":true}"
assert_status "200" "Add feed to digest"

api GET "/digests/$DIGEST_ID/feeds" "$TOKEN"
COUNT=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
assert_equals "$COUNT" "1" "Digest has 1 feed"

# 6e: Remove feed from digest. Expected: 204.
api DELETE "/digests/$DIGEST_ID/feeds/$FEED_ID" "$TOKEN"
assert_status "204" "Remove feed from digest"

api GET "/digests/$DIGEST_ID/feeds" "$TOKEN"
COUNT=$(echo "$HTTP_BODY" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if d else 0)")
assert_equals "$COUNT" "0" "Digest has 0 feeds after removal"

# 6f: Remove digest. Expected: 204.
api DELETE "/digests/$DIGEST_ID" "$TOKEN"
assert_status "204" "Remove digest"

api GET /digests "$TOKEN"
COUNT=$(echo "$HTTP_BODY" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if d else 0)")
assert_equals "$COUNT" "0" "Digest list empty after removal"

results
