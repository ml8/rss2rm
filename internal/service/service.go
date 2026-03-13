// Package service implements the core business logic for rss2rm,
// including feed polling, article processing, PDF generation,
// and delivery to configured destinations.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"rss2rm/internal/converter"
	"rss2rm/internal/db"
	"rss2rm/internal/importer"
	"rss2rm/internal/processor"
)

// Service defines the interface for all feed, destination, and digest
// operations. It is implemented by [LocalService] for direct database
// access and by [client.RemoteService] for HTTP-based access.
type Service interface {
	AddFeed(ctx context.Context, feed db.Feed) error
	UpdateFeed(ctx context.Context, feed db.Feed) error
	RemoveFeed(ctx context.Context, feedURL string) error
	RemoveFeedByID(ctx context.Context, id string) error
	ListFeeds(ctx context.Context) ([]db.Feed, error)
	PollFeeds(ctx context.Context, filterURLs []string, onEvent func(PollEvent)) error

	// Feed delivery management
	GetFeedDelivery(ctx context.Context, feedID string) (*db.FeedDelivery, error)
	SetFeedDelivery(ctx context.Context, fd db.FeedDelivery) error
	RemoveFeedDelivery(ctx context.Context, feedID string) error

	// Destination management
	AddDestination(ctx context.Context, destType, name string, config map[string]string, isDefault bool) (string, error)
	ListDestinations(ctx context.Context) ([]db.Destination, error)
	RemoveDestination(ctx context.Context, id string) error
	SetDefaultDestination(ctx context.Context, id string) error
	TestDestination(ctx context.Context, id string) error
	UpdateDestinationConfig(ctx context.Context, id string, config map[string]string) error
	UpdateDestination(ctx context.Context, id string, name string, config map[string]string) error
	GetDestinationByID(ctx context.Context, id string) (*db.Destination, error)

	// Digest management
	AddDigest(ctx context.Context, digest db.Digest) (string, error)
	ListDigests(ctx context.Context) ([]db.Digest, error)
	RemoveDigest(ctx context.Context, id string) error
	UpdateDigest(ctx context.Context, digest db.Digest) error
	GetDigestsForFeed(ctx context.Context, feedID string) ([]db.Digest, error)
	AddFeedToDigest(ctx context.Context, digestID, feedID string, alsoIndividual bool) error
	RemoveFeedFromDigest(ctx context.Context, digestID, feedID string) error
	ListDigestFeeds(ctx context.Context, digestID string) ([]db.Feed, error)
	GenerateDigest(ctx context.Context, digestID string, onEvent func(PollEvent)) error

	// Delivery log
	ListRecentDeliveries(ctx context.Context, limit int) ([]db.DeliveryLogEntry, error)
}

// EventType identifies the kind of event emitted during feed polling.
type EventType string

const (
	EventStart        EventType = "START"
	EventItemFound    EventType = "ITEM_FOUND"
	EventItemUploaded EventType = "ITEM_UPLOADED"
	EventError        EventType = "ERROR"
	EventFinish       EventType = "FINISH"
	EventPollComplete EventType = "POLL_COMPLETE"
)

// DefaultHeadlessCommand is the default shell command template used
// to convert HTML to PDF via pandoc and weasyprint.
const (
	DefaultHeadlessCommand = "pandoc {url} -o {output} --pdf-engine=weasyprint"
	defaultBackfillCount   = 5
)

// PollEvent represents a single event emitted during feed polling,
// streamed to clients via SSE.
type PollEvent struct {
	FeedURL   string
	Type      EventType
	Message   string
	ItemTitle string
}

// PollOptions configures behavior for a poll operation.
type PollOptions struct {
	BackfillLimit int
}

type pollOptionsKey struct{}

// WithPollOptions returns a copy of ctx carrying the given [PollOptions].
func WithPollOptions(ctx context.Context, opts PollOptions) context.Context {
	return context.WithValue(ctx, pollOptionsKey{}, opts)
}

