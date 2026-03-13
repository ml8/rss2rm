// Package importer fetches and parses RSS/Atom feeds.
package importer

import (
	"net/http"
	"time"

	"github.com/mmcdole/gofeed"
)

// Item represents a single entry parsed from an RSS or Atom feed.
type Item struct {
	Title     string
	Link      string
	Published time.Time
	GUID      string
	Content   string
}

// Fetch downloads and parses the feed at url, returning up to limit items
// ordered newest first.
func Fetch(url string, limit int) ([]*Item, error) {
	fp := gofeed.NewParser()
	fp.Client = &http.Client{
		Timeout: 30 * time.Second,
	}
	feed, err := fp.ParseURL(url)
	if err != nil {
		return nil, err
	}

	if len(feed.Items) == 0 {
		return nil, nil
	}

	// Determine how many items to return
	count := limit
	if count <= 0 {
		count = 1
	}
	if count > len(feed.Items) {
		count = len(feed.Items)
	}

	var items []*Item
	// gofeed Items are usually sorted new to old.
	// We want the top 'count' items.
	for i := 0; i < count; i++ {
		item := feed.Items[i]

		pubDate := time.Now()
		if item.PublishedParsed != nil {
			pubDate = *item.PublishedParsed
		} else if item.UpdatedParsed != nil {
			pubDate = *item.UpdatedParsed
		}

		guid := item.GUID
		if guid == "" {
			guid = item.Link
		}

		items = append(items, &Item{
			Title:     item.Title,
			Link:      item.Link,
			Published: pubDate,
			GUID:      guid,
			Content:   item.Content,
		})
	}

	return items, nil
}
