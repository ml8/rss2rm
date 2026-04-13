// Package service implements the core business logic for rss2rm,
// including feed polling, article processing, PDF generation,
// and delivery to configured destinations.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"rss2rm/internal/converter"
	"rss2rm/internal/db"
	"rss2rm/internal/fetcher"
	"rss2rm/internal/importer"
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
	GetDigestByID(ctx context.Context, id string) (*db.Digest, error)
	GetDigestsForFeed(ctx context.Context, feedID string) ([]db.Digest, error)
	GetNewEntriesForDigest(ctx context.Context, digestID string, afterID int64) ([]db.Entry, error)
	AddFeedToDigest(ctx context.Context, digestID, feedID string, alsoIndividual bool) error
	RemoveFeedFromDigest(ctx context.Context, digestID, feedID string) error
	ListDigestFeeds(ctx context.Context, digestID string) ([]db.Feed, error)
	GenerateDigest(ctx context.Context, digestID string, onEvent func(PollEvent)) error

	// Delivery log
	ListRecentDeliveries(ctx context.Context, limit int) ([]db.DeliveryLogEntry, error)

	// Article ingest
	DeliverArticle(ctx context.Context, title, url, content, destID, directory, digestID string) error

	// Webhook management
	AddWebhook(ctx context.Context, webhookType, secret, config string) (string, error)
	ListWebhooks(ctx context.Context) ([]db.Webhook, error)
	GetWebhookByID(ctx context.Context, id string) (*db.Webhook, error)
	RemoveWebhook(ctx context.Context, id string) error

	// Credential management
	ListCredentials(ctx context.Context) ([]db.Credential, error)
	GetCredentialByID(ctx context.Context, id string) (*db.Credential, error)
	AddCredential(ctx context.Context, name, credType, config string) (string, error)
	UpdateCredential(ctx context.Context, cred db.Credential) error
	RemoveCredential(ctx context.Context, id string) error
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
// to convert HTML to PDF via weasyprint.
const (
	DefaultHeadlessCommand = "weasyprint {url} {output}"
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
	db      *db.DB
	fetcher *fetcher.Factory
}

// NewFeedService returns a new [LocalService] backed by the given database.
func NewFeedService(database *db.DB, f *fetcher.Factory) *LocalService {
	return &LocalService{
		db:      database,
		fetcher: f,
	}
}

// credentialCookies returns HTTP cookies from a credential's config, or nil
// if the credential is nil or has no applicable cookies. Supports credential
// types: "substack_cookie" (config key "connect_sid" or "substack_sid").
func credentialCookies(cred *db.Credential) []*http.Cookie {
	if cred == nil || cred.Config == "" {
		return nil
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(cred.Config), &cfg); err != nil {
		return nil
	}
	switch cred.Type {
	case "substack_cookie":
		sid := cfg["connect_sid"]
		if sid == "" {
			sid = cfg["substack_sid"]
		}
		if sid == "" {
			return nil
		}
		return []*http.Cookie{
			{Name: "connect.sid", Value: sid, Path: "/"},
			{Name: "substack.sid", Value: sid, Path: "/"},
		}
	default:
		return nil
	}
}

// lookupCredential fetches the credential for a feed, or nil if none is set.
func (s *LocalService) lookupCredential(ctx context.Context, userID string, credentialID *string) *db.Credential {
	if credentialID == nil {
		return nil
	}
	cred, _ := s.db.GetCredentialByID(ctx, userID, *credentialID)
	return cred
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

	id, err := s.db.InsertDestination(ctx, userID, dest)
	if err != nil {
		return "", err
	}

	if isDefault {
		if err := s.db.SetDefaultDestination(ctx, userID, id); err != nil {
			return id, fmt.Errorf("destination created but failed to set default: %w", err)
		}
	}
	return id, nil
}

func (s *LocalService) ListDestinations(ctx context.Context) ([]db.Destination, error) {
	return s.db.GetDestinations(ctx, UserIDFromContext(ctx))
}

func (s *LocalService) RemoveDestination(ctx context.Context, id string) error {
	return s.db.RemoveDestination(ctx, UserIDFromContext(ctx), id)
}

