package converter

import (
	"os"
	"strings"
	"testing"
)

func TestGenerateDigestHTML(t *testing.T) {
	articles := []DigestArticle{
		{
			Title:    "First Article",
			Byline:   "Author One",
			Content:  "<p>Content of the first article.</p>",
			FeedName: "Tech Blog",
		},
		{
			Title:    "Second Article",
			Byline:   "",
			Content:  "<p>Content of the second article.</p>",
			FeedName: "News Feed",
		},
	}

	path, err := GenerateDigestHTML("Morning Reading", articles)
	if err != nil {
		t.Fatalf("GenerateDigestHTML: %v", err)
	}
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	html := string(content)

	// Check title
	if !strings.Contains(html, "<title>Morning Reading</title>") {
		t.Error("missing digest title in <title>")
	}
	if !strings.Contains(html, "<h1>Morning Reading</h1>") {
		t.Error("missing digest title in <h1>")
	}

	// Check TOC
	if !strings.Contains(html, `<a href="#article-0">First Article</a>`) {
		t.Error("missing TOC link for first article")
	}
	if !strings.Contains(html, `<a href="#article-1">Second Article</a>`) {
		t.Error("missing TOC link for second article")
	}
	if !strings.Contains(html, "Tech Blog") {
		t.Error("missing feed name for first article")
	}

	// Check article content
	if !strings.Contains(html, `id="article-0"`) {
		t.Error("missing article-0 anchor")
	}
	if !strings.Contains(html, "Content of the first article") {
		t.Error("missing first article content")
	}
	if !strings.Contains(html, "By Author One") {
		t.Error("missing byline for first article")
	}

	// Second article has no byline
	if strings.Contains(html, "By  ·") {
		t.Error("empty byline should not render 'By'")
	}
}

func TestGenerateDigestHTML_Empty(t *testing.T) {
	path, err := GenerateDigestHTML("Empty Digest", nil)
	if err != nil {
		t.Fatalf("GenerateDigestHTML: %v", err)
	}
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	html := string(content)
	if !strings.Contains(html, "Empty Digest") {
		t.Error("missing title in empty digest")
	}
}