// PollOptionsFromContext extracts [PollOptions] from ctx, returning
// a zero value if none is set.
func PollOptionsFromContext(ctx context.Context) PollOptions {
	if opts, ok := ctx.Value(pollOptionsKey{}).(PollOptions); ok {
		return opts
	}
	return PollOptions{}
}

type contextKey string

// UserIDKey is the context key used to store the authenticated user's ID.
const UserIDKey contextKey = "userID"

// UserIDFromContext extracts the authenticated user's ID from the context.
func UserIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(UserIDKey).(string); ok {
		return id
	}
	return ""
}

// LocalService implements [Service] using direct database access.
type LocalService struct {
	db *db.DB
}

// NewFeedService returns a new [LocalService] backed by the given database.
func NewFeedService(database *db.DB) Service {
	return &LocalService{
		db: database,
	}
}

func (s *LocalService) AddDestination(ctx context.Context, destType, name string, config map[string]string, isDefault bool) (string, error) {
	userID := UserIDFromContext(ctx)
	configJSON, err := json.Marshal(config)
	if err != nil {
		return "", err
	}

	dest := db.Destination{
		Name:      name,
		Type:      destType,
		Config:    string(configJSON),
		IsDefault: isDefault,
	}

	id, err := s.db.InsertDestination(userID, dest)
	if err != nil {
		return "", err
	}

	if isDefault {
		if err := s.db.SetDefaultDestination(userID, id); err != nil {
			return id, fmt.Errorf("destination created but failed to set default: %w", err)
		}
	}
	return id, nil
}

func (s *LocalService) ListDestinations(ctx context.Context) ([]db.Destination, error) {
	return s.db.GetDestinations(UserIDFromContext(ctx))
}

func (s *LocalService) RemoveDestination(ctx context.Context, id string) error {
	return s.db.RemoveDestination(UserIDFromContext(ctx), id)
}

func (s *LocalService) SetDefaultDestination(ctx context.Context, id string) error {
	return s.db.SetDefaultDestination(UserIDFromContext(ctx), id)
}

func (s *LocalService) TestDestination(ctx context.Context, id string) error {
	destRecord, err := s.db.GetDestinationByID(UserIDFromContext(ctx), id)
	if err != nil {
		return err
	}
	if destRecord == nil {
		return fmt.Errorf("destination not found")
	}

	destInstance, err := CreateDestinationInstance(destRecord.Type, destRecord.Config)
	if err != nil {
		return err
	}

	return destInstance.TestConnection(ctx)
}

func (s *LocalService) UpdateDestinationConfig(ctx context.Context, id string, config map[string]string) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return s.db.UpdateDestinationConfig(UserIDFromContext(ctx), id, string(configJSON))
}

func (s *LocalService) UpdateDestination(ctx context.Context, id string, name string, config map[string]string) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return s.db.UpdateDestination(UserIDFromContext(ctx), id, name, string(configJSON))
}

func (s *LocalService) GetDestinationByID(ctx context.Context, id string) (*db.Destination, error) {
	return s.db.GetDestinationByID(UserIDFromContext(ctx), id)
}

// AddFeed adds a new feed to the database with a default individual delivery.
func (s *LocalService) AddFeed(ctx context.Context, feed db.Feed) error {
	userID := UserIDFromContext(ctx)
	feed.URL = NormalizeURL(feed.URL)
	existing, err := s.db.GetActiveFeedByURL(userID, feed.URL)
	if err != nil {
		return err
	}

	if existing != nil {
		if feed.Name == "" {
			feed.Name = existing.Name
		}
		feed.Active = true
		feed.ID = existing.ID
		if err := s.db.UpdateFeed(userID, feed); err != nil {
			return err
		}
		return s.db.DeactivateFeedsByURLExceptID(userID, feed.URL, feed.ID)
	}

	if feed.Name == "" {
		feed.Name = GenerateNameFromURL(feed.URL)
	}
	feed.Active = true
	feedID, err := s.db.InsertFeed(userID, feed)
	if err != nil {
		return err
	}

	// Create default individual delivery
	fd := db.FeedDelivery{
		FeedID:    feedID,
		Directory: SanitizeFilename(feed.Name),
	}
	return s.db.SetFeedDelivery(userID, fd)
}

