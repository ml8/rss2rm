// Package processor extracts clean article content from web pages
// using the go-readability library.
package processor

import (
	"bytes"
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

// Process fetches the web page at url, extracts the main article content
// using readability, and returns a structured [Article].
func Process(url string) (*Article, error) {
	// Using a default user agent and timeout
	article, err := readability.FromURL(url, 30*time.Second)
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
