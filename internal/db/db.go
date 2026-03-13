// Package db provides SQLite storage for rss2rm, including schema
// management, migrations, and CRUD operations for feeds, entries,
// digests, destinations, and delivery configuration.
package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// DB wraps a [sql.DB] connection with driver-specific behavior.
type DB struct {
	*sql.DB
	driver string // "sqlite3" or "mysql"
}

// Feed represents an RSS/Atom feed subscription.
type Feed struct {
	ID         string
	URL        string
	Name       string
	LastPolled time.Time
	Active     bool
	Backfill   int
	UserID     string
}

// Destination represents a configured upload target (e.g., reMarkable, email, file).
type Destination struct {
	ID        string
	Name      string
	Type      string
	Config    string // JSON blob
	IsDefault bool
	UserID    string
}

// Entry represents a single article fetched from a feed.
type Entry struct {
	ID        int64
	FeedID    string
	EntryID   string
	Title     string
	URL       string
	Published time.Time
	Rendered  string // path to rendered PDF, empty if not yet rendered
	UserID    string
}

// Digest represents a scheduled collection of articles from multiple feeds,
// delivered as a single combined PDF.
type Digest struct {
	ID              string
	Name            string
	Directory       string
	Schedule        string // daily time, e.g. "07:00"
	DestinationID   *string
	LastGenerated   time.Time
	LastDeliveredID int64 // cursor: last entry ID included in digest
	Active          bool
	Retain          int
	UserID          string
}

// FeedDelivery tracks individual delivery configuration and progress for a feed.
type FeedDelivery struct {
	FeedID          string
	Directory       string
	DestinationID   *string
	LastDeliveredID int64 // cursor: last entry ID delivered individually
	Retain          int
	UserID          string
}

// User represents a registered user account.
type User struct {
	ID            string
	Email         string
	PasswordHash  string
	Verified      bool
	VerifyToken   string
	VerifyExpires time.Time
	CreatedAt     time.Time
}

// Session represents an active authentication session.
type Session struct {
	Token     string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// DeliveredFile tracks a file uploaded to a destination for retention management.
type DeliveredFile struct {
	ID            int64
	UserID        string
	DeliveryType  string // "individual" or "digest"
	DeliveryRef   string // feed_id or digest_id
	EntryID       int64  // entry row ID (0 for digests)
	RemotePath    string // path/ID returned by Upload
	DestinationID string
	DeliveredAt   time.Time
}

// DeliveryLogEntry is a denormalized view of a delivery for display purposes,
// joining delivered_files with entries, feeds, destinations, and digests.
type DeliveryLogEntry struct {
	ID           int64
	DeliveryType string // "individual" or "digest"
	Title        string // entry title or digest name
	FeedName     string // feed name (empty for digests)
	URL          string // article URL (empty for digests)
	DestName     string // destination name
	DestType     string // destination type (e.g., "remarkable")
	DeliveredAt  time.Time
}

const sessionTokenBytes = 32
const sessionDuration = 30 * 24 * time.Hour // 30 days

// Open opens or creates the database, initializing the schema and
// running any pending migrations. The driver should be "sqlite3" or
// "mysql", and dsn is the driver-specific connection string.
func Open(driver, dsn string) (*DB, error) {
	sqlDB, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}

	d := &DB{DB: sqlDB, driver: driver}

	if err := d.initSchema(); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return d, nil
}

