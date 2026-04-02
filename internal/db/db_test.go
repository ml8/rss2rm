package db

import (
"os"
"testing"
"time"
)

const testUserID = "test-user-id"

func setupTestDB(t *testing.T) *DB {
t.Helper()
f, err := os.CreateTemp("", "rss2rm-test-*.db")
if err != nil {
t.Fatal(err)
}
f.Close()
t.Cleanup(func() { os.Remove(f.Name()) })

db, err := Open("sqlite3", f.Name())
if err != nil {
t.Fatal(err)
}
t.Cleanup(func() { db.Close() })
return db
}

func TestDigestCRUD(t *testing.T) {
db := setupTestDB(t)

// Insert
d := Digest{Name: "Morning Reading", Schedule: "07:00", Active: true}
id, err := db.InsertDigest(testUserID, d)
if err != nil {
t.Fatalf("InsertDigest: %v", err)
}
if id == "" {
t.Fatal("expected non-empty digest ID")
}

// Get by ID
got, err := db.GetDigestByID(testUserID, id)
if err != nil {
t.Fatalf("GetDigestByID: %v", err)
}
if got == nil {
t.Fatal("expected digest, got nil")
}
if got.Name != "Morning Reading" || got.Schedule != "07:00" || !got.Active {
t.Fatalf("unexpected digest: %+v", got)
}
if got.DestinationID != nil {
t.Fatalf("expected nil destination_id, got %v", got.DestinationID)
}

// List
digests, err := db.GetDigests(testUserID)
if err != nil {
t.Fatalf("GetDigests: %v", err)
}
if len(digests) != 1 {
t.Fatalf("expected 1 digest, got %d", len(digests))
}

// Active digests
active, err := db.GetActiveDigests(testUserID)
if err != nil {
t.Fatalf("GetActiveDigests: %v", err)
}
if len(active) != 1 {
t.Fatalf("expected 1 active digest, got %d", len(active))
}

// Remove
if err := db.RemoveDigest(testUserID, id); err != nil {
t.Fatalf("RemoveDigest: %v", err)
}
got, err = db.GetDigestByID(testUserID, id)
if err != nil {
t.Fatalf("GetDigestByID after remove: %v", err)
}
if got != nil {
t.Fatal("expected nil after remove")
}
}

func TestDigestWithDestination(t *testing.T) {
db := setupTestDB(t)

// Create a destination first
destID, err := db.InsertDestination(testUserID, Destination{Name: "test-dest", Type: "file", Config: `{"path":"/tmp"}`, IsDefault: false})
if err != nil {
t.Fatalf("InsertDestination: %v", err)
}

d := Digest{Name: "With Dest", Schedule: "08:00", DestinationID: &destID, Active: true}
id, err := db.InsertDigest(testUserID, d)
if err != nil {
t.Fatalf("InsertDigest: %v", err)
}

got, err := db.GetDigestByID(testUserID, id)
if err != nil {
t.Fatalf("GetDigestByID: %v", err)
}
if got.DestinationID == nil || *got.DestinationID != destID {
t.Fatalf("expected destination_id=%s, got %v", destID, got.DestinationID)
}
}

