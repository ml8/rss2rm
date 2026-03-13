#!/bin/bash
# Test 8: CLI local mode.
#
# Verifies:
#   - CLI auto-creates a local@localhost user on first run
#   - CLI add/list/edit/digest commands work with local user
#   - Retain field shows in digest list
source "$(dirname "$0")/helpers.sh"
echo "Test 8: CLI local mode"

build
fresh_db

# 8a: First CLI command auto-creates local user.
# Expected: feed added, local@localhost user exists.
OUTPUT=$(cli add --url="http://example.com/feed" --name="CLI Feed" 2>&1)
assert_contains "$OUTPUT" "Successfully added" "CLI add feed succeeds"

OUTPUT=$(cli user list 2>&1)
assert_contains "$OUTPUT" "local@localhost" "Local user auto-created"

# 8b: List shows the feed.
# Expected: table with CLI Feed.
OUTPUT=$(cli list 2>&1)
assert_contains "$OUTPUT" "CLI Feed" "CLI list shows feed"
assert_contains "$OUTPUT" "example.com/feed" "CLI list shows URL"

# 8c: Edit feed name.
FEED_ID=$(cli list 2>&1 | grep "CLI Feed" | awk '{print $1}')
OUTPUT=$(cli edit "$FEED_ID" --name="Renamed Feed" 2>&1)
assert_contains "$OUTPUT" "updated" "CLI edit succeeds"

OUTPUT=$(cli list 2>&1)
assert_contains "$OUTPUT" "Renamed Feed" "CLI list shows renamed feed"

# 8d: Digest add with retain.
# Expected: digest created with retain shown.
OUTPUT=$(cli digest add --name="Daily" --schedule=09:00 --retain=3 2>&1)
assert_contains "$OUTPUT" "Digest added" "CLI digest add succeeds"

# 8e: Digest list shows retain.
# Expected: retain column shows 3.
OUTPUT=$(cli digest list 2>&1)
assert_contains "$OUTPUT" "Daily" "Digest name in list"
assert_contains "$OUTPUT" "3" "Retain value in list"

results
