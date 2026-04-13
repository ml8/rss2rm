// Package fetcher provides a factory that selects the appropriate content
// fetching strategy based on article URL patterns. It handles authentication
// for paywalled sources (Substack via SSO) and content extraction for
// metadata-only sources (Hacker News via Miniflux).
package fetcher

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"rss2rm/internal/db"
	"rss2rm/internal/processor"
)

// Result holds the content returned by a fetcher.
type Result struct {
	Title   string
	Byline  string
	Content string
}

// Factory selects and runs the appropriate content fetcher for a given
// article URL. It looks up credentials from the database when needed
// and caches Substack domain detection via DNS CNAME lookups.
type Factory struct {
	db            *db.DB
	substackCache map[string]bool   // domain → is Substack
	slugCache     map[string]string // domain → publication slug
	cacheMu       sync.RWMutex
}

// NewFactory creates a fetcher factory backed by the given database.
func NewFactory(database *db.DB) *Factory {
	return &Factory{
		db:            database,
		substackCache: make(map[string]bool),
		slugCache:     make(map[string]string),
	}
}

// FetchContent returns article content for the given URL. The factory
// examines the URL, storedContent, and user credentials to decide:
//   - HN metadata content: extract the real article URL and fetch that
//   - User has Substack credential: always fetch the page. For verified
//     Substack domains, use SSO to authenticate. Otherwise plain fetch.
//   - Default: use storedContent if non-empty, otherwise fetch the URL.
func (f *Factory) FetchContent(articleURL, storedContent, userID string) (*Result, error) {
	if realURL, ok := parseHNContent(storedContent); ok {
		log.Printf("[Fetcher] HN metadata detected, fetching article URL: %s", realURL)
		return f.fetchPage(realURL, userID)
	}

	if f.userHasSubstackCredential(userID) {
		return f.fetchPage(articleURL, userID)
	}

	return f.fetchDefault(articleURL, storedContent)
}

