#!/bin/bash
# Test 1: Build and unit tests.
#
# Verifies:
#   - Go binary compiles without errors
#   - All unit tests pass
#   - Web frontend builds
source "$(dirname "$0")/helpers.sh"
echo "Test 1: Build and unit tests"

cd "$(dirname "$0")/.."

echo "  Building Go binary..."
OUTPUT=$(go build -o rss2rm ./cmd/rss2rm 2>&1)
assert_equals "$?" "0" "Go binary compiles"

echo "  Running Go tests..."
OUTPUT=$(go test ./... 2>&1)
STATUS=$?
assert_equals "$STATUS" "0" "All unit tests pass"
if [ "$STATUS" -ne 0 ]; then
  echo "$OUTPUT"
fi

echo "  Building web frontend..."
OUTPUT=$(cd web && npm run build 2>&1)
STATUS=$?
assert_equals "$STATUS" "0" "Frontend builds"

results