func (s *LocalService) SetDefaultDestination(ctx context.Context, id string) error {
	return s.db.SetDefaultDestination(ctx, UserIDFromContext(ctx), id)
}

func (s *LocalService) TestDestination(ctx context.Context, id string) error {
	destRecord, err := s.db.GetDestinationByID(ctx, UserIDFromContext(ctx), id)
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
	return s.db.UpdateDestinationConfig(ctx, UserIDFromContext(ctx), id, string(configJSON))
}

func (s *LocalService) UpdateDestination(ctx context.Context, id string, name string, config map[string]string) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return s.db.UpdateDestination(ctx, UserIDFromContext(ctx), id, name, string(configJSON))
}

func (s *LocalService) GetDestinationByID(ctx context.Context, id string) (*db.Destination, error) {
	return s.db.GetDestinationByID(ctx, UserIDFromContext(ctx), id)
}

// AddFeed adds a new feed to the database with a default individual delivery.
func (s *LocalService) AddFeed(ctx context.Context, feed db.Feed) error {
	userID := UserIDFromContext(ctx)
	feed.URL = NormalizeURL(feed.URL)
	existing, err := s.db.GetActiveFeedByURL(ctx, userID, feed.URL)
	if err != nil {
		return err
	}

	if existing != nil {
		if feed.Name == "" {
			feed.Name = existing.Name
		}
		feed.Active = true
		feed.ID = existing.ID
		if err := s.db.UpdateFeed(ctx, userID, feed); err != nil {
			return err
		}
		return s.db.DeactivateFeedsByURLExceptID(ctx, userID, feed.URL, feed.ID)
	}

	if feed.Name == "" {
		feed.Name = GenerateNameFromURL(feed.URL)
	}
	feed.Active = true
	feedID, err := s.db.InsertFeed(ctx, userID, feed)
	if err != nil {
		return err
	}

	// Create default individual delivery
	fd := db.FeedDelivery{
		FeedID:    feedID,
		Directory: SanitizeFilename(feed.Name),
	}
	return s.db.SetFeedDelivery(ctx, userID, fd)
}

func (s *LocalService) UpdateFeed(ctx context.Context, feed db.Feed) error {
	return s.db.UpdateFeed(ctx, UserIDFromContext(ctx), feed)
}

// RemoveFeed marks a feed as inactive.
func (s *LocalService) RemoveFeed(ctx context.Context, feedURL string) error {
	feedURL = NormalizeURL(feedURL)
	return s.db.DeactivateFeed(ctx, UserIDFromContext(ctx), feedURL)
}

func (s *LocalService) RemoveFeedByID(ctx context.Context, id string) error {
	return s.db.DeactivateFeedByID(ctx, UserIDFromContext(ctx), id)
}

// ListFeeds returns all active feeds.
func (s *LocalService) ListFeeds(ctx context.Context) ([]db.Feed, error) {
	return s.db.GetActiveFeeds(ctx, UserIDFromContext(ctx))
}

func (s *LocalService) GetFeedDelivery(ctx context.Context, feedID string) (*db.FeedDelivery, error) {
	return s.db.GetFeedDelivery(ctx, UserIDFromContext(ctx), feedID)
}

func (s *LocalService) SetFeedDelivery(ctx context.Context, fd db.FeedDelivery) error {
	return s.db.SetFeedDelivery(ctx, UserIDFromContext(ctx), fd)
}

func (s *LocalService) RemoveFeedDelivery(ctx context.Context, feedID string) error {
	return s.db.RemoveFeedDelivery(ctx, UserIDFromContext(ctx), feedID)
}

func (s *LocalService) AddDigest(ctx context.Context, digest db.Digest) (string, error) {
	if digest.Directory == "" {
		digest.Directory = digest.Name
	}
	digest.Active = true
	return s.db.InsertDigest(ctx, UserIDFromContext(ctx), digest)
}

func (s *LocalService) ListDigests(ctx context.Context) ([]db.Digest, error) {
	return s.db.GetDigests(ctx, UserIDFromContext(ctx))
}

func (s *LocalService) RemoveDigest(ctx context.Context, id string) error {
	return s.db.RemoveDigest(ctx, UserIDFromContext(ctx), id)
}