func TestFeedDigestColumns(t *testing.T) {
db := setupTestDB(t)

// Create a feed (no Directory/DigestID on Feed anymore)
feedID, err := db.InsertFeed(testUserID, Feed{
URL: "https://example.com/feed", Name: "Test Feed", Active: true, Backfill: 5,
})
if err != nil {
t.Fatalf("InsertFeed: %v", err)
}

// Create a FeedDelivery for it
if err := db.SetFeedDelivery(testUserID, FeedDelivery{FeedID: feedID, Directory: "test"}); err != nil {
t.Fatalf("SetFeedDelivery: %v", err)
}
fd, err := db.GetFeedDelivery(testUserID, feedID)
if err != nil {
t.Fatalf("GetFeedDelivery: %v", err)
}
if fd == nil || fd.Directory != "test" {
t.Fatalf("expected directory=test, got %+v", fd)
}

// Create a digest and add feed to it
digestID, err := db.InsertDigest(testUserID, Digest{Name: "Test Digest", Schedule: "07:00", Active: true})
if err != nil {
t.Fatalf("InsertDigest: %v", err)
}
if err := db.AddFeedToDigest(digestID, feedID); err != nil {
t.Fatalf("AddFeedToDigest: %v", err)
}

// Verify GetFeedsForDigest
digestFeeds, err := db.GetFeedsForDigest(testUserID, digestID)
if err != nil {
t.Fatalf("GetFeedsForDigest: %v", err)
}
if len(digestFeeds) != 1 || digestFeeds[0].ID != feedID {
t.Fatalf("expected 1 feed for digest, got %d", len(digestFeeds))
}

// Verify GetDigestsForFeed
digests, err := db.GetDigestsForFeed(testUserID, feedID)
if err != nil {
t.Fatalf("GetDigestsForFeed: %v", err)
}
if len(digests) != 1 || digests[0].ID != digestID {
t.Fatalf("expected 1 digest for feed, got %d", len(digests))
}

// RemoveFeedFromDigest
if err := db.RemoveFeedFromDigest(digestID, feedID); err != nil {
t.Fatalf("RemoveFeedFromDigest: %v", err)
}
digestFeeds, _ = db.GetFeedsForDigest(testUserID, digestID)
if len(digestFeeds) != 0 {
t.Fatalf("expected 0 feeds after remove, got %d", len(digestFeeds))
}

// RemoveFeedDelivery
if err := db.RemoveFeedDelivery(testUserID, feedID); err != nil {
t.Fatalf("RemoveFeedDelivery: %v", err)
}
fd, _ = db.GetFeedDelivery(testUserID, feedID)
if fd != nil {
t.Fatal("expected nil FeedDelivery after remove")
}

// Re-add feed to digest, then RemoveDigest should also remove from digest_feeds
db.AddFeedToDigest(digestID, feedID)
db.RemoveDigest(testUserID, digestID)
digests, _ = db.GetDigestsForFeed(testUserID, feedID)
if len(digests) != 0 {
t.Fatal("expected no digests for feed after digest removal")
}
}

func TestHasEntryAndRendered(t *testing.T) {
db := setupTestDB(t)

feedID, err := db.InsertFeed(testUserID, Feed{
URL: "https://example.com/feed", Name: "Test",
Active: true, Backfill: 5,
})
if err != nil {
t.Fatal(err)
}

// No entry yet
has, err := db.HasEntry(testUserID, feedID, "guid-1")
if err != nil {
t.Fatal(err)
}
if has {
t.Fatal("expected HasEntry=false for unseen entry")
}

// Create entry (no Uploaded field)
err = db.CreateEntry(testUserID, Entry{
FeedID: feedID, EntryID: "guid-1", Title: "Article 1",
URL: "https://example.com/1", Published: time.Now(),
})
if err != nil {
t.Fatal(err)
}

// HasEntry should be true now
has, err = db.HasEntry(testUserID, feedID, "guid-1")
if err != nil {
t.Fatal(err)
}
if !has {
t.Fatal("expected HasEntry=true after creation")
}

// GetEntry should have empty Rendered
e, err := db.GetEntry(testUserID, feedID, "guid-1")
if err != nil {
t.Fatal(err)
}
if e == nil {
t.Fatal("expected entry, got nil")
}
if e.Rendered != "" {
t.Fatalf("expected empty Rendered, got %q", e.Rendered)
}

// UpdateEntryRendered
if err := db.UpdateEntryRendered(e.ID, "/tmp/article.pdf"); err != nil {
t.Fatal(err)
}
e, _ = db.GetEntry(testUserID, feedID, "guid-1")
if e.Rendered != "/tmp/article.pdf" {
t.Fatalf("expected Rendered=/tmp/article.pdf, got %q", e.Rendered)
}
}