func (s *LocalService) UpdateFeed(ctx context.Context, feed db.Feed) error {
	return s.db.UpdateFeed(UserIDFromContext(ctx), feed)
}

// RemoveFeed marks a feed as inactive.
func (s *LocalService) RemoveFeed(ctx context.Context, feedURL string) error {
	feedURL = NormalizeURL(feedURL)
	return s.db.DeactivateFeed(UserIDFromContext(ctx), feedURL)
}

func (s *LocalService) RemoveFeedByID(ctx context.Context, id string) error {
	return s.db.DeactivateFeedByID(UserIDFromContext(ctx), id)
}

// ListFeeds returns all active feeds.
func (s *LocalService) ListFeeds(ctx context.Context) ([]db.Feed, error) {
	return s.db.GetActiveFeeds(UserIDFromContext(ctx))
}

func (s *LocalService) GetFeedDelivery(ctx context.Context, feedID string) (*db.FeedDelivery, error) {
	return s.db.GetFeedDelivery(UserIDFromContext(ctx), feedID)
}

func (s *LocalService) SetFeedDelivery(ctx context.Context, fd db.FeedDelivery) error {
	return s.db.SetFeedDelivery(UserIDFromContext(ctx), fd)
}

func (s *LocalService) RemoveFeedDelivery(ctx context.Context, feedID string) error {
	return s.db.RemoveFeedDelivery(UserIDFromContext(ctx), feedID)
}

func (s *LocalService) AddDigest(ctx context.Context, digest db.Digest) (string, error) {
	if digest.Directory == "" {
		digest.Directory = digest.Name
	}
	digest.Active = true
	return s.db.InsertDigest(UserIDFromContext(ctx), digest)
}

func (s *LocalService) ListDigests(ctx context.Context) ([]db.Digest, error) {
	return s.db.GetDigests(UserIDFromContext(ctx))
}

func (s *LocalService) RemoveDigest(ctx context.Context, id string) error {
	return s.db.RemoveDigest(UserIDFromContext(ctx), id)
}

func (s *LocalService) UpdateDigest(ctx context.Context, digest db.Digest) error {
	return s.db.UpdateDigest(UserIDFromContext(ctx), digest)
}

func (s *LocalService) GetDigestsForFeed(ctx context.Context, feedID string) ([]db.Digest, error) {
	return s.db.GetDigestsForFeed(UserIDFromContext(ctx), feedID)
}

func (s *LocalService) AddFeedToDigest(ctx context.Context, digestID, feedID string, alsoIndividual bool) error {
	if err := s.db.AddFeedToDigest(digestID, feedID); err != nil {
		return err
	}
	if !alsoIndividual {
		return s.db.RemoveFeedDelivery(UserIDFromContext(ctx), feedID)
	}
	return nil
}

func (s *LocalService) RemoveFeedFromDigest(ctx context.Context, digestID, feedID string) error {
	return s.db.RemoveFeedFromDigest(digestID, feedID)
}

func (s *LocalService) ListDigestFeeds(ctx context.Context, digestID string) ([]db.Feed, error) {
	return s.db.GetFeedsForDigest(UserIDFromContext(ctx), digestID)
}

func (s *LocalService) ListRecentDeliveries(ctx context.Context, limit int) ([]db.DeliveryLogEntry, error) {
	return s.db.GetRecentDeliveries(UserIDFromContext(ctx), limit)
}