func (s *LocalService) UpdateDigest(ctx context.Context, digest db.Digest) error {
	return s.db.UpdateDigest(ctx, UserIDFromContext(ctx), digest)
}

func (s *LocalService) GetDigestByID(ctx context.Context, id string) (*db.Digest, error) {
	return s.db.GetDigestByID(ctx, UserIDFromContext(ctx), id)
}

func (s *LocalService) GetDigestsForFeed(ctx context.Context, feedID string) ([]db.Digest, error) {
	return s.db.GetDigestsForFeed(ctx, UserIDFromContext(ctx), feedID)
}

func (s *LocalService) GetNewEntriesForDigest(ctx context.Context, digestID string, afterID int64) ([]db.Entry, error) {
	return s.db.GetNewEntriesForDigest(ctx, digestID, afterID)
}

func (s *LocalService) AddFeedToDigest(ctx context.Context, digestID, feedID string, alsoIndividual bool) error {
	if err := s.db.AddFeedToDigest(ctx, digestID, feedID); err != nil {
		return err
	}
	if !alsoIndividual {
		return s.db.RemoveFeedDelivery(ctx, UserIDFromContext(ctx), feedID)
	}
	return nil
}

func (s *LocalService) RemoveFeedFromDigest(ctx context.Context, digestID, feedID string) error {
	return s.db.RemoveFeedFromDigest(ctx, digestID, feedID)
}

func (s *LocalService) ListDigestFeeds(ctx context.Context, digestID string) ([]db.Feed, error) {
	return s.db.GetFeedsForDigest(ctx, UserIDFromContext(ctx), digestID)
}

func (s *LocalService) ListRecentDeliveries(ctx context.Context, limit int) ([]db.DeliveryLogEntry, error) {
	return s.db.GetRecentDeliveries(ctx, UserIDFromContext(ctx), limit)
}

