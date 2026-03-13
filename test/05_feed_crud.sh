#!/bin/bash
# Test 5: Feed CRUD and delivery configuration.
#
# Verifies:
#   - Add feed returns 201
#   - Feed appears in list with correct fields
#   - Edit feed name, directory, retain
#   - Toggle individual delivery off (feed_delivery removed)
#   - Toggle individual delivery back on (feed_delivery recreated)
#   - Remove feed returns 204
source "$(dirname "$0")/helpers.sh"
echo "Test 5: Feed CRUD and delivery configuration"

build
fresh_db
cli user add --email=user@test.com --password=password123 >/dev/null 2>&1
start_server

api POST /auth/login "" '{"email":"user@test.com","password":"password123"}'
TOKEN=$(echo "$HTTP_BODY" | json_field "['token']")

# 5a: Add feed. Expected: 201.
api POST /feeds "$TOKEN" '{"url":"http://example.com/rss","name":"Test Feed"}'
assert_status "201" "Add feed"

# 5b: Feed in list. Expected: deliver_individually=true, retain=0.
api GET /feeds "$TOKEN"
assert_status "200" "List feeds"
FEED_ID=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
DELIVER=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['deliver_individually'])")
RETAIN=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['retain'])")
assert_equals "$DELIVER" "True" "New feed delivers individually by default"
assert_equals "$RETAIN" "0" "Default retain is 0 (unlimited)"

# 5c: Edit name, directory, retain. Expected: 200, fields updated.
api PUT "/feeds/$FEED_ID" "$TOKEN" '{"name":"Renamed","directory":"MyDir","retain":5}'
assert_status "200" "Edit feed"

api GET /feeds "$TOKEN"
NAME=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['name'])")
DIR=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['directory'])")
RETAIN=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['retain'])")
assert_equals "$NAME" "Renamed" "Name updated"
assert_equals "$DIR" "MyDir" "Directory updated"
assert_equals "$RETAIN" "5" "Retain updated"

# 5d: Disable individual delivery. Expected: deliver_individually=false.
api PUT "/feeds/$FEED_ID" "$TOKEN" '{"deliver_individually":false}'
assert_status "200" "Disable individual delivery"

api GET /feeds "$TOKEN"
DELIVER=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['deliver_individually'])")
assert_equals "$DELIVER" "False" "Individual delivery disabled"

# 5e: Re-enable with new directory.
api PUT "/feeds/$FEED_ID" "$TOKEN" '{"deliver_individually":true,"directory":"NewDir"}'
api GET /feeds "$TOKEN"
DELIVER=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['deliver_individually'])")
DIR=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['directory'])")
assert_equals "$DELIVER" "True" "Individual delivery re-enabled"
assert_equals "$DIR" "NewDir" "Directory set on re-enable"

# 5f: Remove feed. Expected: 204, list empty.
api DELETE "/feeds/$FEED_ID" "$TOKEN"
assert_status "204" "Remove feed"

api GET /feeds "$TOKEN"
COUNT=$(echo "$HTTP_BODY" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if d else 0)")
assert_equals "$COUNT" "0" "Feed list empty after removal"

results
