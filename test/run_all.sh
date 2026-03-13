#!/bin/bash
# End-to-end test harness. Runs all test scripts in order.
# Usage: ./test/run_all.sh
#
# Each script is independent (own DB, own server). A failure in one
# does not prevent others from running. Exit code is non-zero if any
# test failed.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TOTAL_PASS=0
TOTAL_FAIL=0
FAILED_TESTS=""

echo "========================================"
echo "  rss2rm end-to-end tests"
echo "========================================"
echo ""

for test_script in "$SCRIPT_DIR"/[0-9]*.sh; do
  name=$(basename "$test_script")
  echo "--- $name ---"
  if bash "$test_script"; then
    TOTAL_PASS=$((TOTAL_PASS + 1))
  else
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    FAILED_TESTS="$FAILED_TESTS $name"
  fi
  echo ""
done

echo "========================================"
TOTAL=$((TOTAL_PASS + TOTAL_FAIL))
if [ "$TOTAL_FAIL" -eq 0 ]; then
  echo "  All $TOTAL test suites passed."
else
  echo "  $TOTAL_FAIL of $TOTAL test suites failed:$FAILED_TESTS"
fi
echo "========================================"
exit "$TOTAL_FAIL"