func (s *LocalService) GenerateDigest(ctx context.Context, digestID string, onEvent func(PollEvent)) error {
	userID := UserIDFromContext(ctx)
	digest, err := s.db.GetDigestByID(userID, digestID)
	if err != nil {
		return fmt.Errorf("failed to get digest: %w", err)
	}
	if digest == nil {
		return fmt.Errorf("digest not found: %s", digestID)
	}

	log.Printf("[Digest] Starting generation for %q (id=%s, cursor=%d)", digest.Name, digestID, digest.LastDeliveredID)
	onEvent(PollEvent{Type: EventStart, Message: fmt.Sprintf("Generating digest: %s", digest.Name)})

	entries, err := s.db.GetNewEntriesForDigest(digestID, digest.LastDeliveredID)
	if err != nil {
		return fmt.Errorf("failed to get entries: %w", err)
	}
	if len(entries) == 0 {
		log.Printf("[Digest] No new articles for %q", digest.Name)
		onEvent(PollEvent{Type: EventFinish, Message: "No new articles for digest"})
		return nil
	}

	log.Printf("[Digest] Found %d new entries for %q", len(entries), digest.Name)

	// Build feed name lookup
	digestFeeds, _ := s.db.GetFeedsForDigest(userID, digestID)
	feedNames := make(map[string]string)
	for _, f := range digestFeeds {
		feedNames[f.ID] = f.Name
	}

	// Render articles
	var articles []converter.DigestArticle
	var maxEntryID int64

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		log.Printf("[Digest] Rendering article %q for digest %q", entry.Title, digest.Name)
		onEvent(PollEvent{Type: EventItemFound, ItemTitle: entry.Title, Message: "Rendering for digest"})

		article, err := processor.Process(entry.URL)
		if err != nil {
			log.Printf("[Digest] Processing failed for %q: %v", entry.Title, err)
			onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("Processing failed: %v", err)})
			continue
		}
		articles = append(articles, converter.DigestArticle{
			Title: article.Title, Byline: article.Byline, Content: article.Content,
			FeedName: feedNames[entry.FeedID],
		})
		if entry.ID > maxEntryID {
			maxEntryID = entry.ID
		}
	}

	if len(articles) == 0 {
		log.Printf("[Digest] All articles failed to render for %q", digest.Name)
		onEvent(PollEvent{Type: EventFinish, Message: "All articles failed to render"})
		return nil
	}

	// Generate combined PDF
	log.Printf("[Digest] Generating combined HTML for %q (%d articles)", digest.Name, len(articles))
	htmlPath, err := converter.GenerateDigestHTML(digest.Name, articles)
	if err != nil {
		return fmt.Errorf("digest HTML generation failed: %w", err)
	}

	// Per-render temp directory for isolation
	renderDir, err := os.MkdirTemp("", "rss2rm-digest-*")
	if err != nil {
		os.Remove(htmlPath)
		return fmt.Errorf("failed to create render dir: %w", err)
	}
	defer os.RemoveAll(renderDir)

	pdfName := fmt.Sprintf("%s - %s.pdf", time.Now().Format("2006-01-02"), SanitizeFilename(digest.Name))
	tmpPDF := filepath.Join(renderDir, pdfName)

	log.Printf("[Digest] Converting to PDF: %s", pdfName)
	if err := converter.HTMLToPDF(htmlPath, tmpPDF, DefaultHeadlessCommand); err != nil {
		return fmt.Errorf("digest PDF conversion failed: %w", err)
	}

	// Resolve destination and upload
	destInstance, destID, err := s.resolveDestination(userID, digest.DestinationID)
	if err != nil {
		return fmt.Errorf("digest destination error: %w", err)
	}

	uploadTarget := digest.Directory
	if uploadTarget == "" {
		uploadTarget = SanitizeFilename(digest.Name)
	}

	log.Printf("[Digest] Uploading %q → %s (dest=%s)", pdfName, uploadTarget, destID)
	remotePath, err := destInstance.Upload(ctx, tmpPDF, uploadTarget)
	if err != nil {
		log.Printf("[Digest] Upload failed for %q: %v", digest.Name, err)
		return fmt.Errorf("digest upload failed: %w", err)
	}

	// Persist updated config
	s.persistDestinationConfig(userID, destInstance, destID)

	// Record delivered file for retention tracking
	digestDestID := ""
	if digest.DestinationID != nil {
		digestDestID = *digest.DestinationID
	}
	s.db.RecordDeliveredFile(db.DeliveredFile{
		UserID:        userID,
		DeliveryType:  "digest",
		DeliveryRef:   digestID,
		EntryID:       0,
		RemotePath:    remotePath,
		DestinationID: digestDestID,
	})

	if digest.Retain > 0 {
		s.cleanupOldDeliveries(ctx, userID, "digest", digestID, digest.Retain)
	}

	// Advance cursor
	if err := s.db.MarkDigestGenerated(digestID, maxEntryID); err != nil {
		return fmt.Errorf("failed to mark digest generated: %w", err)
	}

	log.Printf("[Digest] Digest %q uploaded successfully (%d articles, cursor→%d)", digest.Name, len(articles), maxEntryID)
	onEvent(PollEvent{Type: EventItemUploaded, Message: fmt.Sprintf("Digest uploaded: %d articles", len(articles))})
	onEvent(PollEvent{Type: EventFinish, Message: "Digest generation complete"})
	return nil
}