func (s *LocalService) GenerateDigest(ctx context.Context, digestID string, onEvent func(PollEvent)) error {
	userID := UserIDFromContext(ctx)
	digest, err := s.db.GetDigestByID(ctx, userID, digestID)
	if err != nil {
		return fmt.Errorf("failed to get digest: %w", err)
	}
	if digest == nil {
		return fmt.Errorf("digest not found: %s", digestID)
	}

	slog.Info("starting generation", "component", "digest", "name", digest.Name, "id", digestID, "cursor", digest.LastDeliveredID)
	onEvent(PollEvent{Type: EventStart, Message: fmt.Sprintf("Generating digest: %s", digest.Name)})

	entries, err := s.db.GetNewEntriesForDigest(ctx, digestID, digest.LastDeliveredID)
	if err != nil {
		return fmt.Errorf("failed to get entries: %w", err)
	}
	if len(entries) == 0 {
		slog.Info("no new articles", "component", "digest", "name", digest.Name)
		onEvent(PollEvent{Type: EventFinish, Message: "No new articles for digest"})
		return nil
	}

	slog.Info("found new entries", "component", "digest", "count", len(entries), "name", digest.Name)

	// Build feed name lookup
	digestFeeds, _ := s.db.GetFeedsForDigest(ctx, userID, digestID)
	feedNames := make(map[string]string)
	for _, f := range digestFeeds {
		feedNames[f.ID] = f.Name
	}

	// Render articles
	var articles []converter.DigestArticle
	var maxEntryID int64
	var failedEntries []db.Entry

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		slog.Info("rendering article", "component", "digest", "title", entry.Title, "name", digest.Name)
		onEvent(PollEvent{Type: EventItemFound, ItemTitle: entry.Title, Message: "Rendering for digest"})

		result, err := s.fetcher.FetchContent(ctx, entry.URL, entry.Content, userID)
		if err != nil {
			slog.Error("processing failed", "component", "digest", "title", entry.Title, "error", err)
			onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("Processing failed: %v", err)})
			failedEntries = append(failedEntries, entry)
			continue
		}
		title := result.Title
		if title == "" {
			title = entry.Title
		}
		byline := result.Byline
		content := result.Content
		articles = append(articles, converter.DigestArticle{
			Title: title, Byline: byline, Content: content,
			FeedName: feedNames[entry.FeedID],
		})
		if entry.ID > maxEntryID {
			maxEntryID = entry.ID
		}
	}

	// Include failed entries in cursor advancement
	for _, fe := range failedEntries {
		if fe.ID > maxEntryID {
			maxEntryID = fe.ID
		}
	}

	if len(articles) == 0 {
		slog.Error("all articles failed to render", "component", "digest", "name", digest.Name)
		onEvent(PollEvent{Type: EventFinish, Message: "All articles failed to render"})
		// Re-enqueue failed entries even if nothing was delivered
		s.reEnqueueEntries(ctx, userID, failedEntries)
		return nil
	}

	// Generate combined PDF
	slog.Info("generating combined HTML", "component", "digest", "name", digest.Name, "count", len(articles))
	htmlPath, err := converter.GenerateDigestHTML(ctx, digest.Name, articles)
	if err != nil {
		return fmt.Errorf("digest HTML generation failed: %w", err)
	}
	defer os.Remove(htmlPath)

	// Per-render temp directory for isolation
	renderDir, err := os.MkdirTemp("", "rss2rm-digest-*")
	if err != nil {
		return fmt.Errorf("failed to create render dir: %w", err)
	}
	defer os.RemoveAll(renderDir)

	pdfName := fmt.Sprintf("%s - %s.pdf", time.Now().Format("2006-01-02"), SanitizeFilename(digest.Name))
	tmpPDF := filepath.Join(renderDir, pdfName)

	slog.Info("converting to PDF", "component", "digest", "path", pdfName)
	if err := converter.HTMLToPDF(ctx, htmlPath, tmpPDF, DefaultHeadlessCommand); err != nil {
		return fmt.Errorf("digest PDF conversion failed: %w", err)
	}

	// Resolve destination and upload
	destInstance, destID, err := s.resolveDestination(ctx, userID, digest.DestinationID)
	if err != nil {
		return fmt.Errorf("digest destination error: %w", err)
	}

	uploadTarget := digest.Directory
	if uploadTarget == "" {
		uploadTarget = SanitizeFilename(digest.Name)
	}

	slog.Info("uploading", "component", "digest", "path", pdfName, "target", uploadTarget, "dest", destID)
	remotePath, err := destInstance.Upload(ctx, tmpPDF, uploadTarget)
	if err != nil {
		slog.Error("upload failed", "component", "digest", "name", digest.Name, "error", err)
		return fmt.Errorf("digest upload failed: %w", err)
	}

	// Persist updated config
	s.persistDestinationConfig(ctx, userID, destInstance, destID)

	// Record delivered file for retention tracking
	digestDestID := ""
	if digest.DestinationID != nil {
		digestDestID = *digest.DestinationID
	}
	s.db.RecordDeliveredFile(ctx, db.DeliveredFile{
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
	if err := s.db.MarkDigestGenerated(ctx, digestID, maxEntryID); err != nil {
		return fmt.Errorf("failed to mark digest generated: %w", err)
	}

	// Re-enqueue failed entries above the new cursor so they appear in the next run
	s.reEnqueueEntries(ctx, userID, failedEntries)

	slog.Info("digest uploaded successfully", "component", "digest", "name", digest.Name, "count", len(articles), "cursor", maxEntryID)
	onEvent(PollEvent{Type: EventItemUploaded, Message: fmt.Sprintf("Digest uploaded: %d articles", len(articles))})
	onEvent(PollEvent{Type: EventFinish, Message: "Digest generation complete"})
	return nil
}

// reEnqueueEntries re-creates failed entries with new IDs so they appear
// above the digest cursor in the next generation run.
func (s *LocalService) reEnqueueEntries(ctx context.Context, userID string, entries []db.Entry) {
	for _, e := range entries {
		newEntry := db.Entry{
			FeedID:    e.FeedID,
			EntryID:   fmt.Sprintf("%s-retry-%d", e.EntryID, time.Now().UnixNano()),
			Title:     e.Title,
			URL:       e.URL,
			Published: e.Published,
			Content:   e.Content,
		}
		if err := s.db.CreateEntry(ctx, userID, newEntry); err != nil {
			slog.Error("failed to re-enqueue", "component", "digest", "title", e.Title, "error", err)
			continue
		}
		slog.Info("re-enqueued for next digest run", "component", "digest", "title", e.Title)
	}
}

// PollFeeds fetches new items from active feeds, discovers entries, and
// delivers them to their configured destinations. If filterURLs is
// non-empty, only matching feeds are polled.
func (s *LocalService) PollFeeds(ctx context.Context, filterURLs []string, onEvent func(PollEvent)) error {
	userID := UserIDFromContext(ctx)
	feeds, err := s.db.GetActiveFeeds(ctx, userID)
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

	slog.Info("polling feeds", "component", "poll", "count", len(feeds))

	for _, feed := range feeds {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		s.processFeed(ctx, feed, onEvent, opts)
	}

	slog.Info("polling complete", "component", "poll")
	return nil
}

func (s *LocalService) processFeed(ctx context.Context, feed db.Feed, onEvent func(PollEvent), opts PollOptions) {
	userID := UserIDFromContext(ctx)
	slog.Info("starting poll for feed", "component", "poll", "name", feed.Name, "url", feed.URL)
	onEvent(PollEvent{FeedURL: feed.URL, Type: EventStart, Message: "Starting poll"})

	limit := feed.Backfill
	if limit == 0 {
		limit = defaultBackfillCount
	}
	if opts.BackfillLimit > 0 {
		limit = opts.BackfillLimit
	}

	// Look up credential for authenticated fetching (e.g., Substack)
	cred := s.lookupCredential(ctx, userID, feed.CredentialID)
	var fetchOpts *importer.FetchOptions
	if cookies := credentialCookies(cred); len(cookies) > 0 {
		fetchOpts = &importer.FetchOptions{Cookies: cookies}
	}

	items, err := importer.Fetch(ctx, feed.URL, limit, fetchOpts)
	if err != nil {
		slog.Error("fetch failed", "component", "poll", "name", feed.Name, "error", err)
		onEvent(PollEvent{FeedURL: feed.URL, Type: EventError, Message: fmt.Sprintf("Fetch failed: %v", err)})
		return
	}
	slog.Info("fetched items", "component", "poll", "count", len(items), "name", feed.Name)
	if len(items) == 0 {
		onEvent(PollEvent{FeedURL: feed.URL, Type: EventFinish, Message: "No items found"})
		return
	}

	// Discover entries
	newCount := 0
	for _, item := range items {
		seen, err := s.db.HasEntry(ctx, userID, feed.ID, item.GUID)
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
			s.db.CreateEntry(ctx, userID, entry)
		}
	}
	if newCount > 0 {
		slog.Info("discovered new entries", "component", "poll", "count", newCount, "name", feed.Name)
	}

	// Individual delivery
	fd, err := s.db.GetFeedDelivery(ctx, userID, feed.ID)
	if err != nil || fd == nil {
		s.db.MarkFeedPolled(ctx, feed.ID)
		slog.Info("feed has no individual delivery configured", "component", "poll", "name", feed.Name)
		onEvent(PollEvent{FeedURL: feed.URL, Type: EventFinish, Message: "Poll complete (digest only)"})
		return
	}

	undelivered, err := s.db.GetUndeliveredEntries(ctx, feed.ID, fd.LastDeliveredID)
	if err != nil {
		slog.Error("failed to get undelivered entries", "component", "poll", "name", feed.Name, "error", err)
		onEvent(PollEvent{FeedURL: feed.URL, Type: EventError, Message: fmt.Sprintf("Failed to get entries: %v", err)})
		return
	}

	if len(undelivered) > 0 {
		slog.Info("entries to deliver individually", "component", "delivery", "count", len(undelivered), "name", feed.Name, "directory", fd.Directory)
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
		s.db.AdvanceFeedDelivery(ctx, feed.ID, entry.ID)
	}

	s.db.MarkFeedPolled(ctx, feed.ID)
	slog.Info("poll complete", "component", "poll", "name", feed.Name)
	onEvent(PollEvent{FeedURL: feed.URL, Type: EventFinish, Message: "Poll complete"})
}