func (d *DB) initSchema() error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE IF NOT EXISTS feeds (
			id TEXT PRIMARY KEY,
			url TEXT,
			name TEXT,
			last_polled TIMESTAMP,
			active BOOLEAN,
			backfill INTEGER,
			user_id TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS destinations (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE,
			type TEXT,
			config TEXT,
			is_default BOOLEAN,
			user_id TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS entries (
			id INTEGER PRIMARY KEY` + d.autoIncrement() + `,
			feed_id TEXT,
			entry_id TEXT,
			title TEXT,
			url TEXT,
			published TIMESTAMP,
			rendered TEXT,
			user_id TEXT,
			UNIQUE(feed_id, entry_id),
			FOREIGN KEY(feed_id) REFERENCES feeds(id)
		)`,
		`CREATE TABLE IF NOT EXISTS digests (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE,
			directory TEXT,
			schedule TEXT,
			destination_id TEXT,
			last_generated TIMESTAMP,
			last_delivered_id INTEGER DEFAULT 0,
			active BOOLEAN DEFAULT 1,
			retain INTEGER DEFAULT 0,
			user_id TEXT,
			FOREIGN KEY(destination_id) REFERENCES destinations(id)
		)`,
		`CREATE TABLE IF NOT EXISTS feed_delivery (
			feed_id TEXT PRIMARY KEY,
			directory TEXT,
			destination_id TEXT,
			last_delivered_id INTEGER DEFAULT 0,
			retain INTEGER DEFAULT 0,
			user_id TEXT,
			FOREIGN KEY(feed_id) REFERENCES feeds(id),
			FOREIGN KEY(destination_id) REFERENCES destinations(id)
		)`,
		`CREATE TABLE IF NOT EXISTS digest_feeds (
			digest_id TEXT,
			feed_id TEXT,
			PRIMARY KEY(digest_id, feed_id),
			FOREIGN KEY(digest_id) REFERENCES digests(id),
			FOREIGN KEY(feed_id) REFERENCES feeds(id)
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			verified BOOLEAN DEFAULT 1,
			verify_token TEXT,
			verify_expires TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS delivered_files (
			id INTEGER PRIMARY KEY` + d.autoIncrement() + `,
			user_id TEXT NOT NULL,
			delivery_type TEXT NOT NULL,
			delivery_ref TEXT NOT NULL,
			entry_id INTEGER DEFAULT 0,
			remote_path TEXT NOT NULL,
			destination_id TEXT NOT NULL,
			delivered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	// Create settings first for version check
	if _, err := d.Exec(tables[0]); err != nil {
		return err
	}

	for _, ddl := range tables[1:] {
		if _, err := d.Exec(ddl); err != nil {
			return fmt.Errorf("schema init failed: %w", err)
		}
	}

	d.upsertSetting("schema_version", "5")
	return nil
}

// autoIncrement returns the SQL fragment for auto-incrementing integer PKs.
func (d *DB) autoIncrement() string {
	if d.driver == "mysql" {
		return " AUTO_INCREMENT"
	}
	return "" // SQLite auto-increments INTEGER PRIMARY KEY implicitly
}

// upsertSetting inserts or updates a setting value.
func (d *DB) upsertSetting(key, value string) error {
	if d.driver == "mysql" {
		_, err := d.Exec("INSERT INTO settings (`key`, value) VALUES (?, ?) ON DUPLICATE KEY UPDATE value = ?", key, value, value)
		return err
	}
	_, err := d.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, value)
	return err
}

// --- Destination methods ---

func (db *DB) InsertDestination(userID string, d Destination) (string, error) {
	d.ID = uuid.New().String()
	d.UserID = userID
	_, err := db.Exec(`
		INSERT INTO destinations (id, name, type, config, is_default, user_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, d.ID, d.Name, d.Type, d.Config, d.IsDefault, d.UserID)
	if err != nil {
		return "", err
	}
	return d.ID, nil
}

func (db *DB) GetDestinations(userID string) ([]Destination, error) {
	query := `SELECT id, name, type, config, is_default, user_id FROM destinations WHERE user_id = ?`
	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dests := []Destination{}
	for rows.Next() {
		var d Destination
		if err := rows.Scan(&d.ID, &d.Name, &d.Type, &d.Config, &d.IsDefault, &d.UserID); err != nil {
			return nil, err
		}
		dests = append(dests, d)
	}
	return dests, nil
}

func (db *DB) GetDestinationByID(userID string, id string) (*Destination, error) {
	query := `SELECT id, name, type, config, is_default, user_id FROM destinations WHERE id = ? AND user_id = ?`
	row := db.QueryRow(query, id, userID)
	var d Destination
	if err := row.Scan(&d.ID, &d.Name, &d.Type, &d.Config, &d.IsDefault, &d.UserID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (db *DB) GetDefaultDestination(userID string) (*Destination, error) {
	query := `SELECT id, name, type, config, is_default, user_id FROM destinations WHERE is_default = 1 AND user_id = ? LIMIT 1`
	row := db.QueryRow(query, userID)
	var d Destination
	if err := row.Scan(&d.ID, &d.Name, &d.Type, &d.Config, &d.IsDefault, &d.UserID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (db *DB) SetDefaultDestination(userID string, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE destinations SET is_default = 0 WHERE user_id = ?", userID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE destinations SET is_default = 1 WHERE id = ? AND user_id = ?", id, userID); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) RemoveDestination(userID string, id string) error {
	_, err := db.Exec("DELETE FROM destinations WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (db *DB) UpdateDestinationConfig(userID string, id string, config string) error {
	_, err := db.Exec("UPDATE destinations SET config = ? WHERE id = ? AND user_id = ?", config, id, userID)
	return err
}

func (db *DB) UpdateDestination(userID string, id string, name string, config string) error {
	_, err := db.Exec("UPDATE destinations SET name = ?, config = ? WHERE id = ? AND user_id = ?", name, config, id, userID)
	return err
}

// --- Feed methods ---

func (db *DB) InsertFeed(userID string, f Feed) (string, error) {
	f.ID = uuid.New().String()
	f.UserID = userID
	_, err := db.Exec(`
		INSERT INTO feeds (id, url, name, active, backfill, user_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, f.ID, f.URL, f.Name, f.Active, f.Backfill, f.UserID)
	if err != nil {
		return "", err
	}
	return f.ID, nil
}

func (db *DB) DeactivateFeed(userID string, url string) error {
	_, err := db.Exec("UPDATE feeds SET active = 0 WHERE url = ? AND user_id = ?", url, userID)
	return err
}

func (db *DB) DeactivateFeedByID(userID string, id string) error {
	_, err := db.Exec("UPDATE feeds SET active = 0 WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (db *DB) DeactivateFeedsByURLExceptID(userID string, url string, id string) error {
	_, err := db.Exec("UPDATE feeds SET active = 0 WHERE url = ? AND id != ? AND user_id = ?", url, id, userID)
	return err
}

func (db *DB) GetActiveFeedByURL(userID string, url string) (*Feed, error) {
	query := `SELECT id, url, name, last_polled, active, backfill, user_id FROM feeds WHERE active = 1 AND url = ? AND user_id = ? ORDER BY id LIMIT 1`
	row := db.QueryRow(query, url, userID)

	f, err := scanFeed(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (db *DB) UpdateFeed(userID string, f Feed) error {
	_, err := db.Exec(`
		UPDATE feeds SET url = ?, name = ?, active = ?, backfill = ?
		WHERE id = ? AND user_id = ?
	`, f.URL, f.Name, f.Active, f.Backfill, f.ID, userID)
	return err
}

func (db *DB) GetActiveFeeds(userID string) ([]Feed, error) {
	query := `SELECT id, url, name, last_polled, active, backfill, user_id FROM feeds WHERE active = 1 AND user_id = ?`
	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	feeds := []Feed{}
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, *f)
	}
	return feeds, nil
}

func (db *DB) MarkFeedPolled(id string) error {
	_, err := db.Exec("UPDATE feeds SET last_polled = ? WHERE id = ?", time.Now(), id)
	return err
}

// scanner is implemented by both [sql.Row] and [sql.Rows], allowing
// shared scan helpers for both single-row and multi-row queries.
type scanner interface {
	Scan(dest ...any) error
}

// scanFeed scans a single feed row into a [Feed].
func scanFeed(s scanner) (*Feed, error) {
	var f Feed
	var lastPolled sql.NullTime
	var backfill sql.NullInt32

	if err := s.Scan(&f.ID, &f.URL, &f.Name, &lastPolled, &f.Active, &backfill, &f.UserID); err != nil {
		return nil, err
	}
	if lastPolled.Valid {
		f.LastPolled = lastPolled.Time
	}
	if backfill.Valid {
		f.Backfill = int(backfill.Int32)
	}
	return &f, nil
}

// scanUser scans a single user row into a [User].
func scanUser(s scanner) (*User, error) {
	var u User
	var verified sql.NullBool
	var verifyToken sql.NullString
	var verifyExpires sql.NullTime
	if err := s.Scan(&u.ID, &u.Email, &u.PasswordHash, &verified, &verifyToken, &verifyExpires, &u.CreatedAt); err != nil {
		return nil, err
	}
	u.Verified = !verified.Valid || verified.Bool
	if verifyToken.Valid {
		u.VerifyToken = verifyToken.String
	}
	if verifyExpires.Valid {
		u.VerifyExpires = verifyExpires.Time
	}
	return &u, nil
}

// --- Entry methods ---

func (db *DB) HasEntry(userID string, feedID string, entryID string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM entries WHERE feed_id = ? AND entry_id = ? AND user_id = ?", feedID, entryID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (db *DB) CreateEntry(userID string, e Entry) error {
	_, err := db.Exec(`
		INSERT INTO entries (feed_id, entry_id, title, url, published, rendered, user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, e.FeedID, e.EntryID, e.Title, e.URL, e.Published, e.Rendered, userID)
	return err
}

func (db *DB) GetEntry(userID string, feedID string, entryID string) (*Entry, error) {
	query := `SELECT id, feed_id, entry_id, title, url, published, COALESCE(rendered, ''), user_id FROM entries WHERE feed_id = ? AND entry_id = ? AND user_id = ?`
	row := db.QueryRow(query, feedID, entryID, userID)
	var e Entry
	if err := row.Scan(&e.ID, &e.FeedID, &e.EntryID, &e.Title, &e.URL, &e.Published, &e.Rendered, &e.UserID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// UpdateEntryRendered sets the rendered PDF path for an entry.
func (db *DB) UpdateEntryRendered(entryID int64, rendered string) error {
	_, err := db.Exec("UPDATE entries SET rendered = ? WHERE id = ?", rendered, entryID)
	return err
}

// --- FeedDelivery methods ---

// GetFeedDelivery returns the delivery config for a feed.
func (db *DB) GetFeedDelivery(userID string, feedID string) (*FeedDelivery, error) {
	query := `SELECT feed_id, directory, destination_id, last_delivered_id, retain, user_id FROM feed_delivery WHERE feed_id = ? AND user_id = ?`
	row := db.QueryRow(query, feedID, userID)

	var fd FeedDelivery
	var destID sql.NullString
	if err := row.Scan(&fd.FeedID, &fd.Directory, &destID, &fd.LastDeliveredID, &fd.Retain, &fd.UserID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if destID.Valid {
		fd.DestinationID = &destID.String
	}
	return &fd, nil
}

// SetFeedDelivery upserts a feed delivery record.
func (db *DB) SetFeedDelivery(userID string, fd FeedDelivery) error {
	if db.driver == "mysql" {
		_, err := db.Exec(`INSERT INTO feed_delivery (feed_id, directory, destination_id, last_delivered_id, retain, user_id) VALUES (?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE directory = VALUES(directory), destination_id = VALUES(destination_id), last_delivered_id = VALUES(last_delivered_id), retain = VALUES(retain)`,
			fd.FeedID, fd.Directory, fd.DestinationID, fd.LastDeliveredID, fd.Retain, userID)
		return err
	}
	_, err := db.Exec(`INSERT OR REPLACE INTO feed_delivery (feed_id, directory, destination_id, last_delivered_id, retain, user_id) VALUES (?, ?, ?, ?, ?, ?)`,
		fd.FeedID, fd.Directory, fd.DestinationID, fd.LastDeliveredID, fd.Retain, userID)
	return err
}

// RemoveFeedDelivery deletes the delivery config for a feed.
func (db *DB) RemoveFeedDelivery(userID string, feedID string) error {
	_, err := db.Exec("DELETE FROM feed_delivery WHERE feed_id = ? AND user_id = ?", feedID, userID)
	return err
}

// AdvanceFeedDelivery updates the last_delivered_id cursor.
func (db *DB) AdvanceFeedDelivery(feedID string, entryID int64) error {
	_, err := db.Exec("UPDATE feed_delivery SET last_delivered_id = ? WHERE feed_id = ?", entryID, feedID)
	return err
}

// GetUndeliveredEntries returns entries newer than lastDeliveredID for a feed.
func (db *DB) GetUndeliveredEntries(feedID string, lastDeliveredID int64) ([]Entry, error) {
	query := `SELECT id, feed_id, entry_id, title, url, published, COALESCE(rendered, '') FROM entries WHERE feed_id = ? AND id > ? ORDER BY id`
	rows, err := db.Query(query, feedID, lastDeliveredID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.FeedID, &e.EntryID, &e.Title, &e.URL, &e.Published, &e.Rendered); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// --- Digest methods ---

// InsertDigest creates a new digest.
func (db *DB) InsertDigest(userID string, d Digest) (string, error) {
	d.ID = uuid.New().String()
	d.UserID = userID
	_, err := db.Exec(`
		INSERT INTO digests (id, name, directory, schedule, destination_id, active, retain, user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, d.ID, d.Name, d.Directory, d.Schedule, d.DestinationID, d.Active, d.Retain, d.UserID)
	if err != nil {
		return "", err
	}
	return d.ID, nil
}

// GetDigests returns all digests.
func (db *DB) GetDigests(userID string) ([]Digest, error) {
	query := `SELECT id, name, COALESCE(directory, ''), schedule, destination_id, last_generated, COALESCE(last_delivered_id, 0), active, retain, user_id FROM digests WHERE user_id = ?`
	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	digests := []Digest{}
	for rows.Next() {
		d, err := scanDigest(rows)
		if err != nil {
			return nil, err
		}
		digests = append(digests, *d)
	}
	return digests, nil
}

// GetActiveDigests returns digests where active = 1.
func (db *DB) GetActiveDigests(userID string) ([]Digest, error) {
	query := `SELECT id, name, COALESCE(directory, ''), schedule, destination_id, last_generated, COALESCE(last_delivered_id, 0), active, retain, user_id FROM digests WHERE active = 1 AND user_id = ?`
	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	digests := []Digest{}
	for rows.Next() {
		d, err := scanDigest(rows)
		if err != nil {
			return nil, err
		}
		digests = append(digests, *d)
	}
	return digests, nil
}

// GetDigestByID returns a single digest by ID.
func (db *DB) GetDigestByID(userID string, id string) (*Digest, error) {
	query := `SELECT id, name, COALESCE(directory, ''), schedule, destination_id, last_generated, COALESCE(last_delivered_id, 0), active, retain, user_id FROM digests WHERE id = ? AND user_id = ?`
	row := db.QueryRow(query, id, userID)

	d, err := scanDigest(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// UpdateDigest updates a digest's mutable fields.
func (db *DB) UpdateDigest(userID string, d Digest) error {
	_, err := db.Exec(`UPDATE digests SET name = ?, directory = ?, schedule = ?, destination_id = ?, active = ?, retain = ? WHERE id = ? AND user_id = ?`,
		d.Name, d.Directory, d.Schedule, d.DestinationID, d.Active, d.Retain, d.ID, userID)
	return err
}

// RemoveDigest deletes a digest and its feed associations.
func (db *DB) RemoveDigest(userID string, id string) error {
	if _, err := db.Exec("DELETE FROM digest_feeds WHERE digest_id = ?", id); err != nil {
		return err
	}
	_, err := db.Exec("DELETE FROM digests WHERE id = ? AND user_id = ?", id, userID)
	return err
}

// MarkDigestGenerated updates last_generated and last_delivered_id.
func (db *DB) MarkDigestGenerated(id string, lastEntryID int64) error {
	_, err := db.Exec("UPDATE digests SET last_generated = ?, last_delivered_id = ? WHERE id = ?", time.Now(), lastEntryID, id)
	return err
}

// scanDigest scans a single digest row into a [Digest].
func scanDigest(s scanner) (*Digest, error) {
	var d Digest
	var destID sql.NullString
	var lastGen sql.NullTime

	if err := s.Scan(&d.ID, &d.Name, &d.Directory, &d.Schedule, &destID, &lastGen, &d.LastDeliveredID, &d.Active, &d.Retain, &d.UserID); err != nil {
		return nil, err
	}
	if destID.Valid {
		d.DestinationID = &destID.String
	}
	if lastGen.Valid {
		d.LastGenerated = lastGen.Time
	}
	return &d, nil
}

// --- digest_feeds methods ---

// AddFeedToDigest links a feed to a digest.
func (db *DB) AddFeedToDigest(digestID, feedID string) error {
	if db.driver == "mysql" {
		_, err := db.Exec("INSERT IGNORE INTO digest_feeds (digest_id, feed_id) VALUES (?, ?)", digestID, feedID)
		return err
	}
	_, err := db.Exec("INSERT OR IGNORE INTO digest_feeds (digest_id, feed_id) VALUES (?, ?)", digestID, feedID)
	return err
}

// RemoveFeedFromDigest unlinks a feed from a digest.
func (db *DB) RemoveFeedFromDigest(digestID, feedID string) error {
	_, err := db.Exec("DELETE FROM digest_feeds WHERE digest_id = ? AND feed_id = ?", digestID, feedID)
	return err
}

// GetFeedsForDigest returns all active feeds belonging to a digest.
func (db *DB) GetFeedsForDigest(userID string, digestID string) ([]Feed, error) {
	query := `SELECT f.id, f.url, f.name, f.last_polled, f.active, f.backfill, f.user_id
		FROM feeds f
		JOIN digest_feeds df ON f.id = df.feed_id
		WHERE df.digest_id = ? AND f.active = 1 AND f.user_id = ?`
	rows, err := db.Query(query, digestID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	feeds := []Feed{}
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, *f)
	}
	return feeds, nil
}

// GetDigestsForFeed returns all digests that include a feed.
func (db *DB) GetDigestsForFeed(userID string, feedID string) ([]Digest, error) {
	query := `SELECT d.id, d.name, COALESCE(d.directory, ''), d.schedule, d.destination_id, d.last_generated, COALESCE(d.last_delivered_id, 0), d.active, d.retain, d.user_id
		FROM digests d
		JOIN digest_feeds df ON d.id = df.digest_id
		WHERE df.feed_id = ? AND d.user_id = ?`
	rows, err := db.Query(query, feedID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var digests []Digest
	for rows.Next() {
		d, err := scanDigest(rows)
		if err != nil {
			return nil, err
		}
		digests = append(digests, *d)
	}
	return digests, nil
}

// GetNewEntriesForDigest returns entries newer than lastDeliveredID for all feeds in a digest.
func (db *DB) GetNewEntriesForDigest(digestID string, lastDeliveredID int64) ([]Entry, error) {
	query := `
		SELECT e.id, e.feed_id, e.entry_id, e.title, e.url, e.published, COALESCE(e.rendered, '')
		FROM entries e
		JOIN digest_feeds df ON e.feed_id = df.feed_id
		WHERE df.digest_id = ? AND e.id > ?
		ORDER BY e.id
	`
	rows, err := db.Query(query, digestID, lastDeliveredID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.FeedID, &e.EntryID, &e.Title, &e.URL, &e.Published, &e.Rendered); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// CreateUser inserts a new user with a bcrypt-hashed password.
func (db *DB) CreateUser(email, password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	id := uuid.New().String()
	_, err = db.Exec("INSERT INTO users (id, email, password_hash, verified) VALUES (?, ?, ?, 1)", id, email, string(hash))
	if err != nil {
		return "", err
	}
	return id, nil
}

// CreateUnverifiedUser inserts a new user with verified=false and a verification token.
func (db *DB) CreateUnverifiedUser(email, password string, verifyTimeout time.Duration) (id, token string, err error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("failed to hash password: %w", err)
	}
	id = uuid.New().String()
	b := make([]byte, 32)
	rand.Read(b)
	token = hex.EncodeToString(b)
	expires := time.Now().Add(verifyTimeout)
	_, err = db.Exec("INSERT INTO users (id, email, password_hash, verified, verify_token, verify_expires) VALUES (?, ?, ?, 0, ?, ?)",
		id, email, string(hash), token, expires)
	if err != nil {
		return "", "", err
	}
	return id, token, nil
}

// VerifyUserByToken finds a user by verification token and marks them verified.
// Returns the user ID if successful, empty string if token is invalid or expired.
func (db *DB) VerifyUserByToken(token string) (string, error) {
	var userID string
	err := db.QueryRow("SELECT id FROM users WHERE verify_token = ? AND verify_expires > ?", token, time.Now()).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	_, err = db.Exec("UPDATE users SET verified = 1, verify_token = NULL, verify_expires = NULL WHERE id = ?", userID)
	return userID, err
}

// GetUserByEmail returns the user with the given email, or nil if not found.
func (db *DB) GetUserByEmail(email string) (*User, error) {
	row := db.QueryRow("SELECT id, email, password_hash, verified, verify_token, verify_expires, created_at FROM users WHERE email = ?", email)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByID returns the user with the given ID, or nil if not found.
func (db *DB) GetUserByID(id string) (*User, error) {
	row := db.QueryRow("SELECT id, email, password_hash, verified, verify_token, verify_expires, created_at FROM users WHERE id = ?", id)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserCount returns the total number of registered users.
func (db *DB) GetUserCount() (int, error) {
var count int
err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
return count, err
}

// CheckPassword verifies a password against a user's stored bcrypt hash.
func CheckPassword(user *User, password string) bool {
return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil
}

// CreateSession generates a cryptographically random session token and stores it.
func (db *DB) CreateSession(userID string) (*Session, error) {
b := make([]byte, sessionTokenBytes)
if _, err := rand.Read(b); err != nil {
return nil, fmt.Errorf("failed to generate session token: %w", err)
}
token := hex.EncodeToString(b)
expiresAt := time.Now().Add(sessionDuration)

_, err := db.Exec(
"INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
token, userID, expiresAt,
)
if err != nil {
return nil, err
}
return &Session{Token: token, UserID: userID, CreatedAt: time.Now(), ExpiresAt: expiresAt}, nil
}

// GetSession returns the session for a token, or nil if not found or expired.
func (db *DB) GetSession(token string) (*Session, error) {
row := db.QueryRow(
"SELECT token, user_id, created_at, expires_at FROM sessions WHERE token = ? AND expires_at > ?",
token, time.Now(),
)
var s Session
if err := row.Scan(&s.Token, &s.UserID, &s.CreatedAt, &s.ExpiresAt); err != nil {
if err == sql.ErrNoRows {
return nil, nil
}
return nil, err
}
return &s, nil
}

// DeleteSession removes a session by token (logout).
func (db *DB) DeleteSession(token string) error {
_, err := db.Exec("DELETE FROM sessions WHERE token = ?", token)
return err
}

// CleanExpiredSessions removes all expired sessions.
func (db *DB) CleanExpiredSessions() error {
_, err := db.Exec("DELETE FROM sessions WHERE expires_at < ?", time.Now())
return err
}

// GetAllUsers returns all registered users.
func (db *DB) GetAllUsers() ([]User, error) {
	rows, err := db.Query("SELECT id, email, password_hash, verified, verify_token, verify_expires, created_at FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, nil
}

// RecordDeliveredFile stores a record of a file uploaded to a destination.
func (db *DB) RecordDeliveredFile(f DeliveredFile) error {
	_, err := db.Exec(`INSERT INTO delivered_files (user_id, delivery_type, delivery_ref, entry_id, remote_path, destination_id) VALUES (?, ?, ?, ?, ?, ?)`,
		f.UserID, f.DeliveryType, f.DeliveryRef, f.EntryID, f.RemotePath, f.DestinationID)
	return err
}

// GetDeliveredFiles returns delivered files for a delivery, ordered newest first.
func (db *DB) GetDeliveredFiles(userID, deliveryType, deliveryRef string) ([]DeliveredFile, error) {
	rows, err := db.Query(`SELECT id, user_id, delivery_type, delivery_ref, entry_id, remote_path, destination_id, delivered_at 
		FROM delivered_files WHERE user_id = ? AND delivery_type = ? AND delivery_ref = ? ORDER BY delivered_at DESC, id DESC`,
		userID, deliveryType, deliveryRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []DeliveredFile
	for rows.Next() {
		var f DeliveredFile
		if err := rows.Scan(&f.ID, &f.UserID, &f.DeliveryType, &f.DeliveryRef, &f.EntryID, &f.RemotePath, &f.DestinationID, &f.DeliveredAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// DeleteDeliveredFile removes a delivered file record by ID.
func (db *DB) DeleteDeliveredFile(id int64) error {
	_, err := db.Exec("DELETE FROM delivered_files WHERE id = ?", id)
	return err
}

// GetRecentDeliveries returns the most recent deliveries for a user,
// joining with entries, feeds, destinations, and digests to produce
// display-ready rows.
func (db *DB) GetRecentDeliveries(userID string, limit int) ([]DeliveryLogEntry, error) {
	query := `
		SELECT
			df.id,
			df.delivery_type,
			CASE
				WHEN df.delivery_type = 'individual' THEN COALESCE(e.title, '')
				ELSE COALESCE(d.name, '')
			END AS title,
			COALESCE(f.name, '') AS feed_name,
			CASE
				WHEN df.delivery_type = 'individual' THEN COALESCE(e.url, '')
				ELSE ''
			END AS url,
			COALESCE(dest.name, '') AS dest_name,
			COALESCE(dest.type, '') AS dest_type,
			df.delivered_at
		FROM delivered_files df
		LEFT JOIN entries e ON df.entry_id = e.id AND df.delivery_type = 'individual'
		LEFT JOIN feeds f ON e.feed_id = f.id
		LEFT JOIN digests d ON df.delivery_ref = d.id AND df.delivery_type = 'digest'
		LEFT JOIN destinations dest ON df.destination_id = dest.id
		WHERE df.user_id = ?
		ORDER BY df.delivered_at DESC, df.id DESC
		LIMIT ?
	`
	rows, err := db.Query(query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []DeliveryLogEntry
	for rows.Next() {
		var e DeliveryLogEntry
		if err := rows.Scan(&e.ID, &e.DeliveryType, &e.Title, &e.FeedName, &e.URL, &e.DestName, &e.DestType, &e.DeliveredAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// UpdateUserPassword updates a user's password with a new bcrypt hash.
func (db *DB) UpdateUserPassword(userID string, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	_, err = db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(hash), userID)
	return err
}

// DeleteUser removes a user and all their data across all tenant-scoped tables.
func (db *DB) DeleteUser(userID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tx.Exec("DELETE FROM delivered_files WHERE user_id = ?", userID)
	tx.Exec("DELETE FROM digest_feeds WHERE digest_id IN (SELECT id FROM digests WHERE user_id = ?)", userID)
	tx.Exec("DELETE FROM feed_delivery WHERE user_id = ?", userID)
	tx.Exec("DELETE FROM entries WHERE user_id = ?", userID)
	tx.Exec("DELETE FROM digests WHERE user_id = ?", userID)
	tx.Exec("DELETE FROM feeds WHERE user_id = ?", userID)
	tx.Exec("DELETE FROM destinations WHERE user_id = ?", userID)
	tx.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	tx.Exec("DELETE FROM users WHERE id = ?", userID)

	return tx.Commit()
}

// DeleteExpiredUnverifiedUsers removes unverified users whose verification has expired.
func (db *DB) DeleteExpiredUnverifiedUsers() (int, error) {
	rows, err := db.Query("SELECT id FROM users WHERE verified = 0 AND verify_expires < ?", time.Now())
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}

	for _, id := range ids {
		db.DeleteUser(id)
	}
	return len(ids), nil
}

// SetUserVerified marks a user as verified and clears verification token fields.
func (db *DB) SetUserVerified(userID string) error {
	_, err := db.Exec("UPDATE users SET verified = 1, verify_token = NULL, verify_expires = NULL WHERE id = ?", userID)
	return err
}

// GetAllSettings returns all settings as a map.
func (db *DB) GetAllSettings() (map[string]string, error) {
	rows, err := db.Query("SELECT `key`, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		settings[k] = v
	}
	return settings, nil
}

// GetSetting returns a single setting value, or empty string if not found.
func (db *DB) GetSetting(key string) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM settings WHERE `key` = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}