// PollFeeds fetches new items from active feeds, discovers entries, and
// delivers them to their configured destinations. If filterURLs is
// non-empty, only matching feeds are polled.
func (s *LocalService) PollFeeds(ctx context.Context, filterURLs []string, onEvent func(PollEvent)) error {
	userID := UserIDFromContext(ctx)
	feeds, err := s.db.GetActiveFeeds(userID)
	if err != nil {
		return err
	}

	opts := PollOptionsFromContext(ctx)

	// Filter if requested
	if len(filterURLs) > 0 {
		filtered := make([]db.Feed, 0)
		lookup := make(map[string]bool)
		for _, u := range filterURLs {
			lookup[NormalizeURL(u)] = true
		}
		for _, f := range feeds {
			if lookup[NormalizeURL(f.URL)] {
				filtered = append(filtered, f)
			}
		}
		feeds = filtered
	}

	log.Printf("[Poll] Polling %d feeds", len(feeds))

	for _, feed := range feeds {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		s.processFeed(ctx, feed, onEvent, opts)
	}

	log.Printf("[Poll] Polling complete")
	return nil
}

func (s *LocalService) processFeed(ctx context.Context, feed db.Feed, onEvent func(PollEvent), opts PollOptions) {
	userID := UserIDFromContext(ctx)
	log.Printf("[Poll] Starting poll for feed %q (%s)", feed.Name, feed.URL)
	onEvent(PollEvent{FeedURL: feed.URL, Type: EventStart, Message: "Starting poll"})

	limit := feed.Backfill
	if limit == 0 {
		limit = defaultBackfillCount
	}
	if opts.BackfillLimit > 0 {
		limit = opts.BackfillLimit
	}

	items, err := importer.Fetch(feed.URL, limit)
	if err != nil {
		log.Printf("[Poll] Fetch failed for %q: %v", feed.Name, err)
		onEvent(PollEvent{FeedURL: feed.URL, Type: EventError, Message: fmt.Sprintf("Fetch failed: %v", err)})
		return
	}
	log.Printf("[Poll] Fetched %d items from %q", len(items), feed.Name)
	if len(items) == 0 {
		onEvent(PollEvent{FeedURL: feed.URL, Type: EventFinish, Message: "No items found"})
		return
	}

	// Discover entries
	newCount := 0
	for _, item := range items {
		seen, err := s.db.HasEntry(userID, feed.ID, item.GUID)
		if err != nil {
			continue
		}
		if !seen {
			newCount++
			onEvent(PollEvent{FeedURL: feed.URL, Type: EventItemFound, ItemTitle: item.Title, Message: "New item found"})
			entry := db.Entry{
				FeedID:    feed.ID,
				EntryID:   item.GUID,
				Title:     item.Title,
				URL:       item.Link,
				Published: item.Published,
			}
			s.db.CreateEntry(userID, entry)
		}
	}
	if newCount > 0 {
		log.Printf("[Poll] Discovered %d new entries for %q", newCount, feed.Name)
	}

	// Individual delivery
	fd, err := s.db.GetFeedDelivery(userID, feed.ID)
	if err != nil || fd == nil {
		s.db.MarkFeedPolled(feed.ID)
		log.Printf("[Poll] Feed %q has no individual delivery configured", feed.Name)
		onEvent(PollEvent{FeedURL: feed.URL, Type: EventFinish, Message: "Poll complete (digest only)"})
		return
	}

	undelivered, err := s.db.GetUndeliveredEntries(feed.ID, fd.LastDeliveredID)
	if err != nil {
		log.Printf("[Poll] Failed to get undelivered entries for %q: %v", feed.Name, err)
		onEvent(PollEvent{FeedURL: feed.URL, Type: EventError, Message: fmt.Sprintf("Failed to get entries: %v", err)})
		return
	}

	if len(undelivered) > 0 {
		log.Printf("[Delivery] %d entries to deliver individually for %q → %s", len(undelivered), feed.Name, fd.Directory)
	}

	for _, entry := range undelivered {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := s.deliverEntry(ctx, *fd, entry, onEvent); err != nil {
			break
		}
		s.db.AdvanceFeedDelivery(feed.ID, entry.ID)
	}

	s.db.MarkFeedPolled(feed.ID)
	log.Printf("[Poll] Poll complete for %q", feed.Name)
	onEvent(PollEvent{FeedURL: feed.URL, Type: EventFinish, Message: "Poll complete"})
}