// deliverEntry renders and uploads a single entry for individual delivery.
func (s *LocalService) deliverEntry(ctx context.Context, fd db.FeedDelivery, entry db.Entry, onEvent func(PollEvent)) error {
	userID := UserIDFromContext(ctx)
	slog.Info("processing entry", "component", "delivery", "title", entry.Title, "id", entry.ID)

	destInstance, destID, err := s.resolveDestination(ctx, userID, fd.DestinationID)
	if err != nil {
		slog.Error("destination error", "component", "delivery", "title", entry.Title, "error", err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("Destination error: %v", err)})
		return err
	}

	result, err := s.fetcher.FetchContent(ctx, entry.URL, entry.Content, userID)
	if err != nil {
		slog.Error("article processing failed", "component", "delivery", "title", entry.Title, "error", err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("Processing failed: %v", err)})
		return err
	}

	htmlPath, err := converter.GenerateHTML(ctx, result.Title, result.Content, result.Byline)
	if err != nil {
		slog.Error("HTML generation failed", "component", "delivery", "title", entry.Title, "error", err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("HTML generation failed: %v", err)})
		return err
	}
	defer os.Remove(htmlPath)

	// Per-render temp directory for isolation
	renderDir, err := os.MkdirTemp("", "rss2rm-render-*")
	if err != nil {
		return fmt.Errorf("failed to create render dir: %w", err)
	}
	defer os.RemoveAll(renderDir)

	pdfName := fmt.Sprintf("%s - %s.pdf", entry.Published.Format("2006-01-02"), SanitizeFilename(result.Title))
	tmpPDF := filepath.Join(renderDir, pdfName)

	slog.Info("converting to PDF", "component", "delivery", "path", pdfName)
	if err := converter.HTMLToPDF(ctx, htmlPath, tmpPDF, DefaultHeadlessCommand); err != nil {
		slog.Error("PDF conversion failed", "component", "delivery", "title", entry.Title, "error", err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("PDF conversion failed: %v", err)})
		return err
	}

	uploadTarget := fd.Directory
	if uploadTarget == "" {
		uploadTarget = "RSS"
	}

	slog.Info("uploading", "component", "delivery", "path", pdfName, "target", uploadTarget, "dest", destID)
	remotePath, err := destInstance.Upload(ctx, tmpPDF, uploadTarget)
	if err != nil {
		slog.Error("upload failed", "component", "delivery", "title", entry.Title, "error", err)
		onEvent(PollEvent{Type: EventError, ItemTitle: entry.Title, Message: fmt.Sprintf("Upload failed: %v", err)})
		return err
	}

	// Persist updated config (token refresh etc.)
	s.persistDestinationConfig(ctx, userID, destInstance, destID)

	// Record delivered file for retention tracking
	fdDestID := ""
	if fd.DestinationID != nil {
		fdDestID = *fd.DestinationID
	}
	s.db.RecordDeliveredFile(ctx, db.DeliveredFile{
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

	slog.Info("uploaded successfully", "component", "delivery", "title", entry.Title)
	onEvent(PollEvent{Type: EventItemUploaded, ItemTitle: entry.Title, Message: "Uploaded"})
	return nil
}

// DeliverArticle processes and delivers a single article from an external
// source (e.g., API ingest, webhook). If content is provided, it is used
// directly; otherwise the article is fetched from url via readability.
// If digestID is non-empty, the article is added to that digest's virtual
// feed instead of being delivered immediately.
func (s *LocalService) DeliverArticle(ctx context.Context, title, articleURL, content, destID, directory, digestID string) error {
	userID := UserIDFromContext(ctx)

	// If targeting a digest, store the article as an entry on a virtual feed
	if digestID != "" {
		return s.ingestToDigest(ctx, userID, title, articleURL, content, digestID)
	}

	// Resolve content if not provided
	byline := ""
	if content == "" {
		if articleURL == "" {
			return fmt.Errorf("either url or content is required")
		}
		result, err := s.fetcher.FetchContent(ctx, articleURL, "", userID)
		if err != nil {
			return fmt.Errorf("failed to process article: %w", err)
		}
		title = result.Title
		byline = result.Byline
		content = result.Content
	}

	// Resolve destination
	var destPtr *string
	if destID != "" {
		destPtr = &destID
	}
	destInstance, resolvedDestID, err := s.resolveDestination(ctx, userID, destPtr)
	if err != nil {
		return fmt.Errorf("destination error: %w", err)
	}

	// Generate PDF
	htmlPath, err := converter.GenerateHTML(ctx, title, content, byline)
	if err != nil {
		return fmt.Errorf("HTML generation failed: %w", err)
	}
	defer os.Remove(htmlPath)

	renderDir, err := os.MkdirTemp("", "rss2rm-ingest-*")
	if err != nil {
		return fmt.Errorf("failed to create render dir: %w", err)
	}
	defer os.RemoveAll(renderDir)

	pdfName := fmt.Sprintf("%s - %s.pdf", time.Now().Format("2006-01-02"), SanitizeFilename(title))
	tmpPDF := filepath.Join(renderDir, pdfName)

	slog.Info("converting to PDF", "component", "ingest", "path", pdfName)
	if err := converter.HTMLToPDF(ctx, htmlPath, tmpPDF, DefaultHeadlessCommand); err != nil {
		return fmt.Errorf("PDF conversion failed: %w", err)
	}

	// Upload
	if directory == "" {
		directory = "Articles"
	}
	slog.Info("uploading", "component", "ingest", "path", pdfName, "target", directory, "dest", resolvedDestID)
	remotePath, err := destInstance.Upload(ctx, tmpPDF, directory)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	s.persistDestinationConfig(ctx, userID, destInstance, resolvedDestID)

	s.db.RecordDeliveredFile(ctx, db.DeliveredFile{
		UserID:        userID,
		DeliveryType:  "article",
		DeliveryRef:   "ingest",
		RemotePath:    remotePath,
		DestinationID: resolvedDestID,
	})

	slog.Info("delivered successfully", "component", "ingest", "title", title)
	return nil
}

// ingestToDigest adds an article to a digest's virtual feed so it will be
// included in the next digest generation.
func (s *LocalService) ingestToDigest(ctx context.Context, userID, title, articleURL, content, digestID string) error {
	// Verify the digest exists
	digest, err := s.db.GetDigestByID(ctx, userID, digestID)
	if err != nil {
		return fmt.Errorf("failed to get digest: %w", err)
	}
	if digest == nil {
		return fmt.Errorf("digest not found: %s", digestID)
	}

	// Get or create the virtual feed for this digest
	virtualFeedURL := "_ingest:" + digestID
	feed, _ := s.db.GetActiveFeedByURL(ctx, userID, virtualFeedURL)
	var feedID string
	if feed == nil {
		newFeed := db.Feed{
			URL:    virtualFeedURL,
			Name:   digest.Name + " (ingested)",
			Active: true,
		}
		id, err := s.db.InsertFeed(ctx, userID, newFeed)
		if err != nil {
			return fmt.Errorf("failed to create virtual feed: %w", err)
		}
		feedID = id
		s.db.AddFeedToDigest(ctx, digestID, feedID)
	} else {
		feedID = feed.ID
	}

	// If content not provided, fetch it
	if content == "" && articleURL != "" {
		result, err := s.fetcher.FetchContent(ctx, articleURL, "", userID)
		if err != nil {
			return fmt.Errorf("failed to process article: %w", err)
		}
		content = result.Content
		if title == "" {
			title = result.Title
		}
	}

	// Create entry on the virtual feed
	entryID := fmt.Sprintf("ingest-%d", time.Now().UnixNano())
	entry := db.Entry{
		FeedID:    feedID,
		EntryID:   entryID,
		Title:     title,
		URL:       articleURL,
		Published: time.Now(),
		Content:   content,
	}
	if err := s.db.CreateEntry(ctx, userID, entry); err != nil {
		return fmt.Errorf("failed to create entry: %w", err)
	}

	slog.Info("added to digest", "component", "ingest", "title", title, "name", digest.Name)
	return nil
}

// resolveDestination creates a Destination instance from a destination ID,
// falling back to the system default if destID is nil.
func (s *LocalService) resolveDestination(ctx context.Context, userID string, destID *string) (Destination, string, error) {
	var destRecord *db.Destination
	var err error
	if destID != nil {
		destRecord, err = s.db.GetDestinationByID(ctx, userID, *destID)
	} else {
		destRecord, err = s.db.GetDefaultDestination(ctx, userID)
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
func (s *LocalService) persistDestinationConfig(ctx context.Context, userID string, dest Destination, destID string) {
	if updater, ok := dest.(ConfigUpdater); ok {
		if newConfig := updater.GetUpdatedConfig(); newConfig != nil {
			configJSON, _ := json.Marshal(newConfig)
			s.db.UpdateDestinationConfig(ctx, userID, destID, string(configJSON))
		}
	}
}

// cleanupOldDeliveries removes delivered files beyond the retention limit
// from both the destination and the tracking database.
func (s *LocalService) cleanupOldDeliveries(ctx context.Context, userID, deliveryType, deliveryRef string, retain int) {
	files, err := s.db.GetDeliveredFiles(ctx, userID, deliveryType, deliveryRef)
	if err != nil {
		slog.Error("failed to get delivered files", "component", "cleanup", "error", err)
		return
	}
	if len(files) <= retain {
		return
	}

	toDelete := files[retain:]
	slog.Info("removing old deliveries", "component", "cleanup", "count", len(toDelete), "type", deliveryType, "ref", deliveryRef, "keeping", retain)

	for _, f := range toDelete {
		var destID *string
		if f.DestinationID != "" {
			destID = &f.DestinationID
		}
		dest, _, err := s.resolveDestination(ctx, userID, destID)
		if err != nil {
			slog.Error("cannot resolve destination", "component", "cleanup", "path", f.RemotePath, "error", err)
			s.db.DeleteDeliveredFile(ctx, f.ID)
			continue
		}
		if err := dest.Delete(ctx, f.RemotePath); err != nil {
			slog.Error("failed to delete", "component", "cleanup", "path", f.RemotePath, "error", err)
			// Still remove the tracking record — the file may already be gone
		}
		s.db.DeleteDeliveredFile(ctx, f.ID)
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

// --- Webhook management ---

// AddWebhook creates a new webhook for the authenticated user.
func (s *LocalService) AddWebhook(ctx context.Context, webhookType, secret, config string) (string, error) {
	userID := UserIDFromContext(ctx)
	w := db.Webhook{
		Type:   webhookType,
		Secret: secret,
		Config: config,
		Active: true,
	}
	return s.db.InsertWebhook(ctx, userID, w)
}

// ListWebhooks returns all webhooks for the authenticated user.
func (s *LocalService) ListWebhooks(ctx context.Context) ([]db.Webhook, error) {
	return s.db.GetWebhooks(ctx, UserIDFromContext(ctx))
}

// RemoveWebhook deletes a webhook.
func (s *LocalService) RemoveWebhook(ctx context.Context, id string) error {
	return s.db.DeleteWebhook(ctx, UserIDFromContext(ctx), id)
}

func (s *LocalService) GetWebhookByID(ctx context.Context, id string) (*db.Webhook, error) {
	return s.db.GetWebhookByID(ctx, UserIDFromContext(ctx), id)
}

// ListCredentials returns all credentials for the authenticated user.
func (s *LocalService) ListCredentials(ctx context.Context) ([]db.Credential, error) {
	return s.db.GetCredentials(ctx, UserIDFromContext(ctx))
}

// GetCredentialByID returns a single credential by ID.
func (s *LocalService) GetCredentialByID(ctx context.Context, id string) (*db.Credential, error) {
	return s.db.GetCredentialByID(ctx, UserIDFromContext(ctx), id)
}

// AddCredential creates a new credential and returns its ID.
func (s *LocalService) AddCredential(ctx context.Context, name, credType, config string) (string, error) {
	cred := db.Credential{
		Name:   name,
		Type:   credType,
		Config: config,
	}
	return s.db.InsertCredential(ctx, UserIDFromContext(ctx), cred)
}

// UpdateCredential updates an existing credential.
func (s *LocalService) UpdateCredential(ctx context.Context, cred db.Credential) error {
	return s.db.UpdateCredential(ctx, UserIDFromContext(ctx), cred)
}

// RemoveCredential deletes a credential by ID.
func (s *LocalService) RemoveCredential(ctx context.Context, id string) error {
	return s.db.DeleteCredential(ctx, UserIDFromContext(ctx), id)
}
