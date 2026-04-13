package fetcher

import (
	"testing"

	"rss2rm/internal/db"
)

func TestIsSubstackDomain(t *testing.T) {
	f := &Factory{substackCache: make(map[string]bool)}

	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.substack.com/p/some-post", true},
		{"https://newsletter.substack.com/p/hello", true},
		{"https://substack.com/profile", true},
		// Custom domains would need DNS — skip in unit tests
	}
	for _, tt := range tests {
		if got := f.isSubstackDomain(tt.url); got != tt.want {
			t.Errorf("isSubstackDomain(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestCheckSubstackCNAME(t *testing.T) {
	// This test does a real DNS lookup. Skip if running in CI without network.
	// platformer.news is a known Substack custom domain.
	if testing.Short() {
		t.Skip("skipping DNS test in short mode")
	}
	// We can't guarantee any domain stays Substack forever,
	// so just test that the function doesn't panic on a real domain.
	_ = checkSubstackCNAME("example.com") // known non-Substack
}

func TestParseHNContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantURL string
		wantOK  bool
	}{
		{
			name: "standard HN metadata",
			content: `Article URL: https://www.example.com/article
Comments URL: https://news.ycombinator.com/item?id=12345
Points: 176
# Comments: 148`,
			wantURL: "https://www.example.com/article",
			wantOK:  true,
		},
		{
			name:    "HN metadata with extra whitespace",
			content: "Article URL:  https://example.com/post  \nComments URL: https://news.ycombinator.com/item?id=1",
			wantURL: "https://example.com/post",
			wantOK:  true,
		},
		{
			name:    "normal article content",
			content: "<p>This is a normal article about technology.</p>",
			wantURL: "",
			wantOK:  false,
		},
		{
			name:    "empty content",
			content: "",
			wantURL: "",
			wantOK:  false,
		},
		{
			name:    "content mentioning Article URL in prose",
			content: "The Article URL: field is used by Miniflux",
			wantURL: "",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotOK := parseHNContent(tt.content)
			if gotURL != tt.wantURL || gotOK != tt.wantOK {
				t.Errorf("parseHNContent() = (%q, %v), want (%q, %v)", gotURL, gotOK, tt.wantURL, tt.wantOK)
			}
		})
	}
}

func TestCredentialCookies(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantSID string
		wantNil bool
	}{
		{
			name:    "valid config",
			config:  `{"substack_sid":"abc123"}`,
			wantSID: "abc123",
		},
		{
			name:    "empty sid",
			config:  `{"substack_sid":""}`,
			wantNil: true,
		},
		{
			name:    "missing sid key",
			config:  `{"other":"value"}`,
			wantNil: true,
		},
		{
			name:    "empty config",
			config:  "",
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cred := &db.Credential{Config: tt.config}
			cookies := credentialCookies(cred)
			if tt.wantNil {
				if cookies != nil {
					t.Errorf("expected nil cookies, got %v", cookies)
				}
				return
			}
			if len(cookies) != 2 {
				t.Fatalf("expected 2 cookies, got %d", len(cookies))
			}
			if cookies[0].Name != "connect.sid" {
				t.Errorf("cookie[0] name = %q, want 'connect.sid'", cookies[0].Name)
			}
			if cookies[1].Name != "substack.sid" {
				t.Errorf("cookie[1] name = %q, want 'substack.sid'", cookies[1].Name)
			}
			for _, c := range cookies {
				if c.Value != tt.wantSID {
					t.Errorf("cookie %q value = %q, want %q", c.Name, c.Value, tt.wantSID)
				}
			}
		})
	}
}