// deliverEntry renders and uploads a single entry for individual delivery.
func (s *LocalService) deliverEntry(ctx context.Context, fd db.FeedDelivery, entry db.Entry, onEvent func(PollEvent)) error {
	userID := UserIDFromContext(ctx)
	log.Printf("[Delivery] Processing entry %q (id=%d)", entry.Title, entry.ID)

	destInstance, destID, err := s.resolveDestination(userID, fd.DestinationID)
	if err != nil {
		log.Printf("[Delivery] Destination error for entry %q: %v", entry.Title, err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("Destination error: %v", err)})
		return err
	}

	article, err := processor.Process(entry.URL)
	if err != nil {
		log.Printf("[Delivery] Article processing failed for %q: %v", entry.Title, err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("Processing failed: %v", err)})
		return err
	}

	htmlPath, err := converter.GenerateHTML(article.Title, article.Content, article.Byline)
	if err != nil {
		log.Printf("[Delivery] HTML generation failed for %q: %v", entry.Title, err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("HTML generation failed: %v", err)})
		return err
	}

	// Per-render temp directory for isolation
	renderDir, err := os.MkdirTemp("", "rss2rm-render-*")
	if err != nil {
		return fmt.Errorf("failed to create render dir: %w", err)
	}
	defer os.RemoveAll(renderDir)

	pdfName := fmt.Sprintf("%s - %s.pdf", entry.Published.Format("2006-01-02"), SanitizeFilename(article.Title))
	tmpPDF := filepath.Join(renderDir, pdfName)

	log.Printf("[Delivery] Converting to PDF: %s", pdfName)
	if err := converter.HTMLToPDF(htmlPath, tmpPDF, DefaultHeadlessCommand); err != nil {
		log.Printf("[Delivery] PDF conversion failed for %q: %v", entry.Title, err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("PDF conversion failed: %v", err)})
		return err
	}

	// Store rendered path
	s.db.UpdateEntryRendered(entry.ID, tmpPDF)

	uploadTarget := fd.Directory
	if uploadTarget == "" {
		uploadTarget = "RSS"
	}

	log.Printf("[Delivery] Uploading %q → %s (dest=%s)", pdfName, uploadTarget, destID)
	remotePath, err := destInstance.Upload(ctx, tmpPDF, uploadTarget)
	if err != nil {
		log.Printf("[Delivery] Upload failed for %q: %v", entry.Title, err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("Upload failed: %v", err)})
		return err
	}

	// Persist updated config (token refresh etc.)
	s.persistDestinationConfig(userID, destInstance, destID)

	// Record delivered file for retention tracking
	fdDestID := ""
	if fd.DestinationID != nil {
		fdDestID = *fd.DestinationID
	}
	s.db.RecordDeliveredFile(db.DeliveredFile{
		UserID:        userID,
		DeliveryType:  "individual",
		DeliveryRef:   fd.FeedID,
		EntryID:       entry.ID,
		RemotePath:    remotePath,
		DestinationID: fdDestID,
	})

	// Clean up old deliveries if retention limit is set
	if fd.Retain > 0 {
		s.cleanupOldDeliveries(ctx, userID, "individual", fd.FeedID, fd.Retain)
	}

	log.Printf("[Delivery] Uploaded %q successfully", entry.Title)
	onEvent(PollEvent{Type: EventItemUploaded, ItemTitle: entry.Title, Message: "Uploaded"})
	return nil
}