func TestGetNewEntriesForDigest(t *testing.T) {
db := setupTestDB(t)

digestID, _ := db.InsertDigest(testUserID, Digest{Name: "Test Digest", Schedule: "07:00", Active: true})
feedID, _ := db.InsertFeed(testUserID, Feed{
URL: "https://example.com/feed", Name: "Test",
Active: true, Backfill: 5,
})
db.AddFeedToDigest(digestID, feedID)

// A feed NOT in the digest
feedID2, _ := db.InsertFeed(testUserID, Feed{
URL: "https://example.com/other", Name: "Other",
Active: true, Backfill: 5,
})

now := time.Now()

db.CreateEntry(testUserID, Entry{
FeedID: feedID, EntryID: "entry-1", Title: "First",
URL: "https://example.com/1", Published: now.Add(-2 * time.Hour),
})
db.CreateEntry(testUserID, Entry{
FeedID: feedID, EntryID: "entry-2", Title: "Second",
URL: "https://example.com/2", Published: now.Add(-1 * time.Hour),
})
db.CreateEntry(testUserID, Entry{
FeedID: feedID, EntryID: "entry-3", Title: "Third",
URL: "https://example.com/3", Published: now,
})

// Article from non-digest feed (should not appear)
db.CreateEntry(testUserID, Entry{
FeedID: feedID2, EntryID: "other-1", Title: "Other Feed",
URL: "https://example.com/other1", Published: now,
})

// lastDeliveredID=0 should return all entries for the digest
entries, err := db.GetNewEntriesForDigest(digestID, 0)
if err != nil {
t.Fatalf("GetNewEntriesForDigest: %v", err)
}
if len(entries) != 3 {
t.Fatalf("expected 3 entries with lastDeliveredID=0, got %d", len(entries))
}
if entries[0].Title != "First" || entries[2].Title != "Third" {
t.Fatalf("unexpected entry order: %v, %v", entries[0].Title, entries[2].Title)
}

// Use the ID of the first entry as cursor — should return only entries after it
cursor := entries[0].ID
entries, err = db.GetNewEntriesForDigest(digestID, cursor)
if err != nil {
t.Fatalf("GetNewEntriesForDigest with cursor: %v", err)
}
if len(entries) != 2 {
t.Fatalf("expected 2 entries after cursor, got %d", len(entries))
}
if entries[0].Title != "Second" || entries[1].Title != "Third" {
t.Fatalf("unexpected entries: %v, %v", entries[0].Title, entries[1].Title)
}
}

func TestMarkDigestGenerated(t *testing.T) {
db := setupTestDB(t)

id, _ := db.InsertDigest(testUserID, Digest{Name: "Test", Schedule: "07:00", Active: true})

// Initially no last_generated and last_delivered_id=0
d, _ := db.GetDigestByID(testUserID, id)
if !d.LastGenerated.IsZero() {
t.Fatal("expected zero LastGenerated initially")
}
if d.LastDeliveredID != 0 {
t.Fatal("expected LastDeliveredID=0 initially")
}

// Mark generated with a specific lastEntryID
if err := db.MarkDigestGenerated(id, 42); err != nil {
t.Fatal(err)
}

d, _ = db.GetDigestByID(testUserID, id)
if d.LastGenerated.IsZero() {
t.Fatal("expected non-zero LastGenerated after marking")
}
if d.LastDeliveredID != 42 {
t.Fatalf("expected LastDeliveredID=42, got %d", d.LastDeliveredID)
}
}

func TestFeedDeliveryCRUD(t *testing.T) {
db := setupTestDB(t)

feedID, _ := db.InsertFeed(testUserID, Feed{URL: "https://example.com/feed", Name: "Test", Active: true, Backfill: 5})
destID, _ := db.InsertDestination(testUserID, Destination{Name: "test-dest", Type: "file", Config: `{"path":"/tmp"}`})

// Initially no delivery
fd, err := db.GetFeedDelivery(testUserID, feedID)
if err != nil {
t.Fatal(err)
}
if fd != nil {
t.Fatal("expected nil FeedDelivery initially")
}

// SetFeedDelivery
if err := db.SetFeedDelivery(testUserID, FeedDelivery{FeedID: feedID, Directory: "articles", DestinationID: &destID}); err != nil {
t.Fatal(err)
}
fd, _ = db.GetFeedDelivery(testUserID, feedID)
if fd == nil {
t.Fatal("expected FeedDelivery after set")
}
if fd.Directory != "articles" {
t.Fatalf("expected directory=articles, got %q", fd.Directory)
}
if fd.DestinationID == nil || *fd.DestinationID != destID {
t.Fatalf("expected destination_id=%s, got %v", destID, fd.DestinationID)
}
if fd.LastDeliveredID != 0 {
t.Fatalf("expected LastDeliveredID=0, got %d", fd.LastDeliveredID)
}

// AdvanceFeedDelivery
if err := db.AdvanceFeedDelivery(feedID, 10); err != nil {
t.Fatal(err)
}
fd, _ = db.GetFeedDelivery(testUserID, feedID)
if fd.LastDeliveredID != 10 {
t.Fatalf("expected LastDeliveredID=10, got %d", fd.LastDeliveredID)
}

// Upsert via SetFeedDelivery (should replace)
if err := db.SetFeedDelivery(testUserID, FeedDelivery{FeedID: feedID, Directory: "new-dir"}); err != nil {
t.Fatal(err)
}
fd, _ = db.GetFeedDelivery(testUserID, feedID)
if fd.Directory != "new-dir" {
t.Fatalf("expected directory=new-dir after upsert, got %q", fd.Directory)
}

// RemoveFeedDelivery
if err := db.RemoveFeedDelivery(testUserID, feedID); err != nil {
t.Fatal(err)
}
fd, _ = db.GetFeedDelivery(testUserID, feedID)
if fd != nil {
t.Fatal("expected nil after RemoveFeedDelivery")
}
}

