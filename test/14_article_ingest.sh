#!/bin/bash
# Test article ingest API and webhook CRUD.
source "$(dirname "$0")/helpers.sh"

echo "=== Article Ingest & Webhooks ==="

fresh_db
cli user add --email=ingest@test.com --password=password123 >/dev/null
start_server

# Authenticate
api POST /auth/login "" '{"email":"ingest@test.com","password":"password123"}'
assert_status "200" "Login"
TOKEN=$(echo "$HTTP_BODY" | json_field "['token']")

# --- Article ingest (immediate delivery requires a destination, so test validation) ---

# Missing both url and content
api POST /articles "$TOKEN" '{"title":"Test"}'
assert_status "400" "Ingest: reject missing url and content"

# Missing title when no content
api POST /articles "$TOKEN" '{"url":"https://example.com"}'
assert_status "400" "Ingest: reject missing title without content"
assert_contains "$HTTP_BODY" "title is required" "Ingest: error message mentions title"

# Valid request but no destination configured → should fail gracefully
api POST /articles "$TOKEN" '{"title":"Test Article","url":"https://example.com","content":"<p>Hello</p>"}'
assert_status "500" "Ingest: fails without destination"
assert_contains "$HTTP_BODY" "destination" "Ingest: error mentions destination"

# --- Webhook CRUD ---

# List webhooks (empty)
api GET /webhooks "$TOKEN"
assert_status "200" "List webhooks (empty)"
assert_contains "$HTTP_BODY" '\[\]' "Webhooks list is empty array"

# Add webhook with unsupported type
api POST /webhooks "$TOKEN" '{"type":"unknown","secret":"x"}'
assert_status "400" "Reject unsupported webhook type"

# Add webhook without secret
api POST /webhooks "$TOKEN" '{"type":"miniflux"}'
assert_status "400" "Reject missing secret"

# Add miniflux webhook
api POST /webhooks "$TOKEN" '{"type":"miniflux","secret":"test-secret-for-e2e-news","config":"{\"directory\":\"News\"}"}'
assert_status "201" "Create miniflux webhook"
assert_contains "$HTTP_BODY" "miniflux" "Webhook response contains type"
WEBHOOK_ID=$(echo "$HTTP_BODY" | json_field "['ID']")

# List webhooks (should have 1)
api GET /webhooks "$TOKEN"
assert_status "200" "List webhooks (1 webhook)"
WEBHOOK_COUNT=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
assert_equals "$WEBHOOK_COUNT" "1" "Webhook count is 1"

# Add a second webhook
api POST /webhooks "$TOKEN" '{"type":"miniflux","secret":"second-test-secret"}'
assert_status "201" "Create second webhook"
WEBHOOK2_ID=$(echo "$HTTP_BODY" | json_field "['ID']")

# List should show 2
api GET /webhooks "$TOKEN"
WEBHOOK_COUNT=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
assert_equals "$WEBHOOK_COUNT" "2" "Webhook count is 2"

# Delete first webhook
api DELETE "/webhooks/$WEBHOOK_ID" "$TOKEN"
assert_status "204" "Delete first webhook"

# List should show 1
api GET /webhooks "$TOKEN"
WEBHOOK_COUNT=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
assert_equals "$WEBHOOK_COUNT" "1" "Webhook count is 1 after delete"

# Delete second webhook
api DELETE "/webhooks/$WEBHOOK2_ID" "$TOKEN"
assert_status "204" "Delete second webhook"

# --- Miniflux webhook receiver ---

# Create a fresh webhook for receiver testing
api POST /webhooks "$TOKEN" '{"type":"miniflux","secret":"test-secret-for-e2e-receiver","config":"{\"directory\":\"Saved\"}"}'
assert_status "201" "Create webhook for receiver test"
WEBHOOK_SECRET="test-secret-for-e2e-receiver"

# Request without signature
WEBHOOK_URL="http://localhost:${SERVER_PORT}/api/v1/webhook/miniflux"
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
  -d '{"event_type":"save_entry","entry":{"title":"Test","url":"https://example.com","content":"<p>Hi</p>"}}' \
  "$WEBHOOK_URL")
assert_equals "$HTTP_CODE" "401" "Webhook: reject missing signature"

# Request with wrong signature
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -H 'X-Miniflux-Signature: 0000000000000000000000000000000000000000000000000000000000000000' \
  -H 'X-Miniflux-Event-Type: save_entry' \
  -d '{"event_type":"save_entry","entry":{"title":"Test","url":"https://example.com","content":"<p>Hi</p>"}}' \
  "$WEBHOOK_URL")
assert_equals "$HTTP_CODE" "401" "Webhook: reject wrong signature"

# Compute valid HMAC signature
PAYLOAD='{"event_type":"save_entry","entry":{"title":"Webhook Article","url":"https://example.com/article","content":"<p>Webhook content</p>"}}'
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/.* //')

# Valid signature but no destination → 500 (processing fails)
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -H "X-Miniflux-Signature: $SIGNATURE" \
  -H 'X-Miniflux-Event-Type: save_entry' \
  -d "$PAYLOAD" \
  "$WEBHOOK_URL")
assert_equals "$HTTP_CODE" "500" "Webhook: valid sig, fails without destination"

# Non-save events should be accepted without processing
PAYLOAD2='{"event_type":"new_entries","feed":{"id":1},"entries":[]}'
SIGNATURE2=$(echo -n "$PAYLOAD2" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/.* //')
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -H "X-Miniflux-Signature: $SIGNATURE2" \
  -H 'X-Miniflux-Event-Type: new_entries' \
  -d "$PAYLOAD2" \
  "$WEBHOOK_URL")
assert_equals "$HTTP_CODE" "200" "Webhook: non-save events accepted"

results