// resolveDestination creates a Destination instance from a destination ID,
// falling back to the system default if destID is nil.
func (s *LocalService) resolveDestination(userID string, destID *string) (Destination, string, error) {
	var destRecord *db.Destination
	var err error
	if destID != nil {
		destRecord, err = s.db.GetDestinationByID(userID, *destID)
	} else {
		destRecord, err = s.db.GetDefaultDestination(userID)
	}
	if err != nil {
		return nil, "", err
	}
	if destRecord == nil {
		return nil, "", fmt.Errorf("no destination configured")
	}
	dest, err := CreateDestinationInstance(destRecord.Type, destRecord.Config)
	if err != nil {
		return nil, "", err
	}
	return dest, destRecord.ID, nil
}

// persistDestinationConfig saves updated configuration (e.g., refreshed tokens)
// if the destination implements ConfigUpdater.
func (s *LocalService) persistDestinationConfig(userID string, dest Destination, destID string) {
	if updater, ok := dest.(ConfigUpdater); ok {
		if newConfig := updater.GetUpdatedConfig(); newConfig != nil {
			configJSON, _ := json.Marshal(newConfig)
			s.db.UpdateDestinationConfig(userID, destID, string(configJSON))
		}
	}
}

// cleanupOldDeliveries removes delivered files beyond the retention limit
// from both the destination and the tracking database.
func (s *LocalService) cleanupOldDeliveries(ctx context.Context, userID, deliveryType, deliveryRef string, retain int) {
	files, err := s.db.GetDeliveredFiles(userID, deliveryType, deliveryRef)
	if err != nil {
		log.Printf("[Cleanup] Failed to get delivered files: %v", err)
		return
	}
	if len(files) <= retain {
		return
	}

	toDelete := files[retain:]
	log.Printf("[Cleanup] Removing %d old deliveries for %s/%s (keeping %d)", len(toDelete), deliveryType, deliveryRef, retain)

	for _, f := range toDelete {
		var destID *string
		if f.DestinationID != "" {
			destID = &f.DestinationID
		}
		dest, _, err := s.resolveDestination(userID, destID)
		if err != nil {
			log.Printf("[Cleanup] Cannot resolve destination for %s: %v", f.RemotePath, err)
			s.db.DeleteDeliveredFile(f.ID)
			continue
		}
		if err := dest.Delete(ctx, f.RemotePath); err != nil {
			log.Printf("[Cleanup] Failed to delete %s: %v", f.RemotePath, err)
			// Still remove the tracking record — the file may already be gone
		}
		s.db.DeleteDeliveredFile(f.ID)
	}
}

// GenerateNameFromURL derives a short feed name from the URL's hostname.
func GenerateNameFromURL(feedURL string) string {
	u, err := url.Parse(feedURL)
	if err != nil {
		return "Unknown_Feed"
	}
	host := u.Host
	host = strings.TrimPrefix(host, "www.")
	host = strings.ReplaceAll(host, ".", "_")
	if host == "" {
		return "Unknown_Feed"
	}
	return host
}

// SanitizeFilename removes characters that are unsafe in filenames and
// truncates the result to 200 characters.
func SanitizeFilename(name string) string {
	re := regexp.MustCompile(`[<>:"/\\|?*]`)
	name = re.ReplaceAllString(name, "_")
	name = strings.TrimSpace(name)
	if len(name) > 200 {
		name = name[:200]
	}
	if name == "" {
		return "Unknown"
	}
	return name
}

// NormalizeURL canonicalizes a feed URL by lowercasing the host,
// trimming trailing slashes, and prepending https:// if no scheme is present.
func NormalizeURL(u string) string {
	if u == "" {
		return ""
	}
	u = strings.TrimSpace(u)
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return strings.TrimSuffix(u, "/")
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed.String()
}
