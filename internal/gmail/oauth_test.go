package gmail

import (
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestNewOAuthConfig(t *testing.T) {
	config := NewOAuthConfig("test-client-id", "test-client-secret", "http://localhost:8080/callback")

	if config.ClientID != "test-client-id" {
		t.Errorf("ClientID = %q, want %q", config.ClientID, "test-client-id")
	}
	if config.ClientSecret != "test-client-secret" {
		t.Errorf("ClientSecret = %q, want %q", config.ClientSecret, "test-client-secret")
	}
	if config.RedirectURL != "http://localhost:8080/callback" {
		t.Errorf("RedirectURL = %q, want %q", config.RedirectURL, "http://localhost:8080/callback")
	}
	if len(config.Scopes) != 1 || config.Scopes[0] != "https://www.googleapis.com/auth/gmail.send" {
		t.Errorf("Scopes = %v, want [gmail.send scope]", config.Scopes)
	}
}

func TestGetAuthURL(t *testing.T) {
	config := NewOAuthConfig("test-client-id", "test-client-secret", "http://localhost:8080/callback")
	state := "destination-123"

	url := GetAuthURL(config, state)

	// Verify URL contains required components
	if url == "" {
		t.Error("GetAuthURL returned empty string")
	}

	// Check for required URL parameters
	requiredParams := []string{
		"client_id=test-client-id",
		"redirect_uri=",
		"state=destination-123",
		"access_type=offline",
		"prompt=consent",
	}

	for _, param := range requiredParams {
		if !contains(url, param) {
			t.Errorf("URL missing required parameter %q: %s", param, url)
		}
	}
}

func TestHasValidToken(t *testing.T) {
	tests := []struct {
		name  string
		token *Token
		want  bool
	}{
		{
			name:  "nil token",
			token: nil,
			want:  false,
		},
		{
			name:  "empty token",
			token: &Token{},
			want:  false,
		},
		{
			name: "only access token",
			token: &Token{
				AccessToken: "access-token",
			},
			want: false,
		},
		{
			name: "only refresh token",
			token: &Token{
				RefreshToken: "refresh-token",
			},
			want: false,
		},
		{
			name: "valid token with both tokens",
			token: &Token{
				AccessToken:  "access-token",
				RefreshToken: "refresh-token",
				Expiry:       time.Now().Add(time.Hour),
			},
			want: true,
		},
		{
			name: "valid token even if expired (refresh token present)",
			token: &Token{
				AccessToken:  "access-token",
				RefreshToken: "refresh-token",
				Expiry:       time.Now().Add(-time.Hour), // expired
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasValidToken(tt.token)
			if got != tt.want {
				t.Errorf("HasValidToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenSource(t *testing.T) {
	config := NewOAuthConfig("test-client-id", "test-client-secret", "http://localhost:8080/callback")
	token := &Token{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		Expiry:       time.Now().Add(time.Hour),
	}

	ts := TokenSource(nil, config, token)

	if ts == nil {
		t.Error("TokenSource returned nil")
	}

	// Verify it's a valid oauth2.TokenSource
	var _ oauth2.TokenSource = ts
}

func TestTokenFromSource(t *testing.T) {
	// Create a static token source for testing
	expectedToken := &oauth2.Token{
		AccessToken:  "test-access",
		RefreshToken: "test-refresh",
		Expiry:       time.Now().Add(time.Hour),
		TokenType:    "Bearer",
	}
	ts := oauth2.StaticTokenSource(expectedToken)

	token, err := TokenFromSource(ts)
	if err != nil {
		t.Fatalf("TokenFromSource() error = %v", err)
	}

	if token.AccessToken != expectedToken.AccessToken {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, expectedToken.AccessToken)
	}
	if token.RefreshToken != expectedToken.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, expectedToken.RefreshToken)
	}
}

// helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
