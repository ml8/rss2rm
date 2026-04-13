// Package importer fetches and parses RSS/Atom feeds.
package importer

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
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

// FetchOptions holds optional configuration for feed fetching, such as
// authentication cookies for paywalled sources.
type FetchOptions struct {
	Cookies []*http.Cookie
}

// Fetch downloads and parses the feed at feedURL, returning up to limit items
// ordered newest first. If opts is non-nil, its cookies are attached to the
// HTTP request.
func Fetch(ctx context.Context, feedURL string, limit int, opts *FetchOptions) ([]*Item, error) {
	fp := gofeed.NewParser()
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	if opts != nil && len(opts.Cookies) > 0 {
		jar, _ := newSimpleJar(feedURL, opts.Cookies)
		client.Jar = jar
	}
	fp.Client = client
	feed, err := fp.ParseURL(feedURL)
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

// newSimpleJar creates a cookie jar pre-loaded with the given cookies
// for the domain of feedURL.
func newSimpleJar(feedURL string, cookies []*http.Cookie) (http.CookieJar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(feedURL)
	if err != nil {
		return nil, err
	}
	jar.SetCookies(parsed, cookies)
	return jar, nil
}
