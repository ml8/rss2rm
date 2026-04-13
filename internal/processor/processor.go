// Package processor extracts clean article content from web pages
// using the go-readability library.
package processor

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
)

// Article holds the extracted content of a web page after readability
// processing.
type Article struct {
	Title       string
	Content     string
	Byline      string
	Excerpt     string
	SiteName    string
	TextContent string
}

// FetchOptions holds optional configuration for content fetching, such as
// authentication cookies for paywalled sources.
type FetchOptions struct {
	Cookies []*http.Cookie
}

// Process fetches the web page at url, extracts the main article content
// using readability, and returns a structured [Article]. If opts is non-nil,
// its cookies are attached to the HTTP request.
func Process(url string, opts *FetchOptions) (*Article, error) {
	var modifiers []readability.RequestWith
	if opts != nil && len(opts.Cookies) > 0 {
		modifiers = append(modifiers, func(r *http.Request) {
			for _, c := range opts.Cookies {
				r.AddCookie(c)
			}
		})
	}
	article, err := readability.FromURL(url, 30*time.Second, modifiers...)
	if err != nil {
		return nil, err
	}

	var contentBuf bytes.Buffer
	if err := article.RenderHTML(&contentBuf); err != nil {
		return nil, err
	}

	var textBuf bytes.Buffer
	if err := article.RenderText(&textBuf); err != nil {
		return nil, err
	}

	return &Article{
		Title:       article.Title(),
		Content:     contentBuf.String(),
		Byline:      article.Byline(),
		Excerpt:     article.Excerpt(),
		SiteName:    article.SiteName(),
		TextContent: textBuf.String(),
	}, nil
}

// ProcessReader extracts article content from an already-fetched HTML body.
// Used when the caller manages the HTTP request (e.g., for cookie-jar auth).
func ProcessReader(body io.Reader, pageURL *url.URL) (*Article, error) {
	article, err := readability.FromReader(body, pageURL)
	if err != nil {
		return nil, err
	}

	var contentBuf bytes.Buffer
	if err := article.RenderHTML(&contentBuf); err != nil {
		return nil, err
	}

	return &Article{
		Title:   article.Title(),
		Content: contentBuf.String(),
		Byline:  article.Byline(),
	}, nil
}