// fetchDefault uses stored content if available, otherwise fetches the
// URL with readability.
func (f *Factory) fetchDefault(articleURL, storedContent string) (*Result, error) {
	if storedContent != "" {
		return &Result{Content: storedContent}, nil
	}
	if articleURL == "" {
		return nil, fmt.Errorf("no content and no URL to fetch")
	}
	article, err := processor.Process(articleURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	return &Result{Title: article.Title, Byline: article.Byline, Content: article.Content}, nil
}

// fetchPage fetches the article page. For verified Substack domains,
// performs SSO to authenticate. For everything else, plain readability fetch.
func (f *Factory) fetchPage(articleURL, userID string) (*Result, error) {
	if f.isSubstackDomain(articleURL) {
		sid := f.getSubstackSID(userID)
		if sid != "" {
			return f.fetchWithSSO(articleURL, sid)
		}
	}

	article, err := processor.Process(articleURL, nil)
	if err != nil {
		return nil, fmt.Errorf("page fetch failed: %w", err)
	}
	return &Result{Title: article.Title, Byline: article.Byline, Content: article.Content}, nil
}

// fetchWithSSO authenticates to a Substack publication via SSO, then
// fetches and extracts article content. The flow:
// 1. Find the publication slug (from URL or cached page lookup)
// 2. GET substack.com/sign-in?for_pub={slug} with substack.sid cookie
// 3. Follow redirects — publication sets connect.sid in the cookie jar
// 4. Fetch article with the cookie jar (now has connect.sid)
// 5. Extract content with readability
func (f *Factory) fetchWithSSO(articleURL, substackSID string) (*Result, error) {
	parsed, err := url.Parse(articleURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	host := parsed.Hostname()

	slug, err := f.findPubSlug(host)
	if err != nil {
		log.Printf("[Fetcher] Could not find pub slug for %s, plain fetch: %v", host, err)
		article, ferr := processor.Process(articleURL, nil)
		if ferr != nil {
			return nil, ferr
		}
		return &Result{Title: article.Title, Byline: article.Byline, Content: article.Content}, nil
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar error: %w", err)
	}
	substackURL, _ := url.Parse("https://substack.com")
	jar.SetCookies(substackURL, []*http.Cookie{
		{Name: "substack.sid", Value: substackSID, Path: "/"},
	})

	client := &http.Client{Timeout: 30 * time.Second, Jar: jar}

	// SSO exchange: follow redirects to get connect.sid on publication domain
	ssoURL := fmt.Sprintf("https://substack.com/sign-in?redirect=%%2F&for_pub=%s", slug)
	log.Printf("[Fetcher] SSO exchange for pub=%s (domain=%s)", slug, host)
	ssoResp, err := client.Get(ssoURL)
	if err != nil {
		return nil, fmt.Errorf("SSO exchange failed: %w", err)
	}
	defer ssoResp.Body.Close()
	io.Copy(io.Discard, ssoResp.Body)

	// Verify SSO succeeded by checking for connect.sid in the cookie jar
	pubURL, _ := url.Parse("https://" + host)
	ssoOK := false
	for _, c := range jar.Cookies(pubURL) {
		if c.Name == "connect.sid" {
			ssoOK = true
			break
		}
	}
	if !ssoOK {
		log.Printf("[Fetcher] SSO did not produce connect.sid for %s — cookie may be expired", host)
	}

	// Fetch authenticated article page
	log.Printf("[Fetcher] Fetching authenticated page: %s", articleURL)
	resp, err := client.Get(articleURL)
	if err != nil {
		return nil, fmt.Errorf("authenticated fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return nil, fmt.Errorf("not an HTML page: %s", resp.Header.Get("Content-Type"))
	}

	parsedURL, _ := url.Parse(articleURL)
	article, err := processor.ProcessReader(resp.Body, parsedURL)
	if err != nil {
		return nil, fmt.Errorf("readability extraction failed: %w", err)
	}

	return &Result{Title: article.Title, Byline: article.Byline, Content: article.Content}, nil
}

// findPubSlug returns the Substack publication slug for a domain.
// For *.substack.com, it's the subdomain. For custom domains, it's
// extracted from the page HTML and cached.
func (f *Factory) findPubSlug(host string) (string, error) {
	if before, ok := strings.CutSuffix(host, ".substack.com"); ok {
		return before, nil
	}

	f.cacheMu.RLock()
	slug, found := f.slugCache[host]
	f.cacheMu.RUnlock()
	if found {
		return slug, nil
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://" + host)
	if err != nil {
		return "", fmt.Errorf("failed to fetch homepage: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	skip := map[string]bool{"api": true, "on": true, "www": true, "cdn": true}
	for _, m := range slugRe.FindAllStringSubmatch(string(body), -1) {
		if !skip[m[1]] {
			slug = m[1]
			break
		}
	}
	if slug == "" {
		return "", fmt.Errorf("could not find pub slug in page HTML")
	}

	f.cacheMu.Lock()
	f.slugCache[host] = slug
	f.cacheMu.Unlock()
	log.Printf("[Fetcher] Discovered pub slug for %s: %s", host, slug)
	return slug, nil
}

// slugRe matches substack subdomains in both raw and URL-encoded contexts.
var slugRe = regexp.MustCompile(`(?:%2F|/)([a-zA-Z0-9_-]+)\.substack\.com`)

// isSubstackDomain checks if the URL's domain is a Substack publication,
// using a cache backed by DNS CNAME lookups.
func (f *Factory) isSubstackDomain(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()

	if strings.HasSuffix(host, ".substack.com") || host == "substack.com" {
		return true
	}

	f.cacheMu.RLock()
	cached, found := f.substackCache[host]
	f.cacheMu.RUnlock()
	if found {
		return cached
	}

	isSubstack := checkSubstackCNAME(host)

	f.cacheMu.Lock()
	f.substackCache[host] = isSubstack
	f.cacheMu.Unlock()

	if isSubstack {
		log.Printf("[Fetcher] DNS confirms %s is a Substack domain", host)
	}
	return isSubstack
}

func checkSubstackCNAME(host string) bool {
	cname, err := net.LookupCNAME(host)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(cname), "substack")
}

func (f *Factory) userHasSubstackCredential(userID string) bool {
	creds, err := f.db.GetCredentials(userID)
	if err != nil {
		return false
	}
	for _, c := range creds {
		if c.Type == "substack_cookie" {
			return true
		}
	}
	return false
}

// getSubstackSID returns the substack.sid value from the user's credential.
func (f *Factory) getSubstackSID(userID string) string {
	creds, err := f.db.GetCredentials(userID)
	if err != nil {
		return ""
	}
	for _, c := range creds {
		if c.Type == "substack_cookie" {
			var cfg map[string]string
			if err := json.Unmarshal([]byte(c.Config), &cfg); err == nil {
				return cfg["substack_sid"]
			}
		}
	}
	return ""
}

var hnPattern = regexp.MustCompile(`(?m)^Article URL:\s*(https?://\S+)`)

func parseHNContent(content string) (string, bool) {
	if content == "" {
		return "", false
	}
	if !strings.Contains(content, "Article URL:") {
		return "", false
	}
	matches := hnPattern.FindStringSubmatch(content)
	if len(matches) < 2 {
		return "", false
	}
	return strings.TrimSpace(matches[1]), true
}

// credentialCookies extracts HTTP cookies from a Substack credential config.
// Sets both connect.sid and substack.sid with the same value.
func credentialCookies(cred *db.Credential) []*http.Cookie {
	if cred == nil || cred.Config == "" {
		return nil
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(cred.Config), &cfg); err != nil {
		return nil
	}
	sid := cfg["substack_sid"]
	if sid == "" {
		return nil
	}
	return []*http.Cookie{
		{Name: "connect.sid", Value: sid, Path: "/"},
		{Name: "substack.sid", Value: sid, Path: "/"},
	}
}