func TestGetUndeliveredEntries(t *testing.T) {
db := setupTestDB(t)

feedID, _ := db.InsertFeed(testUserID, Feed{URL: "https://example.com/feed", Name: "Test", Active: true, Backfill: 5})

now := time.Now()
db.CreateEntry(testUserID, Entry{FeedID: feedID, EntryID: "a", Title: "A", URL: "https://example.com/a", Published: now.Add(-2 * time.Hour)})
db.CreateEntry(testUserID, Entry{FeedID: feedID, EntryID: "b", Title: "B", URL: "https://example.com/b", Published: now.Add(-1 * time.Hour)})
db.CreateEntry(testUserID, Entry{FeedID: feedID, EntryID: "c", Title: "C", URL: "https://example.com/c", Published: now})

// All entries with lastDeliveredID=0
entries, err := db.GetUndeliveredEntries(feedID, 0)
if err != nil {
t.Fatal(err)
}
if len(entries) != 3 {
t.Fatalf("expected 3 entries, got %d", len(entries))
}

// Use first entry's ID as cursor
cursor := entries[0].ID
entries, _ = db.GetUndeliveredEntries(feedID, cursor)
if len(entries) != 2 {
t.Fatalf("expected 2 entries after cursor, got %d", len(entries))
}
if entries[0].Title != "B" || entries[1].Title != "C" {
t.Fatalf("unexpected entries: %v, %v", entries[0].Title, entries[1].Title)
}

// Use last entry's ID as cursor — should return nothing
cursor = entries[1].ID
entries, _ = db.GetUndeliveredEntries(feedID, cursor)
if len(entries) != 0 {
t.Fatalf("expected 0 entries after last cursor, got %d", len(entries))
}
}

func TestDigestFeedsManyToMany(t *testing.T) {
db := setupTestDB(t)

feedID, _ := db.InsertFeed(testUserID, Feed{URL: "https://example.com/feed", Name: "Shared Feed", Active: true, Backfill: 5})
d1, _ := db.InsertDigest(testUserID, Digest{Name: "Morning", Schedule: "07:00", Active: true})
d2, _ := db.InsertDigest(testUserID, Digest{Name: "Evening", Schedule: "19:00", Active: true})

db.AddFeedToDigest(d1, feedID)
db.AddFeedToDigest(d2, feedID)

// Feed should be in both digests
digests, err := db.GetDigestsForFeed(testUserID, feedID)
if err != nil {
t.Fatal(err)
}
if len(digests) != 2 {
t.Fatalf("expected feed in 2 digests, got %d", len(digests))
}

// Both digests should list the feed
f1, _ := db.GetFeedsForDigest(testUserID, d1)
f2, _ := db.GetFeedsForDigest(testUserID, d2)
if len(f1) != 1 || len(f2) != 1 {
t.Fatalf("expected 1 feed in each digest, got %d and %d", len(f1), len(f2))
}

// Remove from one digest — should remain in the other
db.RemoveFeedFromDigest(d1, feedID)
digests, _ = db.GetDigestsForFeed(testUserID, feedID)
if len(digests) != 1 || digests[0].ID != d2 {
t.Fatalf("expected feed in 1 digest after removal, got %d", len(digests))
}

// Add an entry and verify it appears in the remaining digest's new entries
db.CreateEntry(testUserID, Entry{FeedID: feedID, EntryID: "x", Title: "X", URL: "https://example.com/x", Published: time.Now()})
entries, _ := db.GetNewEntriesForDigest(d2, 0)
if len(entries) != 1 {
t.Fatalf("expected 1 entry in digest d2, got %d", len(entries))
}
entries, _ = db.GetNewEntriesForDigest(d1, 0)
if len(entries) != 0 {
t.Fatalf("expected 0 entries in digest d1 after feed removed, got %d", len(entries))
}
}

func TestEntryContent(t *testing.T) {
	db := setupTestDB(t)

	feedID, _ := db.InsertFeed(testUserID, Feed{URL: "https://example.com/feed", Name: "Test", Active: true})

	// Entry without content
	db.CreateEntry(testUserID, Entry{
		FeedID: feedID, EntryID: "no-content", Title: "No Content",
		URL: "https://example.com/1", Published: time.Now(),
	})
	e, _ := db.GetEntry(testUserID, feedID, "no-content")
	if e.Content != "" {
		t.Fatalf("expected empty Content, got %q", e.Content)
	}

	// Entry with content
	db.CreateEntry(testUserID, Entry{
		FeedID: feedID, EntryID: "with-content", Title: "With Content",
		URL: "https://example.com/2", Published: time.Now(),
		Content: "<p>Hello world</p>",
	})
	e, _ = db.GetEntry(testUserID, feedID, "with-content")
	if e.Content != "<p>Hello world</p>" {
		t.Fatalf("expected content '<p>Hello world</p>', got %q", e.Content)
	}

	// Content should appear in GetUndeliveredEntries
	entries, _ := db.GetUndeliveredEntries(feedID, 0)
	found := false
	for _, entry := range entries {
		if entry.EntryID == "with-content" && entry.Content == "<p>Hello world</p>" {
			found = true
		}
	}
	if !found {
		t.Fatal("content not preserved in GetUndeliveredEntries")
	}

	// Content should appear in GetNewEntriesForDigest
	digestID, _ := db.InsertDigest(testUserID, Digest{Name: "Content Digest", Schedule: "07:00", Active: true})
	db.AddFeedToDigest(digestID, feedID)
	digestEntries, _ := db.GetNewEntriesForDigest(digestID, 0)
	found = false
	for _, entry := range digestEntries {
		if entry.EntryID == "with-content" && entry.Content == "<p>Hello world</p>" {
			found = true
		}
	}
	if !found {
		t.Fatal("content not preserved in GetNewEntriesForDigest")
	}
}

func TestWebhookCRUD(t *testing.T) {
	db := setupTestDB(t)

	// Create a user for webhook ownership
	userID, err := db.CreateUser("webhook@test.com", "password")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Insert webhook
	id, err := db.InsertWebhook(userID, Webhook{Type: "miniflux", Secret: "test-hmac-secret", Config: `{"digest_id":"abc"}`, Active: true})
	if err != nil {
		t.Fatalf("InsertWebhook: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty webhook ID")
	}

	// List webhooks
	webhooks, err := db.GetWebhooks(userID)
	if err != nil {
		t.Fatalf("GetWebhooks: %v", err)
	}
	if len(webhooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(webhooks))
	}
	if webhooks[0].Type != "miniflux" {
		t.Fatalf("expected type=miniflux, got %q", webhooks[0].Type)
	}
	if webhooks[0].Secret != "test-hmac-secret" {
		t.Fatalf("expected secret=test-hmac-secret, got %q", webhooks[0].Secret)
	}
	if webhooks[0].Config != `{"digest_id":"abc"}` {
		t.Fatalf("expected config preserved, got %q", webhooks[0].Config)
	}

	// Get by ID
	w, err := db.GetWebhookByID(userID, id)
	if err != nil {
		t.Fatalf("GetWebhookByID: %v", err)
	}
	if w == nil {
		t.Fatal("expected webhook, got nil")
	}
	if w.ID != id {
		t.Fatalf("expected ID=%s, got %s", id, w.ID)
	}

	// Wrong user can't see it
	w2, _ := db.GetWebhookByID("other-user", id)
	if w2 != nil {
		t.Fatal("other user should not see this webhook")
	}

	// Delete webhook
	if err := db.DeleteWebhook(userID, id); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	webhooks, _ = db.GetWebhooks(userID)
	if len(webhooks) != 0 {
		t.Fatalf("expected 0 webhooks after delete, got %d", len(webhooks))
	}
}

func TestVirtualFeedFiltering(t *testing.T) {
	db := setupTestDB(t)

	// Create a normal feed and a virtual feed
	db.InsertFeed(testUserID, Feed{URL: "https://example.com/real", Name: "Real Feed", Active: true})
	db.InsertFeed(testUserID, Feed{URL: "_ingest:abc123", Name: "Virtual Feed", Active: true})

	// GetActiveFeeds should only return the real feed
	feeds, err := db.GetActiveFeeds(testUserID)
	if err != nil {
		t.Fatalf("GetActiveFeeds: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("expected 1 active feed (virtual filtered), got %d", len(feeds))
	}
	if feeds[0].URL != "https://example.com/real" {
		t.Fatalf("expected real feed, got %q", feeds[0].URL)
	}
}
